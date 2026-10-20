package dedup

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"ai-informations/internal/models"

	"gorm.io/gorm"
)

// Service handles news deduplication using URL normalization and content fingerprinting.
type Service struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

// IsDuplicate checks if the given item is a duplicate based on URL or fingerprint.
func (s *Service) IsDuplicate(item models.RawNewsItem) (bool, string) {
	// Check by normalized URL first
	cleanURL := normalizeURL(item.OriginURL)
	if cleanURL != "" {
		var count int64
		err := s.db.Model(&models.Article{}).Where("origin_url = ?", cleanURL).Count(&count).Error
		if err == nil && count > 0 {
			return true, "url_duplicate"
		}
	}

	// Check by content fingerprint
	fp := computeFingerprint(item)
	if fp != "" {
		var count int64
		err := s.db.Model(&models.Article{}).Where("fingerprint = ?", fp).Count(&count).Error
		if err == nil && count > 0 {
			return true, "fingerprint_duplicate"
		}
	}

	return false, ""
}

// normalizeURL cleans and standardizes a URL for comparison.
func normalizeURL(rawURL string) string {
	url := strings.TrimSpace(rawURL)
	// Remove query parameters that don't affect content
	parts := strings.SplitN(url, "?", 2)
	if len(parts) > 1 {
		// Keep only essential query params
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
	// Remove trailing slash (except for root)
	url = strings.TrimRight(url, "/")
	if url == "" {
		url = "/"
	}
	return url
}

// computeFingerprint generates a SimHash-like fingerprint from the content.
func computeFingerprint(item models.RawNewsItem) string {
	// Simple content hash as fingerprint (SimHash would be more sophisticated)
	content := strings.ToLower(item.Title + " " + item.Summary + " " + item.Lead)
	h := sha256.New()
	h.Write([]byte(content))
	return hex.EncodeToString(h.Sum(nil)[:16]) // Use first 16 bytes as fingerprint
}

// GetArticleIDsByFingerprints returns existing article IDs matching the given fingerprints.
func (s *Service) GetArticleIDsByFingerprints(fps []string) ([]uint, error) {
	var ids []uint
	if err := s.db.Model(&models.Article{}).Select("id").Where("fingerprint IN ?", fps).Find(&ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}
