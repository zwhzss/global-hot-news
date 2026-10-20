package repository

import (
	"strings"
	"testing"
	"time"

	"ai-informations/internal/models"

	"gorm.io/gorm"
)

// These tests pin the behaviour the UI now depends on: "today only" by default,
// newest first, and a title search that works in either language. They also pin
// the timestamp handling, because publish_time arrives with different UTC
// offsets from different feeds and a naive string comparison silently reorders
// the list.

func at(y int, m time.Month, d, h int) time.Time {
	return time.Date(y, m, d, h, 0, 0, 0, time.Local)
}

// seedArticle inserts one article with explicit timestamps so the assertions do
// not depend on the clock.
func seedArticle(t *testing.T, db *gorm.DB, title, titleZh, titleEn, summary string, fetched, published time.Time) *models.Article {
	t.Helper()
	a := &models.Article{
		Title:       title,
		TitleZh:     titleZh,
		TitleEn:     titleEn,
		Summary:     summary,
		OriginURL:   "https://example.com/" + title,
		FetchedAt:   fetched,
		PublishTime: published,
		Fingerprint: title,
		ExpireAt:    fetched.AddDate(0, 0, 90),
	}
	if err := NewArticleRepository(db).Create(a); err != nil {
		t.Fatalf("create article %q: %v", title, err)
	}
	return a
}

func titles(articles []models.Article) []string {
	out := make([]string, 0, len(articles))
	for _, a := range articles {
		out = append(out, a.Title)
	}
	return out
}

// TestQueryDatePresetToday checks that the default board view returns only
// today's articles, which is what keeps the list usable as the table grows.
func TestQueryDatePresetToday(t *testing.T) {
	db := newTestDB(t)
	repo := NewArticleRepository(db)

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, time.Local)
	yesterday := today.AddDate(0, 0, -1)
	lastWeek := today.AddDate(0, 0, -7)

	seedArticle(t, db, "today-a", "", "", "", today, today)
	seedArticle(t, db, "today-b", "", "", "", today.Add(time.Hour), today.Add(time.Hour))
	seedArticle(t, db, "yesterday", "", "", "", yesterday, yesterday)
	seedArticle(t, db, "lastweek", "", "", "", lastWeek, lastWeek)

	got, total, err := repo.Query(models.NewsQuery{Page: 1, PageSize: 50, DatePreset: models.DatePresetToday})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if total != 2 {
		t.Fatalf("today total = %d, want 2 (%v)", total, titles(got))
	}
	for _, a := range got {
		if a.Title == "yesterday" || a.Title == "lastweek" {
			t.Fatalf("today filter leaked an older article: %v", titles(got))
		}
	}
}

// TestQuerySortedNewestFirst uses a published timestamp in UTC (+00:00) and one
// in local time (+08:00) for the same instants, which is exactly the mix the
// real database contains.
func TestQuerySortedNewestFirst(t *testing.T) {
	db := newTestDB(t)
	repo := NewArticleRepository(db)

	base := time.Now().Add(-2 * time.Hour)
	older := base.Add(-3 * time.Hour)

	// Same wall-clock instants, expressed in two different offsets.
	a := seedArticle(t, db, "newer-utc", "", "", "", base.UTC(), base.UTC())
	b := seedArticle(t, db, "older-local", "", "", "", older, older)

	got, _, err := repo.Query(models.NewsQuery{Page: 1, PageSize: 10, DatePreset: models.DatePresetAll})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d articles, want 2", len(got))
	}
	if got[0].ID != a.ID || got[1].ID != b.ID {
		t.Fatalf("order = %v, want [newer-utc older-local]", titles(got))
	}
}

// TestQueryFallsBackToFetchedAtWhenPublishTimeIsZero covers feed items without a
// date: they must still land in the right place instead of sorting to year 1.
func TestQueryFallsBackToFetchedAtWhenPublishTimeIsZero(t *testing.T) {
	db := newTestDB(t)
	repo := NewArticleRepository(db)

	now := time.Now()
	recent := seedArticle(t, db, "no-publish-time", "", "", "", now, time.Time{})

	got, total, err := repo.Query(models.NewsQuery{Page: 1, PageSize: 10, DatePreset: models.DatePresetToday})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if total != 1 || len(got) != 1 || got[0].ID != recent.ID {
		t.Fatalf("zero publish_time article was not treated as today: total=%d got=%v", total, titles(got))
	}
}

// TestQueryTitleKeywordMatchesEitherLanguage is the requirement "标题模糊查询":
// searching must find an article whether the user types Chinese or English.
func TestQueryTitleKeywordMatchesEitherLanguage(t *testing.T) {
	db := newTestDB(t)
	repo := NewArticleRepository(db)

	now := time.Now()
	seedArticle(t, db, "央行维持利率不变", "央行维持利率不变", "Central bank holds rates", "", now, now)
	seedArticle(t, db, "Quarterly GDP beats forecasts", "季度GDP超预期", "Quarterly GDP beats forecasts", "", now, now)

	cases := []struct {
		keyword string
		want    int
	}{
		{"利率", 1},           // original + Chinese title of the first row
		{"Central bank", 1}, // English translation of the first row only
		{"超预期", 1},          // present ONLY in the Chinese translation of the second row
		{"GDP", 1},          // present in the English title of the second row
		{"nothing-matches", 0},
	}
	for _, tc := range cases {
		got, total, err := repo.Query(models.NewsQuery{Page: 1, PageSize: 10, TitleKeyword: tc.keyword, DatePreset: models.DatePresetAll})
		if err != nil {
			t.Fatalf("query %q: %v", tc.keyword, err)
		}
		if int(total) != tc.want {
			t.Fatalf("keyword %q -> %d rows (%v), want %d", tc.keyword, total, titles(got), tc.want)
		}
	}
}

// TestQueryTitleKeywordEscapesWildcards makes sure a literal % does not turn a
// search into "match everything".
func TestQueryTitleKeywordEscapesWildcards(t *testing.T) {
	db := newTestDB(t)
	repo := NewArticleRepository(db)

	now := time.Now()
	seedArticle(t, db, "Rates unchanged", "利率不变", "Rates unchanged", "", now, now)
	seedArticle(t, db, "Revenue up 12%", "营收增长12%", "Revenue up 12%", "", now, now)

	got, total, err := repo.Query(models.NewsQuery{Page: 1, PageSize: 10, TitleKeyword: "%", DatePreset: models.DatePresetAll})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if total != 1 {
		t.Fatalf("literal %% matched %d rows (%v), want only the one containing it", total, titles(got))
	}
}

// TestQueryCustomDateRangeIsInclusive pins the boundary behaviour of the date
// picker.
func TestQueryCustomDateRangeIsInclusive(t *testing.T) {
	db := newTestDB(t)
	repo := NewArticleRepository(db)

	day := time.Date(2026, 3, 10, 12, 0, 0, 0, time.Local)
	startOfDay := time.Date(2026, 3, 10, 0, 0, 1, 0, time.Local)
	endOfDay := time.Date(2026, 3, 10, 23, 59, 59, 0, time.Local)

	seedArticle(t, db, "inside-start", "", "", "", startOfDay, startOfDay)
	seedArticle(t, db, "inside-end", "", "", "", endOfDay, endOfDay)
	seedArticle(t, db, "day-before", "", "", "", startOfDay.AddDate(0, 0, -1), startOfDay.AddDate(0, 0, -1))
	seedArticle(t, db, "day-after", "", "", "", endOfDay.AddDate(0, 0, 1), endOfDay.AddDate(0, 0, 1))
	_ = day

	got, total, err := repo.Query(models.NewsQuery{
		Page: 1, PageSize: 20,
		DatePreset: models.DatePresetCustom,
		DateFrom:   "2026-03-10",
		DateTo:     "2026-03-10",
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if total != 2 {
		t.Fatalf("custom range total = %d (%v), want 2", total, titles(got))
	}
}

// TestQueryRejectsMalformedDate keeps a bad value from reaching SQL.
func TestQueryRejectsMalformedDate(t *testing.T) {
	db := newTestDB(t)
	repo := NewArticleRepository(db)

	now := time.Now()
	seedArticle(t, db, "anything", "", "", "", now, now)

	q := models.NewsQuery{Page: 1, PageSize: 10, DatePreset: models.DatePresetCustom, DateFrom: "'; DROP TABLE articles; --"}
	got, total, err := repo.Query(q)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	// The malformed value is dropped, so the range is unbounded and the row shows.
	if total != 1 || len(got) != 1 {
		t.Fatalf("malformed date was not ignored: total=%d", total)
	}
}

// TestQueryCategoryFilterDoesNotDuplicate guards the JOIN-to-subquery change: an
// article linked to two categories counted twice before.
func TestQueryCategoryFilterDoesNotDuplicate(t *testing.T) {
	db := newTestDB(t)
	repo := NewArticleRepository(db)

	now := time.Now()
	a := seedArticle(t, db, "multi-category", "", "", "", now, now)

	catA := models.Category{Name: "A", Slug: "cat-a"}
	catB := models.Category{Name: "B", Slug: "cat-b"}
	if err := db.Create(&catA).Error; err != nil {
		t.Fatalf("create category: %v", err)
	}
	if err := db.Create(&catB).Error; err != nil {
		t.Fatalf("create category: %v", err)
	}
	if err := db.Create(&models.ArticleCategory{ArticleID: a.ID, CategoryID: catA.ID}).Error; err != nil {
		t.Fatalf("link category: %v", err)
	}
	if err := db.Create(&models.ArticleCategory{ArticleID: a.ID, CategoryID: catB.ID}).Error; err != nil {
		t.Fatalf("link category: %v", err)
	}

	got, total, err := repo.Query(models.NewsQuery{Page: 1, PageSize: 10, CategorySlug: "cat-a"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if total != 1 || len(got) != 1 {
		t.Fatalf("category filter returned %d rows, want 1", total)
	}
	if len(got[0].Categories) != 2 {
		t.Fatalf("categories not preloaded: %d", len(got[0].Categories))
	}
}

// TestQueryCategoryPrefixSelectsWholeModule covers a board's "All" chip, which
// shows every article in the module rather than one sub-category.
func TestQueryCategoryPrefixSelectsWholeModule(t *testing.T) {
	db := newTestDB(t)
	repo := NewArticleRepository(db)

	now := time.Now()
	finance := seedArticle(t, db, "finance-one", "", "", "", now, now)
	tech := seedArticle(t, db, "tech-one", "", "", "", now, now)
	seedArticle(t, db, "uncategorised", "", "", "", now, now)

	catFinance := models.Category{Name: "宏观", Slug: "finance-macro"}
	catTech := models.Category{Name: "AI", Slug: "tech-it-ai"}
	if err := db.Create(&catFinance).Error; err != nil {
		t.Fatalf("create category: %v", err)
	}
	if err := db.Create(&catTech).Error; err != nil {
		t.Fatalf("create category: %v", err)
	}
	db.Create(&models.ArticleCategory{ArticleID: finance.ID, CategoryID: catFinance.ID})
	db.Create(&models.ArticleCategory{ArticleID: tech.ID, CategoryID: catTech.ID})

	got, total, err := repo.Query(models.NewsQuery{Page: 1, PageSize: 10, CategoryPrefix: "finance-"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if total != 1 || len(got) != 1 || got[0].ID != finance.ID {
		t.Fatalf("prefix filter = %v, want only the finance article", titles(got))
	}

	// An explicit slug wins over the prefix so a single card click is exact.
	got, total, err = repo.Query(models.NewsQuery{Page: 1, PageSize: 10, CategorySlug: "tech-it-ai", CategoryPrefix: "finance-"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if total != 1 || got[0].ID != tech.ID {
		t.Fatalf("slug should override prefix: %v", titles(got))
	}
}

// TestSeedDefaultCategoriesIsBilingualAndUpgradesExistingRows covers the
// category data the UI switches language on: a fresh install gets both names and
// both descriptions, and a database seeded by the older version (English text in
// the Chinese description column) is repaired in place.
func TestSeedDefaultCategoriesIsBilingualAndUpgradesExistingRows(t *testing.T) {
	db := newTestDB(t)

	// Simulate the old seed: English description sitting in `description`, no
	// English name, and a hand-edited row that must be left alone.
	old := models.Category{
		Name: "宏观经济", Slug: "finance-macro",
		Description: "GDP, CPI, PPI, unemployment, PMI, trade",
	}
	custom := models.Category{
		Name: "手改分类", Slug: "finance-securities",
		Description: "我自己的说明",
	}
	if err := db.Create(&old).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := db.Create(&custom).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := SeedDefaultCategories(db); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var got models.Category
	if err := db.Where("slug = ?", "finance-macro").First(&got).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.NameEn != "Macro Economy" {
		t.Fatalf("name_en = %q, want the English name to be filled", got.NameEn)
	}
	if got.DescriptionEn == "" {
		t.Fatal("description_en was not filled")
	}
	if got.Description == "GDP, CPI, PPI, unemployment, PMI, trade" {
		t.Fatalf("Chinese description still holds the English default: %q", got.Description)
	}
	if !strings.Contains(got.Description, "国内生产总值") {
		t.Fatalf("Chinese description = %q, want the seeded Chinese text", got.Description)
	}

	// A hand-edited description must survive.
	var kept models.Category
	if err := db.Where("slug = ?", "finance-securities").First(&kept).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if kept.Description != "我自己的说明" {
		t.Fatalf("hand-edited description was overwritten: %q", kept.Description)
	}
	if kept.NameEn != "Securities Market" {
		t.Fatalf("name_en should still be filled, got %q", kept.NameEn)
	}

	// Every seeded category must have both sides populated.
	var missing int64
	if err := db.Model(&models.Category{}).
		Where("name_en = '' OR description_en = '' OR description = '' OR description = description_en").
		Count(&missing).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if missing != 0 {
		t.Fatalf("%d categories are not properly bilingual", missing)
	}
}

// TestQueryFollowOnlyIgnoresDeletedFollows covers unfollowing an article: the
// soft-deleted row must not keep the article on the board.
func TestQueryFollowOnlyIgnoresDeletedFollows(t *testing.T) {
	db := newTestDB(t)
	repo := NewArticleRepository(db)

	now := time.Now()
	kept := seedArticle(t, db, "still-followed", "", "", "", now, now)
	removed := seedArticle(t, db, "unfollowed", "", "", "", now, now)

	db.Create(&models.Follow{FollowType: "article", TargetID: kept.ID})
	gone := &models.Follow{FollowType: "article", TargetID: removed.ID}
	db.Create(gone)
	if err := db.Delete(gone).Error; err != nil {
		t.Fatalf("soft delete follow: %v", err)
	}

	got, total, err := repo.Query(models.NewsQuery{Page: 1, PageSize: 10, FollowOnly: true})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if total != 1 || len(got) != 1 || got[0].ID != kept.ID {
		t.Fatalf("followOnly = %v, want only the still-followed article", titles(got))
	}
}

// TestDistinctDatesSkipsEmptyValues covers the date picker's data source.
func TestDistinctDatesSkipsEmptyValues(t *testing.T) {
	db := newTestDB(t)
	repo := NewArticleRepository(db)

	d1 := time.Date(2026, 1, 2, 10, 0, 0, 0, time.Local)
	d2 := time.Date(2026, 1, 5, 10, 0, 0, 0, time.Local)
	seedArticle(t, db, "one", "", "", "", d1, d1)
	seedArticle(t, db, "two", "", "", "", d2, d2)

	dates, err := repo.DistinctDates(10)
	if err != nil {
		t.Fatalf("distinct dates: %v", err)
	}
	if len(dates) != 2 {
		t.Fatalf("dates = %v, want 2 entries", dates)
	}
	if dates[0] != "2026-01-05" || dates[1] != "2026-01-02" {
		t.Fatalf("dates = %v, want newest first", dates)
	}
}

// TestBackfillBilingualFillsOriginalSide covers the upgrade path for rows that
// predate the bilingual columns.
func TestBackfillBilingualFillsOriginalSide(t *testing.T) {
	db := newTestDB(t)

	now := time.Now()
	zh := seedArticle(t, db, "央行降息", "", "", "中国人民银行宣布降息", now, now)
	en := seedArticle(t, db, "Fed holds rates steady", "", "", "The Federal Reserve left rates unchanged", now, now)

	n, err := BackfillBilingual(db)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if n != 2 {
		t.Fatalf("backfill updated %d rows, want 2", n)
	}

	var gotZh, gotEn models.Article
	if err := db.First(&gotZh, zh.ID).Error; err != nil {
		t.Fatalf("reload zh: %v", err)
	}
	if gotZh.Lang != "zh" || gotZh.TitleZh != "央行降息" || gotZh.TitleEn != "" {
		t.Fatalf("zh article = lang:%q zh:%q en:%q", gotZh.Lang, gotZh.TitleZh, gotZh.TitleEn)
	}
	if gotZh.TransStatus != models.TransPending {
		t.Fatalf("zh article status = %q, want pending", gotZh.TransStatus)
	}

	if err := db.First(&gotEn, en.ID).Error; err != nil {
		t.Fatalf("reload en: %v", err)
	}
	if gotEn.Lang != "en" || gotEn.TitleEn != "Fed holds rates steady" || gotEn.TitleZh != "" {
		t.Fatalf("en article = lang:%q zh:%q en:%q", gotEn.Lang, gotEn.TitleZh, gotEn.TitleEn)
	}

	// The original columns must be untouched.
	if gotZh.Title != "央行降息" || gotEn.Title != "Fed holds rates steady" {
		t.Fatalf("backfill modified the original title columns")
	}
}

// TestListPendingTranslationSelectsOnlyNeedyRows checks the queue the worker
// consumes: complete rows and unknown-language rows stay out of it.
func TestListPendingTranslationSelectsOnlyNeedyRows(t *testing.T) {
	db := newTestDB(t)
	repo := NewArticleRepository(db)

	now := time.Now()
	needsEn := seedArticle(t, db, "需要翻译", "需要翻译", "", "", now, now)
	needsEn.Lang = "zh"
	complete := seedArticle(t, db, "done", "已完成", "done", "", now, now)
	complete.Lang = "zh"
	unknown := seedArticle(t, db, "12345", "", "", "", now, now)
	unknown.Lang = ""

	for _, a := range []*models.Article{needsEn, complete, unknown} {
		if err := db.Save(a).Error; err != nil {
			t.Fatalf("save article: %v", err)
		}
	}

	got, err := repo.ListPendingTranslation(10, 3)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(got) != 1 || got[0].ID != needsEn.ID {
		t.Fatalf("pending = %v, want only the article missing its English title", titles(got))
	}
}
