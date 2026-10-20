package recommend

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"time"

	"ai-informations/internal/models"
	"ai-informations/internal/repository"

	"gorm.io/gorm"
)

// candidateLimit caps how many recent articles are scored in memory for the
// personalised path. Scoring needs the category relation of every candidate,
// so an unbounded scan would be wasteful once the table grows; 300 recent
// articles is far more than a recommendation page can ever show.
const candidateLimit = 300

// Service handles personalized recommendation logic.
type Service struct {
	db       *gorm.DB
	articles *repository.ArticleRepository
}

// NewService creates a new recommendation service.
func NewService(db *gorm.DB) *Service {
	return &Service{db: db, articles: repository.NewArticleRepository(db)}
}

// Search returns recommended articles for the given filter/sort request.
//
// Cold start (no browsing behaviour recorded yet) returns the hot list, which is
// what the previous implementation did; the difference is that every filter from
// the filter bar is applied to it now, and the article relation is loaded.
func (s *Service) Search(q models.NewsQuery) ([]models.Article, int64, error) {
	q.Normalize()

	behaviors, err := repository.NewUserBehaviorRepository(s.db).GetRecent(20)
	if err != nil || len(behaviors) == 0 {
		cold := q
		// Default the cold-start board to the hot list unless the caller asked
		// for something else explicitly.
		if cold.SortBy != models.SortByTime {
			cold.SortBy = models.SortByHot
		}
		return s.articles.Query(cold)
	}

	// Personalised path: pull a bounded, filtered candidate set and score it.
	candidates, err := s.articles.QueryLimited(models.NewsQuery{
		CategorySlug: q.CategorySlug,
		SourceID:     q.SourceID,
		FollowOnly:   q.FollowOnly,
		TitleKeyword: q.TitleKeyword,
		DateFrom:     q.DateFrom,
		DateTo:       q.DateTo,
		SortBy:       models.SortByTime,
	}, candidateLimit)
	if err != nil {
		return nil, 0, err
	}

	categoryWeights := s.categoryWeights(behaviors)
	sourceWeights := s.sourceWeights(behaviors)

	type scored struct {
		article models.Article
		score   float64
	}
	ranked := make([]scored, 0, len(candidates))
	for _, a := range candidates {
		if alreadyRead(a.ID, behaviors) {
			continue
		}
		ranked = append(ranked, scored{article: a, score: s.score(a, categoryWeights, sourceWeights)})
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score == ranked[j].score {
			// Equal score: newest first, so the order is deterministic.
			return ranked[i].article.FetchedAt.After(ranked[j].article.FetchedAt)
		}
		return ranked[i].score > ranked[j].score
	})

	total := int64(len(ranked))
	start := (q.Page - 1) * q.PageSize
	if start >= len(ranked) {
		return []models.Article{}, total, nil
	}
	end := start + q.PageSize
	if end > len(ranked) {
		end = len(ranked)
	}

	out := make([]models.Article, 0, end-start)
	for _, sa := range ranked[start:end] {
		out = append(out, sa.article)
	}
	return out, total, nil
}

// GetRecommendations keeps the original binding signature working.
func (s *Service) GetRecommendations(page, pageSize int) ([]models.Article, int64, error) {
	return s.Search(models.NewsQuery{Page: page, PageSize: pageSize, SortBy: models.SortByHot})
}

func alreadyRead(articleID uint, behaviors []models.UserBehavior) bool {
	for _, b := range behaviors {
		if b.ArticleID == articleID {
			return true
		}
	}
	return false
}

// categoryWeights sums the time spent on each category. Category ids are used
// as string keys because that is what the profile JSON exposes.
func (s *Service) categoryWeights(behaviors []models.UserBehavior) map[string]float64 {
	weights := make(map[string]float64)
	ids := make([]uint, 0, len(behaviors))
	for _, b := range behaviors {
		if b.ArticleID != 0 {
			ids = append(ids, b.ArticleID)
		}
	}
	if len(ids) == 0 {
		return weights
	}

	// One query for every category link of every recent article, instead of a
	// query per behaviour.
	var links []models.ArticleCategory
	s.db.Where("article_id IN ?", ids).Find(&links)

	perArticle := make(map[uint]float64, len(behaviors))
	for _, b := range behaviors {
		w := float64(b.StayMs) / 1000.0
		if b.ClickedURL {
			w += 2.0
		}
		perArticle[b.ArticleID] += w
	}
	for _, l := range links {
		weights[strconv.FormatUint(uint64(l.CategoryID), 10)] += perArticle[l.ArticleID]
	}
	return weights
}

// sourceWeights sums the time spent per source.
func (s *Service) sourceWeights(behaviors []models.UserBehavior) map[string]float64 {
	weights := make(map[string]float64)
	for _, b := range behaviors {
		// strconv, not string(rune(id)): the previous conversion produced a
		// control character key, which "worked" only by accident.
		key := strconv.FormatUint(uint64(b.SourceID), 10)
		weights[key] += float64(b.StayMs) / 1000.0
		if b.ClickedURL {
			weights[key] += 2.0
		}
	}
	return weights
}

// score combines category affinity (60%) with source affinity (40%) and decays
// with age so a stale but well-matched article does not outrank today's news.
func (s *Service) score(a models.Article, categoryWeights, sourceWeights map[string]float64) float64 {
	catScore := 0.0
	for _, c := range a.Categories {
		catScore += categoryWeights[strconv.FormatUint(uint64(c.ID), 10)]
	}
	srcScore := sourceWeights[strconv.FormatUint(uint64(a.SourceID), 10)]

	// Normalize the unbounded time sums into 0..1 so the weights stay comparable.
	normalize := func(v float64) float64 { return 1 - math.Exp(-v/30.0) }

	base := normalize(catScore)*0.6 + normalize(srcScore)*0.4
	// A brand-new visitor still gets a sensible order: no affinity means the
	// freshness term decides.
	if base == 0 {
		base = 0.05
	}

	ageHours := time.Since(a.FetchedAt).Hours()
	if ageHours < 0 {
		ageHours = 0
	}
	timeDecay := 1.0 / (1.0 + ageHours/12.0)

	return base * timeDecay
}

// GetProfile returns the current user profile weights as JSON.
func (s *Service) GetProfile() (string, error) {
	behaviors, err := repository.NewUserBehaviorRepository(s.db).GetRecent(50)
	if err != nil {
		return "{}", nil
	}
	profile := map[string]interface{}{
		"categoryWeights": s.categoryWeights(behaviors),
		"sourceWeights":   s.sourceWeights(behaviors),
		"behaviorCount":   len(behaviors),
	}
	data, err := json.Marshal(profile)
	if err != nil {
		return "{}", err
	}
	return string(data), nil
}

// ResetProfile clears all user behavior data and profile.
func (s *Service) ResetProfile() error {
	s.db.Exec("DELETE FROM user_behaviors")
	s.db.Exec("DELETE FROM user_profiles")
	return nil
}
