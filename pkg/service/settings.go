package service

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/store"
)

type SettingsService struct {
	settingsStore  store.SettingsStore
	debug          atomic.Bool
	normalLogLevel zerolog.Level
	log            zerolog.Logger
}

func NewSettingsService(settingsStore store.SettingsStore, log zerolog.Logger) *SettingsService {
	return &SettingsService{
		settingsStore:  settingsStore,
		normalLogLevel: zerolog.GlobalLevel(),
		log:            log.With().Str("service", "settings").Logger(),
	}
}

func (s *SettingsService) Get(ctx context.Context, key string) (string, error) {
	setting, err := s.settingsStore.Get(ctx, key)
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
	if err := s.settingsStore.Set(ctx, key, value); err != nil {
		return fmt.Errorf("setting %q: %w", key, err)
	}
	if key == "debug_enabled" {
		s.setDebug(value == "true")
	}
	return nil
}

func (s *SettingsService) ResolveGlobalDefaults(ctx context.Context) (hwaccel, videoCodec string) {
	hwaccel = "none"
	videoCodec = "copy"
	if val, _ := s.Get(ctx, "default_hwaccel"); val != "" {
		hwaccel = val
	}
	if val, _ := s.Get(ctx, "default_video_codec"); val != "" {
		videoCodec = val
	}
	return hwaccel, videoCodec
}


var apiVisibleKeys = map[string]bool{
	"vod_profile_selector":       true,
	"default_hwaccel":            true,
	"default_video_codec":        true,
	"dlna_enabled":               true,
	"debug_enabled":              true,
	"tmdb_api_key":               true,
}

func IsAPISettable(key string) bool {
	return apiVisibleKeys[key]
}

func (s *SettingsService) List(ctx context.Context) ([]models.CoreSetting, error) {
	settings, err := s.settingsStore.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing settings: %w", err)
	}
	filtered := make([]models.CoreSetting, 0, len(apiVisibleKeys))
	for _, setting := range settings {
		if apiVisibleKeys[setting.Key] {
			filtered = append(filtered, setting)
		}
	}
	return filtered, nil
}

func (s *SettingsService) Delete(ctx context.Context, key string) error {
	if err := s.settingsStore.Delete(ctx, key); err != nil {
		return fmt.Errorf("deleting setting %q: %w", key, err)
	}
	return nil
}
