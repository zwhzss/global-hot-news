package translator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"ai-informations/internal/models"
	"ai-informations/internal/repository"
)

// These tests exercise the two REST translation engines against stubs that
// speak their real wire formats (MTranServer /translate[/batch], LibreTranslate
// /translate), plus the engine-selection logic in Service.

// setSettings writes arbitrary system settings into the service's database.
func setSettings(t *testing.T, svc *Service, settings map[string]string) {
	t.Helper()
	repo := repository.NewSystemSettingRepository(svc.db)
	for k, v := range settings {
		if err := repo.Set(k, v, ""); err != nil {
			t.Fatalf("set setting %s: %v", k, err)
		}
	}
}

// fakeMTran stubs MTranServer: /health, /translate/batch, /translate.
type fakeMTran struct {
	server       *httptest.Server
	batchCalls   int32
	singleCalls  int32
	mu           sync.Mutex
	lastFrom     string
	lastTo       string
	requireToken string
}

func newFakeMTran(t *testing.T) *fakeMTran {
	t.Helper()
	f := &fakeMTran{}
	mux := http.NewServeMux()
	authorized := func(w http.ResponseWriter, r *http.Request) bool {
		if f.requireToken != "" && r.Header.Get("Authorization") != "Bearer "+f.requireToken {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return false
		}
		return true
	}
	remember := func(from, to string) {
		f.mu.Lock()
		f.lastFrom, f.lastTo = from, to
		f.mu.Unlock()
	}
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"ok"}`)
	})
	mux.HandleFunc("/translate/batch", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&f.batchCalls, 1)
		if !authorized(w, r) {
			return
		}
		var req mtRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Texts) == 0 {
			http.Error(w, `{"error":"bad batch request"}`, http.StatusBadRequest)
			return
		}
		remember(req.From, req.To)
		out := make([]string, 0, len(req.Texts))
		for _, s := range req.Texts {
			out = append(out, "T:"+s)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": out})
	})
	mux.HandleFunc("/translate", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&f.singleCalls, 1)
		if !authorized(w, r) {
			return
		}
		var req mtRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Text == "" {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		remember(req.From, req.To)
		_ = json.NewEncoder(w).Encode(map[string]string{"result": "T:" + req.Text})
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

// codes returns the last from/to pair the stub observed.
func (f *fakeMTran) codes() (string, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastFrom, f.lastTo
}

func TestMTranServerTranslateBatchAndCodes(t *testing.T) {
	fake := newFakeMTran(t)
	fake.requireToken = "sekrit"
	p := &MTranServerProvider{BaseURL: fake.server.URL, Token: "sekrit", Client: fake.server.Client()}

	out, err := p.Translate(context.Background(), []string{"hello", "world"}, "zh")
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if len(out) != 2 || out[0] != "T:hello" || out[1] != "T:world" {
		t.Fatalf("out = %q, want two T: prefixed translations", out)
	}
	if atomic.LoadInt32(&fake.batchCalls) != 1 {
		t.Fatalf("batch calls = %d, want 1", fake.batchCalls)
	}
	// en article → Chinese: from en, to zh-Hans (MTranServer's code for zh).
	if from, to := fake.codes(); from != "en" || to != "zh-Hans" {
		t.Fatalf("from/to = %q/%q, want en/zh-Hans", from, to)
	}
}

func TestMTranServerTranslateChineseToEnglish(t *testing.T) {
	fake := newFakeMTran(t)
	p := &MTranServerProvider{BaseURL: fake.server.URL, Client: fake.server.Client()}

	out, err := p.Translate(context.Background(), []string{"央行维持利率不变"}, "en")
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if len(out) != 1 || out[0] == "" {
		t.Fatalf("out = %q, want one translation", out)
	}
	if from, to := fake.codes(); from != "zh-Hans" || to != "en" {
		t.Fatalf("from/to = %q/%q, want zh-Hans/en", from, to)
	}
}

func TestMTranServerFallsBackToSingleWhenBatchFails(t *testing.T) {
	fake := newFakeMTran(t)
	// Break only the batch endpoint.
	mux := http.NewServeMux()
	mux.HandleFunc("/translate/batch", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	mux.HandleFunc("/translate", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&fake.singleCalls, 1)
		var req mtRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		_ = json.NewEncoder(w).Encode(map[string]string{"result": "S:" + req.Text})
	})
	fake.server.Config.Handler = mux

	p := &MTranServerProvider{BaseURL: fake.server.URL, Client: fake.server.Client()}
	out, err := p.Translate(context.Background(), []string{"a", "b", "c"}, "zh")
	if err != nil {
		t.Fatalf("Translate with broken batch: %v", err)
	}
	if len(out) != 3 || out[0] != "S:a" || out[2] != "S:c" {
		t.Fatalf("out = %q, want three S: prefixed singles", out)
	}
	if atomic.LoadInt32(&fake.singleCalls) != 3 {
		t.Fatalf("single calls = %d, want 3", fake.singleCalls)
	}
}

// fakeLibre stubs LibreTranslate: /languages and /translate.
type fakeLibre struct {
	server     *httptest.Server
	translateN int32
	mu         sync.Mutex
	lastReq    struct {
		Q      json.RawMessage `json:"q"`
		Source string          `json:"source"`
		Target string          `json:"target"`
		Format string          `json:"format"`
		APIKey string          `json:"api_key"`
	}
}

// req returns the last /translate request the stub observed.
func (f *fakeLibre) req() (source, target, format, apiKey string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastReq.Source, f.lastReq.Target, f.lastReq.Format, f.lastReq.APIKey
}

func newFakeLibre(t *testing.T) *fakeLibre {
	t.Helper()
	f := &fakeLibre{}
	mux := http.NewServeMux()
	mux.HandleFunc("/languages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"code":"en","name":"English"},{"code":"zh","name":"Chinese"}]`)
	})
	mux.HandleFunc("/translate", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&f.translateN, 1)
		var got struct {
			Q      json.RawMessage `json:"q"`
			Source string          `json:"source"`
			Target string          `json:"target"`
			Format string          `json:"format"`
			APIKey string          `json:"api_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.lastReq = got
		f.mu.Unlock()
		var many []string
		if err := json.Unmarshal(got.Q, &many); err == nil {
			out := make([]string, 0, len(many))
			for _, s := range many {
				out = append(out, "T:"+s)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"translatedText": out})
			return
		}
		var one string
		_ = json.Unmarshal(got.Q, &one)
		_ = json.NewEncoder(w).Encode(map[string]string{"translatedText": "T:" + one})
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func TestLibreTranslateTranslateAndCodes(t *testing.T) {
	fake := newFakeLibre(t)
	p := &LibreTranslateProvider{BaseURL: fake.server.URL, APIKey: "k1", Client: fake.server.Client()}

	out, err := p.Translate(context.Background(), []string{"hello", "world"}, "zh")
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if len(out) != 2 || out[0] != "T:hello" || out[1] != "T:world" {
		t.Fatalf("out = %q, want two T: prefixed translations", out)
	}
	if source, target, format, apiKey := fake.req(); source != "en" || target != "zh" || format != "text" {
		t.Fatalf("source/target/format = %q/%q/%q, want en/zh/text", source, target, format)
	} else if apiKey != "k1" {
		t.Fatalf("api_key = %q, want k1", apiKey)
	}
}

func TestLibreTranslateSingleStringResponse(t *testing.T) {
	fake := newFakeLibre(t)
	p := &LibreTranslateProvider{BaseURL: fake.server.URL, Client: fake.server.Client()}

	out, err := p.Translate(context.Background(), []string{"央行维持利率不变"}, "en")
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if len(out) != 1 || out[0] != "T:央行维持利率不变" {
		t.Fatalf("out = %q, want the single translated string", out)
	}
	if source, target, _, _ := fake.req(); source != "zh" || target != "en" {
		t.Fatalf("source/target = %q/%q, want zh/en", source, target)
	}
}

func TestLibreTranslateSurfacesEngineError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"Invalid target language"}`)
	}))
	t.Cleanup(srv.Close)

	p := &LibreTranslateProvider{BaseURL: srv.URL, Client: srv.Client()}
	if _, err := p.Translate(context.Background(), []string{"hello"}, "xx"); err == nil {
		t.Fatal("expected an error carrying the engine's message")
	} else if !strings.Contains(err.Error(), "Invalid target language") {
		t.Fatalf("error = %v, want it to include the engine message", err)
	}
}

// TestProviderEngineSwitch covers Service.provider for every engine value.
func TestProviderEngineSwitch(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)
	fake := newFakeMTran(t)

	setSettings(t, svc, map[string]string{
		"translate_enabled":  "true",
		"translate_engine":   EngineMTranServer,
		"mtranserver_url":    fake.server.URL,
		"mtranserver_token":  "",
		"translate_provider": ProviderLocal,
		"llm_base_url":       "http://127.0.0.1:11434/v1",
		"llm_model":          "qwen2.5:7b",
	})
	cfg := svc.Config()
	if cfg.Engine != EngineMTranServer {
		t.Fatalf("cfg.Engine = %q, want mtranserver", cfg.Engine)
	}
	if !cfg.Configured() {
		t.Fatal("mtranserver engine should be configured when enabled with a URL")
	}
	p, err := svc.provider(cfg)
	if err != nil || p == nil {
		t.Fatalf("provider = %v, %v; want *MTranServerProvider", p, err)
	}
	if _, ok := p.(*MTranServerProvider); !ok {
		t.Fatalf("provider type = %T, want *MTranServerProvider", p)
	}

	// LibreTranslate engine.
	setSettings(t, svc, map[string]string{"translate_engine": EngineLibreTranslate})
	p, err = svc.provider(svc.Config())
	if err != nil || p == nil {
		t.Fatalf("provider = %v, %v; want *LibreTranslateProvider", p, err)
	}
	if _, ok := p.(*LibreTranslateProvider); !ok {
		t.Fatalf("provider type = %T, want *LibreTranslateProvider", p)
	}

	// The llm engine keeps the OpenAI-compatible provider.
	setSettings(t, svc, map[string]string{"translate_engine": EngineLLM, "translate_provider": ProviderLocal})
	p, err = svc.provider(svc.Config())
	if err != nil || p == nil {
		t.Fatalf("provider = %v, %v; want *OpenAICompatProvider", p, err)
	}
	if _, ok := p.(*OpenAICompatProvider); !ok {
		t.Fatalf("provider type = %T, want *OpenAICompatProvider", p)
	}

	// Unknown engine is an error; disabled translation yields (nil, nil).
	if _, err := svc.provider(Config{Engine: "bogus", Enabled: true}); err == nil {
		t.Fatal("expected an error for an unknown engine")
	}
	setSettings(t, svc, map[string]string{"translate_engine": EngineMTranServer, "translate_enabled": "false"})
	if p, err := svc.provider(svc.Config()); p != nil || err != nil {
		t.Fatalf("disabled engine: provider = %v, %v; want nil, nil", p, err)
	}
}

// TestClassifierProviderSurvivesRestEngine: classification must keep its chat
// model (and must not panic on a type assert) while translation runs on a REST
// engine.
func TestClassifierProviderSurvivesRestEngine(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)
	fakeLLM := newFakeLLM(t)

	setSettings(t, svc, map[string]string{
		"translate_enabled":  "true",
		"translate_engine":   EngineMTranServer,
		"mtranserver_url":    "http://127.0.0.1:1",
		"translate_provider": ProviderLocal,
		"cls_base_url":       fakeLLM.baseURL(),
		"cls_model":          "qwen2.5:7b",
	})
	p, err := svc.ClassifierProvider()
	if err != nil {
		t.Fatalf("ClassifierProvider: %v", err)
	}
	if p == nil {
		t.Fatal("ClassifierProvider = nil; the cls_* settings must back classification")
	}
	if p.ModelID != "qwen2.5:7b" {
		t.Fatalf("ModelID = %q, want qwen2.5:7b", p.ModelID)
	}
}

// TestClassifierModelIndependentFromTranslation: translation and classification
// each have their own model settings — the classifier keeps working while
// translation is disabled, and llm_* never leaks into the classifier (or the
// other way round).
func TestClassifierModelIndependentFromTranslation(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)

	setSettings(t, svc, map[string]string{
		"translate_enabled":  "false",
		"translate_provider": ProviderLocal,
		"llm_base_url":       "http://127.0.0.1:11434/v1",
		"llm_model":          "translate-model",
		"cls_base_url":       "http://127.0.0.1:11434/v1",
		"cls_model":          "classify-model",
	})

	p, err := svc.ClassifierProvider()
	if err != nil {
		t.Fatalf("ClassifierProvider: %v", err)
	}
	if p == nil {
		t.Fatal("ClassifierProvider = nil; classification must not depend on translate_enabled")
	}
	if p.ModelID != "classify-model" {
		t.Fatalf("classifier ModelID = %q, want classify-model (cls_* must win over llm_*)", p.ModelID)
	}

	// Translation stays off; the classifier's model must not leak into it.
	cfg := svc.Config()
	tp, err := svc.provider(cfg)
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	if tp != nil {
		t.Fatalf("translation provider = %T, want nil while translate_enabled=false", tp)
	}
}

// TestRunOnceWithMTranServerEngine is the end-to-end path: engine setting →
// provider → /health probe → article translated through the REST engine.
func TestRunOnceWithMTranServerEngine(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)
	fake := newFakeMTran(t)

	setSettings(t, svc, map[string]string{
		"translate_enabled":         "true",
		"translate_engine":          EngineMTranServer,
		"mtranserver_url":           fake.server.URL,
		"translate_provider":        ProviderLocal,
		"llm_base_url":              "http://127.0.0.1:11434/v1",
		"llm_model":                 "qwen2.5:7b",
		"translate_scope":           ScopeTitleSummary,
		"translate_batch_size":      "5",
		"translate_timeout_seconds": "20",
		"translate_daily_limit":     "0",
	})

	a := seedArticle(t, db, &models.Article{
		Title:   "Central bank holds rates steady",
		Summary: "The central bank left its benchmark rate unchanged.",
		Lang:    "en",
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
		t.Fatalf("trans_status = %q, want done (error: %s)", got.TransStatus, got.TransError)
	}
	if !strings.HasPrefix(got.TitleZh, "T:") {
		t.Fatalf("title_zh = %q, want the REST engine's output", got.TitleZh)
	}
	if from, to := fake.codes(); from != "en" || to != "zh-Hans" {
		t.Fatalf("from/to = %q/%q, want en/zh-Hans", from, to)
	}
}

// TestStatusMapsEngine: the settings page must see the active engine's URL.
func TestStatusMapsEngine(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)
	setSettings(t, svc, map[string]string{
		"translate_enabled":  "true",
		"translate_engine":   EngineLibreTranslate,
		"libretranslate_url": "http://127.0.0.1:1",
		"translate_provider": ProviderLocal,
	})
	st := svc.Status()
	if st.Engine != EngineLibreTranslate || st.Provider != EngineLibreTranslate {
		t.Fatalf("engine/provider = %q/%q, want libretranslate", st.Engine, st.Provider)
	}
	if st.BaseURL != "http://127.0.0.1:1" {
		t.Fatalf("baseUrl = %q, want the LibreTranslate URL", st.BaseURL)
	}
	if st.Model != "libretranslate" {
		t.Fatalf("model = %q, want libretranslate", st.Model)
	}
	if !st.Configured {
		t.Fatal("engine with URL and enabled must be configured")
	}
	if st.Reachable {
		t.Fatal("nothing listens on 127.0.0.1:1, reachable must be false")
	}
}
