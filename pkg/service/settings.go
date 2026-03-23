package service

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
)

type SettingsService struct {
	settingsRepo   *repository.CoreSettingsRepository
	debug          atomic.Bool
	normalLogLevel zerolog.Level
}

func NewSettingsService(settingsRepo *repository.CoreSettingsRepository) *SettingsService {
	return &SettingsService{
		settingsRepo:   settingsRepo,
		normalLogLevel: zerolog.GlobalLevel(),
	}
}

func (s *SettingsService) Get(ctx context.Context, key string) (string, error) {
	setting, err := s.settingsRepo.Get(ctx, key)
	if err != nil {
		return "", fmt.Errorf("getting setting %q: %w", key, err)
	}
	return setting.Value, nil
}

func (s *SettingsService) IsDebug() bool {
	return s.debug.Load()
}

func (s *SettingsService) LoadDebugFlag(ctx context.Context) {
	val, err := s.Get(ctx, "debug_enabled")
	s.setDebug(err == nil && val == "true")
}

func (s *SettingsService) setDebug(on bool) {
	s.debug.Store(on)
	if on {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	} else {
		zerolog.SetGlobalLevel(s.normalLogLevel)
	}
}

func (s *SettingsService) Set(ctx context.Context, key, value string) error {
	if err := s.settingsRepo.Set(ctx, key, value); err != nil {
		return fmt.Errorf("setting %q: %w", key, err)
	}
	if key == "debug_enabled" {
		s.setDebug(value == "true")
	}
	return nil
}

func (s *SettingsService) List(ctx context.Context) ([]models.CoreSetting, error) {
	settings, err := s.settingsRepo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing settings: %w", err)
	}
	return settings, nil
}

func (s *SettingsService) Delete(ctx context.Context, key string) error {
	if err := s.settingsRepo.Delete(ctx, key); err != nil {
		return fmt.Errorf("deleting setting %q: %w", key, err)
	}
	return nil
}
