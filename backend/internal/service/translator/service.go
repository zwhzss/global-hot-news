package translator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"ai-informations/internal/langdetect"
	"ai-informations/internal/models"
	"ai-informations/internal/repository"
	"ai-informations/internal/service/llmqueue"

	"gorm.io/gorm"
)

// maxAttempts bounds retries per article so a broken provider or a bad API key
// cannot spin on the same rows forever.
const maxAttempts = 3

// Service owns the bilingual pipeline: it decides which side of an article is
// the original, asks a provider for the missing side, caches every result and
// writes both variants back.
//
// Everything is driven by system settings and read fresh on every run, so the
// user can paste an API key in the settings page without restarting the app.
// Runs are no longer started automatically: the settings page enqueues them on
// the shared LLM queue, which serves one request at a time and can cancel a
// run mid-flight.
type Service struct {
	db       *gorm.DB
	articles *repository.ArticleRepository
	cache    *repository.TranslationRepository
	settings *repository.SystemSettingRepository
	client   *http.Client
	stopCh   chan struct{}
	stopOnce sync.Once

	mu         sync.Mutex
	isRunning  bool
	lastRun    time.Time
	lastError  string
	lastCount  int
	translated int64
	cacheHits  int64
	// Liveness probe cache, so a batch does not re-probe the endpoint per
	// article while still noticing quickly when the local model is started.
	probeKey string
	probeAt  time.Time
	probeOK  bool
}

func NewService(db *gorm.DB) *Service {
	return &Service{
		db:       db,
		articles: repository.NewArticleRepository(db),
		cache:    repository.NewTranslationRepository(db),
		settings: repository.NewSystemSettingRepository(db),
		client:   &http.Client{Timeout: 60 * time.Second},
		stopCh:   make(chan struct{}),
	}
}

// Provider presets. Both presets speak the OpenAI /chat/completions protocol, so
// they share one implementation; the preset only decides the defaults and how
// the UI describes the cost.
const (
	ProviderLocal = "local" // free: a model running on this machine
	ProviderCloud = "cloud" // paid: a commercial API
	ProviderOff   = "off"
)

// Translation engines: which implementation provider() builds. Within the
// llm engine the translate_provider preset (local/cloud/off) still decides
// the chat endpoint and the cost label; the two REST engines are always
// free, self-hosted services with their own base URL settings.
const (
	EngineLLM            = "llm"            // OpenAI-compatible chat model (default)
	EngineMTranServer    = "mtranserver"    // xxnuo/MTranServer REST API (default :8989)
	EngineLibreTranslate = "libretranslate" // LibreTranslate REST API (Docker, default :5000)
)

// Scope decides how much text is sent to the model. Local models are slow, and
// a headline plus its abstract is what the boards actually display, so
// "title_summary" is the default and full-body translation is opt-in.
const (
	ScopeTitle        = "title"
	ScopeTitleSummary = "title_summary"
	ScopeAll          = "all"
)

// Local model endpoints probed by DetectLocalModels. All three are free and
// run on the user's own hardware.
var localEndpoints = []struct {
	Name    string
	BaseURL string
}{
	{"Ollama", "http://127.0.0.1:11434/v1"},
	{"LM Studio", "http://127.0.0.1:1234/v1"},
	{"llama.cpp", "http://127.0.0.1:8080/v1"},
	{"LocalAI", "http://127.0.0.1:8081/v1"},
}

// Config is the translation configuration resolved from system settings.
type Config struct {
	Enabled    bool
	Provider   string
	BaseURL    string
	Model      string
	APIKey     string
	Scope      string
	BatchSize  int
	Timeout    time.Duration
	DailyLimit int
	// Concurrency is how many articles may be in flight at once inside one
	// job (llm_max_concurrency, clamped to 1..8). 1 keeps the old serial run.
	Concurrency int
	// Engine picks the translation backend: llm (chat model), mtranserver
	// or libretranslate (both plain REST, no prompt/JSON parsing involved).
	Engine string
	// MTranURL / MTranToken configure the MTranServer engine (token is only
	// needed when the server runs with MT_API_TOKEN set).
	MTranURL   string
	MTranToken string
	// LibreURL / LibreAPIKey configure the LibreTranslate engine (key only
	// needed when the deployment enforces REQUIRE_API_KEY).
	LibreURL    string
	LibreAPIKey string
	// ClsBaseURL / ClsModel / ClsAPIKey configure the classifier's own chat
	// model (cls_* settings). Classification is fully independent from
	// translation: it keeps its model even when translation is disabled or
	// runs on a REST engine, and moving llm_* never moves cls_*.
	ClsBaseURL string
	ClsModel   string
	ClsAPIKey  string
}

// clampConcurrency keeps llm_max_concurrency inside the supported 1..8 range;
// a missing or garbled setting degrades to 1 (serial), never to 0 (stall).
func clampConcurrency(n int) int {
	if n < 1 {
		return 1
	}
	if n > 8 {
		return 8
	}
	return n
}

// llmIsLocal reports whether the configured translation chat endpoint runs on
// this machine. It deliberately ignores the engine: the label must describe
// the llm_* endpoint even while translation runs on a REST engine.
func (c Config) llmIsLocal() bool {
	return c.Provider == ProviderLocal || isLocalURL(c.BaseURL)
}

// IsLocal reports whether the active engine's endpoint runs on this machine.
// Used to label the cost in the UI and to pick the test-connection direction.
func (c Config) IsLocal() bool {
	switch c.Engine {
	case EngineMTranServer:
		return isLocalURL(c.MTranURL)
	case EngineLibreTranslate:
		return isLocalURL(c.LibreURL)
	}
	return c.llmIsLocal()
}

// ConfiguredLLM reports whether a usable chat provider is configured for the
// llm translation engine. Classification has its own gate (ClassifierProvider)
// and does not depend on this.
func (c Config) ConfiguredLLM() bool {
	if !c.Enabled || c.Provider == ProviderOff {
		return false
	}
	if strings.TrimSpace(c.BaseURL) == "" || strings.TrimSpace(c.Model) == "" {
		return false
	}
	// A cloud provider without a key would only produce 401s.
	if !c.llmIsLocal() && strings.TrimSpace(c.APIKey) == "" {
		return false
	}
	return true
}

// Configured reports whether the active translation engine is ready.
func (c Config) Configured() bool {
	switch c.Engine {
	case EngineMTranServer:
		return c.Enabled && strings.TrimSpace(c.MTranURL) != ""
	case EngineLibreTranslate:
		return c.Enabled && strings.TrimSpace(c.LibreURL) != ""
	default:
		return c.ConfiguredLLM()
	}
}

func isLocalURL(raw string) bool {
	u := strings.ToLower(strings.TrimSpace(raw))
	return strings.Contains(u, "127.0.0.1") || strings.Contains(u, "localhost") ||
		strings.Contains(u, "0.0.0.0") || strings.Contains(u, "[::1]")
}

// Config reads the current translation settings.
func (s *Service) Config() Config {
	get := func(key, def string) string {
		if v, err := s.settings.Get(key); err == nil && strings.TrimSpace(v.Value) != "" {
			return strings.TrimSpace(v.Value)
		}
		return def
	}
	getInt := func(key string, def int) int {
		if n, err := strconv.Atoi(get(key, "")); err == nil && n >= 0 {
			return n
		}
		return def
	}

	cfg := Config{
		Enabled:     get("translate_enabled", "false") == "true",
		Provider:    get("translate_provider", ProviderLocal),
		BaseURL:     get("llm_base_url", "http://127.0.0.1:11434/v1"),
		Model:       get("llm_model", "qwen2.5:7b"),
		APIKey:      get("llm_api_key", ""),
		Scope:       get("translate_scope", ScopeTitleSummary),
		BatchSize:   getInt("translate_batch_size", 10),
		Timeout:     time.Duration(getInt("translate_timeout_seconds", 120)) * time.Second,
		DailyLimit:  getInt("translate_daily_limit", 300),
		Concurrency: clampConcurrency(getInt("llm_max_concurrency", 1)),

		Engine:      get("translate_engine", EngineLLM),
		MTranURL:    get("mtranserver_url", "http://localhost:8989"),
		MTranToken:  get("mtranserver_token", ""),
		LibreURL:    get("libretranslate_url", "http://localhost:5000"),
		LibreAPIKey: get("libretranslate_api_key", ""),

		// Classification model: its own keys, deliberately with no fallback
		// to llm_* — the two configurations must never move together.
		ClsBaseURL: get("cls_base_url", "http://127.0.0.1:11434/v1"),
		ClsModel:   get("cls_model", "qwen2.5:7b"),
		ClsAPIKey:  get("cls_api_key", ""),
	}
	// Reading the setting is also the moment the shared inference gate learns
	// the limit, so every caller (translation, classification, probes) sees an
	// up-to-date budget before it sends its next request.
	llmqueue.SetGateMax(cfg.Concurrency)
	return cfg
}

// provider builds the provider for the active engine. A nil provider with a
// nil error means translation is switched off or not configured yet.
func (s *Service) provider(cfg Config) (Provider, error) {
	switch cfg.Engine {
	case EngineMTranServer:
		if !cfg.Configured() {
			return nil, nil
		}
		return &MTranServerProvider{
			BaseURL: cfg.MTranURL,
			Token:   cfg.MTranToken,
			Client:  engineClient(cfg),
		}, nil
	case EngineLibreTranslate:
		if !cfg.Configured() {
			return nil, nil
		}
		return &LibreTranslateProvider{
			BaseURL: cfg.LibreURL,
			APIKey:  cfg.LibreAPIKey,
			Client:  engineClient(cfg),
		}, nil
	case EngineLLM, "":
		return s.llmProvider(cfg)
	default:
		return nil, fmt.Errorf("unknown translation engine %q", cfg.Engine)
	}
}

// llmProvider builds the OpenAI-compatible chat provider for the llm
// translation engine from the llm_* and translate_provider settings.
// Classification does not come through here — it has its own cls_* model via
// ClassifierProvider, so switching translation to a REST engine (or off)
// never touches the classifier's endpoint.
func (s *Service) llmProvider(cfg Config) (Provider, error) {
	switch cfg.Provider {
	case ProviderOff:
		return nil, nil
	case ProviderLocal, ProviderCloud, "", "openai-compatible":
		if !cfg.ConfiguredLLM() {
			return nil, nil
		}
		return &OpenAICompatProvider{
			BaseURL: cfg.BaseURL,
			APIKey:  cfg.APIKey,
			ModelID: cfg.Model,
			Client:  engineClient(cfg),
			Local:   cfg.llmIsLocal(),
		}, nil
	default:
		return nil, fmt.Errorf("unknown translation provider %q", cfg.Provider)
	}
}

// engineClient picks the HTTP client for provider requests: the user-configured
// request timeout, falling back to 120s like the previous inline construction.
func engineClient(cfg Config) *http.Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &http.Client{Timeout: timeout}
}

// ClassifierProvider returns the classifier's own chat model built from the
// cls_* settings, independent of the translation engine, translate_enabled
// and translate_provider. It returns (nil, nil) when no classifier model is
// configured, so the classifier silently falls back to keyword-only mode.
func (s *Service) ClassifierProvider() (*OpenAICompatProvider, error) {
	cfg := s.Config()
	if strings.TrimSpace(cfg.ClsBaseURL) == "" || strings.TrimSpace(cfg.ClsModel) == "" {
		return nil, nil
	}
	// A cloud endpoint without a key would only produce 401s.
	if !isLocalURL(cfg.ClsBaseURL) && strings.TrimSpace(cfg.ClsAPIKey) == "" {
		return nil, nil
	}
	return &OpenAICompatProvider{
		BaseURL: cfg.ClsBaseURL,
		APIKey:  cfg.ClsAPIKey,
		ModelID: cfg.ClsModel,
		Client:  engineClient(cfg),
		Local:   isLocalURL(cfg.ClsBaseURL),
	}, nil
}

// reachable probes the active engine's endpoint before any article is marked
// as failed. Without this, starting the app before the local model would burn
// every article's retry budget on connection errors. Each engine gets its own
// cheap liveness probe; the LLM path still uses GET /models.
func (s *Service) reachable(ctx context.Context, cfg Config) error {
	key := cfg.Engine + "|" + engineEndpoint(cfg)
	s.mu.Lock()
	if s.probeOK && s.probeKey == key && time.Since(s.probeAt) < 30*time.Second {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	var err error
	switch cfg.Engine {
	case EngineMTranServer:
		err = probeEndpoint(ctx, s.client, strings.TrimRight(cfg.MTranURL, "/")+"/health")
	case EngineLibreTranslate:
		err = probeEndpoint(ctx, s.client, strings.TrimRight(cfg.LibreURL, "/")+"/languages")
	default:
		_, err = s.ListModels(ctx, cfg)
	}
	s.mu.Lock()
	s.probeKey = key
	s.probeAt = time.Now()
	s.probeOK = err == nil
	s.mu.Unlock()
	if err != nil {
		return fmt.Errorf("translation endpoint %s is not reachable: %w", engineEndpoint(cfg), err)
	}
	return nil
}

// engineEndpoint is the URL shown and probed for the active engine.
func engineEndpoint(cfg Config) string {
	switch cfg.Engine {
	case EngineMTranServer:
		return cfg.MTranURL
	case EngineLibreTranslate:
		return cfg.LibreURL
	default:
		return cfg.BaseURL
	}
}

// probeEndpoint GETs url and fails on anything outside 2xx. Used by the REST
// engines, whose liveness endpoints are plain HTTP GETs without an OpenAI
// /models contract.
func probeEndpoint(ctx context.Context, client *http.Client, url string) error {
	if strings.TrimSpace(url) == "" {
		return fmt.Errorf("endpoint URL is empty")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet(raw))
	}
	return nil
}

// ListModels queries GET {base}/models. Every OpenAI-compatible server
// implements it (Ollama, LM Studio, vLLM, DeepSeek, ...), which makes it a
// reliable liveness probe and lets the settings page offer the installed models.
func (s *Service) ListModels(ctx context.Context, cfg Config) ([]string, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("base URL is empty")
	}
	endpoint := strings.TrimRight(cfg.BaseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	client := s.client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet(raw))
	}
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("unexpected /models response: %s", snippet(raw))
	}
	models := make([]string, 0, len(parsed.Data))
	for _, m := range parsed.Data {
		if strings.TrimSpace(m.ID) != "" {
			models = append(models, m.ID)
		}
	}
	return models, nil
}

// LocalModel is one detected local model server.
type LocalModel struct {
	Name    string   `json:"name"`
	BaseURL string   `json:"baseUrl"`
	Models  []string `json:"models"`
}

// DetectLocalModels probes the well-known local model ports. This is what makes
// the free path usable: the user starts Ollama (or LM Studio) and clicks
// "detect", instead of having to know the port and model name.
func (s *Service) DetectLocalModels(ctx context.Context) []LocalModel {
	var found []LocalModel
	for _, ep := range localEndpoints {
		probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		models, err := s.ListModels(probeCtx, Config{BaseURL: ep.BaseURL})
		cancel()
		if err == nil && len(models) > 0 {
			found = append(found, LocalModel{Name: ep.Name, BaseURL: ep.BaseURL, Models: models})
		}
	}
	return found
}

// translatedToday counts articles completed today, for the daily cost guard.
func (s *Service) translatedToday() (int64, error) {
	var n int64
	start := time.Now().Truncate(24 * time.Hour)
	// Truncate works on UTC; compare against the local midnight instead.
	now := time.Now()
	start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	err := s.db.Model(&models.Article{}).
		Where("trans_status = ? AND translated_at >= ?", models.TransDone, start).
		Count(&n).Error
	return n, err
}

// ---- original-language handling -------------------------------------------

// EnsureOriginal fills the language tag and the original-language columns of a
// freshly fetched article. The original text is authoritative and is never
// overwritten later by a translation.
func EnsureOriginal(a *models.Article) {
	if a.Lang == "" {
		a.Lang = langdetect.Detect(a.Title + " " + a.Summary)
	}
	switch a.Lang {
	case langdetect.ZH:
		if a.TitleZh == "" {
			a.TitleZh = a.Title
		}
		if a.SummaryZh == "" {
			a.SummaryZh = a.Summary
		}
		if a.LeadZh == "" {
			a.LeadZh = a.Lead
		}
	case langdetect.EN:
		if a.TitleEn == "" {
			a.TitleEn = a.Title
		}
		if a.SummaryEn == "" {
			a.SummaryEn = a.Summary
		}
		if a.LeadEn == "" {
			a.LeadEn = a.Lead
		}
	}
	if repository.NeedsTranslation(a) {
		a.TransStatus = models.TransPending
	} else if a.Lang != langdetect.Unknown {
		a.TransStatus = models.TransDone
	}
}

// ---- pipeline --------------------------------------------------------------

// TranslateArticle produces the missing language variant of one article.
// Progress (start / done / skip / error) is reported to the task monitor when
// the caller runs inside a queue job; outside one the reporter is nil.
func (s *Service) TranslateArticle(ctx context.Context, a models.Article, cfg Config, p Provider) error {
	rep := llmqueue.ReporterFrom(ctx)
	rep.ItemStart(a.ID, a.Title)
	target := opposite(a.Lang)
	if target == "" {
		// Original language unknown: record it as skipped rather than failing
		// forever on a row that has nothing to translate from.
		rep.ItemSkip(a.ID, a.Title)
		return s.articles.MarkTranslationAttempt(a.ID, models.TransSkipped, "original language unknown", a.TransAttempts)
	}
	if !repository.NeedsTranslation(&a) {
		rep.ItemSkip(a.ID, a.Title)
		return nil
	}

	// Collect the texts that are missing, together with where they belong.
	type slot struct {
		field string
		text  string
	}
	var slots []slot
	if strings.TrimSpace(a.Title) != "" {
		slots = append(slots, slot{"title", a.Title})
	}
	if cfg.Scope != ScopeTitle && strings.TrimSpace(a.Summary) != "" {
		slots = append(slots, slot{"summary", a.Summary})
	}
	if cfg.Scope == ScopeAll && strings.TrimSpace(a.Lead) != "" {
		slots = append(slots, slot{"lead", a.Lead})
	}
	if len(slots) == 0 {
		rep.ItemSkip(a.ID, a.Title)
		return s.articles.MarkTranslationAttempt(a.ID, models.TransSkipped, "nothing to translate", a.TransAttempts)
	}

	results := make(map[string]string, len(slots))
	var missingTexts []string
	var missingFields []string
	hits := 0

	for _, sl := range slots {
		hash := textHash(sl.text)
		if cached, ok := s.cache.Lookup(hash, target); ok {
			results[sl.field] = cached
			hits++
			continue
		}
		missingTexts = append(missingTexts, sl.text)
		missingFields = append(missingFields, sl.field)
	}

	if len(missingTexts) > 0 {
		translated, err := p.Translate(ctx, missingTexts, target)
		if err != nil {
			attempts := a.TransAttempts + 1
			_ = s.articles.MarkTranslationAttempt(a.ID, models.TransFailed, err.Error(), attempts)
			rep.ItemError(a.ID, a.Title, err)
			return err
		}
		for i, field := range missingFields {
			results[field] = translated[i]
			// Cache by source text + target language so an identical wire story
			// from another source is never translated (or paid for) twice.
			_ = s.cache.Store(&models.Translation{
				SourceHash: textHash(missingTexts[i]),
				TargetLang: target,
				Provider:   p.Name(),
				ModelName:  p.Model(),
				SourceText: missingTexts[i],
				Translated: translated[i],
			})
		}
	}

	u := repository.TranslationUpdate{
		TitleZh:   a.TitleZh,
		TitleEn:   a.TitleEn,
		SummaryZh: a.SummaryZh,
		SummaryEn: a.SummaryEn,
		LeadZh:    a.LeadZh,
		LeadEn:    a.LeadEn,
		Status:    models.TransPending,
		Attempts:  a.TransAttempts,
	}
	assign := func(field, value string) {
		if value == "" {
			return
		}
		switch field {
		case "title":
			if target == langdetect.ZH {
				u.TitleZh = value
			} else {
				u.TitleEn = value
			}
		case "summary":
			if target == langdetect.ZH {
				u.SummaryZh = value
			} else {
				u.SummaryEn = value
			}
		case "lead":
			if target == langdetect.ZH {
				u.LeadZh = value
			} else {
				u.LeadEn = value
			}
		}
	}
	for _, sl := range slots {
		assign(sl.field, results[sl.field])
	}

	// Complete only when the key field of the target language is present; a
	// partial result (title landed, summary did not) stays pending for a retry.
	if (target == langdetect.ZH && u.TitleZh != "") || (target == langdetect.EN && u.TitleEn != "") {
		u.Status = models.TransDone
		u.Error = ""
	} else {
		u.Status = models.TransFailed
		u.Error = "provider returned no usable title translation"
		u.Attempts = a.TransAttempts + 1
	}

	now := time.Now()
	u.TranslatedAt = &now

	s.mu.Lock()
	s.cacheHits += int64(hits)
	s.mu.Unlock()

	if err := s.articles.ApplyTranslation(a.ID, u); err != nil {
		rep.ItemError(a.ID, a.Title, err)
		return err
	}
	if u.Status == models.TransDone {
		rep.ItemDone(a.ID, a.Title, a.Lang+"→"+target)
	} else {
		rep.ItemError(a.ID, a.Title, errors.New(u.Error))
	}
	return nil
}

// RunOnce translates up to limit pending articles. It returns how many were
// completed.
//
// Two guards keep this safe to leave running: the endpoint must answer a
// liveness probe, and the daily limit must not be exhausted. Both fail without
// touching trans_status, so nothing is lost and the work resumes as soon as the
// local model is started (or the limit resets).
func (s *Service) RunOnce(ctx context.Context, limit int) (int, error) {
	cfg := s.Config()
	p, err := s.provider(cfg)
	if err != nil {
		return 0, err
	}
	if p == nil {
		return 0, errors.New(offHint(cfg))
	}
	if err := s.reachable(ctx, cfg); err != nil {
		return 0, err
	}

	// Cost guard. Only meaningful for a paid endpoint, but harmless for a local
	// one, and it stops a runaway loop either way.
	if cfg.DailyLimit > 0 {
		done, err := s.translatedToday()
		if err != nil {
			return 0, err
		}
		if done >= int64(cfg.DailyLimit) {
			return 0, fmt.Errorf("daily translation limit reached (%d articles); raise translate_daily_limit to continue", cfg.DailyLimit)
		}
	}

	if limit <= 0 {
		limit = cfg.BatchSize
	}
	if limit <= 0 {
		limit = 10
	}

	pending, err := s.articles.ListPendingTranslation(limit, maxAttempts)
	if err != nil {
		return 0, err
	}
	if len(pending) == 0 {
		return 0, nil
	}

	s.mu.Lock()
	s.isRunning = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.isRunning = false
		s.mu.Unlock()
	}()

	// ---- Phase 1: plan each article's slots -----------------------------------
	// A slot is one text field that needs translation into a target language.
	type slot struct {
		field string
		text  string
		hash  string
	}
	type articlePlan struct {
		article models.Article
		target  string
		slots   []slot
	}
	plans := make([]articlePlan, 0, len(pending))
	for _, a := range pending {
		target := opposite(a.Lang)
		if target == "" {
			_ = s.articles.MarkTranslationAttempt(a.ID, models.TransSkipped, "original language unknown", a.TransAttempts)
			continue
		}
		if !repository.NeedsTranslation(&a) {
			continue
		}
		var slots []slot
		if strings.TrimSpace(a.Title) != "" {
			slots = append(slots, slot{"title", a.Title, textHash(a.Title)})
		}
		if cfg.Scope != ScopeTitle && strings.TrimSpace(a.Summary) != "" {
			slots = append(slots, slot{"summary", a.Summary, textHash(a.Summary)})
		}
		if cfg.Scope == ScopeAll && strings.TrimSpace(a.Lead) != "" {
			slots = append(slots, slot{"lead", a.Lead, textHash(a.Lead)})
		}
		if len(slots) == 0 {
			_ = s.articles.MarkTranslationAttempt(a.ID, models.TransSkipped, "nothing to translate", a.TransAttempts)
			continue
		}
		plans = append(plans, articlePlan{article: a, target: target, slots: slots})
	}

	if len(plans) == 0 {
		return 0, nil
	}

	// ---- Phase 2: group by target language, deduplicate texts -----------------
	// All articles targeting the same language share one provider call; identical
	// texts are translated only once thanks to the cache.
	type langGroup struct {
		target   string
		articles []articlePlan
		// uniqueHashes preserves insertion order for deterministic provider calls.
		uniqueHashes []string
		hashToText   map[string]string
	}
	groups := make(map[string]*langGroup)
	for _, ap := range plans {
		g := groups[ap.target]
		if g == nil {
			g = &langGroup{target: ap.target, hashToText: make(map[string]string)}
			groups[ap.target] = g
		}
		g.articles = append(g.articles, ap)
		for _, sl := range ap.slots {
			if _, seen := g.hashToText[sl.hash]; !seen {
				g.hashToText[sl.hash] = sl.text
				g.uniqueHashes = append(g.uniqueHashes, sl.hash)
			}
		}
	}

	// ---- Phase 3: one model call per article, up to cfg.Concurrency at once --
	// The queue still runs one job at a time; inside the job RunParallel fans
	// the plans out to llm_max_concurrency in-flight articles so a model that
	// accepts several requests is not forced to serve them one by one.
	// Cancelling the job stops the launch loop and aborts the in-flight calls
	// through the shared context.
	done := 0      // articles that reached TransDone; failures are counted apart
	processed := 0 // articles that reached a terminal state (progress display)
	var firstErr error
	var runErr error  // non-nil when the run was aborted (cancel / shutdown)
	var mu sync.Mutex // guards done, processed and firstErr across parallel items
	rep := llmqueue.ReporterFrom(ctx)
	rep.SetTotal(len(plans))

	process := func(i int) {
		ap := &plans[i]
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		default:
		}
		// A panic inside one article must not take down the job and, with it,
		// every other article of this run.
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("translator: article %d panicked: %v\n%s", ap.article.ID, r, debug.Stack())
					_ = s.articles.MarkTranslationAttempt(ap.article.ID, models.TransFailed,
						fmt.Sprintf("panic while translating: %v", r), ap.article.TransAttempts+1)
					rep.ItemError(ap.article.ID, ap.article.Title, fmt.Errorf("panic: %v", r))
					mu.Lock()
					if firstErr == nil {
						firstErr = fmt.Errorf("translate article %d: %v", ap.article.ID, r)
					}
					processed++
					p := processed
					mu.Unlock()
					rep.SetProgress(p, len(plans))
				}
			}()
			target := ap.target
			rep.ItemStart(ap.article.ID, ap.article.Title)

			u := repository.TranslationUpdate{
				TitleZh:   ap.article.TitleZh,
				TitleEn:   ap.article.TitleEn,
				SummaryZh: ap.article.SummaryZh,
				SummaryEn: ap.article.SummaryEn,
				LeadZh:    ap.article.LeadZh,
				LeadEn:    ap.article.LeadEn,
				Status:    models.TransPending,
				Attempts:  ap.article.TransAttempts,
			}

			var slotErr error // captures the first slot error for this article
			// For each slot: check cache first; if missing, call the provider
			// with a single-item request so one misbehaving text cannot poison
			// the whole batch.
			for _, sl := range ap.slots {
				if cached, ok := s.cache.Lookup(sl.hash, target); ok {
					assignSlot(&u, sl.field, cached, target)
					continue
				}
				results, err := p.Translate(ctx, []string{sl.text}, target)
				if err != nil {
					if ctx.Err() != nil {
						// Cancelled mid-flight: leave this article untouched
						// (no failed attempt burned); RunParallel reports the abort.
						return
					}
					if slotErr == nil {
						slotErr = err
					}
					mu.Lock()
					if firstErr == nil {
						firstErr = fmt.Errorf("translate article %d (%s): %w", ap.article.ID, sl.field, err)
					}
					mu.Unlock()
					log.Printf("translator: article %d slot %s failed: %v", ap.article.ID, sl.field, err)
					continue
				}
				if len(results) == 0 || results[0] == "" {
					log.Printf("translator: article %d slot %s returned empty translation", ap.article.ID, sl.field)
					if slotErr == nil {
						slotErr = fmt.Errorf("empty translation for %s", sl.field)
					}
					continue
				}
				assignSlot(&u, sl.field, results[0], target)
				// Cache the result so an identical wire story is never paid for twice.
				_ = s.cache.Store(&models.Translation{
					SourceHash: sl.hash, TargetLang: target,
					Provider: p.Name(), ModelName: p.Model(),
					SourceText: sl.text, Translated: results[0],
				})
			}

			if (target == langdetect.ZH && u.TitleZh != "") || (target == langdetect.EN && u.TitleEn != "") {
				u.Status = models.TransDone
				u.Error = ""
			} else {
				u.Status = models.TransFailed
				if slotErr == nil {
					slotErr = fmt.Errorf("provider returned no usable title translation")
				}
				u.Error = slotErr.Error()
				u.Attempts = ap.article.TransAttempts + 1
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("translate article %d: %s", ap.article.ID, u.Error)
				}
				mu.Unlock()
			}
			now := time.Now()
			u.TranslatedAt = &now

			if err := s.articles.ApplyTranslation(ap.article.ID, u); err != nil {
				rep.ItemError(ap.article.ID, ap.article.Title, err)
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				processed++
				p := processed
				mu.Unlock()
				rep.SetProgress(p, len(plans))
				log.Printf("translator: article %d write failed: %v", ap.article.ID, err)
				return
			}
			mu.Lock()
			if u.Status == models.TransDone {
				done++
			}
			processed++
			p := processed
			mu.Unlock()
			rep.SetProgress(p, len(plans))
			if u.Status == models.TransDone {
				rep.ItemDone(ap.article.ID, ap.article.Title, ap.article.Lang+"→"+target)
			} else {
				rep.ItemError(ap.article.ID, ap.article.Title, errors.New(u.Error))
			}
		}()
	}

	if err := llmqueue.RunParallel(ctx, cfg.Concurrency, len(plans), process); err != nil {
		runErr = err
	}
	rep.Summary(done)

	s.mu.Lock()
	s.translated += int64(done)
	s.lastRun = time.Now()
	s.lastCount = done
	if firstErr != nil {
		s.lastError = firstErr.Error()
	} else {
		s.lastError = ""
	}
	s.mu.Unlock()

	// Per-article failures stay on their own rows (and in Status().LastError):
	// the run itself only fails when the model could not finish a single
	// article, which is what a broken configuration looks like. A cancelled
	// run reports the cancellation after recording its partial progress.
	if runErr != nil {
		return done, runErr
	}
	if firstErr != nil && done == 0 {
		return 0, firstErr
	}
	return done, nil
}

// Stop marks the service as shut down: a RunOnce caller still running sees
// stopCh closed and returns early. The LLM queue owns job lifecycle now, so
// there is no background worker left to reap — Stop is the shutdown backstop
// for callers that run outside the queue.
func (s *Service) Stop() {
	s.stopOnce.Do(func() { close(s.stopCh) })
}

// Status describes the worker for the settings page.
type Status struct {
	Configured    bool      `json:"configured"`
	Engine        string    `json:"engine"`
	Provider      string    `json:"provider"`
	Model         string    `json:"model"`
	BaseURL       string    `json:"baseUrl"`
	Local         bool      `json:"local"`
	Scope         string    `json:"scope"`
	DailyLimit    int       `json:"dailyLimit"`
	TranslatedDay int64     `json:"translatedToday"`
	Reachable     bool      `json:"reachable"`
	IsRunning     bool      `json:"isRunning"`
	LastRun       time.Time `json:"lastRun"`
	LastError     string    `json:"lastError"`
	LastCount     int       `json:"lastCount"`
}

// Status returns the current worker state.
func (s *Service) Status() Status {
	cfg := s.Config()
	translatedToday, _ := s.translatedToday()

	// Present the active engine as the provider so the settings page shows
	// what is really serving requests; the llm engine keeps its local/cloud
	// preset label.
	provider, endpoint, model := cfg.Provider, cfg.BaseURL, cfg.Model
	switch cfg.Engine {
	case EngineMTranServer:
		provider, endpoint, model = EngineMTranServer, cfg.MTranURL, "mtranserver"
	case EngineLibreTranslate:
		provider, endpoint, model = EngineLibreTranslate, cfg.LibreURL, "libretranslate"
	}

	// A live probe is what tells the user "Ollama is running / is not running",
	// which is the single most useful fact when the free path is chosen.
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	reachable := false
	if cfg.Configured() {
		reachable = s.reachable(ctx, cfg) == nil
	}
	cancel()

	s.mu.Lock()
	defer s.mu.Unlock()
	return Status{
		Configured:    cfg.Configured(),
		Engine:        cfg.Engine,
		Provider:      provider,
		Model:         model,
		BaseURL:       endpoint,
		Local:         cfg.IsLocal(),
		Scope:         cfg.Scope,
		DailyLimit:    cfg.DailyLimit,
		TranslatedDay: translatedToday,
		Reachable:     reachable,
		IsRunning:     s.isRunning,
		LastRun:       s.lastRun,
		LastError:     s.lastError,
		LastCount:     s.lastCount,
	}
}

// Stats reports article-level and cache-level translation progress.
func (s *Service) Stats() (models.TranslationStats, error) {
	st, err := s.articles.TranslationStats()
	if err != nil {
		return st, err
	}
	n, err := s.cache.Count()
	if err != nil {
		return st, err
	}
	st.CacheHit = n
	return st, nil
}

// TestConnection performs one real translation so the settings page can prove
// the endpoint works before the user relies on it.
func (s *Service) TestConnection(ctx context.Context) (string, error) {
	cfg := s.Config()
	p, err := s.provider(cfg)
	if err != nil {
		return "", err
	}
	if p == nil {
		return "", errors.New(offHint(cfg))
	}
	if err := s.reachable(ctx, cfg); err != nil {
		return "", err
	}
	// The REST engines prove both directions of the en/zh pair in one click,
	// because that is exactly what the pipeline does with them (en articles to
	// Chinese, Chinese articles to English).
	if cfg.Engine == EngineMTranServer || cfg.Engine == EngineLibreTranslate {
		enSample := "Central bank holds interest rates steady at 4.5%"
		zhSample := "央行将利率维持在 4.5% 不变"
		toZH, errZH := p.Translate(ctx, []string{enSample}, langdetect.ZH)
		toEN, errEN := p.Translate(ctx, []string{zhSample}, langdetect.EN)
		if errZH != nil {
			return "", fmt.Errorf("en→zh test failed: %w", errZH)
		}
		msg := fmt.Sprintf("en→zh: %s", firstTranslation(toZH))
		if errEN != nil {
			msg += fmt.Sprintf(" · zh→en failed: %v", errEN)
		} else {
			msg += fmt.Sprintf(" · zh→en: %s", firstTranslation(toEN))
		}
		return msg, nil
	}
	// Translate a sample in the direction the pending articles actually need, so
	// the test exercises the same code path as the worker.
	sample := "Central bank holds interest rates steady at 4.5%"
	target := langdetect.ZH
	if cfg.IsLocal() {
		sample = "央行将利率维持在 4.5% 不变"
		target = langdetect.EN
	}
	out, err := p.Translate(ctx, []string{sample}, target)
	if err != nil {
		return "", err
	}
	if len(out) == 0 {
		return "", fmt.Errorf("provider returned no translation")
	}
	return fmt.Sprintf("%s  ->  %s", sample, out[0]), nil
}

// TranslateArticleByID translates a single article on demand, so a user can
// force one translation without waiting for the worker.
func (s *Service) TranslateArticleByID(ctx context.Context, id uint) error {
	cfg := s.Config()
	p, err := s.provider(cfg)
	if err != nil {
		return err
	}
	if p == nil {
		return errors.New(offHint(cfg))
	}
	if err := s.reachable(ctx, cfg); err != nil {
		return err
	}
	a, err := s.articles.Get(id)
	if err != nil {
		return err
	}
	// Re-derive the original side first: rows created before the bilingual
	// columns existed have no language tag yet.
	if a.Lang == "" {
		EnsureOriginal(a)
		if err := s.db.Model(&models.Article{}).Where("id = ?", a.ID).Updates(map[string]interface{}{
			"lang": a.Lang, "title_zh": a.TitleZh, "title_en": a.TitleEn,
			"summary_zh": a.SummaryZh, "summary_en": a.SummaryEn,
			"lead_zh": a.LeadZh, "lead_en": a.LeadEn, "trans_status": a.TransStatus,
		}).Error; err != nil {
			return err
		}
	}
	// An explicit user request overrides the daily cost guard and the retry cap.
	if a.TransAttempts > 0 {
		a.TransAttempts = 0
	}
	return s.TranslateArticle(ctx, *a, cfg, p)
}

// ResetFailed clears the attempt counter of failed articles so a fixed
// configuration can retry rows that already exhausted their attempts.
func (s *Service) ResetFailed() (int64, error) {
	res := s.db.Model(&models.Article{}).
		Where("trans_status = ?", models.TransFailed).
		Updates(map[string]interface{}{"trans_attempts": 0, "trans_status": models.TransPending, "trans_error": ""})
	return res.RowsAffected, res.Error
}

// assignSlot writes a translated string into the correct field of u based on
// the slot's field name and the target language.
func assignSlot(u *repository.TranslationUpdate, field, text, target string) {
	if text == "" {
		return
	}
	switch field {
	case "title":
		if target == langdetect.ZH {
			u.TitleZh = text
		} else {
			u.TitleEn = text
		}
	case "summary":
		if target == langdetect.ZH {
			u.SummaryZh = text
		} else {
			u.SummaryEn = text
		}
	case "lead":
		if target == langdetect.ZH {
			u.LeadZh = text
		} else {
			u.LeadEn = text
		}
	}
}

// offHint names the active engine in the "translation is off" error so the
// user is pointed at the right part of Settings.
func offHint(cfg Config) string {
	switch cfg.Engine {
	case EngineMTranServer:
		return "translation is off: enable it in Settings (MTranServer engine)"
	case EngineLibreTranslate:
		return "translation is off: enable it in Settings (LibreTranslate engine)"
	}
	if cfg.IsLocal() {
		return "translation is off: enable it in Settings (free local model, no API key needed)"
	}
	return "translation is off: enable it and set an API key in Settings"
}

// firstTranslation pulls the first usable string out of a provider reply for
// the test-connection message.
func firstTranslation(out []string) string {
	if len(out) == 0 || strings.TrimSpace(out[0]) == "" {
		return "(empty)"
	}
	return out[0]
}

func opposite(lang string) string {
	switch lang {
	case langdetect.ZH:
		return langdetect.EN
	case langdetect.EN:
		return langdetect.ZH
	default:
		return ""
	}
}

func textHash(s string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(s)))
	return hex.EncodeToString(sum[:])
}
