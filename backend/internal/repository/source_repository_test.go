package repository

import (
	"path/filepath"
	"testing"
	"time"

	"ai-informations/internal/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// These tests guard the data-source write paths. A full-row `Save` on a partial
// struct used to blank name/type/url and reset created_at to the Go zero time on
// every fetch, after which SeedDefaultSources re-inserted the same defaults on
// the next launch — an unbounded growth of unusable `sources` rows.

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	sqlDB, err := db.DB()
	if err == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func createSource(t *testing.T, db *gorm.DB, name, url string) *models.Source {
	t.Helper()
	src := &models.Source{Name: name, Type: "rss", URL: url, IsActive: true, Status: "unknown"}
	if err := NewSourceRepository(db).Create(src); err != nil {
		t.Fatalf("create source: %v", err)
	}
	return src
}

func ptrTime(t time.Time) *time.Time { return &t }

// TestSourceUpdate_RejectsPartialStruct is the regression guard: the exact call
// the collector used to make must fail instead of wiping the row.
func TestSourceUpdate_RejectsPartialStruct(t *testing.T) {
	db := newTestDB(t)
	repo := NewSourceRepository(db)
	src := createSource(t, db, "国家统计局", "https://example.com/rss.xml")

	before := *src
	err := repo.Update(&models.Source{Model: gorm.Model{ID: src.ID}, LastFetchedAt: ptrTime(time.Now())})
	if err == nil {
		t.Fatal("partial struct Update must be rejected, got nil error")
	}

	var after models.Source
	if err := db.First(&after, src.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if after.Name != before.Name || after.Type != before.Type || after.URL != before.URL {
		t.Fatalf("identity columns destroyed: %q/%q/%q", after.Name, after.Type, after.URL)
	}
	if !after.CreatedAt.Equal(before.CreatedAt) {
		t.Fatalf("created_at was reset: %v -> %v", before.CreatedAt, after.CreatedAt)
	}
}

// TestTouchLastFetchedAt_OnlyWritesOneColumn covers the replacement call used at
// the end of FetchAndProcess.
func TestTouchLastFetchedAt_OnlyWritesOneColumn(t *testing.T) {
	db := newTestDB(t)
	repo := NewSourceRepository(db)
	src := createSource(t, db, "国家统计局", "https://example.com/rss.xml")
	before := *src

	stamp := time.Now()
	if err := repo.TouchLastFetchedAt(src.ID, stamp); err != nil {
		t.Fatalf("TouchLastFetchedAt: %v", err)
	}

	var after models.Source
	db.First(&after, src.ID)
	if after.Name != before.Name || after.Type != before.Type || after.URL != before.URL {
		t.Fatalf("identity columns changed: %q/%q/%q", after.Name, after.Type, after.URL)
	}
	if !after.CreatedAt.Equal(before.CreatedAt) {
		t.Fatalf("created_at changed: %v -> %v", before.CreatedAt, after.CreatedAt)
	}
	if after.IsActive != before.IsActive || after.Status != before.Status {
		t.Fatalf("unrelated columns changed")
	}
	if after.LastFetchedAt == nil || !after.LastFetchedAt.Equal(stamp) {
		t.Fatalf("last_fetched_at not recorded: %v", after.LastFetchedAt)
	}
	if err := repo.TouchLastFetchedAt(0, stamp); err == nil {
		t.Fatal("id=0 must be rejected")
	}
}

// TestUpdate_FullRowStillWorks ensures the data-source edit path is unaffected.
func TestUpdate_FullRowStillWorks(t *testing.T) {
	db := newTestDB(t)
	repo := NewSourceRepository(db)
	src := createSource(t, db, "Old", "https://example.com/a.xml")

	loaded, err := repo.Get(src.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	loaded.Name = "New"
	loaded.URL = "https://example.com/b.xml"
	if err := repo.Update(loaded); err != nil {
		t.Fatalf("full-row update: %v", err)
	}

	var after models.Source
	db.First(&after, src.ID)
	if after.Name != "New" || after.URL != "https://example.com/b.xml" || after.Type != "rss" {
		t.Fatalf("update did not apply correctly: %+v", after)
	}
}

// TestDeleteBlankSources_RemovesOnlyBlanksAndBreaksTheLoop reproduces the whole
// failure cycle and asserts it can no longer grow.
func TestDeleteBlankSources_RemovesOnlyBlanksAndBreaksTheLoop(t *testing.T) {
	db := newTestDB(t)
	repo := NewSourceRepository(db)

	good := createSource(t, db, "国家统计局", "https://example.com/stats.xml")
	// Emulate rows left behind by the old buggy write path.
	db.Exec("INSERT INTO sources (name, type, url, created_at) VALUES ('', '', '', '0001-01-01 00:00:00+00:00')")
	db.Exec("INSERT INTO sources (name, type, url, created_at) VALUES ('', '', '', '0001-01-01 00:00:00+00:00')")

	removed, err := repo.DeleteBlankSources()
	if err != nil {
		t.Fatalf("DeleteBlankSources: %v", err)
	}
	if removed != 2 {
		t.Fatalf("removed=%d, want 2", removed)
	}

	var live []models.Source
	db.Find(&live)
	if len(live) != 1 || live[0].ID != good.ID {
		t.Fatalf("real source removed or extra rows kept: %+v", live)
	}

	// Re-seeding must restore any default that is genuinely missing, exactly once,
	// and the fetch touch must leave it findable by url afterwards.
	if err := SeedDefaultSources(db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	for _, name := range []string{"Defense News", "Fox News", "Hacker News"} {
		var rows []models.Source
		db.Where("name = ?", name).Find(&rows)
		if len(rows) != 1 {
			t.Fatalf("want exactly 1 %q, got %d", name, len(rows))
		}
		if err := repo.TouchLastFetchedAt(rows[0].ID, time.Now()); err != nil {
			t.Fatalf("touch: %v", err)
		}
	}

	// One more start-up pass must add nothing: every default is findable by URL.
	var beforeReSeed int64
	db.Model(&models.Source{}).Count(&beforeReSeed)
	if err := SeedDefaultSources(db); err != nil {
		t.Fatalf("re-seed: %v", err)
	}
	var afterReSeed int64
	db.Model(&models.Source{}).Count(&afterReSeed)
	if afterReSeed != beforeReSeed {
		t.Fatalf("seeder still duplicates rows: %d -> %d", beforeReSeed, afterReSeed)
	}

	var blanks int64
	db.Model(&models.Source{}).
		Where("(name IS NULL OR TRIM(name)='') AND (type IS NULL OR TRIM(type)='') AND (url IS NULL OR TRIM(url)='')").
		Count(&blanks)
	if blanks != 0 {
		t.Fatalf("blank rows regenerated: %d", blanks)
	}
}

// TestSourceCreate_RejectsBlank still guards the insert path the user asked about.
func TestSourceCreate_RejectsBlank(t *testing.T) {
	db := newTestDB(t)
	repo := NewSourceRepository(db)

	for _, tc := range []models.Source{
		{Name: "", Type: "rss", URL: "https://example.com"},
		{Name: "x", Type: "", URL: "https://example.com"},
		{Name: "x", Type: "rss", URL: ""},
		{Name: "   ", Type: "  ", URL: "  "},
	} {
		if err := repo.Create(&tc); err == nil {
			t.Fatalf("blank source accepted: %+v", tc)
		}
	}
	var n int64
	db.Model(&models.Source{}).Count(&n)
	if n != 0 {
		t.Fatalf("blank rows inserted: %d", n)
	}
}
