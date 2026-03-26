package database

import (
	"context"
	"database/sql"

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

func SeedClientDefaults(ctx context.Context, db *sql.DB, defs *defaults.ClientDefaults, profileStore store.ProfileStore) error {
	if defs == nil {
		return nil
	}

	if _, err := db.ExecContext(ctx, `DELETE FROM client_match_rules`); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM clients`); err != nil {
		return err
	}

	profileStore.RemoveClientProfiles()

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

		clientID := uuid.New().String()
		if _, err := db.ExecContext(ctx,
			`INSERT INTO clients (id, name, priority, stream_profile_id, is_enabled) VALUES (?, ?, ?, ?, 1)`,
			clientID, c.Name, c.Priority, profile.ID); err != nil {
			return err
		}

		for _, r := range c.MatchRules {
			ruleID := uuid.New().String()
			if _, err := db.ExecContext(ctx,
				`INSERT INTO client_match_rules (id, client_id, header_name, match_type, match_value) VALUES (?, ?, ?, ?, ?)`,
				ruleID, clientID, r.HeaderName, r.MatchType, r.MatchValue); err != nil {
				return err
			}
		}
	}

	return profileStore.Save()
}
