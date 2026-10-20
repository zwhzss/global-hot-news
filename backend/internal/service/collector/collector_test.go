package collector

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"ai-informations/internal/config"
	"ai-informations/internal/models"
	"ai-informations/internal/repository"
	"ai-informations/internal/service/classifier"
	"ai-informations/internal/service/datasource"
	"ai-informations/internal/service/filter"
	"ai-informations/internal/service/parser"
	"ai-informations/internal/service/robots"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// One collection cycle must visit EVERY active source.
//
// The concurrency permit used to be released with a `defer` placed inside the
// per-source loop body, which binds the defer to runCollectionCycle itself.
// Every permit was therefore held until the cycle ended, and with
// max_concurrency=3 the loop blocked forever on the fourth active source: the
// rest of the sources were never fetched and the scheduler never ticked again.
// In production only the three newest sources were ever collected, which is why
// none of the Chinese sources ever produced an article.
func TestRunCycleVisitsEveryActiveSource(t *testing.T) {
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

	cfg := config.DefaultConfig()
	// More sources than permits, so the old bug would hang on source #4.
	cfg.MaxConcurrency = 2

	const sourceCount = 6
	for i := 0; i < sourceCount; i++ {
		// A closed port: connection refused immediately, so the test is fast and
		// needs no network. A fetch log is still written for every attempt.
		src := &models.Source{
			Name:            fmt.Sprintf("src-%d", i),
			Type:            "rss",
			URL:             "http://127.0.0.1:1/feed.xml",
			IsActive:        true,
			RequestInterval: 1,
		}
		if err := repository.NewSourceRepository(db).Create(src); err != nil {
			t.Fatalf("create source: %v", err)
		}
	}
	// One inactive source must be skipped.
	//
	// Note the explicit Activate call: `IsActive` carries a `default:true` GORM
	// tag, so Create() silently drops a false value and the column default wins.
	inactive := &models.Source{Name: "inactive", Type: "rss", URL: "http://127.0.0.1:1/off.xml", IsActive: true}
	if err := repository.NewSourceRepository(db).Create(inactive); err != nil {
		t.Fatalf("create inactive source: %v", err)
	}
	if err := repository.NewSourceRepository(db).Activate(inactive.ID, false); err != nil {
		t.Fatalf("deactivate source: %v", err)
	}

	dsService := datasource.NewService(db, robots.NewParser(), parser.NewFactory(), filter.NewService(db), classifier.NewService(db), cfg)
	svc := NewService(db, dsService, filter.NewService(db), cfg)
	svc.SetJitterMax(0)

	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.RunCycle()
	}()

	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("RunCycle did not finish: a permit was never released (semaphore deadlock)")
	}

	var logs int64
	if err := db.Model(&models.FetchLog{}).Count(&logs).Error; err != nil {
		t.Fatalf("count fetch logs: %v", err)
	}
	if int(logs) != sourceCount {
		t.Fatalf("fetch logs = %d, want %d: only the first sources were collected", logs, sourceCount)
	}

	seen := map[uint]bool{}
	var entries []models.FetchLog
	if err := db.Find(&entries).Error; err != nil {
		t.Fatalf("load fetch logs: %v", err)
	}
	for _, e := range entries {
		if e.SourceID == inactive.ID {
			t.Fatal("an inactive source was collected")
		}
		seen[e.SourceID] = true
	}
	if len(seen) != sourceCount {
		t.Fatalf("cycle reached %d distinct sources, want %d", len(seen), sourceCount)
	}
}
