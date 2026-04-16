package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/gavinmcnair/tvproxy/pkg/defaults"
	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/store"
)

func resolveGlobalDefaults(settingsStore store.SettingsStore) (hwaccel, videoCodec string) {
	hwaccel = "none"
	videoCodec = "copy"
	if settingsStore == nil {
		return
	}
	ctx := context.Background()
	if s, err := settingsStore.Get(ctx, "default_hwaccel"); err == nil && s.Value != "" {
		hwaccel = s.Value
	}
	if s, err := settingsStore.Get(ctx, "default_video_codec"); err == nil && s.Value != "" {
		videoCodec = s.Value
	}
	return
}

func SeedClientDefaults(ctx context.Context, defs *defaults.ClientDefaults, profileStore store.ProfileStore, clientStore store.ClientStore, settingsStore store.SettingsStore) error {
	if defs == nil {
		return nil
	}

	return seedClientDefaultsForce(ctx, defs, profileStore, clientStore, settingsStore, false)
}

func ForceSeedClientDefaults(ctx context.Context, defs *defaults.ClientDefaults, profileStore store.ProfileStore, clientStore store.ClientStore, settingsStore store.SettingsStore) error {
	return seedClientDefaultsForce(ctx, defs, profileStore, clientStore, settingsStore, true)
}

func seedClientDefaultsForce(_ context.Context, defs *defaults.ClientDefaults, profileStore store.ProfileStore, clientStore store.ClientStore, settingsStore store.SettingsStore, force bool) error {
	if defs == nil {
		return nil
	}

	if !force {
		existing, _ := clientStore.List(context.Background())
		if len(existing) > 0 {
			return nil
		}
	}

	clientStore.Clear()
	profileStore.RemoveClientProfiles()

	now := time.Now()

	for _, c := range defs.Clients {
		hwaccel := c.HWAccel

		profile := &models.StreamProfile{
			ID:         uuid.New().String(),
			Name:       c.Name,
			StreamMode: "ffmpeg",
			HWAccel:    hwaccel,
			Container:  c.Container,
			Delivery:   c.Delivery,
			AutoDetect: c.AutoDetect,
			Command:    "ffmpeg",
			IsClient:   true,
		}

		if !c.AutoDetect {
			globalHW, globalCodec := resolveGlobalDefaults(settingsStore)
			if hwaccel == "default" {
				hwaccel = globalHW
			}
			profile.VideoCodec = globalCodec
			profile.FPSMode = "auto"
		}

		profileStore.CreateDirect(profile)

		rules := make([]models.ClientMatchRule, len(c.MatchRules))
		for j, r := range c.MatchRules {
			rules[j] = models.ClientMatchRule{
				ID:         uuid.New().String(),
				HeaderName: r.HeaderName,
				MatchType:  r.MatchType,
				MatchValue: r.MatchValue,
			}
		}

		client := &models.Client{
			ID:              uuid.New().String(),
			Name:            c.Name,
			Priority:        c.Priority,
			ListenPort:      c.ListenPort,
			StreamProfileID: profile.ID,
			IsEnabled:       true,
			MatchRules:      rules,
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		clientStore.AddDirect(client)
	}

	if err := profileStore.Save(); err != nil {
		return err
	}
	return clientStore.Save()
}
