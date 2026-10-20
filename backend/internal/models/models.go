package models

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

// Source represents a news data source configuration.
type Source struct {
	gorm.Model
	Name             string     `gorm:"size:255;not null" json:"name"`
	Type             string     `gorm:"size:50;not null" json:"type"` // rss, html, api
	URL              string     `gorm:"size:1024;not null" json:"url"`
	IsActive         bool       `gorm:"default:true" json:"isActive"`
	CronExpression   string     `gorm:"size:50" json:"cronExpression"`
	RequestInterval  int        `gorm:"default:60" json:"requestInterval"` // seconds between requests
	BindFilterRuleID *uint      `json:"bindFilterRuleId"`
	Publisher        string     `gorm:"size:255;default:''" json:"publisher"` // original publisher name, e.g. "人民网", "央视新闻"
	Status           string     `gorm:"size:50;default:unknown" json:"status"` // active, error, unknown, rate_limited, robots_blocked
	LastError        string     `gorm:"size:512" json:"lastError"`
	LastFetchedAt    *time.Time `json:"lastFetchedAt"`
	SampleData       string     `gorm:"type:text" json:"sampleData"`
	FetchCount       int        `gorm:"default:0" json:"fetchCount"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
}

// SourceFilterRule represents a content filter rule.
type SourceFilterRule struct {
	gorm.Model
	Name      string    `gorm:"size:255;not null" json:"name"`
	RuleType  string    `gorm:"size:50" json:"ruleType"`   // global, source_private
	Keywords  string    `gorm:"type:text" json:"keywords"` // comma-separated keywords
	SourceID  *uint     `json:"sourceId"`                  // nil means global
	IsActive  bool      `gorm:"default:true" json:"isActive"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Article represents a fetched news item (metadata only, no full text).
type Article struct {
	gorm.Model
	SourceID    uint      `gorm:"index" json:"sourceId"`
	Title       string    `gorm:"size:1024;not null" json:"title"`
	Summary     string    `gorm:"type:text" json:"summary"`
	Lead        string    `gorm:"type:text" json:"lead"`
	OriginURL   string    `gorm:"size:2048;index" json:"originUrl"`
	Publisher   string    `gorm:"size:255;default:''" json:"publisher"` // original publisher, e.g. "人民网", "央视新闻"
	PublishTime time.Time `json:"publishTime"`
	FetchedAt   time.Time `json:"fetchedAt"`
	Fingerprint string    `gorm:"size:64;index" json:"fingerprint"`
	ExpireAt    time.Time `json:"expireAt"`
	Locations   string    `gorm:"type:text" json:"locations"` // JSON array
	Persons     string    `gorm:"type:text" json:"persons"`   // JSON array
	Events      string    `gorm:"type:text" json:"events"`    // JSON array
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`

	// ---- Bilingual content -------------------------------------------------
	// Title/Summary/Lead always hold the ORIGINAL text exactly as fetched.
	// The Zh/En columns hold both language variants so the UI can switch the
	// displayed language with a tab. Lang records which side of that pair is
	// the authoritative original (and therefore must never be overwritten by a
	// translation); the other side is filled by the translator service.
	Lang          string     `gorm:"size:8;index" json:"lang"` // zh | en | "" (unknown yet)
	TitleZh       string     `gorm:"size:1024" json:"titleZh"`
	TitleEn       string     `gorm:"size:1024" json:"titleEn"`
	SummaryZh     string     `gorm:"type:text" json:"summaryZh"`
	SummaryEn     string     `gorm:"type:text" json:"summaryEn"`
	LeadZh        string     `gorm:"type:text" json:"leadZh"`
	LeadEn        string     `gorm:"type:text" json:"leadEn"`
	TransStatus   string     `gorm:"size:16;index;default:pending" json:"transStatus"` // pending | done | failed | skipped
	TransAttempts int        `gorm:"default:0" json:"transAttempts"`
	TransError    string     `gorm:"size:512" json:"transError"`
	TranslatedAt  *time.Time `json:"translatedAt"`

	// ---- Classification provenance -------------------------------------------
	// ClassifiedBy records how the article was placed into categories:
	//   ""       = not classified yet
	//   "keyword"= placed by the local keyword rules
	//   "llm"    = placed by the large language model
	// The audit panel uses this to skip articles the model already handled.
	ClassifiedBy string `gorm:"size:16;index;default:''" json:"classifiedBy"`

	// Categories is the many-to-many relation backed by the article_categories
	// join table. It must be declared here: repository queries call
	// Preload("Categories"), which fails with "unsupported relations for
	// schema Article" when this field is absent.
	Categories []Category `gorm:"many2many:article_categories" json:"categories,omitempty"`
}

// Category represents a news category in a tree structure.
type Category struct {
	gorm.Model
	Name          string    `gorm:"size:255;not null" json:"name"`
	NameEn        string    `gorm:"size:255" json:"nameEn"`
	Slug          string    `gorm:"size:100;uniqueIndex" json:"slug"`
	ParentID      *uint     `gorm:"index" json:"parentId"`
	SortOrder     int       `gorm:"default:0" json:"sortOrder"`
	Description   string    `gorm:"size:512" json:"description"`
	DescriptionEn string    `gorm:"size:512" json:"descriptionEn"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// ArticleCategory is the join table for articles and categories.
type ArticleCategory struct {
	gorm.Model
	ArticleID  uint `gorm:"uniqueIndex:idx_article_cat"`
	CategoryID uint `gorm:"uniqueIndex:idx_article_cat"`
}

// Tag represents a news tag.
type Tag struct {
	gorm.Model
	Name    string    `gorm:"size:100;uniqueIndex" json:"name"`
	Created time.Time `json:"created"`
}

// ArticleTag is the join table for articles and tags.
type ArticleTag struct {
	gorm.Model
	ArticleID uint `gorm:"uniqueIndex:idx_article_tag"`
	TagID     uint `gorm:"uniqueIndex:idx_article_tag"`
}

// Follow represents user follows (articles or sources).
type Follow struct {
	gorm.Model
	FollowType string    `gorm:"size:20;not null" json:"followType"` // article, source
	TargetID   uint      `gorm:"index" json:"targetId"`
	CreatedAt  time.Time `json:"createdAt"`
}

// UserBehavior records user browsing behavior for recommendation.
type UserBehavior struct {
	gorm.Model
	ArticleID  uint      `gorm:"index" json:"articleId"`
	SourceID   uint      `json:"sourceId"`
	StayMs     int       `json:"stayMs"` // milliseconds spent viewing
	ClickedURL bool      `gorm:"default:false" json:"clickedUrl"`
	CreatedAt  time.Time `json:"createdAt"`
}

// UserProfile stores user preference weights for recommendation.
type UserProfile struct {
	gorm.Model
	CategoryWeights string    `gorm:"type:text" json:"categoryWeights"` // JSON object: category -> weight
	SourceWeights   string    `gorm:"type:text" json:"sourceWeights"`   // JSON object: source_id -> weight
	UpdatedAt       time.Time `json:"updatedAt"`
}

// HotRank represents a hot ranking snapshot.
type HotRank struct {
	gorm.Model
	ArticleID uint      `gorm:"index" json:"articleId"`
	Rank      int       `json:"rank"`
	Score     float64   `json:"score"`
	SourceID  uint      `json:"sourceId"`
	FetchedAt time.Time `json:"fetchedAt"`
}

// FetchLog records collection task status (no content stored).
type FetchLog struct {
	gorm.Model
	SourceID   uint      `gorm:"index" json:"sourceId"`
	Status     string    `gorm:"size:50" json:"status"` // success, error, skipped
	Message    string    `gorm:"size:512" json:"message"`
	ItemCount  int       `json:"itemCount"`
	DurationMs int       `json:"durationMs"`
	CreatedAt  time.Time `json:"createdAt"`
}

// FilterLog records filtered/discarded content (hash only, no content).
type FilterLog struct {
	gorm.Model
	SourceID    uint      `gorm:"index" json:"sourceId"`
	RuleID      uint      `json:"ruleId"`
	ReasonCode  string    `gorm:"size:50" json:"reasonCode"`
	ContentHash string    `gorm:"size:64;index" json:"contentHash"`
	CreatedAt   time.Time `json:"createdAt"`
}

// SystemSetting stores global system configuration.
type SystemSetting struct {
	gorm.Model
	Key         string `gorm:"size:100;uniqueIndex" json:"key"`
	Value       string `gorm:"size:2048" json:"value"`
	Description string `gorm:"size:255" json:"description"`
}

// RawNewsItem is the intermediate structure for fetched news before filtering.
type RawNewsItem struct {
	Title       string
	Summary     string
	Lead        string
	PublishTime time.Time
	OriginURL   string
	Locations   []string
	Persons     []string
	Events      []string
	Publisher   string
}

// TestResult holds the result of a data source connectivity test.
type TestResult struct {
	Available bool          `json:"available"`
	Message   string        `json:"message"`
	Samples   []RawNewsItem `json:"samples,omitempty"`
}

// RankWithArticle holds a hot rank entry with its article.
type RankWithArticle struct {
	HotRank HotRank
	Article Article
}

// ArticleListResult wraps articles with their total count for Wails bindings.
type ArticleListResult struct {
	Articles []Article `json:"articles"`
	Total    int64     `json:"total"`
}

// RankListResult wraps a filtered hot-rank page with its total count.
type RankListResult struct {
	Ranks []RankWithArticle `json:"ranks"`
	Total int64             `json:"total"`
}

// ---- Translation -----------------------------------------------------------

// Translation status values stored on Article.TransStatus.
const (
	TransPending = "pending" // bilingual content incomplete, waiting for the translator
	TransDone    = "done"    // both language variants are present
	TransFailed  = "failed"  // the provider returned an error (see TransError)
	TransSkipped = "skipped" // nothing to translate (unknown source language)
)

// Translation is the persistent translation cache. The same headline+summary
// (for example an identical wire story picked up by two sources) must never be
// paid for twice, so every provider call is keyed by
// (sha256(sourceText), targetLang) and stored here.
type Translation struct {
	gorm.Model
	SourceHash string `gorm:"size:64;uniqueIndex:idx_translation_cache" json:"sourceHash"`
	TargetLang string `gorm:"size:8;uniqueIndex:idx_translation_cache" json:"targetLang"`
	Provider   string `gorm:"size:50" json:"provider"`
	// ModelName, not Model: the embedded gorm.Model already claims that name.
	ModelName  string `gorm:"size:100" json:"modelName"`
	SourceText string `gorm:"type:text" json:"sourceText"`
	Translated string `gorm:"type:text" json:"translated"`
}

// ---- Query ----------------------------------------------------------------

// Date filter presets accepted in NewsQuery.DatePreset. The backend resolves
// them against its own clock so every page agrees on what "today" means.
const (
	DatePresetToday  = "today"
	DatePreset7Days  = "7d"
	DatePreset30Days = "30d"
	DatePresetAll    = "all"
	DatePresetCustom = "custom"
)

// Sort modes accepted in NewsQuery.SortBy.
const (
	SortByTime = "time" // newest first (default)
	SortByHot  = "hot"  // hot-rank score first, newest as the tie-breaker
)

// NewsQuery is the single filter/sort/pagination request used by every article
// list in the UI (recommend, follows, hot rank and the five topic boards), so
// the filter bar behaves identically everywhere. It is passed to Wails as a
// struct, which keeps the binding signatures stable when filters are added.
type NewsQuery struct {
	Page         int    `json:"page"`
	PageSize     int    `json:"pageSize"`
	CategorySlug string `json:"categorySlug"`
	// CategoryPrefix selects a whole module by slug prefix ("finance-", "tech-"),
	// which is what a topic board's "All" chip needs.
	CategoryPrefix string `json:"categoryPrefix"`
	SourceID       uint   `json:"sourceId"`
	FollowOnly     bool   `json:"followOnly"`
	// TitleKeyword is a fuzzy (contains) match against the title in ANY
	// language, so searching works no matter which variant the user reads.
	TitleKeyword string `json:"titleKeyword"`
	// DateFrom/DateTo are inclusive local-time dates formatted YYYY-MM-DD.
	// They are only used when DatePreset is "custom".
	DateFrom string `json:"dateFrom"`
	DateTo   string `json:"dateTo"`
	// DatePreset is one of today|7d|30d|all|custom ("" behaves like "all").
	DatePreset string `json:"datePreset"`
	// SortBy is one of time|hot ("" behaves like "time").
	SortBy string `json:"sortBy"`
}

// Normalize fills in sane defaults and resolves the date preset into a concrete
// inclusive local-date range. The zero values are deliberately "show
// everything, newest first": callers that want today-only must ask for it.
func (q *NewsQuery) Normalize() {
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.PageSize <= 0 {
		q.PageSize = 20
	}
	if q.PageSize > 200 {
		q.PageSize = 200
	}
	q.CategorySlug = strings.TrimSpace(q.CategorySlug)
	q.CategoryPrefix = strings.TrimSpace(q.CategoryPrefix)
	q.TitleKeyword = strings.TrimSpace(q.TitleKeyword)
	if q.SortBy != SortByHot {
		q.SortBy = SortByTime
	}

	today := time.Now()
	switch q.DatePreset {
	case DatePresetToday:
		q.DateFrom = today.Format("2006-01-02")
		q.DateTo = q.DateFrom
	case DatePreset7Days:
		q.DateFrom = today.AddDate(0, 0, -6).Format("2006-01-02")
		q.DateTo = today.Format("2006-01-02")
	case DatePreset30Days:
		q.DateFrom = today.AddDate(0, 0, -29).Format("2006-01-02")
		q.DateTo = today.Format("2006-01-02")
	case DatePresetCustom:
		q.DateFrom = normalizeDateArg(q.DateFrom)
		q.DateTo = normalizeDateArg(q.DateTo)
		// A half-open custom range is valid: treat a missing side as unbounded.
	default: // "" and "all"
		q.DatePreset = DatePresetAll
		q.DateFrom = ""
		q.DateTo = ""
	}

	if q.DateFrom != "" && q.DateTo != "" && q.DateFrom > q.DateTo {
		q.DateFrom, q.DateTo = q.DateTo, q.DateFrom
	}
}

// normalizeDateArg accepts only a well-formed YYYY-MM-DD prefix so a malformed
// value can never reach the SQL layer.
func normalizeDateArg(v string) string {
	v = strings.TrimSpace(v)
	if len(v) < 10 {
		return ""
	}
	v = v[:10]
	if _, err := time.Parse("2006-01-02", v); err != nil {
		return ""
	}
	return v
}

// TranslationStats reports how much of the article table carries both language
// variants, for the settings page.
type TranslationStats struct {
	Total      int64 `json:"total"`
	Pending    int64 `json:"pending"`     // missing a variant (includes exhausted-retry articles)
	Processable int64 `json:"processable"` // missing a variant AND within retry limit — what the worker actually processes
	Done       int64 `json:"done"`
	Failed     int64 `json:"failed"`
	Skipped    int64 `json:"skipped"`
	CacheHit   int64 `json:"cacheHit"`
}
