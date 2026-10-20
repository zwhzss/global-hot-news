package repository

import (
	"testing"

	"ai-informations/internal/models"
)

// TestSeedDefaultSettingsClassifierInheritsLLMOnce: cls_* must start as a
// copy of the installation's current llm_* values (so the upgrade changes
// nothing), then stay independent — later llm_* edits never move the
// classifier's model.
func TestSeedDefaultSettingsClassifierInheritsLLMOnce(t *testing.T) {
	db := newTestDB(t)

	// Simulate an existing installation: the user tuned llm_* by hand.
	for k, v := range map[string]string{
		"llm_base_url": "http://127.0.0.1:8888/v1",
		"llm_model":    "Hy-MT2-1.8B-Q4_K_M",
		"llm_api_key":  "sk-secret",
	} {
		if err := db.Create(&models.SystemSetting{Key: k, Value: v}).Error; err != nil {
			t.Fatalf("pre-create %s: %v", k, err)
		}
	}

	if err := SeedDefaultSettings(db); err != nil {
		t.Fatalf("seed: %v", err)
	}

	get := func(key string) string {
		t.Helper()
		var row models.SystemSetting
		if err := db.Where("key = ?", key).First(&row).Error; err != nil {
			t.Fatalf("read %s: %v", key, err)
		}
		return row.Value
	}

	if got := get("cls_base_url"); got != "http://127.0.0.1:8888/v1" {
		t.Fatalf("cls_base_url = %q, want the inherited llm_base_url", got)
	}
	if got := get("cls_model"); got != "Hy-MT2-1.8B-Q4_K_M" {
		t.Fatalf("cls_model = %q, want the inherited llm_model", got)
	}
	if got := get("cls_api_key"); got != "sk-secret" {
		t.Fatalf("cls_api_key = %q, want the inherited llm_api_key", got)
	}

	// A later edit of the translation model must not move the classifier.
	if err := db.Model(&models.SystemSetting{}).Where("key = ?", "llm_base_url").
		Update("value", "http://127.0.0.1:9999/v1").Error; err != nil {
		t.Fatalf("edit llm_base_url: %v", err)
	}
	if err := SeedDefaultSettings(db); err != nil {
		t.Fatalf("re-seed: %v", err)
	}
	if got := get("cls_base_url"); got != "http://127.0.0.1:8888/v1" {
		t.Fatalf("cls_base_url after llm edit = %q; the two configs must stay independent", got)
	}
	if got := get("llm_base_url"); got != "http://127.0.0.1:9999/v1" {
		t.Fatalf("llm_base_url = %q; re-seed must not reset a user's value", got)
	}
}

// TestSeedDefaultSettingsClassifierDefaultsOnFreshDB: a fresh install without
// any llm_* rows gets the static defaults for the classifier model.
func TestSeedDefaultSettingsClassifierDefaultsOnFreshDB(t *testing.T) {
	db := newTestDB(t)
	if err := SeedDefaultSettings(db); err != nil {
		t.Fatalf("seed: %v", err)
	}
	var row models.SystemSetting
	if err := db.Where("key = ?", "cls_base_url").First(&row).Error; err != nil {
		t.Fatalf("read cls_base_url: %v", err)
	}
	if row.Value != "http://127.0.0.1:11434/v1" {
		t.Fatalf("cls_base_url = %q, want the static default", row.Value)
	}
}
