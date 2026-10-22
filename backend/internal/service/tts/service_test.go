package tts

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ai-informations/internal/models"
	"ai-informations/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := repository.AutoMigrate(db); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	svc := NewService(db, t.TempDir())
	// Skip interpreter probing (subprocesses with timeouts): the generate seam
	// is faked in every test, so a cached non-empty value passes preflight.
	svc.python = "python"
	svc.pythonProbed = true
	return svc, db
}

func addArticle(t *testing.T, db *gorm.DB, title string) uint {
	t.Helper()
	a := &models.Article{Title: title, Summary: "摘要内容", OriginURL: "http://x/" + title}
	if err := db.Create(a).Error; err != nil {
		t.Fatalf("create article: %v", err)
	}
	return a.ID
}

// fakeGenerate writes a marker file, proving the round loop called the seam
// exactly where a real engine invocation would go.
func countingFake(calls *int32, delay time.Duration) func(ctx context.Context, text, dst string) error {
	return func(ctx context.Context, text, dst string) error {
		atomic.AddInt32(calls, 1)
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return os.WriteFile(dst, []byte("ID3fake"), 0o644)
	}
}

func waitReady(t *testing.T, s *Service) Status {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		st := s.Status()
		if st.State == StateReady {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("round never reached ready, status = %+v", s.Status())
	return Status{}
}

// A finished round must flip synthesizing -> ready and publish one playlist
// entry per article, in fetch order, with the /media/ URL the WebView streams.
func TestRoundCompleteSynthesisesAndPublishesPlaylist(t *testing.T) {
	svc, db := newTestService(t)
	var calls int32
	svc.generate = countingFake(&calls, 0)

	id1 := addArticle(t, db, "央行降准")
	id2 := addArticle(t, db, "股市收涨")

	svc.RoundComplete([]uint{id1, id2})
	st := waitReady(t, svc)

	if st.State != StateReady {
		t.Fatalf("state = %q, want ready", st.State)
	}
	if st.Done != 2 || st.Failed != 0 {
		t.Fatalf("done=%d failed=%d, want 2/0", st.Done, st.Failed)
	}
	if st.RoundID != 1 {
		t.Fatalf("roundId = %d, want 1", st.RoundID)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("generate calls = %d, want 2", got)
	}

	pl := svc.Playlist()
	if len(pl) != 2 {
		t.Fatalf("playlist len = %d, want 2", len(pl))
	}
	if pl[0].ArticleID != id1 || pl[1].ArticleID != id2 {
		t.Fatalf("playlist order = [%d %d], want [%d %d]", pl[0].ArticleID, pl[1].ArticleID, id1, id2)
	}
	wantURL := "/media/tts/" + itoa(id1) + ".mp3"
	if pl[0].URL != wantURL {
		t.Fatalf("url = %q, want %q", pl[0].URL, wantURL)
	}
	if pl[0].Title != "央行降准" {
		t.Fatalf("title = %q", pl[0].Title)
	}

	// The audio must actually be on disk where assetHandler will serve it.
	if _, err := os.Stat(filepath.Join(svc.mediaDir, "tts", itoa(id1)+".mp3")); err != nil {
		t.Fatalf("audio file missing: %v", err)
	}
}

// tts_enabled=false must keep the service idle: no goroutine, no round bump.
func TestRoundCompleteRespectsDisabledSetting(t *testing.T) {
	svc, db := newTestService(t)
	if err := repository.NewSystemSettingRepository(db).Set("tts_enabled", "false", ""); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	var calls int32
	svc.generate = countingFake(&calls, 0)

	svc.RoundComplete([]uint{addArticle(t, db, "标题")})
	time.Sleep(50 * time.Millisecond)

	st := svc.Status()
	if st.State != StateIdle || st.RoundID != 0 {
		t.Fatalf("status = %+v, want idle/round 0", st)
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("generate calls = %d, want 0", got)
	}
}

// An audio file left over from an earlier round makes the article free: the
// loop must reuse it instead of paying the endpoint again.
func TestRoundCompleteReusesExistingAudio(t *testing.T) {
	svc, db := newTestService(t)
	var calls int32
	svc.generate = countingFake(&calls, 0)

	id := addArticle(t, db, "已有音频")
	dir := filepath.Join(svc.mediaDir, "tts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, itoa(id)+".mp3"), []byte("ID3old"), 0o644); err != nil {
		t.Fatal(err)
	}

	svc.RoundComplete([]uint{id})
	st := waitReady(t, svc)

	if st.Done != 1 || st.Failed != 0 {
		t.Fatalf("done=%d failed=%d, want 1/0", st.Done, st.Failed)
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("generate calls = %d, want 0 (file reused)", got)
	}
	if len(svc.Playlist()) != 1 {
		t.Fatalf("reused file must still appear in the playlist")
	}
}

// When a new round starts while the old one is mid-file, the old goroutine
// must vanish: final status belongs to round 2 (roundId, totals), and the
// stale progress write must not corrupt the new counters.
func TestNewerRoundSupersedesOlder(t *testing.T) {
	svc, db := newTestService(t)
	idOld1 := addArticle(t, db, "旧一")
	idOld2 := addArticle(t, db, "旧二")
	idNew1 := addArticle(t, db, "新一")
	idNew2 := addArticle(t, db, "新二")

	release := make(chan struct{})
	var once sync.Once
	var slowCalls int32
	svc.generate = func(ctx context.Context, text, dst string) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// The seam receives the .tmp path (synthesize renames on success).
		if dst == filepath.Join(svc.mediaDir, "tts", itoa(idOld1)+".mp3.tmp") {
			atomic.AddInt32(&slowCalls, 1)
			select {
			case <-release:
			case <-time.After(15 * time.Second):
				return context.DeadlineExceeded
			}
		}
		return os.WriteFile(dst, []byte("ID3fake"), 0o644)
	}

	svc.RoundComplete([]uint{idOld1, idOld2})
	// Wait until round 1 is actually inside generate.
	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadInt32(&slowCalls) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if atomic.LoadInt32(&slowCalls) == 0 {
		t.Fatal("round 1 never started generating")
	}

	svc.RoundComplete([]uint{idNew1, idNew2})
	once.Do(func() { close(release) })

	st := waitReady(t, svc)
	if st.RoundID != 2 {
		t.Fatalf("roundId = %d, want 2", st.RoundID)
	}
	if st.Done != 2 || st.Total != 2 {
		t.Fatalf("done=%d total=%d, want 2/2: stale round corrupted the counters", st.Done, st.Total)
	}
	if len(svc.Playlist()) != 2 || svc.Playlist()[0].ArticleID != idNew1 {
		t.Fatalf("playlist = %+v, want round 2's articles only", svc.Playlist())
	}
}

// tts_concurrency=3 must keep three syntheses in flight at once: every fake
// invocation waits for all three to arrive, so a serial loop would deadlock
// on the first one and the round would fail.
func TestRoundCompleteSynthesisesConcurrently(t *testing.T) {
	svc, db := newTestService(t)
	if err := repository.NewSystemSettingRepository(db).Set("tts_concurrency", "3", ""); err != nil {
		t.Fatalf("set setting: %v", err)
	}

	arrived := make(chan struct{}, 8)
	proceed := make(chan struct{})
	svc.generate = func(ctx context.Context, text, dst string) error {
		arrived <- struct{}{}
		select {
		case <-proceed:
		case <-time.After(8 * time.Second):
			return fmt.Errorf("timed out waiting for concurrent workers")
		}
		return os.WriteFile(dst, []byte("ID3fake"), 0o644)
	}
	go func() {
		for i := 0; i < 3; i++ {
			<-arrived
		}
		close(proceed)
	}()

	ids := []uint{
		addArticle(t, db, "并发一"),
		addArticle(t, db, "并发二"),
		addArticle(t, db, "并发三"),
	}
	svc.RoundComplete(ids)
	st := waitReady(t, svc)

	if st.Done != 3 || st.Failed != 0 {
		t.Fatalf("done=%d failed=%d, want 3/0 (serial execution would time out)", st.Done, st.Failed)
	}
	if len(svc.Playlist()) != 3 {
		t.Fatalf("playlist len = %d, want 3", len(svc.Playlist()))
	}
	// Concurrency must not scramble the playlist: fetch order is preserved.
	for i, id := range ids {
		if svc.Playlist()[i].ArticleID != id {
			t.Fatalf("playlist[%d] = %d, want %d (order must survive concurrent workers)", i, svc.Playlist()[i].ArticleID, id)
		}
	}
}

// The api engine must speak the OpenAI-compatible protocol: POST
// <url>/audio/speech with a bearer token and JSON body, mp3 bytes back.
func TestRoundCompleteUsesAPIEngine(t *testing.T) {
	type capture struct {
		auth string
		body string
	}
	got := make(chan capture, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/speech" {
			http.NotFound(w, r)
			return
		}
		var payload map[string]string
		_ = json.NewDecoder(r.Body).Decode(&payload)
		raw, _ := json.Marshal(payload)
		got <- capture{auth: r.Header.Get("Authorization"), body: string(raw)}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("ID3api"))
	}))
	defer srv.Close()

	svc, db := newTestService(t)
	// Forget the cached interpreter: the api engine must not probe python.
	svc.python = ""
	svc.pythonProbed = false

	setting := repository.NewSystemSettingRepository(db)
	for k, v := range map[string]string{
		"tts_engine":    "api",
		"tts_api_url":   srv.URL + "/v1",
		"tts_api_key":   "sk-test",
		"tts_api_model": "tts-1",
		"tts_api_voice": "alloy",
	} {
		if err := setting.Set(k, v, ""); err != nil {
			t.Fatalf("set %s: %v", k, err)
		}
	}

	id := addArticle(t, db, "API合成")
	svc.RoundComplete([]uint{id})
	st := waitReady(t, svc)

	if st.Done != 1 || st.Failed != 0 {
		t.Fatalf("done=%d failed=%d, want 1/0", st.Done, st.Failed)
	}
	req := <-got
	if req.auth != "Bearer sk-test" {
		t.Fatalf("authorization = %q", req.auth)
	}
	if !strings.Contains(req.body, `"model":"tts-1"`) {
		t.Fatalf("payload missing model: %s", req.body)
	}
	if !strings.Contains(req.body, "API合成") {
		t.Fatalf("payload missing text: %s", req.body)
	}
	if !strings.Contains(req.body, `"voice":"alloy"`) {
		t.Fatalf("payload missing voice: %s", req.body)
	}
	if _, err := os.Stat(filepath.Join(svc.mediaDir, "tts", itoa(id)+".mp3")); err != nil {
		t.Fatalf("audio file missing: %v", err)
	}
}

// A missing api endpoint must fail the whole round up front with a readable
// message, not time out once per article.
func TestAPIEnginePreflightFailsFast(t *testing.T) {
	svc, db := newTestService(t)
	svc.python = ""
	svc.pythonProbed = false
	if err := repository.NewSystemSettingRepository(db).Set("tts_engine", "api", ""); err != nil {
		t.Fatalf("set engine: %v", err)
	}

	var calls int32
	svc.generate = countingFake(&calls, 0)

	svc.RoundComplete([]uint{addArticle(t, db, "无端点")})
	st := waitReady(t, svc)

	if st.Done != 0 || st.Failed != 1 {
		t.Fatalf("done=%d failed=%d, want 0/1", st.Done, st.Failed)
	}
	if !strings.Contains(st.Message, "tts_api_url") {
		t.Fatalf("message = %q, want the tts_api_url hint", st.Message)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatalf("generate calls = %d, want 0 (preflight must abort before any synthesis)", calls)
	}
}

// Test() must synthesise its fixed sentence and hand back a streamable URL.
func TestServiceTestProducesAudio(t *testing.T) {
	svc, _ := newTestService(t)
	var gotText string
	svc.generate = func(ctx context.Context, text, dst string) error {
		gotText = text
		return os.WriteFile(dst, []byte("ID3fake"), 0o644)
	}
	url, err := svc.Test(context.Background())
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if url != "/media/tts/_test.mp3" {
		t.Fatalf("url = %q", url)
	}
	if gotText == "" {
		t.Fatal("generate received no text")
	}
	if _, err := os.Stat(filepath.Join(svc.mediaDir, "tts", "_test.mp3")); err != nil {
		t.Fatalf("test audio missing: %v", err)
	}
}

// speakText strips markup, unescapes doubled entities and caps length so one
// long article never runs for minutes of audio.
func TestSpeakTextCleansAndClips(t *testing.T) {
	a := &models.Article{
		Title:   "&lt;b&gt;央行&lt;/b&gt;降准",
		Summary: "<p>今日宣布&nbsp;下调准备金率</p>",
	}
	got := speakText(a)
	if got == "" {
		t.Fatal("empty text")
	}
	for _, bad := range []string{"<p>", "</b>", "&lt;", "&nbsp;"} {
		if strings.Contains(got, bad) {
			t.Fatalf("text %q still contains %q", got, bad)
		}
	}
	if !strings.Contains(got, "央行降准") {
		t.Fatalf("text %q lost the title", got)
	}

	long := &models.Article{Title: "长标题", Summary: ""}
	for i := 0; i < 400; i++ {
		long.Summary += "字"
	}
	if clipped := speakText(long); len([]rune(clipped)) > 500 {
		t.Fatalf("clipped len = %d, want <= 500", len([]rune(clipped)))
	}
}

// TestCleanupTmpFiles removes .tmp files older than the cleanup age and keeps
// recent ones as well as non-tmp files untouched.
func TestCleanupTmpFiles(t *testing.T) {
	svc, _ := newTestService(t)
	dir := filepath.Join(svc.mediaDir, "tts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a fresh .tmp (should be kept).
	freshTmp := filepath.Join(dir, "fresh.tmp")
	if err := os.WriteFile(freshTmp, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create an old .tmp (should be removed).
	oldTmp := filepath.Join(dir, "old.tmp")
	if err := os.WriteFile(oldTmp, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Backdate the old tmp to force it past the cleanup threshold.
	oldTime := time.Now().Add(-2 * tmpCleanupAge)
	if err := os.Chtimes(oldTmp, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	// A non-tmp file must survive.
	if err := os.WriteFile(filepath.Join(dir, "existing.mp3"), []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	svc.cleanupTmpFiles()

	if _, err := os.Stat(freshTmp); err != nil {
		t.Fatalf("fresh tmp was removed: %v", err)
	}
	if _, err := os.Stat(oldTmp); !os.IsNotExist(err) {
		t.Fatalf("old tmp was not removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "existing.mp3")); err != nil {
		t.Fatalf("non-tmp file was removed: %v", err)
	}
}

// TestCleanupTmpFilesEmptyDir verifies cleanup handles a missing or empty
// tts directory without panicking.
func TestCleanupTmpFilesEmptyDir(t *testing.T) {
	svc, _ := newTestService(t)
	// Point mediaDir at a temp dir that has no tts subdirectory.
	svc.mediaDir = t.TempDir()
	// Should not panic.
	svc.cleanupTmpFiles()
}

// TestRoundContextPropagation verifies that when the parent context is
// cancelled (e.g. newer round starts), the round's internal workers stop
// promptly and the round finishes with a cancellation indicator rather than
// hanging until the per-article 90s timeout.
func TestRoundContextPropagation(t *testing.T) {
	svc, db := newTestService(t)
	block := make(chan struct{})
	var calls int32
	svc.generate = func(ctx context.Context, text, dst string) error {
		atomic.AddInt32(&calls, 1)
		// Block until the test releases us or the context is cancelled.
		select {
		case <-block:
			return os.WriteFile(dst, []byte("ID3fake"), 0o644)
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	id1 := addArticle(t, db, "传播一")
	id2 := addArticle(t, db, "传播二")
	svc.RoundComplete([]uint{id1, id2})

	// Wait until round 1 has entered generate.
	deadline := time.Now().Add(3 * time.Second)
	for atomic.LoadInt32(&calls) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if atomic.LoadInt32(&calls) == 0 {
		t.Fatal("round 1 never entered generate")
	}

	// Start round 2: this cancels round 1's context.
	id3 := addArticle(t, db, "传播三")
	svc.RoundComplete([]uint{id3})

	// Round 1's worker should detect the cancelled context and exit.
	// Release the block so round 2 can finish.
	close(block)

	// Wait for round 2 to finish.
	st := waitReady(t, svc)
	if st.RoundID != 2 {
		t.Fatalf("roundId = %d, want 2", st.RoundID)
	}
	// Round 1's generate call should have been aborted by context cancellation.
	if atomic.LoadInt32(&calls) < 2 {
		t.Fatalf("generate calls = %d, want >= 2 (round 1 abort + round 2)", atomic.LoadInt32(&calls))
	}
}

func itoa(u uint) string {
	return strconv.Itoa(int(u))
}

func TestEdgeTTSVersionOld(t *testing.T) {
	cases := map[string]bool{
		"7.2.8":         true, // sends the retired v1 Sec-MS-GEC token
		"7.3.0":         true,
		"7.3.1":         false, // first v2 token release
		"7.3.9":         false,
		"7.4":           false,
		"8.0.0":         false,
		"7.3.1.dev1":    false,
		"":              false, // unparseable must never block working synthesis
		"unknown":       false,
		"not-a-version": false,
	}
	for got, want := range cases {
		if old := edgeTTSVersionOld(got); old != want {
			t.Errorf("edgeTTSVersionOld(%q) = %v, want %v", got, old, want)
		}
	}
}
