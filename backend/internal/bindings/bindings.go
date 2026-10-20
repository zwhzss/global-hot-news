package bindings

import (
	"context"
	"fmt"
	"strings"
	"time"

	"ai-informations/internal/models"
	"ai-informations/internal/service/classifier"
	"ai-informations/internal/service/collector"
	"ai-informations/internal/service/datasource"
	"ai-informations/internal/service/follow"
	"ai-informations/internal/service/hotrank"
	"ai-informations/internal/service/llmqueue"
	"ai-informations/internal/service/news"
	"ai-informations/internal/service/recommend"
	"ai-informations/internal/service/setting"
	"ai-informations/internal/service/translator"
	"ai-informations/internal/service/tts"
)

// Bindings exposes all backend services to the Angular frontend via Wails.
type Bindings struct {
	dsService         *datasource.Service
	collectorService  *collector.Service
	hotrankService    *hotrank.Service
	followService     *follow.Service
	recommendService  *recommend.Service
	newsService       *news.Service
	settingService    *setting.Service
	translatorService *translator.Service
	classifierService *classifier.Service
	ttsService        *tts.Service
	// queue serialises every task that talks to the language model: one job
	// at a time, FIFO, cancellable by type or globally.
	queue *llmqueue.Queue
}

func NewBindings(dsService *datasource.Service, collectorService *collector.Service, hotrankService *hotrank.Service, followService *follow.Service, recommendService *recommend.Service, newsService *news.Service, settingService *setting.Service, translatorService *translator.Service, classifierService *classifier.Service, ttsService *tts.Service, queue *llmqueue.Queue) *Bindings {
	return &Bindings{
		dsService:         dsService,
		collectorService:  collectorService,
		hotrankService:    hotrankService,
		followService:     followService,
		recommendService:  recommendService,
		newsService:       newsService,
		settingService:    settingService,
		translatorService: translatorService,
		classifierService: classifierService,
		ttsService:        ttsService,
		queue:             queue,
	}
}

// ===== Data Source APIs =====

func (b *Bindings) DataSourceList() ([]models.Source, error) {
	return b.dsService.List()
}

func (b *Bindings) DataSourceCreate(name, sourceType, url, cronExpr string, interval int) (*models.Source, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("source name is required")
	}
	if strings.TrimSpace(sourceType) == "" {
		return nil, fmt.Errorf("source type is required")
	}
	if strings.TrimSpace(url) == "" {
		return nil, fmt.Errorf("source URL is required")
	}
	src := &models.Source{
		Name:            name,
		Type:            sourceType,
		URL:             url,
		CronExpression:  cronExpr,
		RequestInterval: interval,
	}
	if err := b.dsService.Create(src); err != nil {
		return nil, err
	}
	return src, nil
}

func (b *Bindings) DataSourceUpdate(id uint, name, sourceType, url string, interval int) error {
	src, err := b.dsService.Get(id)
	if err != nil {
		return err
	}
	src.Name = name
	src.Type = sourceType
	src.URL = url
	src.RequestInterval = interval
	return b.dsService.Update(src)
}

func (b *Bindings) DataSourceSetPublisher(id uint, publisher string) error {
	return b.dsService.SetPublisher(id, publisher)
}

func (b *Bindings) DataSourceDelete(id uint) error {
	return b.dsService.Delete(id)
}

func (b *Bindings) DataSourceToggleActive(id uint, active bool) error {
	return b.dsService.ToggleActive(id, active)
}

func (b *Bindings) DataSourceTest(id uint) (*models.TestResult, error) {
	return b.dsService.TestConnectivity(id)
}

func (b *Bindings) DataSourceFetchNow(id uint) (int, error) {
	ids, err := b.dsService.FetchAndProcess(id)
	// A manual fetch counts as its own round: the collector's cycle hook never
	// sees it, so hand the new article IDs to TTS here or "Fetch now" would
	// silently produce no narration (and no auto-play).
	if err == nil && len(ids) > 0 {
		b.ttsService.RoundComplete(ids)
	}
	return len(ids), err
}

// ===== News APIs =====

// NewsList is the original five-argument binding, kept so nothing that already
// calls it breaks. New code should use NewsSearch, which takes every filter.
func (b *Bindings) NewsList(page, pageSize int, categorySlug, sourceID string, followOnly bool) (models.ArticleListResult, error) {
	q := models.NewsQuery{
		Page:         page,
		PageSize:     pageSize,
		CategorySlug: categorySlug,
		FollowOnly:   followOnly,
		DatePreset:   models.DatePresetAll,
	}
	if sourceID != "" {
		id := uint(0)
		fmt.Sscanf(sourceID, "%d", &id)
		q.SourceID = id
	}
	return b.NewsSearch(q)
}

// NewsSearch is the single article-list entry point for every board: recommend,
// follows, hot rank and the topic boards all pass the same NewsQuery, so the
// filter bar (date range, title keyword, category, source, sort) behaves
// identically everywhere.
func (b *Bindings) NewsSearch(q models.NewsQuery) (models.ArticleListResult, error) {
	articles, total, err := b.newsService.List(q)
	return models.ArticleListResult{Articles: articles, Total: total}, err
}

// NewsDates returns the local dates that actually contain articles, newest
// first, so the UI can offer dates that are guaranteed to have results.
func (b *Bindings) NewsDates(limit int) ([]string, error) {
	return b.newsService.AvailableDates(limit)
}

// NewsReclassify re-runs keyword classification. With rebuild=true it discards
// every existing link first, which is what you want after editing the category
// tree or its keywords; otherwise it queues a job that fills in articles that
// have none (keyword pass first, LLM fallback), served one request at a time
// through the LLM queue. Reports whether a job was queued.
func (b *Bindings) NewsReclassify(rebuild bool, limit int) (bool, error) {
	if rebuild {
		n, err := b.classifierService.ReclassifyAll(limit)
		return n > 0, err
	}
	_, ok, err := b.queue.Enqueue(llmqueue.KindClassify, "fill-unclassified", func(ctx context.Context) error {
		_, err := b.classifierService.ClassifyUncategorized(ctx, limit)
		return err
	})
	return ok, err
}

// ClassifierAudit reports per-category statistics and articles that may be
// misclassified (matched on a single weak keyword).
func (b *Bindings) ClassifierAudit() (classifier.AuditResult, error) {
	return b.classifierService.AuditCategories()
}

// ClassifierCounts returns the cheap counters shown next to the audit buttons
// (total / unclassified / which background jobs are running).
func (b *Bindings) ClassifierCounts() (classifier.AuditCounts, error) {
	return b.classifierService.Counts(), nil
}

// ClassifierAuditUnclassified queues the "classify the unclassified" pass. It
// reports whether a job was queued (false = one with the same name is already
// queued or running), so the UI never blocks on the model.
func (b *Bindings) ClassifierAuditUnclassified(limit int) (bool, error) {
	if !b.classifierService.LLMConfigured() {
		return false, fmt.Errorf("no LLM provider configured")
	}
	_, ok, err := b.queue.Enqueue(llmqueue.KindClassify, "audit-unclassified", func(ctx context.Context) error {
		_, err := b.classifierService.AuditClassifyUnclassified(ctx, limit)
		return err
	})
	return ok, err
}

// ClassifierAuditReclassify queues re-classifying suspect articles. It reports
// whether a job was queued (false = already queued or running).
func (b *Bindings) ClassifierAuditReclassify(limit int) (bool, error) {
	if !b.classifierService.LLMConfigured() {
		return false, fmt.Errorf("no LLM provider configured")
	}
	_, ok, err := b.queue.Enqueue(llmqueue.KindClassify, "audit-reclassify", func(ctx context.Context) error {
		_, err := b.classifierService.AuditReclassifySuspects(ctx, limit)
		return err
	})
	return ok, err
}

// ClassifierCancel stops queued and running classification jobs ("取消分类")
// and reports how many jobs were affected.
func (b *Bindings) ClassifierCancel() (int, error) {
	return b.queue.CancelKind(llmqueue.KindClassify), nil
}

// ClassifierTest sends one tiny chat message to prove the classifier's own
// cls_* endpoint works; the settings page saves first so it tests what is
// actually stored.
func (b *Bindings) ClassifierTest() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return b.classifierService.TestConnection(ctx)
}

// LLMCancelAll stops every queued and running model task — the global stop.
func (b *Bindings) LLMCancelAll() (int, error) {
	return b.queue.CancelAll(), nil
}

// LLMQueueStatus reports queue depth and the currently running job so the
// settings page can show "queued N · current: translate" and flip its action
// buttons into cancel buttons.
func (b *Bindings) LLMQueueStatus() (llmqueue.Status, error) {
	return b.queue.Status(), nil
}

// TaskLogRecent returns up to limit newest task-monitor events in ascending
// id order (limit <= 0: everything buffered, capped at 500).
func (b *Bindings) TaskLogRecent(limit int) ([]llmqueue.Event, error) {
	return b.queue.Events(limit), nil
}

// TaskLogClear empties the task-monitor feed.
func (b *Bindings) TaskLogClear() error {
	b.queue.ClearEvents()
	return nil
}

func (b *Bindings) NewsGet(id uint) (*models.Article, error) {
	return b.newsService.GetByID(id)
}

func (b *Bindings) NewsListCategories() ([]models.Category, error) {
	return b.newsService.ListCategories()
}

// ===== Hot Rank APIs =====

func (b *Bindings) HotRankGetTop(limit int) ([]models.RankWithArticle, error) {
	return b.hotrankService.GetTopWithArticles(limit)
}

// HotRankSearch returns hot-ranked articles that honour the same filters as
// every other board, together with the total number of matches.
func (b *Bindings) HotRankSearch(q models.NewsQuery) (models.RankListResult, error) {
	ranks, total, err := b.hotrankService.Search(q)
	return models.RankListResult{Ranks: ranks, Total: total}, err
}

func (b *Bindings) HotRankRecalculate() error {
	return b.hotrankService.CalculateAndSave()
}

// ===== Follow APIs =====

func (b *Bindings) FollowArticle(articleID uint) error {
	return b.followService.FollowArticle(articleID)
}

func (b *Bindings) UnfollowArticle(articleID uint) error {
	return b.followService.UnfollowArticle(articleID)
}

func (b *Bindings) FollowSource(sourceID uint) error {
	return b.followService.FollowSource(sourceID)
}

func (b *Bindings) UnfollowSource(sourceID uint) error {
	return b.followService.UnfollowSource(sourceID)
}

func (b *Bindings) IsArticleFollowed(articleID uint) (bool, error) {
	return b.followService.IsArticleFollowed(articleID)
}

func (b *Bindings) IsSourceFollowed(sourceID uint) (bool, error) {
	return b.followService.IsSourceFollowed(sourceID)
}

func (b *Bindings) GetFollowedArticles(page, pageSize int) (models.ArticleListResult, error) {
	articles, total, err := b.followService.GetFollowedArticles(page, pageSize)
	return models.ArticleListResult{Articles: articles, Total: total}, err
}

// FollowedSearch returns followed articles with the full filter request applied.
func (b *Bindings) FollowedSearch(q models.NewsQuery) (models.ArticleListResult, error) {
	articles, total, err := b.followService.SearchFollowed(q)
	return models.ArticleListResult{Articles: articles, Total: total}, err
}

func (b *Bindings) GetFollowedSources() ([]models.Source, error) {
	return b.followService.GetFollowedSources()
}

func (b *Bindings) RecordBehavior(articleID, sourceID uint, stayMs int, clickedURL bool) error {
	return b.followService.RecordBehavior(articleID, sourceID, stayMs, clickedURL)
}

// ===== Recommend APIs =====

func (b *Bindings) RecommendList(page, pageSize int) (models.ArticleListResult, error) {
	articles, total, err := b.recommendService.GetRecommendations(page, pageSize)
	return models.ArticleListResult{Articles: articles, Total: total}, err
}

// RecommendSearch returns recommendations with the full filter request applied.
func (b *Bindings) RecommendSearch(q models.NewsQuery) (models.ArticleListResult, error) {
	articles, total, err := b.recommendService.Search(q)
	return models.ArticleListResult{Articles: articles, Total: total}, err
}

func (b *Bindings) RecommendGetProfile() (string, error) {
	return b.recommendService.GetProfile()
}

func (b *Bindings) RecommendResetProfile() error {
	return b.recommendService.ResetProfile()
}

// ===== Collector APIs =====

func (b *Bindings) CollectorRunCycle() (int, error) {
	// Trigger one collection cycle
	b.collectorService.RunCycle()
	return 0, nil
}

// ===== TTS APIs (narration audio for newly fetched articles) =====

// TTSStatus returns the current synthesis round's snapshot for the frontend
// poller: synthesizing -> ready with done/total counters.
func (b *Bindings) TTSStatus() (tts.Status, error) {
	return b.ttsService.Status(), nil
}

// TTSPlaylist returns the articles of the round that finished synthesising,
// in fetch order. Empty until a round reaches "ready".
func (b *Bindings) TTSPlaylist() ([]tts.PlaylistItem, error) {
	return b.ttsService.Playlist(), nil
}

// TTSTest synthesises a fixed sentence so the settings page can verify python,
// edge-tts and the Microsoft endpoint in one click. Returns the audio URL.
func (b *Bindings) TTSTest() (string, error) {
	b.ttsService.InvalidatePythonCache()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	return b.ttsService.Test(ctx)
}

// TTSArticleStatusResult holds the per-article TTS status returned to the frontend.
type TTSArticleStatusResult struct {
	HasAudio     bool `json:"hasAudio"`
	Synthesizing bool `json:"synthesizing"`
}

// TTSArticleStatus reports whether audio for a specific article exists and
// whether it is currently being synthesised as part of an in-flight round.
func (b *Bindings) TTSArticleStatus(articleID uint) (*TTSArticleStatusResult, error) {
	hasAudio, synthesizing, err := b.ttsService.ArticleStatus(articleID)
	if err != nil {
		return nil, err
	}
	return &TTSArticleStatusResult{HasAudio: hasAudio, Synthesizing: synthesizing}, nil
}

// TTSSynthesizeArticle triggers on-demand synthesis for a single article.
// Safe to call concurrently; existing audio is reused.
func (b *Bindings) TTSSynthesizeArticle(articleID uint) error {
	return b.ttsService.SynthesizeArticle(articleID)
}

// ===== Setting APIs =====

func (b *Bindings) SettingGetAll() (map[string]string, error) {
	return b.settingService.GetAll()
}

func (b *Bindings) SettingSet(key, value string) error {
	return b.settingService.Set(key, value, "")
}

// ===== Translation APIs (bilingual article content) =====

// TranslationStatus reports the live state of the translator: which provider is
// configured, whether the endpoint answers, and how much was translated today.
func (b *Bindings) TranslationStatus() (translator.Status, error) {
	return b.translatorService.Status(), nil
}

// TranslationStats reports how many articles already carry both languages.
func (b *Bindings) TranslationStats() (models.TranslationStats, error) {
	return b.translatorService.Stats()
}

// TranslationRunNow queues a translation pass over up to limit pending
// articles. It reports whether a job was queued (false = a pass is already
// queued or running); the model is reached through the single LLM queue, one
// request at a time.
func (b *Bindings) TranslationRunNow(limit int) (bool, error) {
	_, ok, err := b.queue.Enqueue(llmqueue.KindTranslate, "batch", func(ctx context.Context) error {
		runCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
		defer cancel()
		_, err := b.translatorService.RunOnce(runCtx, limit)
		return err
	})
	return ok, err
}

// TranslationCancel stops queued and running translation jobs ("取消翻译")
// and reports how many jobs were affected.
func (b *Bindings) TranslationCancel() (int, error) {
	return b.queue.CancelKind(llmqueue.KindTranslate), nil
}

// TranslationTest performs one real translation to prove the configuration.
func (b *Bindings) TranslationTest() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return b.translatorService.TestConnection(ctx)
}

// TranslationDetectLocal probes the well-known local model ports so the free
// path can be configured without knowing any port or model name.
func (b *Bindings) TranslationDetectLocal() ([]translator.LocalModel, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return b.translatorService.DetectLocalModels(ctx), nil
}

// TranslationListModels lists the models the configured endpoint offers.
func (b *Bindings) TranslationListModels() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return b.translatorService.ListModels(ctx, b.translatorService.Config())
}

// TranslationTranslateArticle queues a single-article translation and waits for
// it to run — the queue serves one request at a time, so the card spinner
// covers the queueing wait too — then returns the updated row. A cancel from
// the settings page aborts the wait with an error.
func (b *Bindings) TranslationTranslateArticle(id uint) (*models.Article, error) {
	job, ok, err := b.queue.Enqueue(llmqueue.KindTranslate, fmt.Sprintf("article:%d", id), func(ctx context.Context) error {
		runCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
		return b.translatorService.TranslateArticleByID(runCtx, id)
	})
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("translation for article %d is already queued", id)
	}
	if err := job.Wait(); err != nil {
		return nil, err
	}
	return b.newsService.GetByID(id)
}

// TranslationResetFailed puts failed articles back in the queue after the user
// fixes the configuration.
func (b *Bindings) TranslationResetFailed() (int64, error) {
	return b.translatorService.ResetFailed()
}
