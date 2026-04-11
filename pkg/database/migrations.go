package database

import (
	"context"
	"database/sql"

	"github.com/google/uuid"

)

type migration struct {
	name string
	sql  string
	fn   func(ctx context.Context, db *sql.DB) error
}

var migrations = []migration{
	{
		name: "create_schema",
		fn: func(ctx context.Context, db *sql.DB) error {
			tables := []string{
				`CREATE TABLE users (
					id TEXT PRIMARY KEY,
					username TEXT NOT NULL UNIQUE,
					password_hash TEXT NOT NULL,
					is_admin INTEGER NOT NULL DEFAULT 0,
					invite_token TEXT,
					invite_expires_at DATETIME,
					created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
					updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
				)`,
				`CREATE UNIQUE INDEX idx_users_invite_token ON users(invite_token) WHERE invite_token IS NOT NULL`,

				`CREATE TABLE m3u_accounts (
					id TEXT PRIMARY KEY,
					name TEXT NOT NULL,
					url TEXT NOT NULL,
					type TEXT NOT NULL DEFAULT 'm3u',
					username TEXT NOT NULL DEFAULT '',
					password TEXT NOT NULL DEFAULT '',
					max_streams INTEGER NOT NULL DEFAULT 1,
					is_enabled INTEGER NOT NULL DEFAULT 1,
					last_refreshed DATETIME,
					stream_count INTEGER NOT NULL DEFAULT 0,
					refresh_interval INTEGER NOT NULL DEFAULT 0,
					last_error TEXT NOT NULL DEFAULT '',
					created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
					updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
				)`,

				`CREATE TABLE logos (
					id TEXT PRIMARY KEY,
					name TEXT NOT NULL,
					url TEXT NOT NULL,
					cached_filename TEXT NOT NULL DEFAULT '',
					created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
				)`,

				`CREATE TABLE stream_profiles (
					id TEXT PRIMARY KEY,
					name TEXT NOT NULL,
					stream_mode TEXT NOT NULL DEFAULT 'ffmpeg',
					source_type TEXT NOT NULL DEFAULT 'direct',
					hwaccel TEXT NOT NULL DEFAULT 'none',
					video_codec TEXT NOT NULL DEFAULT 'copy',
					container TEXT NOT NULL DEFAULT 'mpegts',
					use_custom_args INTEGER NOT NULL DEFAULT 0,
					custom_args TEXT NOT NULL DEFAULT '',
					command TEXT NOT NULL DEFAULT '',
					args TEXT NOT NULL DEFAULT '',
					is_default INTEGER NOT NULL DEFAULT 0,
					is_system INTEGER NOT NULL DEFAULT 0,
					is_client INTEGER NOT NULL DEFAULT 0,
					created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
					updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
				)`,
				`CREATE UNIQUE INDEX idx_stream_profiles_name ON stream_profiles(name)`,

				`CREATE TABLE channel_groups (
					id TEXT PRIMARY KEY,
					user_id TEXT NOT NULL DEFAULT '' REFERENCES users(id),
					name TEXT NOT NULL,
					is_enabled INTEGER NOT NULL DEFAULT 1,
					sort_order INTEGER NOT NULL DEFAULT 0,
					created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
					updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
					UNIQUE(name, user_id)
				)`,
				`CREATE INDEX idx_channel_groups_user_id ON channel_groups(user_id)`,

				`CREATE TABLE epg_sources (
					id TEXT PRIMARY KEY,
					name TEXT NOT NULL,
					url TEXT NOT NULL,
					is_enabled INTEGER NOT NULL DEFAULT 1,
					last_refreshed DATETIME,
					channel_count INTEGER NOT NULL DEFAULT 0,
					program_count INTEGER NOT NULL DEFAULT 0,
					last_error TEXT NOT NULL DEFAULT '',
					created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
					updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
				)`,

				`CREATE TABLE channels (
					id TEXT PRIMARY KEY,
					user_id TEXT NOT NULL DEFAULT '' REFERENCES users(id),
					name TEXT NOT NULL,
					logo_id TEXT,
					tvg_id TEXT NOT NULL DEFAULT '',
					channel_group_id TEXT,
					is_enabled INTEGER NOT NULL DEFAULT 1,
					fail_count INTEGER NOT NULL DEFAULT 0,
					created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
					updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
					FOREIGN KEY (logo_id) REFERENCES logos(id) ON DELETE SET NULL,
					FOREIGN KEY (channel_group_id) REFERENCES channel_groups(id) ON DELETE SET NULL
				)`,
				`CREATE INDEX idx_channels_logo_id ON channels(logo_id)`,
				`CREATE INDEX idx_channels_user_id ON channels(user_id)`,

				`CREATE TABLE channel_streams (
					id TEXT PRIMARY KEY,
					channel_id TEXT NOT NULL,
					stream_id TEXT NOT NULL,
					priority INTEGER NOT NULL DEFAULT 0,
					FOREIGN KEY (channel_id) REFERENCES channels(id) ON DELETE CASCADE,
					UNIQUE(channel_id, stream_id)
				)`,
				`CREATE INDEX idx_channel_streams_channel_id ON channel_streams(channel_id)`,

				`CREATE TABLE hdhr_devices (
					id TEXT PRIMARY KEY,
					name TEXT NOT NULL,
					device_id TEXT NOT NULL UNIQUE,
					device_auth TEXT NOT NULL,
					firmware_version TEXT NOT NULL DEFAULT '20240101',
					tuner_count INTEGER NOT NULL DEFAULT 2,
					port INTEGER NOT NULL DEFAULT 0,
					is_enabled INTEGER NOT NULL DEFAULT 1,
					created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
					updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
				)`,

				`CREATE TABLE hdhr_device_channel_groups (
					hdhr_device_id TEXT NOT NULL,
					channel_group_id TEXT NOT NULL,
					PRIMARY KEY (hdhr_device_id, channel_group_id),
					FOREIGN KEY (hdhr_device_id) REFERENCES hdhr_devices(id) ON DELETE CASCADE,
					FOREIGN KEY (channel_group_id) REFERENCES channel_groups(id) ON DELETE CASCADE
				)`,

				`CREATE TABLE clients (
					id TEXT PRIMARY KEY,
					name TEXT NOT NULL,
					priority INTEGER NOT NULL DEFAULT 0,
					stream_profile_id TEXT NOT NULL,
					is_enabled INTEGER NOT NULL DEFAULT 1,
					created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
					updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
					FOREIGN KEY (stream_profile_id) REFERENCES stream_profiles(id)
				)`,
				`CREATE INDEX idx_clients_priority ON clients(priority)`,

				`CREATE TABLE client_match_rules (
					id TEXT PRIMARY KEY,
					client_id TEXT NOT NULL,
					header_name TEXT NOT NULL,
					match_type TEXT NOT NULL,
					match_value TEXT NOT NULL DEFAULT '',
					FOREIGN KEY (client_id) REFERENCES clients(id) ON DELETE CASCADE
				)`,
				`CREATE INDEX idx_client_match_rules_client_id ON client_match_rules(client_id)`,

				`CREATE TABLE core_settings (
					key TEXT PRIMARY KEY,
					value TEXT NOT NULL DEFAULT ''
				)`,

				`CREATE TABLE scheduled_recordings (
					id TEXT PRIMARY KEY,
					user_id TEXT NOT NULL,
					channel_id TEXT NOT NULL,
					channel_name TEXT NOT NULL DEFAULT '',
					program_title TEXT NOT NULL DEFAULT '',
					start_at DATETIME NOT NULL,
					stop_at DATETIME NOT NULL,
					status TEXT NOT NULL DEFAULT 'pending',
					session_id TEXT NOT NULL DEFAULT '',
					segment_id TEXT NOT NULL DEFAULT '',
					file_path TEXT NOT NULL DEFAULT '',
					last_error TEXT NOT NULL DEFAULT '',
					created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
					updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
					FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
					FOREIGN KEY (channel_id) REFERENCES channels(id) ON DELETE CASCADE
				)`,
				`CREATE INDEX idx_scheduled_recordings_status ON scheduled_recordings(status)`,
				`CREATE INDEX idx_scheduled_recordings_start_at ON scheduled_recordings(start_at)`,
			}

			for _, ddl := range tables {
				if _, err := db.ExecContext(ctx, ddl); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		name: "seed_data",
		fn: func(ctx context.Context, db *sql.DB) error {
			return seedData(ctx, db)
		},
	},
	{
		name: "add_skybox_client",
		fn: func(_ context.Context, _ *sql.DB) error {
			return nil
		},
	},
	{
		name: "replace_quest_with_4xvr",
		fn: func(_ context.Context, _ *sql.DB) error {
			return nil
		},
	},
	{
		name: "add_deinterlace_fps_mode",
		fn: func(ctx context.Context, db *sql.DB) error {
			stmts := []string{
				`ALTER TABLE stream_profiles ADD COLUMN deinterlace INTEGER NOT NULL DEFAULT 0`,
				`ALTER TABLE stream_profiles ADD COLUMN fps_mode TEXT NOT NULL DEFAULT 'auto'`,
			}
			for _, s := range stmts {
				if _, err := db.ExecContext(ctx, s); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		name: "rename_quest_to_4xvr",
		fn: func(_ context.Context, _ *sql.DB) error {
			return nil
		},
	},
	{
		name: "add_recording_profile",
		fn: func(ctx context.Context, db *sql.DB) error {
			return seedRecordingProfile(ctx, db)
		},
	},
	{
		name: "add_user_channel_groups",
		fn: func(ctx context.Context, db *sql.DB) error {
			_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS user_channel_groups (
				user_id TEXT NOT NULL,
				channel_group_id TEXT NOT NULL,
				PRIMARY KEY (user_id, channel_group_id),
				FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
				FOREIGN KEY (channel_group_id) REFERENCES channel_groups(id) ON DELETE CASCADE
			)`)
			return err
		},
	},
	{
		name: "add_channel_stream_profile",
		fn: func(ctx context.Context, db *sql.DB) error {
			_, err := db.ExecContext(ctx, `ALTER TABLE channels ADD COLUMN stream_profile_id TEXT`)
			return err
		},
	},
	{
		name: "add_copy_profile",
		fn: func(ctx context.Context, db *sql.DB) error {
			return seedCopyProfile(ctx, db)
		},
	},
	{
		name: "update_recording_profile_av1",
		fn: func(ctx context.Context, db *sql.DB) error {
			return updateRecordingProfileAV1(ctx, db)
		},
	},
	{
		name: "add_default_hwaccel",
		fn:   addDefaultHWAccelMigration,
	},
	{
		name: "add_default_video_codec",
		fn: func(ctx context.Context, db *sql.DB) error {
			_, err := db.ExecContext(ctx,
				`INSERT OR IGNORE INTO core_settings (key, value) VALUES ('default_video_codec', 'copy')`)
			return err
		},
	},
	{
		name: "remove_recording_copy_profiles",
		fn: func(ctx context.Context, db *sql.DB) error {
			_, err := db.ExecContext(ctx,
				`DELETE FROM stream_profiles WHERE name IN ('Recording', 'Copy') AND is_system = 1`)
			return err
		},
	},
	{
		name: "deterministic_channel_ids",
		fn: func(ctx context.Context, db *sql.DB) error {
			ns := uuid.MustParse("c8a5e2b1-7f3d-4a96-b8e1-d2c4f6a89012")

			rows, err := db.QueryContext(ctx, `SELECT id, name, user_id FROM channels`)
			if err != nil {
				return err
			}
			type ch struct {
				id, name, userID string
			}
			var channels []ch
			for rows.Next() {
				var c ch
				if err := rows.Scan(&c.id, &c.name, &c.userID); err != nil {
					rows.Close()
					return err
				}
				channels = append(channels, c)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}

			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			defer tx.Rollback()

			for _, c := range channels {
				newID := uuid.NewSHA1(ns, []byte(c.name+":"+c.userID)).String()
				if newID == c.id {
					continue
				}
				if _, err := tx.ExecContext(ctx, `UPDATE channel_streams SET channel_id = ? WHERE channel_id = ?`, newID, c.id); err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx, `UPDATE scheduled_recordings SET channel_id = ? WHERE channel_id = ?`, newID, c.id); err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx, `UPDATE channels SET id = ? WHERE id = ?`, newID, c.id); err != nil {
					return err
				}
			}

			return tx.Commit()
		},
	},
}

func addDefaultHWAccelMigration(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx,
		`INSERT OR IGNORE INTO core_settings (key, value) VALUES ('default_hwaccel', 'none')`)
	if err != nil {
		return err
	}

	_, err = db.ExecContext(ctx,
		`UPDATE stream_profiles SET hwaccel = 'none', args = '' WHERE name = 'Recording' AND is_system = 1`)
	return err
}
