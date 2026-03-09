package service

import (
	"context"
	"fmt"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
)

// SettingsService handles application-level key/value settings.
type SettingsService struct {
	settingsRepo *repository.CoreSettingsRepository
}

// NewSettingsService creates a new SettingsService.
func NewSettingsService(settingsRepo *repository.CoreSettingsRepository) *SettingsService {
	return &SettingsService{
		settingsRepo: settingsRepo,
	}
}

// Get retrieves a setting value by key.
func (s *SettingsService) Get(ctx context.Context, key string) (string, error) {
	setting, err := s.settingsRepo.Get(ctx, key)
	if err != nil {
		return "", fmt.Errorf("getting setting %q: %w", key, err)
	}
	return setting.Value, nil
}

// Set stores a setting value by key. If the key already exists, it is overwritten.
func (s *SettingsService) Set(ctx context.Context, key, value string) error {
	if err := s.settingsRepo.Set(ctx, key, value); err != nil {
		return fmt.Errorf("setting %q: %w", key, err)
	}
	return nil
}

// List returns all settings.
func (s *SettingsService) List(ctx context.Context) ([]models.CoreSetting, error) {
	settings, err := s.settingsRepo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing settings: %w", err)
	}
	return settings, nil
}

// Delete removes a setting by key.
func (s *SettingsService) Delete(ctx context.Context, key string) error {
	if err := s.settingsRepo.Delete(ctx, key); err != nil {
		return fmt.Errorf("deleting setting %q: %w", key, err)
	}
	return nil
}
