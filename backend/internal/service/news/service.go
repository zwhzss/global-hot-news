package news

import (
	"ai-informations/internal/models"
	"ai-informations/internal/repository"

	"gorm.io/gorm"
)

// Service handles news/article queries and management.
type Service struct {
	db *gorm.DB
	ar *repository.ArticleRepository
	cr *repository.CategoryRepository
}

func NewService(db *gorm.DB) *Service {
	return &Service{
		db: db,
		ar: repository.NewArticleRepository(db),
		cr: repository.NewCategoryRepository(db),
	}
}

// List returns one page of articles for the given filter/sort request.
func (s *Service) List(q models.NewsQuery) ([]models.Article, int64, error) {
	return s.ar.Query(q)
}

// GetByID returns a single article by ID.
func (s *Service) GetByID(id uint) (*models.Article, error) {
	return s.ar.Get(id)
}

// ListByCategory returns articles for a specific category slug.
func (s *Service) ListByCategory(categorySlug string, page, pageSize int) ([]models.Article, int64, error) {
	return s.ar.Query(models.NewsQuery{Page: page, PageSize: pageSize, CategorySlug: categorySlug})
}

// ListBySource returns articles from a specific source.
func (s *Service) ListBySource(sourceID uint, page, pageSize int) ([]models.Article, int64, error) {
	return s.ar.Query(models.NewsQuery{Page: page, PageSize: pageSize, SourceID: sourceID})
}

// AvailableDates returns the local dates that actually contain articles, newest
// first, for the date filter in the UI.
func (s *Service) AvailableDates(limit int) ([]string, error) {
	return s.ar.DistinctDates(limit)
}

// ListCategories returns all categories.
func (s *Service) ListCategories() ([]models.Category, error) {
	return s.cr.ListAll()
}

// GetCategoryBySlug returns a category by slug.
func (s *Service) GetCategoryBySlug(slug string) (*models.Category, error) {
	return s.cr.GetBySlug(slug)
}
