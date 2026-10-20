package translator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

// These tests exercise the real HTTP path against a stub that speaks the OpenAI
// /models + /chat/completions contract, which is what every supported provider
// (Ollama, LM Studio, DeepSeek, Qwen, OpenAI) implements. Testing the actual
// client instead of a hand-written fake is deliberate: the request/response
// shapes are where this pipeline would break in production.

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	sqlDB, err := db.DB()
	if err == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	if err := repository.AutoMigrate(db); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

// fakeLLM is a stub OpenAI-compatible endpoint.
type fakeLLM struct {
	server      *httptest.Server
	chatCalls   int32
	modelCalls  int32
	lastTarget  string
	lastInputs  []string
	mu          sync.Mutex
	translateFn func(texts []string, target string) ([]string, error)
	// rawContent, when set, is returned verbatim as the assistant message
	// content, bypassing the JSON marshaling. Used to test malformed replies.
	rawContent func(texts []string, target string) string
	modelsErr  bool
	chatErr    bool
	// delay makes each chat request hold the connection open, so concurrent
	// requests overlap and peakInFlight can observe real parallelism.
	delay       time.Duration
	inFlight    int32
	maxInFlight int32
}

// peakInFlight reports the highest number of chat requests the stub ever saw
// executing at the same moment.
func (f *fakeLLM) peakInFlight() int32 { return atomic.LoadInt32(&f.maxInFlight) }

func newFakeLLM(t *testing.T) *fakeLLM {
	t.Helper()
	f := &fakeLLM{}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&f.modelCalls, 1)
		if f.modelsErr {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"id":"qwen2.5:7b"},{"id":"llama3.1:8b"}]}`)
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&f.chatCalls, 1)
		cur := atomic.AddInt32(&f.inFlight, 1)
		defer atomic.AddInt32(&f.inFlight, -1)
		for {
			peak := atomic.LoadInt32(&f.maxInFlight)
			if cur <= peak || atomic.CompareAndSwapInt32(&f.maxInFlight, peak, cur) {
				break
			}
		}
		if f.delay > 0 {
			time.Sleep(f.delay)
		}
		if f.chatErr {
			http.Error(w, `{"error":{"message":"model overloaded"}}`, http.StatusInternalServerError)
			return
		}
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if len(req.Messages) < 2 {
			http.Error(w, "missing messages", http.StatusBadRequest)
			return
		}
		var texts []string
		if err := json.Unmarshal([]byte(req.Messages[1].Content), &texts); err != nil {
			http.Error(w, "input was not a JSON array", http.StatusBadRequest)
			return
		}
		target := "en"
		if strings.Contains(req.Messages[0].Content, "Simplified Chinese") {
			target = "zh"
		}
		f.mu.Lock()
		f.lastTarget = target
		f.lastInputs = texts
		f.mu.Unlock()

		content := ""
		if f.rawContent != nil {
			content = f.rawContent(texts, target)
		} else {
			out := make([]string, 0, len(texts))
			if f.translateFn != nil {
				translated, err := f.translateFn(texts, target)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				out = translated
			} else {
				for _, s := range texts {
					out = append(out, "T:"+s)
				}
			}
			payload, _ := json.Marshal(out)
			content = string(payload)
		}
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"role": "assistant", "content": content}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeLLM) baseURL() string { return f.server.URL + "/v1" }

func (f *fakeLLM) chatCount() int { return int(atomic.LoadInt32(&f.chatCalls)) }

// configure points the service at the stub. httptest always binds 127.0.0.1, so
// the endpoint counts as local and needs no API key.
func configure(t *testing.T, svc *Service, baseURL, scope string) {
	t.Helper()
	settings := map[string]string{
		"translate_enabled":         "true",
		"translate_provider":        ProviderLocal,
		"llm_base_url":              baseURL,
		"llm_model":                 "qwen2.5:7b",
		"llm_api_key":               "",
		"translate_scope":           scope,
		"translate_batch_size":      "5",
		"translate_timeout_seconds": "20",
		"translate_daily_limit":     "0",
	}
	for k, v := range settings {
		if err := repository.NewSystemSettingRepository(svc.db).Set(k, v, ""); err != nil {
			t.Fatalf("set setting %s: %v", k, err)
		}
	}
}

func seedArticle(t *testing.T, db *gorm.DB, a *models.Article) *models.Article {
	t.Helper()
	if a.OriginURL == "" {
		a.OriginURL = "https://example.com/" + a.Title
	}
	if a.FetchedAt.IsZero() {
		a.FetchedAt = time.Now()
	}
	if a.ExpireAt.IsZero() {
		a.ExpireAt = a.FetchedAt.AddDate(0, 0, 90)
	}
	if a.Fingerprint == "" {
		a.Fingerprint = a.Title
	}
	EnsureOriginal(a)
	if err := db.Create(a).Error; err != nil {
		t.Fatalf("create article: %v", err)
	}
	return a
}

// TestRunOnceTranslatesChineseArticleIntoEnglish is the core happy path.
func TestRunOnceTranslatesChineseArticleIntoEnglish(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)
	fake := newFakeLLM(t)
	configure(t, svc, fake.baseURL(), ScopeTitleSummary)

	a := seedArticle(t, db, &models.Article{
		Title:   "央行维持利率不变",
		Summary: "中国人民银行周二宣布维持基准利率不变。",
		Lang:    "zh",
	})
	if a.TitleZh != "央行维持利率不变" {
		t.Fatalf("original side not filled: %+v", a)
	}

	n, err := svc.RunOnce(context.Background(), 5)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if n != 1 {
		t.Fatalf("translated %d articles, want 1", n)
	}

	var got models.Article
	if err := db.First(&got, a.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.TitleEn == "" || got.SummaryEn == "" {
		t.Fatalf("missing English variant: title_en=%q summary_en=%q", got.TitleEn, got.SummaryEn)
	}
	if !strings.HasPrefix(got.TitleEn, "T:") {
		t.Fatalf("title_en = %q, expected the stub translation", got.TitleEn)
	}
	if got.TransStatus != models.TransDone {
		t.Fatalf("status = %q, want done", got.TransStatus)
	}
	if got.TranslatedAt == nil {
		t.Fatal("translated_at was not set")
	}
	// The original text must never be overwritten by a translation.
	if got.Title != "央行维持利率不变" || got.TitleZh != "央行维持利率不变" {
		t.Fatalf("original text was modified: title=%q title_zh=%q", got.Title, got.TitleZh)
	}
	if fake.lastTarget != "en" {
		t.Fatalf("target language sent to provider = %q, want en", fake.lastTarget)
	}
}

// TestRunOnceTranslatesEnglishArticleIntoChinese is the opposite direction,
// which is what the English sources (Defense News, Fox News, Hacker News) need.
func TestRunOnceTranslatesEnglishArticleIntoChinese(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)
	fake := newFakeLLM(t)
	configure(t, svc, fake.baseURL(), ScopeTitleSummary)

	a := seedArticle(t, db, &models.Article{
		Title:   "Fed holds interest rates steady",
		Summary: "The Federal Reserve left its benchmark rate unchanged.",
		Lang:    "en",
	})

	if _, err := svc.RunOnce(context.Background(), 5); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	var got models.Article
	if err := db.First(&got, a.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.TitleZh == "" || got.SummaryZh == "" {
		t.Fatalf("missing Chinese variant: title_zh=%q summary_zh=%q", got.TitleZh, got.SummaryZh)
	}
	if got.TitleEn != "Fed holds interest rates steady" {
		t.Fatalf("English original changed: %q", got.TitleEn)
	}
	if fake.lastTarget != "zh" {
		t.Fatalf("target language = %q, want zh", fake.lastTarget)
	}
}

// TestCacheAvoidsSecondProviderCall is the cost guarantee: the same text is
// never translated (or paid for) twice, across articles and across restarts.
func TestCacheAvoidsSecondProviderCall(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)
	fake := newFakeLLM(t)
	configure(t, svc, fake.baseURL(), ScopeTitle)

	// Two sources carrying the identical wire headline.
	seedArticle(t, db, &models.Article{Title: "央行维持利率不变", Summary: "同一条通稿", Lang: "zh"})
	seedArticle(t, db, &models.Article{Title: "央行维持利率不变", Summary: "同一条通稿", Lang: "zh", OriginURL: "https://other.example.com/x"})

	if _, err := svc.RunOnce(context.Background(), 5); err != nil {
		t.Fatalf("first run: %v", err)
	}
	afterFirst := fake.chatCount()
	if afterFirst != 1 {
		t.Fatalf("first run made %d chat requests, want 1 (articles are processed sequentially, so the second one hits the cache the first one just filled)", afterFirst)
	}

	// A third article with the same headline must be served from the cache.
	seedArticle(t, db, &models.Article{Title: "央行维持利率不变", Summary: "同一条通稿", Lang: "zh", OriginURL: "https://third.example.com/y"})
	if _, err := svc.RunOnce(context.Background(), 5); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if got := fake.chatCount(); got != afterFirst {
		t.Fatalf("cache miss: chat requests went from %d to %d", afterFirst, got)
	}

	// And the cache survives a "restart" (a brand new service on the same db).
	svc2 := NewService(db)
	seedArticle(t, db, &models.Article{Title: "央行维持利率不变", Summary: "同一条通稿", Lang: "zh", OriginURL: "https://fourth.example.com/z"})
	if _, err := svc2.RunOnce(context.Background(), 5); err != nil {
		t.Fatalf("third run: %v", err)
	}
	if got := fake.chatCount(); got != afterFirst {
		t.Fatalf("cache did not survive restart: chat requests = %d, want %d", got, afterFirst)
	}
}

// TestProviderErrorMarksFailedAndCountsAttempts covers a broken/failing model:
// the row must be retried later but not forever.
func TestProviderErrorMarksFailedAndCountsAttempts(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)
	fake := newFakeLLM(t)
	fake.chatErr = true
	configure(t, svc, fake.baseURL(), ScopeTitle)

	a := seedArticle(t, db, &models.Article{Title: "央行维持利率不变", Lang: "zh"})

	_, err := svc.RunOnce(context.Background(), 5)
	if err == nil {
		t.Fatal("expected an error from a failing provider")
	}

	var got models.Article
	if err := db.First(&got, a.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.TransStatus != models.TransFailed {
		t.Fatalf("status = %q, want failed", got.TransStatus)
	}
	if got.TransAttempts != 1 {
		t.Fatalf("attempts = %d, want 1", got.TransAttempts)
	}
	if !strings.Contains(got.TransError, "500") && !strings.Contains(got.TransError, "overloaded") {
		t.Fatalf("trans_error does not explain the failure: %q", got.TransError)
	}

	// Retry until the attempts cap, then the row must drop out of the queue.
	for i := 0; i < maxAttempts+1; i++ {
		_, _ = svc.RunOnce(context.Background(), 5)
	}
	var after models.Article
	if err := db.First(&after, a.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if after.TransAttempts > maxAttempts {
		t.Fatalf("attempts kept growing to %d, cap is %d", after.TransAttempts, maxAttempts)
	}
	pending, err := repository.NewArticleRepository(db).ListPendingTranslation(10, maxAttempts)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("exhausted article is still queued: %d rows", len(pending))
	}
}

// TestUnreachableEndpointDoesNotBurnAttempts is the "user has not started Ollama
// yet" case. It is the single most likely real-world state, and it must not
// destroy the retry budget of every article.
func TestUnreachableEndpointDoesNotBurnAttempts(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)

	// A port that is closed: bind and immediately release one.
	fake := newFakeLLM(t)
	deadURL := fake.baseURL()
	fake.server.Close()

	configure(t, svc, deadURL, ScopeTitle)
	a := seedArticle(t, db, &models.Article{Title: "央行维持利率不变", Lang: "zh"})

	_, err := svc.RunOnce(context.Background(), 5)
	if err == nil {
		t.Fatal("expected an error when the endpoint is unreachable")
	}

	var got models.Article
	if err := db.First(&got, a.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.TransAttempts != 0 {
		t.Fatalf("attempts = %d, want 0: an unreachable endpoint must not consume retries", got.TransAttempts)
	}
	if got.TransStatus == models.TransFailed {
		t.Fatalf("status = %q; unreachable endpoint must leave it pending", got.TransStatus)
	}
}

// TestTranslationDisabledIsNotAnErrorState covers the default configuration:
// translation off must leave rows pending, not failed.
func TestTranslationDisabledIsNotAnErrorState(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)
	// SeedDefaultSettings default is translate_enabled=false.
	if err := repository.SeedDefaultSettings(db); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	a := seedArticle(t, db, &models.Article{Title: "央行维持利率不变", Lang: "zh"})

	if _, err := svc.RunOnce(context.Background(), 5); err == nil {
		t.Fatal("expected an error explaining that translation is off")
	}
	var got models.Article
	if err := db.First(&got, a.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.TransStatus == models.TransFailed || got.TransAttempts != 0 {
		t.Fatalf("disabled translation modified the row: status=%q attempts=%d", got.TransStatus, got.TransAttempts)
	}
	if got.TransStatus != models.TransPending {
		t.Fatalf("status = %q, want pending", got.TransStatus)
	}
}

// TestScopeTitleSkipsSummary keeps local models fast: a user who only wants the
// display headline translated must not pay for abstracts.
func TestScopeTitleSkipsSummary(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)
	fake := newFakeLLM(t)
	configure(t, svc, fake.baseURL(), ScopeTitle)

	a := seedArticle(t, db, &models.Article{Title: "央行维持利率不变", Summary: "很长的一段摘要", Lang: "zh"})

	if _, err := svc.RunOnce(context.Background(), 5); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	fake.mu.Lock()
	inputs := append([]string(nil), fake.lastInputs...)
	fake.mu.Unlock()
	if len(inputs) != 1 {
		t.Fatalf("sent %d texts to the model, want only the title", len(inputs))
	}

	var got models.Article
	if err := db.First(&got, a.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.TitleEn == "" {
		t.Fatal("title was not translated")
	}
	if got.SummaryEn != "" {
		t.Fatalf("summary_en = %q, want empty with scope=title", got.SummaryEn)
	}
	if got.TransStatus != models.TransDone {
		t.Fatalf("status = %q, want done", got.TransStatus)
	}
}

// TestDailyLimitStopsTheWorker is the cost guard for a paid provider.
func TestDailyLimitStopsTheWorker(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)
	fake := newFakeLLM(t)
	configure(t, svc, fake.baseURL(), ScopeTitle)
	if err := repository.NewSystemSettingRepository(db).Set("translate_daily_limit", "1", ""); err != nil {
		t.Fatalf("set limit: %v", err)
	}

	seedArticle(t, db, &models.Article{Title: "第一条", Lang: "zh"})
	if _, err := svc.RunOnce(context.Background(), 5); err != nil {
		t.Fatalf("first run should succeed: %v", err)
	}

	seedArticle(t, db, &models.Article{Title: "第二条", Lang: "zh", OriginURL: "https://e.example.com/2"})
	_, err := svc.RunOnce(context.Background(), 5)
	if err == nil {
		t.Fatal("expected the daily limit to stop the second run")
	}
	if !strings.Contains(err.Error(), "daily translation limit") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestDetectLocalModelsFindsTheStub proves the "detect a free local model"
// button works against a real listening server.
func TestDetectLocalModelsFindsTheStub(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)
	fake := newFakeLLM(t)
	// The stub is not on a well-known port, so point the probe table at it by
	// exercising ListModels directly for the stub and DetectLocalModels for the
	// real table (which will simply find nothing in CI).
	modelsList, err := svc.ListModels(context.Background(), Config{BaseURL: fake.baseURL()})
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(modelsList) != 2 || modelsList[0] != "qwen2.5:7b" {
		t.Fatalf("models = %v, want the two stubbed ids", modelsList)
	}
	found := svc.DetectLocalModels(context.Background())
	for _, f := range found {
		if len(f.Models) == 0 {
			t.Fatalf("local model %s reported with no models", f.Name)
		}
	}
}

// TestParseStringArrayCopesWithModelChatter covers the parsing the pipeline
// depends on: models add markdown fences and wrappers despite instructions.
func TestParseStringArrayCopesWithModelChatter(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"plain", `["a","b"]`, []string{"a", "b"}},
		{"fenced json", "```json\n[\"a\",\"b\"]\n```", []string{"a", "b"}},
		{"fenced bare", "```\n[\"a\",\"b\"]\n```", []string{"a", "b"}},
		{"prose around", "Here you go: [\"a\",\"b\"] — done", []string{"a", "b"}},
		{"wrapper", `{"translations":["a","b"]}`, []string{"a", "b"}},
		{"index keyed", `{"0":"a","1":"b"}`, []string{"a", "b"}},
		// The model quoted only the first half and left the rest outside the
		// quotes: `["[headline]" trailing prose]`. The tail is reattached.
		{"malformed array with trailing prose",
			`["[Two departments: land reserve price]" Jinshi Data, September 29 — the Ministry]`,
			[]string{"[Two departments: land reserve price] Jinshi Data, September 29 — the Ministry"}},
		{"missing comma", `["a" "b"]`, []string{"a", "b"}},
		{"unterminated array", `["a", "b"`, []string{"a", "b"}},
		{"trailing comma", `["a", "b",]`, []string{"a", "b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseStringArray(tc.in)
			if err != nil {
				t.Fatalf("parseStringArray(%q): %v", tc.in, err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// TestProviderSingleItemSuccess verifies that the per-article path works:
// sending one text at a time should always return exactly one result.
func TestProviderSingleItemSuccess(t *testing.T) {
	fake := newFakeLLM(t)
	p := &OpenAICompatProvider{BaseURL: fake.baseURL(), ModelID: "stub", Client: fake.server.Client()}
	results, err := p.Translate(context.Background(), []string{"hello"}, "en")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0] != "T:hello" {
		t.Fatalf("expected [T:hello], got %v", results)
	}
}

// TestProviderRecoversFromMalformedArray covers the real-world reply where the
// model quotes only the first half of the translation and leaves the rest
// outside the quotes: `["[headline]" trailing prose]`. The article must still
// be translated instead of failing the whole run.
func TestProviderRecoversFromMalformedArray(t *testing.T) {
	fake := newFakeLLM(t)
	fake.rawContent = func(texts []string, target string) string {
		return `["[Two departments: The reserve price for the transfer of service industry land can be determined at no less than the minimum standard for agreement-based transfer of land of the corresponding use]" Jinshi Data, September 29 — The Ministry of Natural Resources and the National Forestry and Grassland]`
	}
	p := &OpenAICompatProvider{BaseURL: fake.baseURL(), ModelID: "stub", Client: fake.server.Client()}

	results, err := p.Translate(context.Background(), []string{"【两部门：服务业用地出让底价可按不低于协议出让最低价标准确定】金石数据，9月29日"}, "en")
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1: %v", len(results), results)
	}
	want := "[Two departments: The reserve price for the transfer of service industry land can be determined at no less than the minimum standard for agreement-based transfer of land of the corresponding use] Jinshi Data, September 29 — The Ministry of Natural Resources and the National Forestry and Grassland"
	if results[0] != want {
		t.Fatalf("recovered translation =\n%q\nwant\n%q", results[0], want)
	}
}

// TestRunOnceRecoversFromMalformedProviderOutput is the end-to-end version: a
// model that returns a broken array must not leave the article pending.
func TestRunOnceRecoversFromMalformedProviderOutput(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)
	fake := newFakeLLM(t)
	configure(t, svc, fake.baseURL(), ScopeTitle)
	fake.rawContent = func(texts []string, target string) string {
		return `["[Two departments: land reserve price]" Jinshi Data, September 29 — the Ministry]`
	}

	a := seedArticle(t, db, &models.Article{
		Title:     "【两部门：服务业用地出让底价】金石数据，9月29日——自然资源部",
		Lang:      "zh",
		OriginURL: "https://example.com/malformed",
	})

	n, err := svc.RunOnce(context.Background(), 5)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if n != 1 {
		t.Fatalf("translated %d articles, want 1", n)
	}

	var got models.Article
	if err := db.First(&got, a.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.TransStatus != models.TransDone {
		t.Fatalf("status = %q, want done", got.TransStatus)
	}
	if !strings.Contains(got.TitleEn, "Jinshi Data") || !strings.Contains(got.TitleEn, "Two departments") {
		t.Fatalf("title_en = %q", got.TitleEn)
	}
}

// TestConfigIsLocalAndConfigured pins the free-first defaults.
func TestConfigIsLocalAndConfigured(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)
	if err := repository.SeedDefaultSettings(db); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	cfg := svc.Config()
	if cfg.Provider != ProviderLocal {
		t.Fatalf("default provider = %q, want %q (free local model first)", cfg.Provider, ProviderLocal)
	}
	if !cfg.IsLocal() {
		t.Fatalf("default base URL %q should be recognised as local", cfg.BaseURL)
	}
	if cfg.Enabled {
		t.Fatal("translation must be opt-in, not enabled by default")
	}
	// A cloud endpoint without a key must not count as configured.
	cloud := Config{Enabled: true, Provider: ProviderCloud, BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-chat"}
	if cloud.Configured() {
		t.Fatal("a cloud provider with no API key must not be considered configured")
	}
	cloud.APIKey = "sk-test"
	if !cloud.Configured() {
		t.Fatal("a cloud provider with a key should be configured")
	}
}

// TestOneBrokenArticleDoesNotBlockTheOthers pins the isolation guarantee: the
// run spawns one goroutine per article, so a headline the model cannot process
// is recorded on that row only and never fails (or stops) the rest of the batch.
func TestOneBrokenArticleDoesNotBlockTheOthers(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)
	fake := newFakeLLM(t)
	configure(t, svc, fake.baseURL(), ScopeTitle)

	// The model chokes on exactly one headline and works on every other one.
	fake.translateFn = func(texts []string, target string) ([]string, error) {
		for _, s := range texts {
			if strings.Contains(s, "BROKEN") {
				return nil, fmt.Errorf("model cannot translate this input")
			}
		}
		out := make([]string, 0, len(texts))
		for _, s := range texts {
			out = append(out, "T:"+s)
		}
		return out, nil
	}

	good := seedArticle(t, db, &models.Article{Title: "央行维持利率不变", Lang: "zh"})
	bad := seedArticle(t, db, &models.Article{
		Title:     "BROKEN 头条",
		Lang:      "zh",
		OriginURL: "https://example.com/broken",
	})

	n, err := svc.RunOnce(context.Background(), 5)
	if err != nil {
		t.Fatalf("one unusable article must not fail the whole run: %v", err)
	}
	if n != 1 {
		t.Fatalf("translated %d articles, want 1", n)
	}

	var gotGood models.Article
	if err := db.First(&gotGood, good.ID).Error; err != nil {
		t.Fatalf("reload good: %v", err)
	}
	if gotGood.TransStatus != models.TransDone || gotGood.TitleEn == "" {
		t.Fatalf("healthy article was affected: status=%q title_en=%q", gotGood.TransStatus, gotGood.TitleEn)
	}

	var gotBad models.Article
	if err := db.First(&gotBad, bad.ID).Error; err != nil {
		t.Fatalf("reload bad: %v", err)
	}
	if gotBad.TransStatus != models.TransFailed {
		t.Fatalf("broken article status = %q, want failed", gotBad.TransStatus)
	}
	if gotBad.TransAttempts != 1 {
		t.Fatalf("broken article attempts = %d, want 1", gotBad.TransAttempts)
	}
	if !strings.Contains(gotBad.TransError, "cannot translate") {
		t.Fatalf("broken article trans_error = %q", gotBad.TransError)
	}

	// The failure stays visible for the user even though the run succeeded.
	if st := svc.Status(); st.LastError == "" {
		t.Fatal("status.lastError should explain the article that failed")
	}
}

// TestRunOnceRunsArticlesConcurrently pins the llm_max_concurrency contract:
// with the setting at 4, four articles in one batch must overlap at the
// provider (peak in-flight > 1) while never exceeding the configured ceiling.
func TestRunOnceRunsArticlesConcurrently(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)
	fake := newFakeLLM(t)
	configure(t, svc, fake.baseURL(), ScopeTitle)
	if err := repository.NewSystemSettingRepository(db).Set("llm_max_concurrency", "4", ""); err != nil {
		t.Fatalf("set concurrency: %v", err)
	}
	fake.delay = 150 * time.Millisecond

	for i := 0; i < 4; i++ {
		seedArticle(t, db, &models.Article{
			Title:   fmt.Sprintf("并发测试文章标题%d", i),
			Summary: fmt.Sprintf("这是第%d篇等待翻译的中文摘要。", i),
			Lang:    "zh",
		})
	}

	n, err := svc.RunOnce(context.Background(), 10)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if n != 4 {
		t.Fatalf("translated %d articles, want 4", n)
	}

	peak := fake.peakInFlight()
	if peak < 2 {
		t.Fatalf("peak in-flight = %d, want >1: articles should be processed concurrently", peak)
	}
	if peak > 4 {
		t.Fatalf("peak in-flight = %d, want <= llm_max_concurrency (4)", peak)
	}
}
