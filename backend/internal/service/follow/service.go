package follow

import (
	"ai-informations/internal/models"
	"ai-informations/internal/repository"

	"gorm.io/gorm"
)

// Service manages user follows (articles and sources).
type Service struct {
	db         *gorm.DB
	followRepo *repository.FollowRepository
	arRepo     *repository.ArticleRepository
}

func NewService(db *gorm.DB) *Service {
	return &Service{
		db:         db,
		followRepo: repository.NewFollowRepository(db),
		arRepo:     repository.NewArticleRepository(db),
	}
}

// FollowArticle adds an article to followed items.
func (s *Service) FollowArticle(articleID uint) error {
	exists, _ := s.followRepo.IsFollowing("article", articleID)
	if exists {
		return nil // Already following
	}
	return s.followRepo.Add(&models.Follow{FollowType: "article", TargetID: articleID})
}

// UnfollowArticle removes an article from followed items.
func (s *Service) UnfollowArticle(articleID uint) error {
	var follow models.Follow
	if err := s.db.Where("follow_type = ? AND target_id = ?", "article", articleID).First(&follow).Error; err != nil {
		return err
	}
	return s.followRepo.Remove(follow.ID)
}

// FollowSource adds a source to followed items.
func (s *Service) FollowSource(sourceID uint) error {
	exists, _ := s.followRepo.IsFollowing("source", sourceID)
	if exists {
		return nil
	}
	return s.followRepo.Add(&models.Follow{FollowType: "source", TargetID: sourceID})
}

// UnfollowSource removes a source from followed items.
func (s *Service) UnfollowSource(sourceID uint) error {
	var follow models.Follow
	if err := s.db.Where("follow_type = ? AND target_id = ?", "source", sourceID).First(&follow).Error; err != nil {
		return err
	}
	return s.followRepo.Remove(follow.ID)
}

// IsArticleFollowed checks if an article is followed.
func (s *Service) IsArticleFollowed(articleID uint) (bool, error) {
	return s.followRepo.IsFollowing("article", articleID)
}

// IsSourceFollowed checks if a source is followed.
func (s *Service) IsSourceFollowed(sourceID uint) (bool, error) {
	return s.followRepo.IsFollowing("source", sourceID)
}

// GetFollowedArticles returns paginated followed articles.
//
// Kept for the existing binding; SearchFollowed is the one the UI uses because
// it carries the full filter/sort request.
func (s *Service) GetFollowedArticles(page, pageSize int) ([]models.Article, int64, error) {
	return s.SearchFollowed(models.NewsQuery{Page: page, PageSize: pageSize})
}

// SearchFollowed returns followed articles honouring the same date/title/sort
// filters as every other board. FollowOnly is forced on so a caller cannot
// accidentally turn this into a general article query.
func (s *Service) SearchFollowed(q models.NewsQuery) ([]models.Article, int64, error) {
	q.FollowOnly = true
	return s.arRepo.Query(q)
}

// GetFollowedSources returns all followed sources.
func (s *Service) GetFollowedSources() ([]models.Source, error) {
	return s.followRepo.ListSources()
}

// RecordBehavior records a user browsing behavior event.
func (s *Service) RecordBehavior(articleID, sourceID uint, stayMs int, clickedURL bool) error {
	return repository.NewUserBehaviorRepository(s.db).Record(&models.UserBehavior{
		ArticleID:  articleID,
		SourceID:   sourceID,
		StayMs:     stayMs,
		ClickedURL: clickedURL,
	})
}
