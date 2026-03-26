package database

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"

	"github.com/gavinmcnair/tvproxy/pkg/defaults"
	"github.com/gavinmcnair/tvproxy/pkg/ffmpeg"
	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/store"
)

type execContext interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func seedData(ctx context.Context, db execContext) error {
	return nil
}

func seedRecordingProfile(ctx context.Context, db *sql.DB) error {
	return nil
}

func seedCopyProfile(ctx context.Context, db *sql.DB) error {
	return nil
}

func updateRecordingProfileAV1(ctx context.Context, db *sql.DB) error {
	return nil
}

func SeedClientDefaults(_ context.Context, defs *defaults.ClientDefaults, profileStore store.ProfileStore, clientStore store.ClientStore) error {
	if defs == nil {
		return nil
	}

	clientStore.Clear()
	profileStore.RemoveClientProfiles()

	now := time.Now()

	for _, c := range defs.Clients {
		hwaccel := c.HWAccel
		if hwaccel == "default" {
			hwaccel = "none"
		}
		videoCodec := c.VideoCodec
		if videoCodec == "default" {
			videoCodec = "copy"
		}
		args := ffmpeg.ComposeStreamProfileArgs(ffmpeg.ComposeOptions{SourceType: c.SourceType, HWAccel: hwaccel, VideoCodec: videoCodec, Container: c.Container})

		profile := &models.StreamProfile{
			ID:         uuid.New().String(),
			Name:       c.Name,
			StreamMode: "ffmpeg",
			SourceType: c.SourceType,
			HWAccel:    c.HWAccel,
			VideoCodec: c.VideoCodec,
			Container:  c.Container,
			FPSMode:    "auto",
			Command:    "ffmpeg",
			Args:       args,
			IsClient:   true,
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
