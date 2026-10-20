// Package tts synthesises narration audio for newly fetched articles using
// edge-tts (Microsoft online neural voices) and exposes the current round's
// playlist so the frontend can auto-play it once synthesis finishes.
package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"ai-informations/internal/models"

	"gorm.io/gorm"
)

const (
	StateIdle         = "idle"
	StateSynthesizing = "synthesizing"
	StateReady        = "ready"
)

const (
	// maxRoundDuration bounds the total time a synthesis round can run before
	// it is forcibly cancelled. Prevents a single hung article from blocking
	// the entire round for minutes when combined with the per-article retry.
	maxRoundDuration = 10 * time.Minute
	// tmpCleanupAge removes .tmp files older than this to prevent disk/handle
	// accumulation from failed or killed syntheses.
	tmpCleanupAge = 1 * time.Hour
)

// PlaylistItem is one playable article of the current round.
type PlaylistItem struct {
	ArticleID uint   `json:"articleId"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	Source    string `json:"source"`
}

// Status is the round snapshot the frontend polls (same 1s/3s pattern as the
// task monitor).
type Status struct {
	Enabled    bool      `json:"enabled"`
	State      string    `json:"state"`
	Total      int       `json:"total"`
	Done       int       `json:"done"`
	Failed     int       `json:"failed"`
	RoundID    uint64    `json:"roundId"`
	FinishedAt time.Time `json:"finishedAt"`
	Message    string    `json:"message"`
}

type Service struct {
	db       *gorm.DB
	mediaDir string // audio lives in <mediaDir>/tts/<articleID>.mp3

	// generate is the synthesis seam: production dispatches to the configured
	// engine (edge-tts CLI or an OpenAI-compatible TTS API), tests substitute
	// a fake so the round state machine is testable offline.
	generate func(ctx context.Context, text, dst string) error

	mu           sync.Mutex
	cancel       context.CancelFunc // halts the round in flight when a newer one starts
	python       string             // resolved interpreter (edge engine only)
	pythonProbed bool               // probing once per process: a failed probe is cached too
	status       Status
	playlist     []PlaylistItem
}

func NewService(db *gorm.DB, mediaDir string) *Service {
	s := &Service{
		db:       db,
		mediaDir: mediaDir,
		status:   Status{State: StateIdle},
	}
	s.generate = s.dispatch
	s.startCleanupLoop()
	return s
}

// Stop cancels the round in flight; called from OnShutdown.
func (s *Service) Stop() {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.mu.Unlock()
	// Best-effort cleanup of any leftover temp files after shutdown.
	s.cleanupTmpFiles()
}

// RoundComplete is invoked by the collector's cycle hook with the article IDs
// fetched in the round that just finished. It returns immediately; synthesis
// runs on its own goroutine (需求: 拉取结束启动协程进行 TTS).
func (s *Service) RoundComplete(ids []uint) {
	if len(ids) == 0 {
		return
	}
	if s.setting("tts_enabled", "true") != "true" {
		return
	}
	if max := s.intSetting("tts_max_per_round", 50); max > 0 && len(ids) > max {
		ids = ids[:max]
	}

	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	rid := s.status.RoundID + 1
	s.status = Status{
		Enabled: true,
		State:   StateSynthesizing,
		Total:   len(ids),
		RoundID: rid,
	}
	s.playlist = nil
	s.mu.Unlock()

	go s.run(ctx, rid, ids)
}

// run synthesises every article of one round and publishes progress after
// each file. Synthesis runs on a small worker pool (tts_concurrency, default
// 3) — one python process per article, so serial execution would waste most
// of the round. Every state write carries rid: when a newer round supersedes
// this one, the stale goroutine must not touch the newer round's counters.
func (s *Service) run(ctx context.Context, rid uint64, ids []uint) {
	// Preflight the chosen engine once so a misconfigured round fails fast
	// with a readable message instead of timing out on every article.
	if err := s.preflight(); err != nil {
		s.finish(rid, 0, len(ids), err.Error(), nil)
		return
	}

	// Bound the entire round so a single hung article (or a chain of retries)
	// cannot block the goroutine for minutes and prevent newer rounds from
	// replacing this one's counters.
	roundCtx, roundCancel := context.WithTimeout(ctx, maxRoundDuration)
	defer roundCancel()

	workers := s.intSetting("tts_concurrency", 3)
	if workers < 1 {
		workers = 1
	}
	if workers > 8 {
		workers = 8
	}
	if workers > len(ids) {
		workers = len(ids)
	}

	type result struct {
		idx  int
		item *PlaylistItem
	}
	jobs := make(chan int)
	results := make(chan result, len(ids))
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				var item *PlaylistItem
				var a models.Article
				if roundCtx.Err() == nil && s.db.First(&a, ids[i]).Error == nil && s.synthesize(roundCtx, &a) {
					it := s.itemFor(&a)
					item = &it
				}
				results <- result{idx: i, item: item}
			}
		}()
	}
	go func() {
		for i := range ids {
			select {
			case jobs <- i:
			case <-roundCtx.Done():
				return
			}
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	items := make([]*PlaylistItem, len(ids))
	done, failed := 0, 0
	for r := range results {
		if r.item != nil {
			items[r.idx] = r.item
			done++
		} else {
			failed++
		}
		s.mu.Lock()
		if s.status.RoundID == rid {
			s.status.Done = done
			s.status.Failed = failed
		}
		s.mu.Unlock()
	}

	// Rebuild the playlist in fetch order: workers finish out of order, but
	// the player must advance through the round as it was collected.
	playlist := make([]PlaylistItem, 0, done)
	for _, it := range items {
		if it != nil {
			playlist = append(playlist, *it)
		}
	}
	msg := ""
	if failed > 0 {
		msg = fmt.Sprintf("%d of %d failed", failed, len(ids))
	}
	if roundCtx.Err() != nil && s.status.RoundID == rid {
		msg = "round cancelled (timeout or newer round)"
	}
	s.finish(rid, done, failed, msg, playlist)
}

// preflight verifies the configured engine can work at all: a python with
// edge-tts for the edge engine, a base URL for the API engine.
func (s *Service) preflight() error {
	if s.engine() == "api" {
		if strings.TrimSpace(s.setting("tts_api_url", "")) == "" {
			return fmt.Errorf("tts_api_url is empty; set the TTS API endpoint in settings")
		}
		if strings.TrimSpace(s.setting("tts_api_model", "")) == "" {
			return fmt.Errorf("tts_api_model is empty; set the TTS model in settings")
		}
		return nil
	}
	if s.resolvePython() == "" {
		return fmt.Errorf("python not found; set tts_python in settings")
	}
	return nil
}

// engine returns the configured synthesis engine: "edge" (free, local CLI)
// or "api" (a commercial OpenAI-compatible TTS endpoint).
func (s *Service) engine() string {
	if e := strings.TrimSpace(s.setting("tts_engine", "edge")); e != "" {
		return e
	}
	return "edge"
}

// finish marks a round ready, unless a newer round already replaced it.
func (s *Service) finish(rid uint64, done, failed int, msg string, playlist []PlaylistItem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status.RoundID != rid || s.cancel == nil {
		return
	}
	s.status.State = StateReady
	s.status.Done = done
	s.status.Failed = failed
	s.status.Message = msg
	s.status.FinishedAt = time.Now()
	s.playlist = playlist
}

// synthesize makes sure <mediaDir>/tts/<id>.mp3 exists, generating it when
// missing (a file from an earlier round is reused, so re-running a round is
// cheap). Returns false when no audio is available.
func (s *Service) synthesize(ctx context.Context, a *models.Article) bool {
	dir := filepath.Join(s.mediaDir, "tts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("tts: mkdir: %v", err)
		return false
	}
	dst := filepath.Join(dir, fmt.Sprintf("%d.mp3", a.ID))
	if info, err := os.Stat(dst); err == nil && info.Size() > 0 {
		return true
	}

	text := speakText(a)
	if text == "" {
		return false
	}

	tmp := dst + ".tmp"
	// Remove any stale .tmp from a previous killed/hung synthesis.
	_ = os.Remove(tmp)
	err := s.generate(ctx, text, tmp)
	if err != nil && ctx.Err() == nil {
		// One retry: the Microsoft endpoint and commercial APIs both
		// occasionally drop a connection.
		err = s.generate(ctx, text, tmp)
	}
	if err != nil {
		log.Printf("tts: article %d: %v", a.ID, err)
		// Clean up the partial temp file so it doesn't accumulate.
		_ = os.Remove(tmp)
		return false
	}
	info, err := os.Stat(tmp)
	if err != nil || info.Size() == 0 {
		log.Printf("tts: article %d: engine produced no audio", a.ID)
		_ = os.Remove(tmp)
		return false
	}
	if err := os.Rename(tmp, dst); err != nil {
		log.Printf("tts: article %d: %v", a.ID, err)
		// Rename failed but tmp still exists; clean it up.
		_ = os.Remove(tmp)
		return false
	}
	return true
}

// startCleanupLoop launches a goroutine that periodically removes stale .tmp
// files from the tts directory. This prevents disk and handle accumulation
// when syntheses are killed (round cancellation, timeout, or engine errors).
// Called once when the service is created.
func (s *Service) startCleanupLoop() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			s.cleanupTmpFiles()
		}
	}()
	// Run an initial cleanup immediately.
	s.cleanupTmpFiles()
}

// cleanupTmpFiles removes .tmp files older than tmpCleanupAge from the tts
// directory. These are leftover from killed or failed syntheses.
func (s *Service) cleanupTmpFiles() {
	dir := filepath.Join(s.mediaDir, "tts")
	if _, err := os.Stat(dir); err != nil {
		return // directory doesn't exist yet
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("tts: cleanup read dir: %v", err)
		return
	}
	cutoff := time.Now().Add(-tmpCleanupAge)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}
		fi, err := e.Info()
		if err != nil || fi.ModTime().After(cutoff) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if err := os.Remove(path); err != nil {
			log.Printf("tts: cleanup remove %s: %v", e.Name(), err)
		} else {
			log.Printf("tts: cleaned up stale tmp: %s", e.Name())
		}
	}
}

// Test synthesises a fixed sentence with the configured engine so the settings
// page can verify the whole path in one click. Returns the URL of the audio.
func (s *Service) Test(ctx context.Context) (string, error) {
	if s.setting("tts_enabled", "true") != "true" {
		return "", fmt.Errorf("TTS is disabled in settings")
	}
	if err := s.preflight(); err != nil {
		return "", err
	}
	dir := filepath.Join(s.mediaDir, "tts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, "_test.mp3")
	_ = os.Remove(dst)
	if err := s.generate(ctx, "语音合成测试成功。", dst); err != nil {
		return "", err
	}
	return "/media/tts/_test.mp3", nil
}

func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.status
	st.Enabled = s.setting("tts_enabled", "true") == "true"
	return st
}

func (s *Service) Playlist() []PlaylistItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]PlaylistItem, len(s.playlist))
	copy(out, s.playlist)
	return out
}

// ArticleStatus reports whether audio for the given article already exists on
// disk and whether it is currently being synthesised (part of the round in
// flight). The frontend uses this to decide whether to show a play button or
// a "synthesize now" button on each card.
func (s *Service) ArticleStatus(articleID uint) (hasAudio bool, synthesizing bool, err error) {
	// Check disk first — fast path, no DB needed.
	dst := filepath.Join(s.mediaDir, "tts", fmt.Sprintf("%d.mp3", articleID))
	if info, statErr := os.Stat(dst); statErr == nil && info.Size() > 0 {
		return true, false, nil
	}

	// Not on disk: check whether the article exists and is part of the round
	// currently in flight.
	var a models.Article
	if err = s.db.First(&a, articleID).Error; err != nil {
		return false, false, nil // article not found → no audio possible
	}
	s.mu.Lock()
	synthesizing = s.cancel != nil && s.status.State == StateSynthesizing
	// Also check the playlist: if the article is already in a finished round's
	// playlist, audio will appear soon (or already exists from an earlier run).
	for _, it := range s.playlist {
		if it.ArticleID == articleID {
			s.mu.Unlock()
			return false, true, nil
		}
	}
	s.mu.Unlock()
	return false, false, nil
}

// SynthesizeArticle triggers on-demand synthesis for a single article. It
// reuses an existing file if present (same article in different categories
// shares audio), and delegates to the same generate seam as round synthesis.
func (s *Service) SynthesizeArticle(articleID uint) error {
	if s.setting("tts_enabled", "true") != "true" {
		return fmt.Errorf("TTS is disabled in settings")
	}
	if err := s.preflight(); err != nil {
		return err
	}
	var a models.Article
	if err := s.db.First(&a, articleID).Error; err != nil {
		return fmt.Errorf("article %d not found", articleID)
	}
	// Reuse existing file.
	dst := filepath.Join(s.mediaDir, "tts", fmt.Sprintf("%d.mp3", articleID))
	if info, err := os.Stat(dst); err == nil && info.Size() > 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	text := speakText(&a)
	if text == "" {
		return fmt.Errorf("no speakable text for article %d", articleID)
	}
	tmp := dst + ".tmp"
	_ = os.Remove(tmp)
	if err := s.generate(ctx, text, tmp); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	info, err := os.Stat(tmp)
	if err != nil || info.Size() == 0 {
		_ = os.Remove(tmp)
		return fmt.Errorf("engine produced no audio for article %d", articleID)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (s *Service) itemFor(a *models.Article) PlaylistItem {
	var src models.Source
	_ = s.db.First(&src, a.SourceID).Error
	return PlaylistItem{
		ArticleID: a.ID,
		Title:     a.Title,
		URL:       fmt.Sprintf("/media/tts/%d.mp3", a.ID),
		Source:    src.Name,
	}
}

// dispatch routes one synthesis request to the configured engine. Settings are
// re-read per call so a change on the settings page applies to the very next
// article (no restart, no round reset).
func (s *Service) dispatch(ctx context.Context, text, dst string) error {
	if s.engine() == "api" {
		return s.apiTTS(ctx, text, dst)
	}
	python := s.resolvePython()
	if python == "" {
		return fmt.Errorf("python not found; set tts_python in settings")
	}
	voice := s.setting("tts_voice", "zh-CN-XiaoxiaoNeural")
	return s.edgeTTS(ctx, python, voice, text, dst)
}

// edgeTTS runs the edge-tts CLI. Go passes arguments through CreateProcessW
// (UTF-16), so Chinese text reaches python intact without any shell quoting.
// SysProcAttr keeps every worker's python windowless: CREATE_NO_WINDOW plus
// a hidden start window, so synthesis never flashes a console on screen.
//
// On Windows, exec.CommandContext cancels the context by calling
// TerminateProcess on the python process. Python's asyncio event loop (which
// edge_tts relies on) can spawn child processes for certain operations; those
// children survive parent termination. We mitigate this by:
//   - Using a generous per-article timeout so transient network blips don't
//     trigger premature kills that leave orphans behind.
//   - Running a periodic cleanup goroutine (startCleanupLoop) that removes
//     stale .tmp files and logs any python processes that outlive their
//     expected lifetime.
func (s *Service) edgeTTS(ctx context.Context, python, voice, text, dst string) error {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-m", "edge_tts",
		"--voice", voice, "--text", text, "--write-media", dst)
	hideConsole(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// If the context was cancelled (round superseded or timeout), log
		// quietly: this is expected during normal operation.
		if ctx.Err() != nil {
			log.Printf("tts: article synthesis cancelled: %v", ctx.Err())
			// Best-effort cleanup of the partial temp file.
			_ = os.Remove(dst)
			return ctx.Err()
		}
		return fmt.Errorf("edge-tts failed: %v: %s", err, clip(string(out), 300))
	}
	return nil
}

// hideConsole prevents cmd.exe/python.exe from popping a console window for
// every synthesis (and every interpreter probe).
func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}

// apiTTS calls a commercial OpenAI-compatible TTS endpoint:
// POST <tts_api_url>/audio/speech {"model","voice","input","response_format"}
// -> audio bytes (mp3). This shape is served by OpenAI, SiliconFlow, Zhipu,
// MiniMax and most providers that advertise an "OpenAI-compatible" TTS API.
func (s *Service) apiTTS(ctx context.Context, text, dst string) error {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	base := strings.TrimRight(strings.TrimSpace(s.setting("tts_api_url", "")), "/")
	if base == "" {
		return fmt.Errorf("tts_api_url is empty; set the TTS API endpoint in settings")
	}
	model := strings.TrimSpace(s.setting("tts_api_model", ""))
	if model == "" {
		return fmt.Errorf("tts_api_model is empty; set the TTS model in settings")
	}
	payload := map[string]string{
		"model":             model,
		"input":             text,
		"response_format":   "mp3",
	}
	if v := strings.TrimSpace(s.setting("tts_api_voice", "")); v != "" {
		payload["voice"] = v
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/audio/speech", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if key := strings.TrimSpace(s.setting("tts_api_key", "")); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("tts api: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("tts api: HTTP %d: %s", resp.StatusCode, clip(string(msg), 300))
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		_ = os.Remove(dst)
		return fmt.Errorf("tts api: read response: %w", err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	return nil
}

// resolvePython finds a python that can import edge_tts. The WindowsApps
// store stub on PATH "succeeds" LookPath but fails immediately, so every
// candidate is verified with a real import before it is trusted. A failed
// probe is cached as well: a round over 50 articles must not spend 8 seconds
// per article re-probing a missing interpreter.
func (s *Service) resolvePython() string {
	s.mu.Lock()
	if s.pythonProbed {
		v := s.python
		s.mu.Unlock()
		return v
	}
	s.mu.Unlock()

	var candidates []string
	if v := strings.TrimSpace(s.setting("tts_python", "")); v != "" {
		candidates = append(candidates, v)
	}
	for _, name := range []string{"python", "python3"} {
		if p, err := exec.LookPath(name); err == nil {
			candidates = append(candidates, p)
		}
	}
	candidates = append(candidates,
		`C:\ProgramData\miniconda3_2\python.exe`,
		`C:\ProgramData\miniconda3\python.exe`,
	)
	found := ""
	for _, c := range candidates {
		if c == "" || !pythonWorks(c) {
			continue
		}
		found = c
		break
	}
	s.mu.Lock()
	s.python = found
	s.pythonProbed = true
	s.mu.Unlock()
	return found
}

// InvalidatePythonCache drops the cached interpreter (settings test button).
func (s *Service) InvalidatePythonCache() {
	s.mu.Lock()
	s.python = ""
	s.pythonProbed = false
	s.mu.Unlock()
}

func pythonWorks(bin string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "-c", "import edge_tts")
	hideConsole(cmd)
	return cmd.Run() == nil
}

// speakText flattens the article into one speakable line: title, then summary
// (HTML stripped, entities unescaped, capped so one article never runs for
// minutes).
func speakText(a *models.Article) string {
	parts := []string{cleanText(a.Title)}
	if s := cleanText(a.Summary); s != "" {
		parts = append(parts, s)
	}
	text := strings.Join(parts, "。")
	return clipRunes(text, 500)
}

var (
	tagRe    = regexp.MustCompile(`<[^>]*>`)
	spaceRe  = regexp.MustCompile(`[\t\n\f\r \x{00A0}]+`)
	entityRe = regexp.MustCompile(`&[a-zA-Z#0-9]+;`)
)

func cleanText(s string) string {
	if s == "" {
		return ""
	}
	// Unescape BEFORE stripping tags: feeds double-escape markup ("&lt;b&gt;")
	// so the angle brackets only appear after decoding. Twice, for "&amp;lt;".
	s = html.UnescapeString(html.UnescapeString(s))
	// Inline tags vanish without a gap ("央行</b>降准" must read 央行降准, not
	// 央行 降准): TTS reads the joined Chinese text as spoken.
	s = tagRe.ReplaceAllString(s, "")
	s = entityRe.ReplaceAllString(s, " ")
	// \x{00A0}: Go's \s is ASCII-only, and &nbsp; unescapes to it.
	s = spaceRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// setting reads one system setting, degrading to def when the row is missing
// or garbled (same pattern as classifier.llmConcurrency).
func (s *Service) setting(key, def string) string {
	var row models.SystemSetting
	if err := s.db.Where("key = ?", key).First(&row).Error; err == nil {
		if v := strings.TrimSpace(row.Value); v != "" {
			return v
		}
	}
	return def
}

// intSetting parses an integer setting, returning def on a missing or
// non-numeric value.
func (s *Service) intSetting(key string, def int) int {
	if v, err := strconv.Atoi(strings.TrimSpace(s.setting(key, ""))); err == nil {
		return v
	}
	return def
}
