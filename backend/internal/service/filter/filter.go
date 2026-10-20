package filter

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"ai-informations/internal/models"

	"gorm.io/gorm"
)

// Service handles content filtering and compliance checks.
type Service struct {
	db           *gorm.DB
	globalRules  []string // global blocked keywords
	sourceRules  map[uint][]string // per-source blocked keywords
}

// NewService creates a new filter service.
func NewService(db *gorm.DB) *Service {
	s := &Service{db: db, sourceRules: make(map[uint][]string)}
	s.reloadRules()
	return s
}

// reloadRules reads filter rules from the database.
func (s *Service) reloadRules() {
	var globalRules []models.SourceFilterRule
	s.db.Where("source_id IS NULL AND is_active = ?", true).Find(&globalRules)
	s.globalRules = extractKeywords(globalRules)

	var sourceRules []models.SourceFilterRule
	s.db.Where("is_active = ?", true).Find(&sourceRules)
	s.sourceRules = make(map[uint][]string)
	for _, r := range sourceRules {
		if r.SourceID != nil {
			s.sourceRules[*r.SourceID] = extractKeywords([]models.SourceFilterRule{r})
		}
	}
}

func extractKeywords(rules []models.SourceFilterRule) []string {
	var keywords []string
	for _, r := range rules {
		for _, kw := range strings.Split(r.Keywords, ",") {
			kw = strings.TrimSpace(kw)
			if kw != "" {
				keywords = append(keywords, strings.ToLower(kw))
			}
		}
	}
	return keywords
}

// Check evaluates a raw news item against all active filter rules.
// Returns (passed, reasonCode, hash) - if passed is false, the content should be discarded.
func (s *Service) Check(item models.RawNewsItem, sourceID uint) (bool, string, string) {
	content := item.Title + " " + item.Summary + " " + item.Lead
	hash := computeHash(content)

	// Check global rules
	for _, kw := range s.globalRules {
		if strings.Contains(strings.ToLower(content), kw) {
			s.logFilter(sourceID, 0, "GLOBAL_BLOCKED", hash)
			return false, "GLOBAL_BLOCKED", hash
		}
	}

	// Check source-specific rules
	if srcRules, ok := s.sourceRules[sourceID]; ok {
		for _, kw := range srcRules {
			if strings.Contains(strings.ToLower(content), kw) {
				s.logFilter(sourceID, 0, "SOURCE_BLOCKED", hash)
				return false, "SOURCE_BLOCKED", hash
			}
		}
	}

	return true, "", hash
}

// CheckTitle is a quick check on just the title.
func (s *Service) CheckTitle(title string, sourceID uint) (bool, string, string) {
	hash := computeHash(title)
	for _, kw := range s.globalRules {
		if strings.Contains(strings.ToLower(title), kw) {
			s.logFilter(sourceID, 0, "GLOBAL_BLOCKED_TITLE", hash)
			return false, "GLOBAL_BLOCKED_TITLE", hash
		}
	}
	if srcRules, ok := s.sourceRules[sourceID]; ok {
		for _, kw := range srcRules {
			if strings.Contains(strings.ToLower(title), kw) {
				s.logFilter(sourceID, 0, "SOURCE_BLOCKED_TITLE", hash)
				return false, "SOURCE_BLOCKED_TITLE", hash
			}
		}
	}
	return true, "", hash
}

func (s *Service) logFilter(sourceID uint, ruleID uint, reasonCode, contentHash string) {
	logEntry := &models.FilterLog{
		SourceID:    sourceID,
		RuleID:      ruleID,
		ReasonCode:  reasonCode,
		ContentHash: contentHash,
		CreatedAt:   time.Now(),
	}
	if err := s.db.Create(logEntry).Error; err != nil {
		fmt.Printf("Failed to write filter log: %v\n", err)
	}
}

func computeHash(content string) string {
	h := sha256.New()
	h.Write([]byte(content))
	return hex.EncodeToString(h.Sum(nil))
}

// RefreshRules reloads all filter rules from the database.
func (s *Service) RefreshRules() {
	s.reloadRules()
}
