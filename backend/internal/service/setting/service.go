package setting

import (
	"ai-informations/internal/repository"

	"gorm.io/gorm"
)

// Service manages system settings.
type Service struct {
	repo *repository.SystemSettingRepository
}

func NewService(db *gorm.DB) *Service {
	return &Service{
		repo: repository.NewSystemSettingRepository(db),
	}
}

// GetAll returns all system settings as a map.
func (s *Service) GetAll() (map[string]string, error) {
	return s.repo.GetAll()
}

// Get returns a single setting value by key.
func (s *Service) Get(key string) (string, error) {
	sv, err := s.repo.Get(key)
	if err != nil {
		return "", err
	}
	return sv.Value, nil
}

// Set updates or creates a system setting.
func (s *Service) Set(key, value, description string) error {
	return s.repo.Set(key, value, description)
}
