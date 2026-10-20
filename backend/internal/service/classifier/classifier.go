package classifier

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"ai-informations/internal/models"
	"ai-informations/internal/service/llmqueue"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Rule represents a keyword rule for category classification.
type Rule struct {
	CategoryID uint
	Keywords   []string // lowercase keywords that trigger this category
}

// Service classifies articles into categories based on keyword matching.
//
// Keyword matching is deliberately simple and completely local: no model, no
// network, no cost. Precision comes from the vocabulary (slug + name +
// description + extraKeywords) and from matching ASCII terms as whole words
// rather than substrings.
//
// When keyword matching finds nothing, the service can fall back to an LLM:
// the article title and summary are sent to the configured model, which
// returns the names of matching categories. This costs money and latency, so
// it only runs on articles the local rules could not place.
type Service struct {
	db    *gorm.DB
	rules []Rule
	// llmFn resolves the LLM fallback provider on every use; nil = keyword
	// only. It is a getter (not a stored provider) so saving new classifier
	// model settings applies without restarting the app.
	llmFn func() LLMProvider
	mu    sync.RWMutex
}

// LLMProvider is the minimal chat interface the classifier needs. It is
// satisfied by *translator.OpenAICompatProvider.
type LLMProvider interface {
	Chat(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

func NewService(db *gorm.DB) *Service {
	s := &Service{db: db}
	s.reloadRules()
	return s
}

// SetLLMProvider installs a fixed fallback provider (the tests use this). It
// is optional: without it the service behaves exactly like the keyword-only
// version.
func (s *Service) SetLLMProvider(p LLMProvider) {
	s.SetLLMProviderGetter(func() LLMProvider { return p })
}

// SetLLMProviderGetter installs a factory that is consulted on every model
// use, so newly saved classifier model settings (cls_*) take effect without
// restarting the application.
func (s *Service) SetLLMProviderGetter(fn func() LLMProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.llmFn = fn
}

// nowLLM resolves the provider for this call; the getter runs outside the
// lock because it may read settings from the database.
func (s *Service) nowLLM() LLMProvider {
	s.mu.RLock()
	fn := s.llmFn
	s.mu.RUnlock()
	if fn == nil {
		return nil
	}
	return fn()
}

// LLMConfigured reports whether an LLM fallback is available right now. The
// audit buttons reject a run up front when no model endpoint has been set up yet.
func (s *Service) LLMConfigured() bool {
	return s.nowLLM() != nil
}

// TestConnection proves the cls_* endpoint with one tiny chat call, mirroring
// the translation section's "Test connection" button so a misconfigured model
// fails here instead of during an audit run.
func (s *Service) TestConnection(ctx context.Context) (string, error) {
	llm := s.nowLLM()
	if llm == nil {
		return "", fmt.Errorf("classifier model is not configured: set the base URL and model (a cloud URL also needs an API key; leave both empty to classify by keywords only)")
	}
	reply, err := llm.Chat(ctx,
		"You are a connectivity test assistant. Reply with exactly one word.",
		"ping")
	if err != nil {
		return "", err
	}
	reply = strings.TrimSpace(reply)
	if reply == "" {
		reply = "(empty reply)"
	}
	return fmt.Sprintf("model replied: %s", reply), nil
}

// reloadRules builds the keyword rules from the curated vocabulary plus each
// category's Chinese name.
//
// Neither the slug nor the prose descriptions contribute keywords, and that is
// deliberate. Deriving them from prose produced single generic words ("military",
// "technology", "history", "it") that matched almost everything — and because an
// article's URL was searched too, every item from people.com.cn/rss/military.xml
// landed in "武器装备与技术" via the word "military" in its address. The curated
// lists in keywords.go are the actual classification vocabulary.
func (s *Service) reloadRules() {
	var cats []models.Category
	s.db.Find(&cats)

	s.mu.Lock()
	defer s.mu.Unlock()

	rules := make([]Rule, 0, len(cats))
	for _, cat := range cats {
		seen := make(map[string]bool)
		var kws []string
		add := func(words []string) {
			for _, w := range words {
				w = strings.ToLower(strings.TrimSpace(w))
				// One or two runes would match too broadly (e.g. "A" inside
				// "said", "I" inside "campaign"). Require at least three for
				// ASCII keywords; Chinese keywords keep the existing two-rune
				// minimum since they are matched as substrings.
				if isASCII(w) && len(w) < 3 || !isASCII(w) && len([]rune(w)) < 2 {
					continue
				}
				seen[w] = true
				kws = append(kws, w)
			}
		}

		add([]string{cat.Name})
		add(extraKeywords[cat.Slug])

		if len(kws) > 0 {
			rules = append(rules, Rule{CategoryID: cat.ID, Keywords: kws})
		}
	}
	s.rules = rules
}

// Refresh reloads classification rules from the database.
func (s *Service) Refresh() {
	s.reloadRules()
}

// categoryNames loads the id -> Chinese name map once per job so monitor
// events can say "证券、经济" instead of showing raw ids.
func (s *Service) categoryNames() map[uint]string {
	var cats []models.Category
	names := make(map[uint]string)
	if err := s.db.Find(&cats).Error; err != nil {
		log.Printf("classifier: failed to load category names: %v", err)
		return names
	}
	for _, c := range cats {
		names[c.ID] = c.Name
	}
	return names
}

// llmConcurrency reads llm_max_concurrency (clamped to 1..8) once per job and
// refreshes the shared inference gate so translation and classification draw
// from the same budget. A missing or garbled setting degrades to 1 (serial).
func (s *Service) llmConcurrency() int {
	var row models.SystemSetting
	n := 1
	if err := s.db.Where("key = ?", "llm_max_concurrency").First(&row).Error; err == nil {
		if v, convErr := strconv.Atoi(strings.TrimSpace(row.Value)); convErr == nil {
			n = v
		}
	}
	if n < 1 {
		n = 1
	}
	if n > 8 {
		n = 8
	}
	llmqueue.SetGateMax(n)
	return n
}

// joinCategoryNames renders category ids as "证券、经济" for the monitor feed.
func joinCategoryNames(ids []uint, names map[uint]string) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		if n, ok := names[id]; ok && n != "" {
			parts = append(parts, n)
		} else {
			parts = append(parts, fmt.Sprintf("#%d", id))
		}
	}
	return strings.Join(parts, "、")
}

// insertLinks stores an article's category links immediately — one small
// insert per row instead of a batch at the end of the job — so the
// "unclassified" counter drops live while a long run is still going. The
// unique index on (article_id, category_id) makes duplicates harmless.
func (s *Service) insertLinks(articleID uint, ids []uint) error {
	for _, id := range ids {
		if err := s.db.Clauses(clause.OnConflict{DoNothing: true}).
			Create(&models.ArticleCategory{ArticleID: articleID, CategoryID: id}).Error; err != nil {
			return err
		}
	}
	return nil
}

// ReclassifyAll rebuilds every category link from scratch. Keyword tuning and
// category edits change what should match, and the incremental "uncategorized
// only" pass would leave the stale links behind, so this deletes them first.
func (s *Service) ReclassifyAll(limit int) (int, error) {
	if limit <= 0 {
		limit = 20000
	}
	if err := s.db.Exec("DELETE FROM article_categories").Error; err != nil {
		return 0, err
	}

	var articles []models.Article
	if err := s.db.Where("deleted_at IS NULL").Order("fetched_at DESC").Limit(limit).Find(&articles).Error; err != nil {
		return 0, err
	}

	var links []models.ArticleCategory
	classified := 0
	for i := range articles {
		a := &articles[i]
		ids := s.Classify(a.Title, a.Summary, a.Lead, a.OriginURL, 1)
		if len(ids) == 0 {
			continue
		}
		classified++
		for _, id := range ids {
			links = append(links, models.ArticleCategory{ArticleID: a.ID, CategoryID: id})
		}
	}
	if len(links) == 0 {
		return 0, nil
	}
	if err := s.db.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&links, 200).Error; err != nil {
		return 0, err
	}
	return classified, nil
}

// ClassifyUncategorized gives a category to every live article that has none and
// returns how many it managed to classify.
//
// Two situations need this: articles stored before the classifier could read
// Chinese, and articles whose keywords matched nothing at collection time. The
// query only looks at articles with no links, so it stays cheap on a large
// table even though it runs as a queued job.
//
// Classification strategy: keyword matching first (strict), then LLM fallback.
// Articles the LLM cannot place are left unclassified — they stay visible in
// the audit panel for a manual retry. ctx is the queue job's context: a cancel
// stops the pass between articles.
func (s *Service) ClassifyUncategorized(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 5000
	}

	var articles []models.Article
	err := s.db.
		Where("articles.deleted_at IS NULL").
		Where("NOT EXISTS (SELECT 1 FROM article_categories ac WHERE ac.article_id = articles.id)").
		Order("articles.fetched_at DESC").
		Limit(limit).
		Find(&articles).Error
	if err != nil {
		return 0, err
	}
	if len(articles) == 0 {
		return 0, nil
	}

	rep := llmqueue.ReporterFrom(ctx)
	names := s.categoryNames()
	conc := s.llmConcurrency()
	rep.SetTotal(len(articles))
	var mu sync.Mutex
	classified := 0
	processed := 0
	process := func(i int) {
		if ctx.Err() != nil {
			return // cancelled: this article never started
		}
		a := &articles[i]
		rep.ItemStart(a.ID, a.Title)
		defer func() {
			mu.Lock()
			processed++
			p := processed
			mu.Unlock()
			rep.SetProgress(p, len(articles))
		}()
		ids, by := s.ClassifyOne(ctx, a)
		if len(ids) == 0 {
			rep.ItemSkip(a.ID, a.Title)
			return
		}
		if err := s.insertLinks(a.ID, ids); err != nil {
			log.Printf("classifier: failed to store links for article %d: %v", a.ID, err)
			rep.ItemError(a.ID, a.Title, err)
			return
		}
		if err := s.db.Model(&models.Article{}).Where("id = ?", a.ID).
			Update("classified_by", by).Error; err != nil {
			log.Printf("classifier: failed to set classified_by for article %d: %v", a.ID, err)
		}
		mu.Lock()
		classified++
		mu.Unlock()
		rep.ItemDone(a.ID, a.Title, joinCategoryNames(ids, names))
	}
	runErr := llmqueue.RunParallel(ctx, conc, len(articles), process)
	rep.Summary(classified)
	return classified, runErr
}

// ClassifyKeywords runs only the strict keyword stage for one article. It
// never touches the LLM, so it is safe on the hot ingestion path where
// fetched articles must reach the database without waiting for a model.
// Returns nil when the keywords cannot place the article; the background
// startup pass and the audit buttons handle those via the LLM.
func (s *Service) ClassifyKeywords(a *models.Article) []uint {
	ids := s.Classify(a.Title, a.Summary, a.Lead, a.OriginURL, 1)
	if len(ids) > 0 && !s.isWeakOnly(a, ids) {
		return ids
	}
	return nil
}

// ClassifyOne runs the full classification pipeline for a single article and
// returns the category IDs plus the method that produced them ("keyword" or
// "llm"). An empty result with an empty method means "unclassified".
//
// It may call the LLM, so it must never run on the ingestion path — use
// ClassifyKeywords there and let the queued jobs fill the gaps. ctx is the
// job's context: cancelling it aborts the model call.
func (s *Service) ClassifyOne(ctx context.Context, a *models.Article) ([]uint, string) {
	// Stage 1: strict keyword matching. Require at least one non-weak keyword
	// hit so generic words like "政府" alone cannot place an article.
	if ids := s.ClassifyKeywords(a); len(ids) > 0 {
		return ids, "keyword"
	}

	// Stage 2: LLM fallback for articles the keywords could not place.
	if s.LLMConfigured() {
		callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		llmIDs, err := s.ClassifyWithLLM(callCtx, a.Title, a.Summary)
		cancel()
		if err == nil && len(llmIDs) > 0 {
			return llmIDs, "llm"
		}
		if err != nil {
			log.Printf("classifier: LLM fallback failed for article %d: %v", a.ID, err)
		}
	}
	return nil, ""
}

// isWeakOnly reports whether every matched category rests solely on weak
// keywords — the fingerprint of a misclassification.
func (s *Service) isWeakOnly(a *models.Article, ids []uint) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	content := strings.ToLower(a.Title + " " + a.Summary + " " + a.Lead)
	tokens := tokenize(content)
	hasCJK := false
	for _, r := range content {
		if r >= '\u4e00' && r <= '\u9fff' {
			hasCJK = true
			break
		}
	}

	for _, id := range ids {
		var rule *Rule
		for i := range s.rules {
			if s.rules[i].CategoryID == id {
				rule = &s.rules[i]
				break
			}
		}
		if rule == nil {
			continue
		}
		strongHit := false
		for _, kw := range rule.Keywords {
			if weakKeywords[kw] {
				continue
			}
			if hasCJK || isASCII(kw) {
				if keywordMatches(content, tokens, kw) {
					strongHit = true
					break
				}
			}
		}
		if strongHit {
			return false
		}
	}
	return true
}

// Classify returns category IDs that match the article content, best match
// first. A category needs `threshold` keyword hits (default 1).
//
// originURL is accepted for signature compatibility but is deliberately NOT part
// of the searched text: a feed's own URL contains module words ("military",
// "tech") that describe the feed, not the individual story.
func (s *Service) Classify(title, summary, lead, originURL string, threshold int) []uint {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if threshold <= 0 {
		threshold = 1
	}

	content := strings.ToLower(title + " " + summary + " " + lead)
	_ = originURL
	tokens := tokenize(content)

	// If the content has no Chinese characters, skip Chinese keyword matching
	// entirely. Chinese keywords are matched as substrings, so "历史" would
	// falsely match "history" / "historic" in English text.
	hasCJK := false
	for _, r := range content {
		if r >= '\u4e00' && r <= '\u9fff' {
			hasCJK = true
			break
		}
	}

	type score struct {
		catID uint
		score int
	}
	var scored []score

	for _, rule := range s.rules {
		hits := 0
		for _, kw := range rule.Keywords {
			if hasCJK || isASCII(kw) {
				if keywordMatches(content, tokens, kw) {
					hits++
				}
			}
			// Chinese keyword on purely-English content: skip (would be a
			// substring false-positive like "历史" → "history").
		}
		if hits >= threshold {
			scored = append(scored, score{rule.CategoryID, hits})
		}
	}

	// Sort by score descending (stable so equal scores keep category order).
	for i := 1; i < len(scored); i++ {
		for j := i; j > 0 && scored[j].score > scored[j-1].score; j-- {
			scored[j], scored[j-1] = scored[j-1], scored[j]
		}
	}

	ids := make([]uint, 0, len(scored))
	for _, s := range scored {
		ids = append(ids, s.catID)
	}
	return ids
}

// ClassifyWithLLM asks the model to place an article into one or more
// categories. It is the fallback for articles the keyword rules could not
// classify. The model returns category names; they are matched back to IDs by
// name (case-insensitive, either language).
func (s *Service) ClassifyWithLLM(ctx context.Context, title, summary string) ([]uint, error) {
	llm := s.nowLLM()
	if llm == nil {
		return nil, fmt.Errorf("no LLM provider configured")
	}

	var cats []models.Category
	if err := s.db.Find(&cats).Error; err != nil {
		return nil, err
	}
	if len(cats) == 0 {
		return nil, fmt.Errorf("no categories defined")
	}

	var b strings.Builder
	for _, c := range cats {
		b.WriteString("- ")
		b.WriteString(c.Name)
		if c.NameEn != "" {
			b.WriteString(" (")
			b.WriteString(c.NameEn)
			b.WriteString(")")
		}
		if c.Description != "" {
			b.WriteString(": ")
			b.WriteString(c.Description)
		}
		b.WriteString("\n")
	}
	catalog := b.String()

	systemPrompt := `You are a news categorization engine.
Given a list of categories and an article's title and summary, return the names of ALL categories the article belongs to.
Rules:
1. Return ONLY a JSON array of category names, no markdown fence, no explanation.
2. Use the exact category names from the list, in either Chinese or English.
3. An article can belong to multiple categories — return every one that fits.
4. If nothing fits, return an empty array [].
5. Be precise: do not return a category unless the article is clearly about it.`

	userPrompt := fmt.Sprintf("Categories:\n%s\n\nArticle title: %s\n\nArticle summary: %s", catalog, title, summary)

	reply, err := llm.Chat(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("LLM classify failed: %w", err)
	}

	names := parseCategoryNames(reply)
	if len(names) == 0 {
		return nil, nil
	}

	// Match returned names back to category IDs.
	nameToID := make(map[string]uint, len(cats)*2)
	for _, c := range cats {
		nameToID[strings.ToLower(c.Name)] = c.ID
		if c.NameEn != "" {
			nameToID[strings.ToLower(c.NameEn)] = c.ID
		}
	}

	ids := make([]uint, 0, len(names))
	seen := make(map[uint]bool)
	for _, n := range names {
		for _, cand := range nameCandidates(n) {
			if id, ok := nameToID[strings.ToLower(strings.TrimSpace(cand))]; ok && !seen[id] {
				ids = append(ids, id)
				seen[id] = true
				break
			}
		}
	}
	return ids, nil
}

// nameCandidates lists the lookup spellings for one model reply. Small models
// often echo the catalog line verbatim with the English gloss attached
// ("行业产业资讯 (Industry News)"), which matches neither the zh nor the en
// key on its own — so try the zh prefix and the parenthesised English too.
func nameCandidates(n string) []string {
	n = strings.TrimSpace(n)
	out := []string{n}
	if i := strings.Index(n, " ("); i > 0 {
		inner := strings.TrimSuffix(n[i+2:], ")")
		out = append(out, n[:i], inner)
	}
	return out
}

// parseCategoryNames extracts category names from the model's JSON array reply.
func parseCategoryNames(reply string) []string {
	s := strings.TrimSpace(reply)
	if s == "" {
		return nil
	}
	if m := markdownFenceClassify.FindStringSubmatch(s); m != nil {
		if inner := strings.TrimSpace(m[1]); inner != "" {
			s = inner
		}
	}
	if start := strings.Index(s, "["); start >= 0 {
		if end := strings.LastIndex(s, "]"); end > start {
			s = s[start : end+1]
		}
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err == nil {
		return out
	}
	// Fallback: split on commas and strip quotes. Small models also emit
	// unquoted arrays ("[证券市场, 宏观经济指标]"), so strip the brackets too.
	for _, part := range strings.Split(s, ",") {
		part = strings.Trim(part, ` "'“”[]`)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

var markdownFenceClassify = regexp.MustCompile("(?s)```\\w*\\n?(.*?)```")

// keywordMatches decides how a keyword is looked for.
//
// A Chinese keyword must be a substring: the language has no word separators, so
// token comparison would never match. An ASCII keyword must instead be a whole
// token — substring matching made "AI" hit "said", "chair" and "campaign",
// which is why nearly every article landed in the technology bucket.
func keywordMatches(content string, tokens []string, keyword string) bool {
	if isASCII(keyword) {
		return tokenMatch(tokens, keyword)
	}
	return strings.Contains(content, keyword)
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > unicode.MaxASCII {
			return false
		}
	}
	return true
}

// keywordSeparators splits a description into candidate terms. Whitespace alone
// is not enough: the Chinese descriptions are lists separated by full-width
// punctuation, and splitting only on spaces produced one unusable long token.
const keywordSeparators = " \t\n\r,，、;；:：/|()（）[]【】\"'“”‘’.。!！?？"

func splitWords(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		return strings.ContainsRune(keywordSeparators, r)
	})
}

func tokenize(content string) []string {
	// Tokens are ASCII word runs; that is all tokenMatch needs, because only
	// ASCII keywords go through it.
	var tokens []string
	var current strings.Builder
	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}
	for _, r := range content {
		if r <= unicode.MaxASCII &&
			((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '&' || r == '+') {
			current.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return tokens
}

// tokenMatch reports whether every word of the keyword appears as a standalone
// token. Multi-word keywords therefore need all their words present.
func tokenMatch(tokens []string, keyword string) bool {
	words := strings.Fields(keyword)
	if len(words) == 0 {
		return false
	}
	for _, w := range words {
		found := false
		for _, t := range tokens {
			if t == w {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// AuditItem is one article that may be misclassified: it matched a category on
// the strength of a single weak keyword.
type AuditItem struct {
	ArticleID  uint   `json:"articleId"`
	Title      string `json:"title"`
	CategoryID uint   `json:"categoryId"`
	Category   string `json:"category"`
	Keyword    string `json:"keyword"`
	HitCount   int    `json:"hitCount"`
}

// AuditCategoryStat summarises one category for the audit panel.
type AuditCategoryStat struct {
	CategoryID uint   `json:"categoryId"`
	Category   string `json:"category"`
	Total      int    `json:"total"`
	Weak       int    `json:"weak"` // articles that matched on a single weak keyword
}

// AuditCounts are the cheap per-page counters shown next to the three audit
// buttons, so the user can see how much work each button would start.
type AuditCounts struct {
	Total        int `json:"total"`        // live articles
	Unclassified int `json:"unclassified"` // no category link yet
}

// AuditResult is the full classification audit.
type AuditResult struct {
	Categories []AuditCategoryStat `json:"categories"`
	Suspects   []AuditItem         `json:"suspects"`
	Counts     AuditCounts         `json:"counts"`
}

// Counts returns the cheap counters for the audit buttons: two COUNT queries.
// Safe to poll from the settings page. Whether a job is running comes from the
// LLM queue status, not from the classifier.
func (s *Service) Counts() AuditCounts {
	var c AuditCounts
	var total, unclassified int64
	s.db.Model(&models.Article{}).Where("deleted_at IS NULL").Count(&total)
	s.db.Model(&models.Article{}).
		Where("deleted_at IS NULL").
		Where("NOT EXISTS (SELECT 1 FROM article_categories ac WHERE ac.article_id = articles.id)").
		Count(&unclassified)
	c.Total = int(total)
	c.Unclassified = int(unclassified)
	return c
}

// AuditCategories scans every article-category link and reports the ones that
// rest on a single weak keyword — the typical fingerprint of a misclassification
// (e.g. a securities story that mentions "政府" landing in Domestic Politics).
//
// Weak keywords are generic words that appear across many domains. They are
// still useful as a second signal, but alone they are not enough to place an
// article in a category.
var weakKeywords = map[string]bool{
	"政府": true, "政策": true, "法案": true, "改革": true, "监管": true,
	"市场": true, "经济": true, "企业": true, "公司": true, "行业": true,
	"government": true, "policy": true, "reform": true, "market": true,
	"economic": true, "company": true, "industry": true,
}

// AuditCategories returns per-category statistics and the list of suspect
// articles. It is read-only and safe to call from the settings page.
func (s *Service) AuditCategories() (AuditResult, error) {
	var result AuditResult

	var links []models.ArticleCategory
	if err := s.db.Find(&links).Error; err != nil {
		return result, err
	}
	if len(links) == 0 {
		result.Counts = s.Counts()
		return result, nil
	}

	// articleID -> article, categoryID -> category name
	articleIDs := make([]uint, 0, len(links))
	for _, l := range links {
		articleIDs = append(articleIDs, l.ArticleID)
	}
	var articles []models.Article
	if err := s.db.Where("id IN ?", articleIDs).Find(&articles).Error; err != nil {
		return result, err
	}
	articleByID := make(map[uint]*models.Article, len(articles))
	for i := range articles {
		articleByID[articles[i].ID] = &articles[i]
	}

	var categories []models.Category
	if err := s.db.Find(&categories).Error; err != nil {
		return result, err
	}
	catName := make(map[uint]string, len(categories))
	for _, c := range categories {
		catName[c.ID] = c.Name
	}

	// Per-category totals and weak-link counts.
	catTotal := make(map[uint]int)
	catWeak := make(map[uint]int)

	for _, l := range links {
		catTotal[l.CategoryID]++
		a := articleByID[l.ArticleID]
		if a == nil {
			continue
		}
		hits, weakHit := s.matchDetail(a, l.CategoryID)
		if hits == 1 && weakHit != "" {
			catWeak[l.CategoryID]++
			result.Suspects = append(result.Suspects, AuditItem{
				ArticleID:  a.ID,
				Title:      a.Title,
				CategoryID: l.CategoryID,
				Category:   catName[l.CategoryID],
				Keyword:    weakHit,
				HitCount:   hits,
			})
		}
	}

	for _, c := range categories {
		result.Categories = append(result.Categories, AuditCategoryStat{
			CategoryID: c.ID,
			Category:   c.Name,
			Total:      catTotal[c.ID],
			Weak:       catWeak[c.ID],
		})
	}
	result.Counts = s.Counts()
	return result, nil
}

// matchDetail reports how many keywords of one category an article matches and
// whether the only hit is a weak keyword.
func (s *Service) matchDetail(a *models.Article, categoryID uint) (int, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var rule *Rule
	for i := range s.rules {
		if s.rules[i].CategoryID == categoryID {
			rule = &s.rules[i]
			break
		}
	}
	if rule == nil {
		return 0, ""
	}

	content := strings.ToLower(a.Title + " " + a.Summary + " " + a.Lead)
	tokens := tokenize(content)
	hasCJK := false
	for _, r := range content {
		if r >= '\u4e00' && r <= '\u9fff' {
			hasCJK = true
			break
		}
	}

	hits := 0
	weakHit := ""
	for _, kw := range rule.Keywords {
		if hasCJK || isASCII(kw) {
			if keywordMatches(content, tokens, kw) {
				hits++
				if weakKeywords[kw] {
					weakHit = kw
				}
			}
		}
	}
	return hits, weakHit
}

// AuditClassifyUnclassified asks the LLM to classify every article that has no
// category yet. Articles already marked classified_by=llm are skipped so the
// model is not asked twice about the same story. It runs inside a queue job:
// ctx is the job's context, so cancelling the job stops it between articles.
func (s *Service) AuditClassifyUnclassified(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 500
	}
	var articles []models.Article
	err := s.db.
		Where("articles.deleted_at IS NULL").
		Where("NOT EXISTS (SELECT 1 FROM article_categories ac WHERE ac.article_id = articles.id)").
		Where("COALESCE(articles.classified_by, '') <> ?", "llm").
		Order("articles.fetched_at DESC").
		Limit(limit).
		Find(&articles).Error
	if err != nil {
		return 0, err
	}
	return s.classifyArticlesWithLLM(ctx, articles)
}

// AuditReclassifySuspects asks the LLM to re-classify articles the audit
// flagged as suspect (matched on a single weak keyword). The old keyword-based
// links are removed first so the model's answer replaces them. ctx is the
// queue job's context: cancelling the job stops it between articles.
func (s *Service) AuditReclassifySuspects(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 500
	}

	// Collect suspect article IDs from the audit.
	result, err := s.AuditCategories()
	if err != nil {
		return 0, err
	}
	if len(result.Suspects) == 0 {
		return 0, nil
	}

	suspectIDs := make([]uint, 0, len(result.Suspects))
	for _, s := range result.Suspects {
		suspectIDs = append(suspectIDs, s.ArticleID)
	}
	if len(suspectIDs) > limit {
		suspectIDs = suspectIDs[:limit]
	}

	var articles []models.Article
	if err := s.db.Where("id IN ?", suspectIDs).Find(&articles).Error; err != nil {
		return 0, err
	}
	if len(articles) == 0 {
		return 0, nil
	}

	// Cancelled before anything was destroyed: leave the keyword links alone.
	if ctx.Err() != nil {
		return 0, nil
	}

	// Remove the old keyword-based links so the LLM result replaces them.
	if err := s.db.Where("article_id IN ?", suspectIDs).
		Delete(&models.ArticleCategory{}).Error; err != nil {
		return 0, err
	}
	if err := s.db.Model(&models.Article{}).Where("id IN ?", suspectIDs).
		Update("classified_by", "").Error; err != nil {
		return 0, err
	}

	return s.classifyArticlesWithLLM(ctx, articles)
}

// classifyArticlesWithLLM runs the LLM over a batch of articles and stores the
// resulting links. It is shared by both audit jobs; ctx is the queue job's
// context, so a cancel stops it between articles and the work done so far is
// committed immediately (per article), which is also what lets the monitor
// feed and the unclassified counter move while the job is still running.
func (s *Service) classifyArticlesWithLLM(ctx context.Context, articles []models.Article) (int, error) {
	if s.nowLLM() == nil {
		return 0, fmt.Errorf("no LLM provider configured")
	}
	rep := llmqueue.ReporterFrom(ctx)
	names := s.categoryNames()
	conc := s.llmConcurrency()
	rep.SetTotal(len(articles))
	var mu sync.Mutex
	classified := 0
	processed := 0
	process := func(i int) {
		if ctx.Err() != nil {
			return // cancelled: this article never started
		}
		a := &articles[i]
		rep.ItemStart(a.ID, a.Title)
		defer func() {
			mu.Lock()
			processed++
			p := processed
			mu.Unlock()
			rep.SetProgress(p, len(articles))
		}()
		callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		ids, err := s.ClassifyWithLLM(callCtx, a.Title, a.Summary)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return // job cancelled mid-call: leave the article alone
			}
			log.Printf("classifier: LLM classify failed for article %d: %v", a.ID, err)
			rep.ItemError(a.ID, a.Title, err)
			return
		}
		if len(ids) == 0 {
			rep.ItemSkip(a.ID, a.Title)
			return
		}
		if err := s.insertLinks(a.ID, ids); err != nil {
			log.Printf("classifier: failed to store links for article %d: %v", a.ID, err)
			rep.ItemError(a.ID, a.Title, err)
			return
		}
		if err := s.db.Model(&models.Article{}).Where("id = ?", a.ID).
			Update("classified_by", "llm").Error; err != nil {
			log.Printf("classifier: failed to set classified_by for article %d: %v", a.ID, err)
		}
		mu.Lock()
		classified++
		mu.Unlock()
		rep.ItemDone(a.ID, a.Title, joinCategoryNames(ids, names))
	}
	runErr := llmqueue.RunParallel(ctx, conc, len(articles), process)
	rep.Summary(classified)
	return classified, runErr
}
