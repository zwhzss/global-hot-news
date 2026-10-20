package collector

import (
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"time"

	"ai-informations/internal/config"
	"ai-informations/internal/models"
	"ai-informations/internal/repository"
	"ai-informations/internal/service/datasource"

	"gorm.io/gorm"
)

// Service manages the collection scheduler and task queue.
type Service struct {
	db            *gorm.DB
	dsService     *datasource.Service
	filterService interface{ RefreshRules() }
	cfg           *config.Config
	sourceRepo    *repository.SourceRepository
	stopCh        chan struct{}
	wg            sync.WaitGroup
	semaphore     chan struct{}
	// jitterMax bounds the random pre-request delay that keeps crawling gentle.
	jitterMax time.Duration
	// cycleHook runs at the end of every collection cycle, receiving the IDs of
	// the articles fetched during it. It is how the hot-rank snapshot stays
	// fresh and how the TTS round for this cycle's new articles starts, both
	// without the scheduler knowing about them.
	cycleHook func(newArticleIDs []uint)
}

// SetCycleHook registers a callback invoked after each collection cycle with
// the IDs of the articles fetched during that cycle.
func (s *Service) SetCycleHook(hook func(newArticleIDs []uint)) {
	s.cycleHook = hook
}

// SetJitterMax overrides the pre-request delay bound. Tests set it to 0 so a
// cycle does not sleep for seconds per source.
func (s *Service) SetJitterMax(d time.Duration) {
	s.jitterMax = d
}

func NewService(db *gorm.DB, dsService *datasource.Service, filterService interface{ RefreshRules() }, cfg *config.Config) *Service {
	maxConcurrency := cfg.MaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = 3
	}
	return &Service{
		db:            db,
		dsService:     dsService,
		filterService: filterService,
		cfg:           cfg,
		sourceRepo:    repository.NewSourceRepository(db),
		stopCh:        make(chan struct{}),
		semaphore:     make(chan struct{}, maxConcurrency),
		jitterMax:     5 * time.Second,
	}
}

// Start begins the collection scheduler.
func (s *Service) Start() {
	s.wg.Add(1)
	go s.runScheduler()
}

// Stop signals the scheduler to stop and waits up to timeout for graceful shutdown.
// If timeout is 0, it signals only and returns immediately (non-blocking).
func (s *Service) Stop() {
	select {
	case <-s.stopCh:
		return // already stopped
	default:
	}
	close(s.stopCh)
	// Wait in background to avoid blocking the UI thread during window close.
	// The goroutine will exit after finishing its current fetch cycle.
	go s.wg.Wait()
}

// RunCycle triggers one collection cycle immediately.
func (s *Service) RunCycle() {
	s.runCollectionCycle()
}

func (s *Service) runScheduler() {
	defer s.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			buf := make([]byte, 4096)
			n := runtime.Stack(buf, false)
			fmt.Printf("Collector scheduler recovered from panic: %v\n%s\n", r, buf[:n])
		}
	}()

	// Run initial fetch immediately
	s.runCollectionCycle()

	// Schedule periodic collection
	ticker := time.NewTicker(time.Duration(s.cfg.DefaultInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.runCollectionCycle()
		}
	}
}

// collectSource fetches one source and writes its fetch log.
//
// The concurrency permit is taken and released *here*, not with a `defer` inside
// runCollectionCycle's loop. That defer belonged to the enclosing function, so
// every permit was held until the whole cycle finished: with max_concurrency=3
// the loop blocked forever on the fourth active source and the scheduler never
// ticked again. In practice only the first three sources (the newest, thanks to
// "created_at DESC") were ever collected.
func (s *Service) collectSource(src models.Source) []uint {
	s.semaphore <- struct{}{}
	defer func() { <-s.semaphore }()

	// Gentle crawling: a small random delay before each request (需求.md rule 4).
	if s.jitterMax > 0 {
		select {
		case <-s.stopCh:
			return nil
		case <-time.After(time.Duration(rand.Int63n(int64(s.jitterMax)))):
		}
	}

	startTime := time.Now()
	ids, err := s.dsService.FetchAndProcess(src.ID)
	durationMs := int(time.Since(startTime).Milliseconds())

	logEntry := &models.FetchLog{
		SourceID:   src.ID,
		Status:     "success",
		Message:    "",
		ItemCount:  len(ids),
		DurationMs: durationMs,
		CreatedAt:  time.Now(),
	}
	if err != nil {
		logEntry.Status = "error"
		logEntry.Message = err.Error()
	}
	s.db.Create(logEntry)
	return ids
}

func (s *Service) runCollectionCycle() {
	sources, err := s.sourceRepo.List()
	if err != nil {
		fmt.Printf("Failed to list sources: %v\n", err)
		return
	}

	var newArticleIDs []uint
	for _, src := range sources {
		select {
		case <-s.stopCh:
			return
		default:
		}

		if !src.IsActive {
			continue
		}

		newArticleIDs = append(newArticleIDs, s.collectSource(src)...)
	}

	if s.cycleHook != nil {
		s.cycleHook(newArticleIDs)
	}

	// Run TTL cleanup periodically
	if time.Now().Hour()%6 == 0 {
		s.runTTLCleanup()
	}
}

func (s *Service) runTTLCleanup() {
	expireBefore := time.Now().AddDate(0, 0, -s.cfg.ArticleTTLDays)
	count, err := repository.NewArticleRepository(s.db).DeleteExpired(expireBefore)
	if err != nil {
		fmt.Printf("TTL cleanup error: %v\n", err)
		return
	}
	if count > 0 {
		fmt.Printf("Cleaned up %d expired articles\n", count)
	}
}
