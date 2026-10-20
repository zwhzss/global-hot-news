package datasource

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"ai-informations/internal/config"
	"ai-informations/internal/models"
	"ai-informations/internal/repository"
	"ai-informations/internal/service/classifier"
	"ai-informations/internal/service/filter"
	"ai-informations/internal/service/parser"
	"ai-informations/internal/service/robots"
	"ai-informations/internal/service/translator"

	"gorm.io/gorm"
)

// Service manages news data sources.
type Service struct {
	db            *gorm.DB
	robotParser   *robots.Parser
	parserFactory *parser.Factory
	filterService *filter.Service
	classifier    *classifier.Service
	cfg           *config.Config
	sourceRepo    *repository.SourceRepository
	artCatRepo    *repository.ArticleCategoryRepository
}

func NewService(db *gorm.DB, robotParser *robots.Parser, parserFactory *parser.Factory, filterService *filter.Service, classifier *classifier.Service, cfg *config.Config) *Service {
	return &Service{
		db:            db,
		robotParser:   robotParser,
		parserFactory: parserFactory,
		filterService: filterService,
		classifier:    classifier,
		cfg:           cfg,
		sourceRepo:    repository.NewSourceRepository(db),
		artCatRepo:    repository.NewArticleCategoryRepository(db),
	}
}

// List returns all data sources.
func (s *Service) List() ([]models.Source, error) {
	return s.sourceRepo.List()
}

// Get returns a single data source by ID.
func (s *Service) Get(id uint) (*models.Source, error) {
	return s.sourceRepo.Get(id)
}

// Create adds a new data source.
func (s *Service) Create(src *models.Source) error {
	src.Status = "unknown"
	src.IsActive = true
	return s.sourceRepo.Create(src)
}

// Update modifies an existing data source.
func (s *Service) Update(src *models.Source) error {
	return s.sourceRepo.Update(src)
}

// Delete removes a data source.
func (s *Service) Delete(id uint) error {
	return s.sourceRepo.Delete(id)
}

// ToggleActive activates or deactivates a data source.
func (s *Service) ToggleActive(id uint, active bool) error {
	return s.sourceRepo.Activate(id, active)
}

// SetPublisher updates only the publisher field of a source.
func (s *Service) SetPublisher(id uint, publisher string) error {
	return s.db.Model(&models.Source{}).Where("id = ?", id).Update("publisher", publisher).Error
}

// TestConnectivity tests whether a data source can be reached and returns sample items.
func (s *Service) TestConnectivity(id uint) (*models.TestResult, error) {
	src, err := s.sourceRepo.Get(id)
	if err != nil {
		return nil, fmt.Errorf("source not found: %w", err)
	}
	if src.URL == "" {
		return &models.TestResult{Available: false, Message: "Source URL is empty"}, nil
	}

	allowed, err := s.robotParser.IsAllowed(src.URL)
	if err != nil {
		return &models.TestResult{Available: false, Message: fmt.Sprintf("robots.txt check failed: %v", err)}, nil
	}
	if !allowed {
		return &models.TestResult{Available: false, Message: "robots.txt disallows access to this URL"}, nil
	}

	adapter, err := s.parserFactory.Parse(src.Type)
	if err != nil {
		return &models.TestResult{Available: false, Message: fmt.Sprintf("parser creation failed: %v", err)}, nil
	}

	items, err := adapter.Fetch(src.URL)
	if err != nil {
		return &models.TestResult{Available: false, Message: fmt.Sprintf("fetch failed: %v", err)}, nil
	}

	var samples []models.RawNewsItem
	filtered := 0
	for _, item := range items {
		passed, _, _ := s.filterService.Check(item, id)
		if passed {
			samples = append(samples, item)
			if len(samples) >= 3 {
				break
			}
		} else {
			filtered++
		}
	}

	msg := fmt.Sprintf("Connected successfully, fetched %d items", len(items))
	if filtered > 0 {
		msg += fmt.Sprintf(" (%d filtered by compliance rules)", filtered)
	}

	return &models.TestResult{
		Available: true,
		Message:   msg,
		Samples:   samples,
	}, nil
}

// FetchAndProcess runs one collection cycle for a specific source and returns
// the IDs of the articles it inserted (empty when everything was filtered or
// already known). The IDs feed the TTS round that follows each cycle.
func (s *Service) FetchAndProcess(id uint) ([]uint, error) {
	src, err := s.sourceRepo.Get(id)
	if err != nil {
		return nil, err
	}

	allowed, err := s.robotParser.IsAllowed(src.URL)
	if err != nil {
		s.sourceRepo.UpdateStatus(id, "error", fmt.Sprintf("robots check failed: %v", err))
		return nil, err
	}
	if !allowed {
		s.sourceRepo.UpdateStatus(id, "robots_blocked", "robots.txt prohibits access")
		return nil, fmt.Errorf("robots.txt blocks this URL")
	}

	adapter, err := s.parserFactory.Parse(src.Type)
	if err != nil {
		s.sourceRepo.UpdateStatus(id, "error", fmt.Sprintf("parser error: %v", err))
		return nil, err
	}

	_, _ = time.Now(), 0
	items, err := adapter.Fetch(src.URL)
	if err != nil {
		s.sourceRepo.UpdateStatus(id, "error", err.Error())
		return nil, err
	}

	filtered := 0
	now := time.Now()
	expireAt := now.AddDate(0, 0, s.cfg.ArticleTTLDays)
	newArticles := make([]*models.Article, 0, len(items))

	for _, item := range items {
		passed, reasonCode, hash := s.filterService.Check(item, id)
		if !passed {
			filtered++
			_ = reasonCode
			_ = hash
			continue
		}

		fp := computeFingerprint(item)
		var exists int64
		s.db.Model(&models.Article{}).Where("fingerprint = ? OR origin_url = ?", fp, normalizeURL(item.OriginURL)).Count(&exists)
		if exists > 0 {
			continue
		}

		if item.PublishTime.IsZero() {
			// Safety net: the parser should have already discarded items with no
			// date, but a custom source or a future code path might produce one.
			// Storing it would make the article appear under "today" regardless of
			// when it was actually published, which is the bug we are fixing here.
			filtered++
			continue
		}
		newArticles = append(newArticles, &models.Article{
			SourceID:    id,
			Title:       item.Title,
			Summary:     truncate(item.Summary, 2000),
			Lead:        truncate(item.Lead, 500),
			OriginURL:   normalizeURL(item.OriginURL),
			Publisher:   s.resolvePublisher(id, item.Publisher),
			PublishTime: item.PublishTime,
			FetchedAt:   now,
			Fingerprint: fp,
			ExpireAt:    expireAt,
			Locations:   toJSON(item.Locations),
			Persons:     toJSON(item.Persons),
			Events:      toJSON(item.Events),
		})
		// Tag the original language and copy the original text into the matching
		// side of the bilingual pair, so the UI can render one language
		// immediately and the translator only has one side left to fill.
		translator.EnsureOriginal(newArticles[len(newArticles)-1])
	}

	var artIDs []uint
	if len(newArticles) > 0 {
		if err := s.db.Create(&newArticles).Error; err != nil {
			s.sourceRepo.UpdateStatus(id, "error", fmt.Sprintf("insert failed: %v", err))
			return nil, err
		}

		// Classify articles into categories
		artIDs = make([]uint, len(newArticles))
		for i, a := range newArticles {
			artIDs[i] = a.ID
		}
		if err := s.classifyArticles(newArticles, id); err != nil {
			fmt.Printf("Warning: failed to classify articles: %v\n", err)
		}
	}

	s.sourceRepo.UpdateStatus(id, "active", "")
	s.sourceRepo.IncrementFetchCount(id)
	// NOTE: must be a targeted update. Passing &models.Source{Model: gorm.Model{ID: id}}
	// to the full-row Update/Save call blanked name/type/url and reset created_at,
	// which is what created the endless "blank source" rows.
	s.sourceRepo.TouchLastFetchedAt(id, time.Now())

	return artIDs, nil
}

// classifyArticles assigns categories to newly fetched articles based on
// content. Keyword matching only: the hot ingestion path must never wait on
// the LLM (a local model may be busy translating, and each call can take
// tens of seconds). Articles the keywords cannot place stay unclassified and
// are picked up later by the background startup pass and the audit buttons.
func (s *Service) classifyArticles(articles []*models.Article, sourceID uint) error {
	var batch []models.ArticleCategory
	for _, a := range articles {
		ids := s.classifier.ClassifyKeywords(a)
		for _, catID := range ids {
			batch = append(batch, models.ArticleCategory{
				ArticleID:  a.ID,
				CategoryID: catID,
			})
		}
		if len(ids) > 0 {
			if err := s.db.Model(&models.Article{}).Where("id = ?", a.ID).
				Update("classified_by", "keyword").Error; err != nil {
				log.Printf("datasource: failed to set classified_by for article %d: %v", a.ID, err)
			}
		}
	}
	if len(batch) > 0 {
		return s.artCatRepo.SaveBatch(batch)
	}
	return nil
}

func computeFingerprint(item models.RawNewsItem) string {
	content := strings.ToLower(item.Title + " " + item.Summary)
	h := sha256.New()
	h.Write([]byte(content))
	return fmt.Sprintf("%x", h.Sum(nil)[:16])
}

func normalizeURL(rawURL string) string {
	url := strings.TrimSpace(rawURL)
	parts := strings.SplitN(url, "?", 2)
	if len(parts) > 1 {
		params := strings.Split(parts[1], "&")
		var kept []string
		for _, p := range params {
			key := strings.SplitN(p, "=", 2)[0]
			if key == "utm_source" || key == "utm_medium" || key == "utm_campaign" ||
				key == "utm_content" || key == "utm_term" || key == "fbclid" || key == "gclid" {
				continue
			}
			kept = append(kept, p)
		}
		if len(kept) > 0 {
			url = parts[0] + "?" + strings.Join(kept, "&")
		} else {
			url = parts[0]
		}
	}
	url = strings.TrimRight(url, "/")
	if url == "" {
		url = "/"
	}
	return url
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// resolvePublisher makes sure every article carries a provenance label. The
// RSS <source> tag is often empty, so fall back to the source's own publisher
// name and finally to the source name itself — an article without any
// provenance is worse than one with a slightly less specific label.
func (s *Service) resolvePublisher(sourceID uint, itemPublisher string) string {
	if strings.TrimSpace(itemPublisher) != "" {
		return strings.TrimSpace(itemPublisher)
	}
	var src models.Source
	if err := s.db.Select("publisher", "name").Where("id = ?", sourceID).First(&src).Error; err != nil {
		return ""
	}
	if strings.TrimSpace(src.Publisher) != "" {
		return strings.TrimSpace(src.Publisher)
	}
	return strings.TrimSpace(src.Name)
}

func toJSON(v interface{}) string {
	data, _ := json.Marshal(v)
	return string(data)
}
