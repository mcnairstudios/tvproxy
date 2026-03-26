package database

import (
	"context"
	"database/sql"

	"github.com/google/uuid"

	"github.com/gavinmcnair/tvproxy/pkg/defaults"
	"github.com/gavinmcnair/tvproxy/pkg/ffmpeg"
)

type execContext interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func seedData(ctx context.Context, db execContext) error {
	type profileSeed struct {
		name       string
		streamMode string
		sourceType string
		hwaccel    string
		videoCodec string
		container  string
		isDefault  bool
		isSystem   bool
	}

	profiles := []profileSeed{
		{"Direct", "direct", "m3u", "none", "copy", "mpegts", false, true},
		{"Proxy", "proxy", "m3u", "none", "copy", "mpegts", true, true},
	}

	for _, p := range profiles {
		id := uuid.New().String()
		args := ffmpeg.ComposeStreamProfileArgs(ffmpeg.ComposeOptions{SourceType: p.sourceType, HWAccel: p.hwaccel, VideoCodec: p.videoCodec, Container: p.container})
		command := "ffmpeg"
		if p.streamMode == "direct" || p.streamMode == "proxy" {
			command = ""
			args = ""
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO stream_profiles (id, name, stream_mode, source_type, hwaccel, video_codec, container, custom_args, command, args, is_default, is_system)
			 VALUES (?, ?, ?, ?, ?, ?, ?, '', ?, ?, ?, ?)`,
			id, p.name, p.streamMode, p.sourceType, p.hwaccel, p.videoCodec, p.container, command, args, p.isDefault, p.isSystem); err != nil {
			return err
		}
	}

	return nil
}

func seedRecordingProfile(ctx context.Context, db *sql.DB) error {
	var count int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM stream_profiles WHERE name = 'Recording'`).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	id := uuid.New().String()
	args := ffmpeg.ComposeStreamProfileArgs(ffmpeg.ComposeOptions{SourceType: "m3u", HWAccel: "none", VideoCodec: "av1", Container: "mp4"})
	_, err = db.ExecContext(ctx,
		`INSERT INTO stream_profiles (id, name, stream_mode, source_type, hwaccel, video_codec, container, custom_args, command, args, is_default, is_system)
		 VALUES (?, 'Recording', 'ffmpeg', 'm3u', 'none', 'av1', 'mp4', '', 'ffmpeg', ?, 0, 1)`,
		id, args)
	return err
}

func seedCopyProfile(ctx context.Context, db *sql.DB) error {
	var count int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM stream_profiles WHERE name = 'Copy'`).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	id := uuid.New().String()
	args := ffmpeg.ComposeStreamProfileArgs(ffmpeg.ComposeOptions{SourceType: "m3u", HWAccel: "none", VideoCodec: "h265", Container: "mpegts", Deinterlace: true, FPSMode: "auto"})
	_, err = db.ExecContext(ctx,
		`INSERT INTO stream_profiles (id, name, stream_mode, source_type, hwaccel, video_codec, container, custom_args, command, args, is_default, is_system, deinterlace, fps_mode)
		 VALUES (?, 'Copy', 'ffmpeg', 'm3u', 'none', 'h265', 'mpegts', '', 'ffmpeg', ?, 0, 1, 1, 'auto')`,
		id, args)
	return err
}

func updateRecordingProfileAV1(ctx context.Context, db *sql.DB) error {
	args := ffmpeg.ComposeStreamProfileArgs(ffmpeg.ComposeOptions{SourceType: "m3u", HWAccel: "none", VideoCodec: "av1", Container: "mp4"})
	_, err := db.ExecContext(ctx,
		`UPDATE stream_profiles SET hwaccel = 'none', video_codec = 'av1', args = ? WHERE name = 'Recording' AND is_system = 1`,
		args)
	return err
}

func SeedClientDefaults(ctx context.Context, db *sql.DB, defs *defaults.ClientDefaults) error {
	if defs == nil {
		return nil
	}

	cleanupStmts := []string{
		`DELETE FROM client_match_rules`,
		`DELETE FROM clients`,
		`DELETE FROM stream_profiles WHERE is_client = 1`,
	}
	for _, stmt := range cleanupStmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}

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
		profileID := uuid.New().String()
		if _, err := db.ExecContext(ctx,
			`INSERT INTO stream_profiles (id, name, stream_mode, source_type, hwaccel, video_codec, container, custom_args, command, args, is_default, is_system, is_client)
			 VALUES (?, ?, 'ffmpeg', ?, ?, ?, ?, '', 'ffmpeg', ?, 0, 0, 1)`,
			profileID, c.Name, c.SourceType, c.HWAccel, c.VideoCodec, c.Container, args); err != nil {
			return err
		}

		clientID := uuid.New().String()
		if _, err := db.ExecContext(ctx,
			`INSERT INTO clients (id, name, priority, stream_profile_id, is_enabled) VALUES (?, ?, ?, ?, 1)`,
			clientID, c.Name, c.Priority, profileID); err != nil {
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

	return nil
}
