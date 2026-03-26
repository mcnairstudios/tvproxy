package service

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/ffmpeg"
	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
)

type SettingsService struct {
	settingsRepo      *repository.CoreSettingsRepository
	streamProfileRepo *repository.StreamProfileRepository
	debug             atomic.Bool
	normalLogLevel    zerolog.Level
	log               zerolog.Logger
}

func NewSettingsService(settingsRepo *repository.CoreSettingsRepository, streamProfileRepo *repository.StreamProfileRepository, log zerolog.Logger) *SettingsService {
	return &SettingsService{
		settingsRepo:      settingsRepo,
		streamProfileRepo: streamProfileRepo,
		normalLogLevel:    zerolog.GlobalLevel(),
		log:               log.With().Str("service", "settings").Logger(),
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
	switch key {
	case "debug_enabled":
		s.setDebug(value == "true")
	case "default_hwaccel", "default_video_codec":
		s.RecomposeDefaultProfiles(ctx)
	}
	return nil
}

func (s *SettingsService) RecomposeDefaultProfiles(ctx context.Context) {
	if s.streamProfileRepo == nil {
		return
	}
	profiles, err := s.streamProfileRepo.List(ctx)
	if err != nil {
		return
	}

	hwaccel := "none"
	if val, _ := s.Get(ctx, "default_hwaccel"); val != "" {
		hwaccel = val
	}
	videoCodec := "copy"
	if val, _ := s.Get(ctx, "default_video_codec"); val != "" {
		videoCodec = val
	}

	s.log.Info().Str("hwaccel", hwaccel).Str("video_codec", videoCodec).Msg("recomposing default profiles")

	for i := range profiles {
		p := &profiles[i]
		if p.HWAccel != "default" && p.VideoCodec != "default" {
			continue
		}
		if p.UseCustomArgs {
			continue
		}
		resolvedHW := p.HWAccel
		if resolvedHW == "default" {
			resolvedHW = hwaccel
		}
		resolvedCodec := p.VideoCodec
		if resolvedCodec == "default" {
			resolvedCodec = videoCodec
		}
		p.Args = ffmpeg.ComposeStreamProfileArgs(ffmpeg.ComposeOptions{
			SourceType:  p.SourceType,
			HWAccel:     resolvedHW,
			VideoCodec:  resolvedCodec,
			Container:   p.Container,
			Deinterlace: p.Deinterlace,
			FPSMode:     p.FPSMode,
		})
		s.log.Info().Str("profile", p.Name).Str("hwaccel_db", p.HWAccel).Str("codec_db", p.VideoCodec).Str("resolved_hw", resolvedHW).Str("resolved_codec", resolvedCodec).Msg("recomposed profile")
		if err := s.streamProfileRepo.Update(ctx, p); err != nil {
			s.log.Error().Err(err).Str("profile", p.Name).Msg("failed to recompose profile with default values")
		}
	}
}

var apiVisibleKeys = map[string]bool{
	"vod_profile_selector": true,
	"default_hwaccel":      true,
	"default_video_codec":  true,
	"dlna_enabled":         true,
	"debug_enabled":        true,
}

func IsAPISettable(key string) bool {
	return apiVisibleKeys[key]
}

func (s *SettingsService) List(ctx context.Context) ([]models.CoreSetting, error) {
	settings, err := s.settingsRepo.List(ctx)
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
	if err := s.settingsRepo.Delete(ctx, key); err != nil {
		return fmt.Errorf("deleting setting %q: %w", key, err)
	}
	return nil
}
