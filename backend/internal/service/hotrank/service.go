package hotrank

import (
	"errors"
	"time"

	"ai-informations/internal/models"
	"ai-informations/internal/repository"

	"gorm.io/gorm"
)

// Service manages hot ranking calculations and snapshots.
type Service struct {
	db     *gorm.DB
	hrRepo *repository.HotRankRepository
	arRepo *repository.ArticleRepository
}

func NewService(db *gorm.DB) *Service {
	return &Service{
		db:     db,
		hrRepo: repository.NewHotRankRepository(db),
		arRepo: repository.NewArticleRepository(db),
	}
}

// CalculateAndSave recalculates hot ranks for all articles and saves snapshots.
func (s *Service) CalculateAndSave() error {
	// Get all articles with their source weights
	var articles []models.Article
	if err := s.db.Order("fetched_at DESC").Limit(500).Find(&articles).Error; err != nil {
		return err
	}

	if len(articles) == 0 {
		return nil
	}

	// Calculate scores: score = (N-rank+1)/N * source_weight * time_decay
	now := time.Now()
	ranks := make([]models.HotRank, 0, len(articles))

	for i, article := range articles {
		rank := i + 1
		N := len(articles)

		// Time decay: articles fresher get higher score
		ageHours := now.Sub(article.FetchedAt).Hours()
		timeDecay := 1.0 / (1.0 + ageHours/24.0) // Half-life ~24 hours

		// Source weight (simple uniform for now, can be customized per source)
		sourceWeight := 1.0

		score := float64(N-rank+1) / float64(N) * sourceWeight * timeDecay

		ranks = append(ranks, models.HotRank{
			ArticleID: article.ID,
			Rank:      rank,
			Score:     score,
			SourceID:  article.SourceID,
			FetchedAt: now,
		})
	}

	// Clear old ranks and insert new ones
	s.db.Exec("DELETE FROM hot_ranks")
	return s.db.Create(&ranks).Error
}

// GetTop returns the top N hot ranking items.
func (s *Service) GetTop(limit int) ([]models.HotRank, error) {
	return s.hrRepo.ListTop(limit)
}

// Search returns one page of hot-ranked articles that match the filter, so the
// hot-rank board supports the same date/title filters as every other board.
func (s *Service) Search(q models.NewsQuery) ([]models.RankWithArticle, int64, error) {
	return s.hrRepo.QueryTop(q)
}

// EnsureFresh recalculates the snapshot when it is empty or older than maxAge.
// Called after each collection cycle so the hot-rank board is populated without
// the user having to press "Recalculate".
func (s *Service) EnsureFresh(maxAge time.Duration) error {
	var latest models.HotRank
	err := s.db.Order("fetched_at DESC").First(&latest).Error
	if err == nil && time.Since(latest.FetchedAt) < maxAge {
		return nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return s.CalculateAndSave()
}

// GetTopWithArticles returns top rankings with full article details.
func (s *Service) GetTopWithArticles(limit int) ([]models.RankWithArticle, error) {
	ranks, err := s.hrRepo.ListTop(limit)
	if err != nil {
		return nil, err
	}

	var ranked []models.RankWithArticle
	for _, hr := range ranks {
		var article models.Article
		if err := s.db.Preload("Categories").First(&article, hr.ArticleID).Error; err != nil {
			continue
		}
		ranked = append(ranked, models.RankWithArticle{HotRank: hr, Article: article})
	}
	return ranked, nil
}
