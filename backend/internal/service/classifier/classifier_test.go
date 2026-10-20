package classifier

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ai-informations/internal/models"
	"ai-informations/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// The classifier is what fills the five topic boards, so these tests pin both
// directions: Chinese text must classify (it did not at all before, because the
// keyword splitter only understood spaces), and English keywords must match as
// whole words (substring matching sent nearly every article to "technology").

func newTestDB(t *testing.T) *gorm.DB {
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
	if err := repository.SeedDefaultCategories(db); err != nil {
		t.Fatalf("seed categories: %v", err)
	}
	return db
}

func categoryIDs(t *testing.T, db *gorm.DB, slugs ...string) map[uint]bool {
	t.Helper()
	want := map[uint]bool{}
	for _, slug := range slugs {
		var c models.Category
		if err := db.Where("slug = ?", slug).First(&c).Error; err != nil {
			t.Fatalf("category %s: %v", slug, err)
		}
		want[c.ID] = true
	}
	return want
}

func TestClassifyChineseHeadline(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)
	want := categoryIDs(t, db, "finance-macro")

	got := svc.Classify("8月CPI同比上涨0.6% 工业增加值增速回落", "", "", "https://example.com/a", 1)
	if !containsAny(got, want) {
		t.Fatalf("Chinese macro headline did not match finance-macro: %v", got)
	}

	// A second, differently worded case from the same category.
	got = svc.Classify("统计局：社会消费品零售总额同比增长", "固定资产投资保持平稳", "", "", 1)
	if !containsAny(got, want) {
		t.Fatalf("second Chinese macro headline did not match finance-macro: %v", got)
	}
}

func TestClassifyChineseMilitaryAndTech(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)
	mil := categoryIDs(t, db, "military-equipment", "military-conflicts")
	tech := categoryIDs(t, db, "tech-it-ai")

	got := svc.Classify("新型高超音速导弹完成试射", "", "", "", 1)
	if !containsAny(got, mil) {
		t.Fatalf("weapons headline did not match a military category: %v", got)
	}

	got = svc.Classify("国产大模型发布新版本 算力需求上升", "", "", "", 1)
	if !containsAny(got, tech) {
		t.Fatalf("AI headline did not match tech-it-ai: %v", got)
	}
}

func TestClassifyEnglishHeadline(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)
	want := categoryIDs(t, db, "economy-monetary-fiscal", "finance-macro")

	got := svc.Classify("Fed holds interest rates steady as inflation cools", "", "", "", 1)
	if !containsAny(got, want) {
		t.Fatalf("English rates headline did not match a monetary category: %v", got)
	}
}

// TestShortASCIIKeywordsDoNotMatchInsideWords is the precision regression: "AI"
// used to match "said", "chair", "campaign" and so on, which put 800+
// unclassified articles into the technology bucket.
func TestShortASCIIKeywordsDoNotMatchInsideWords(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)
	tech := categoryIDs(t, db, "tech-it-ai", "tech-frontier", "tech-companies")

	got := svc.Classify("Chairman said the campaign was against all odds", "", "", "", 1)
	if containsAny(got, tech) {
		t.Fatalf("plain prose matched a technology category through a substring: %v", got)
	}

	// And the same word still matches when it really is the token "AI".
	got = svc.Classify("AI startup raises funding for new chips", "", "", "", 1)
	if !containsAny(got, tech) {
		t.Fatalf("a real AI headline did not match: %v", got)
	}
}

func TestClassifyUnrelatedTextMatchesNothing(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)

	if got := svc.Classify("Lunar Terminator Paradox", "", "", "", 1); len(got) != 0 {
		t.Fatalf("an unrelated title matched categories: %v", got)
	}
}

func TestClassifyUncategorizedFillsMissingLinks(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)

	// Stored before the classifier understood Chinese: no links at all.
	articles := []models.Article{
		{Title: "央行宣布下调存款准备金率", OriginURL: "https://example.com/1", Fingerprint: "1", Summary: "货币政策宽松"},
		{Title: "Solar and wind capacity keeps growing", OriginURL: "https://example.com/2", Fingerprint: "2"},
		{Title: "ZZZ nothing here", OriginURL: "https://example.com/3", Fingerprint: "3"},
	}
	for i := range articles {
		if err := db.Create(&articles[i]).Error; err != nil {
			t.Fatalf("create article: %v", err)
		}
	}

	n, err := svc.ClassifyUncategorized(context.Background(), 100)
	if err != nil {
		t.Fatalf("ClassifyUncategorized: %v", err)
	}
	if n != 2 {
		t.Fatalf("classified %d articles, want 2 (the third matches nothing)", n)
	}

	var links int64
	if err := db.Model(&models.ArticleCategory{}).Count(&links).Error; err != nil {
		t.Fatalf("count links: %v", err)
	}
	if links == 0 {
		t.Fatal("no category links were written")
	}

	// Running it again must be a no-op, not a duplicate-key failure.
	again, err := svc.ClassifyUncategorized(context.Background(), 100)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if again != 0 {
		t.Fatalf("second run classified %d more articles, want 0", again)
	}
}

// TestExpandedKeywordsCoversMoreCoverage verifies that the expanded keyword
// tables correctly classify headlines that were previously falling through to
// "no category". These are regression cases for the terminology expansion.
func TestExpandedKeywordsCoversMoreCoverage(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)

	cases := []struct {
		title string
		want  []string // slug prefixes to accept
	}{
		// Politics: president/prime minister + government should hit domestic
		{"President signs new executive order on immigration", []string{"politics-domestic"}},
		{"Prime minister announces cabinet reshuffle ahead of election", []string{"politics-domestic"}},
		// Diplomacy: UN + NATO + EU references
		{"NATO allies agree to increase defense spending at summit", []string{"politics-diplomacy"}},
		{"EU imposes new sanctions on foreign officials", []string{"politics-diplomacy"}},
		// Geopolitics: Taiwan, Middle East, Belt and Road
		{"Tensions rise in the South China Sea over territorial waters", []string{"politics-geopolitics"}},
		{"Belt and Road initiative expands to new African partners", []string{"politics-geopolitics"}},
		// Economy: global economy, emerging markets
		{"Emerging markets face headwinds as global economy slows", []string{"economy-regional-global"}},
		{"Central bank signals rate cut amid weakening labor market", []string{"economy-monetary-fiscal"}},
		// Military: war, combat, nuclear
		{"Civil war intensifies as rebels seize capital city", []string{"military-conflicts"}},
		{"North Korea conducts sixth nuclear test", []string{"military-arm-control"}},
		// Tech: big tech companies
		{"Apple unveils new AI chip for next-gen iPhones", []string{"tech-companies"}},
		{"Google faces antitrust trial over search dominance", []string{"tech-companies"}},
		// Finance: stock market, Wall Street
		{"Wall Street rallies as inflation data comes in lower", []string{"finance-securities"}},
		{"Dollar strengthens against yen as Fed signals hawkish stance", []string{"finance-fixed-income-forex"}},
	}

	for _, tc := range cases {
		got := svc.Classify(tc.title, "", "", "", 1)
		found := false
		for _, id := range got {
			var cat models.Category
			if err := db.First(&cat, id).Error; err != nil {
				t.Fatalf("category %d: %v", id, err)
			}
			for _, prefix := range tc.want {
				if strings.HasPrefix(cat.Slug, prefix) {
					found = true
					break
				}
			}
		}
		if !found {
			t.Fatalf("headline %q matched no expected category (got %v)", tc.title, got)
		}
	}
}

func containsAny(got []uint, want map[uint]bool) bool {
	for _, id := range got {
		if want[id] {
			return true
		}
	}
	return false
}

// stubLLM implements LLMProvider for tests.
type stubLLM struct {
	reply string
	err   error
	// delay holds each Chat call open so concurrent calls overlap and
	// peakInFlight can observe real parallelism; zero means no delay.
	delay       time.Duration
	inFlight    int32
	maxInFlight int32
}

// peakInFlight reports the highest number of Chat calls the stub ever saw
// executing at the same moment.
func (s *stubLLM) peakInFlight() int32 { return atomic.LoadInt32(&s.maxInFlight) }

func (s *stubLLM) Chat(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	cur := atomic.AddInt32(&s.inFlight, 1)
	defer atomic.AddInt32(&s.inFlight, -1)
	for {
		peak := atomic.LoadInt32(&s.maxInFlight)
		if cur <= peak || atomic.CompareAndSwapInt32(&s.maxInFlight, peak, cur) {
			break
		}
	}
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return s.reply, s.err
}

// TestClassifyWithLLMFallback covers the fallback path: when keyword matching
// finds nothing, the model's reply is parsed into category IDs.
func TestClassifyWithLLMFallback(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)
	svc.SetLLMProvider(&stubLLM{reply: `["证券市场", "宏观经济"]`})

	// A title with no keyword hits — only the LLM can place it.
	ids, err := svc.ClassifyWithLLM(context.Background(), "某公司发布全新产品", "这是一篇关于某公司新产品的报道。")
	if err != nil {
		t.Fatalf("ClassifyWithLLM: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("got %d ids, want 2: %v", len(ids), ids)
	}

	// Verify the IDs map to the right categories.
	slugs := make(map[uint]string)
	for _, id := range ids {
		var c models.Category
		if err := db.First(&c, id).Error; err != nil {
			t.Fatalf("category %d: %v", id, err)
		}
		slugs[id] = c.Slug
	}
	if slugs[ids[0]] != "finance-securities" && slugs[ids[0]] != "finance-macro" {
		t.Fatalf("unexpected category for id %d: %s", ids[0], slugs[ids[0]])
	}
}

// TestClassifyWithLLMError covers the error path: when the LLM call fails, the
// error is returned so the caller can decide what to do.
func TestClassifyWithLLMError(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)
	svc.SetLLMProvider(&stubLLM{err: fmt.Errorf("connection refused")})

	_, err := svc.ClassifyWithLLM(context.Background(), "title", "summary")
	if err == nil {
		t.Fatal("expected an error when the LLM call fails")
	}
}

// TestClassifyWithLLMNoProvider covers the case where no LLM is configured.
func TestClassifyWithLLMNoProvider(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)

	_, err := svc.ClassifyWithLLM(context.Background(), "title", "summary")
	if err == nil {
		t.Fatal("expected an error when no LLM provider is set")
	}
}

// TestClassifyWithLLMMatchesCatalogEchoForm: small models echo the catalog
// line verbatim ("证券市场 (Securities Market)"), which used to match neither
// key and silently dropped the article back to unclassified.
func TestClassifyWithLLMMatchesCatalogEchoForm(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)

	var c models.Category
	if err := db.Where("slug = ?", "finance-securities").First(&c).Error; err != nil {
		t.Fatalf("load category: %v", err)
	}
	// First element: zh name + a gloss that is NOT the seeded NameEn, so only
	// the prefix candidate can match. Second: the bare English name.
	svc.SetLLMProvider(&stubLLM{reply: fmt.Sprintf(`["%s (Not The En Name)", "%s"]`, c.Name, c.NameEn)})

	ids, err := svc.ClassifyWithLLM(context.Background(), "某券商发布年度策略", "一篇关于证券市场的报道。")
	if err != nil {
		t.Fatalf("ClassifyWithLLM: %v", err)
	}
	if len(ids) != 1 || ids[0] != c.ID {
		t.Fatalf("ids = %v, want [%d] (catalog echo form must resolve)", ids, c.ID)
	}
}

// TestClassifyWithLLMUnquotedArray: qwen3b-scale models emit
// "[证券市场, 宏观经济指标]" (no quotes) whose bracketed parts used to survive
// the comma fallback and match nothing.
func TestClassifyWithLLMUnquotedArray(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)
	svc.SetLLMProvider(&stubLLM{reply: `[证券市场, 宏观经济指标]`})

	ids, err := svc.ClassifyWithLLM(context.Background(), "前三季度投资增速回落", "宏观数据发布。")
	if err != nil {
		t.Fatalf("ClassifyWithLLM: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("ids = %v, want 2 (unquoted array must parse)", ids)
	}
	want := map[uint]bool{}
	for _, name := range []string{"证券市场", "宏观经济指标"} {
		var c models.Category
		if err := db.Where("name = ?", name).First(&c).Error; err != nil {
			t.Fatalf("seeded category %q: %v", name, err)
		}
		want[c.ID] = true
	}
	for _, id := range ids {
		if !want[id] {
			t.Fatalf("unexpected category id %d in %v", id, ids)
		}
	}
}

// TestClassifyUncategorizedRunsConcurrently pins the llm_max_concurrency
// contract on the classification side: four LLM-fallback articles in one job
// must overlap (peak > 1) but never exceed the configured ceiling.
func TestClassifyUncategorizedRunsConcurrently(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db)
	if err := repository.NewSystemSettingRepository(db).Set("llm_max_concurrency", "4", ""); err != nil {
		t.Fatalf("set concurrency: %v", err)
	}
	stub := &stubLLM{reply: `["证券市场"]`, delay: 120 * time.Millisecond}
	svc.SetLLMProvider(stub)

	// Titles shaped like the proven "matches no keyword" case, so every
	// article must take the LLM fallback path.
	for i := 0; i < 4; i++ {
		if err := db.Create(&models.Article{
			Title:       fmt.Sprintf("ZZZ nothing here %d", i),
			OriginURL:   fmt.Sprintf("https://example.com/conc/%d", i),
			Fingerprint: fmt.Sprintf("conc-%d", i),
		}).Error; err != nil {
			t.Fatalf("create article: %v", err)
		}
	}

	n, err := svc.ClassifyUncategorized(context.Background(), 100)
	if err != nil {
		t.Fatalf("ClassifyUncategorized: %v", err)
	}
	if n != 4 {
		t.Fatalf("classified %d articles, want 4", n)
	}

	peak := stub.peakInFlight()
	if peak < 2 {
		t.Fatalf("peak in-flight = %d, want >1: LLM fallback should run concurrently", peak)
	}
	if peak > 4 {
		t.Fatalf("peak in-flight = %d, want <= llm_max_concurrency (4)", peak)
	}
}
