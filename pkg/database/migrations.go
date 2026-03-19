package database

import (
	"context"
	"database/sql"

	"github.com/google/uuid"

	"github.com/gavinmcnair/tvproxy/pkg/ffmpeg"
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
					created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
					updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
				)`,

				`CREATE TABLE logos (
					id TEXT PRIMARY KEY,
					name TEXT NOT NULL,
					url TEXT NOT NULL,
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

				`CREATE TABLE channel_profiles (
					id TEXT PRIMARY KEY,
					name TEXT NOT NULL,
					stream_profile TEXT NOT NULL DEFAULT '',
					sort_order INTEGER NOT NULL DEFAULT 0,
					created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
					updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
				)`,

				`CREATE TABLE epg_sources (
					id TEXT PRIMARY KEY,
					name TEXT NOT NULL,
					url TEXT NOT NULL,
					is_enabled INTEGER NOT NULL DEFAULT 1,
					last_refreshed DATETIME,
					channel_count INTEGER NOT NULL DEFAULT 0,
					program_count INTEGER NOT NULL DEFAULT 0,
					created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
					updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
				)`,

				`CREATE TABLE streams (
					id TEXT PRIMARY KEY,
					m3u_account_id TEXT NOT NULL,
					name TEXT NOT NULL,
					url TEXT NOT NULL,
					"group" TEXT NOT NULL DEFAULT '',
					logo TEXT NOT NULL DEFAULT '',
					tvg_id TEXT NOT NULL DEFAULT '',
					tvg_name TEXT NOT NULL DEFAULT '',
					content_hash TEXT NOT NULL DEFAULT '',
					is_active INTEGER NOT NULL DEFAULT 1,
					created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
					updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
					FOREIGN KEY (m3u_account_id) REFERENCES m3u_accounts(id) ON DELETE CASCADE
				)`,
				`CREATE INDEX idx_streams_m3u_account_id ON streams(m3u_account_id)`,
				`CREATE INDEX idx_streams_content_hash ON streams(content_hash)`,

				`CREATE TABLE channels (
					id TEXT PRIMARY KEY,
					user_id TEXT NOT NULL DEFAULT '' REFERENCES users(id),
					name TEXT NOT NULL,
					logo_id TEXT,
					tvg_id TEXT NOT NULL DEFAULT '',
					channel_group_id TEXT,
					channel_profile_id TEXT,
					is_enabled INTEGER NOT NULL DEFAULT 1,
					fail_count INTEGER NOT NULL DEFAULT 0,
					created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
					updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
					FOREIGN KEY (logo_id) REFERENCES logos(id) ON DELETE SET NULL,
					FOREIGN KEY (channel_group_id) REFERENCES channel_groups(id) ON DELETE SET NULL,
					FOREIGN KEY (channel_profile_id) REFERENCES channel_profiles(id) ON DELETE SET NULL
				)`,
				`CREATE INDEX idx_channels_logo_id ON channels(logo_id)`,
				`CREATE INDEX idx_channels_user_id ON channels(user_id)`,

				`CREATE TABLE channel_streams (
					id TEXT PRIMARY KEY,
					channel_id TEXT NOT NULL,
					stream_id TEXT NOT NULL,
					priority INTEGER NOT NULL DEFAULT 0,
					FOREIGN KEY (channel_id) REFERENCES channels(id) ON DELETE CASCADE,
					FOREIGN KEY (stream_id) REFERENCES streams(id) ON DELETE CASCADE,
					UNIQUE(channel_id, stream_id)
				)`,
				`CREATE INDEX idx_channel_streams_channel_id ON channel_streams(channel_id)`,

				`CREATE TABLE epg_data (
					id TEXT PRIMARY KEY,
					epg_source_id TEXT NOT NULL,
					channel_id TEXT NOT NULL,
					name TEXT NOT NULL,
					icon TEXT NOT NULL DEFAULT '',
					FOREIGN KEY (epg_source_id) REFERENCES epg_sources(id) ON DELETE CASCADE
				)`,
				`CREATE INDEX idx_epg_data_channel_id ON epg_data(channel_id)`,
				`CREATE INDEX idx_epg_data_epg_source_id ON epg_data(epg_source_id)`,

				`CREATE TABLE program_data (
					id TEXT PRIMARY KEY,
					epg_data_id TEXT NOT NULL,
					title TEXT NOT NULL,
					description TEXT NOT NULL DEFAULT '',
					start DATETIME NOT NULL,
					stop DATETIME NOT NULL,
					category TEXT NOT NULL DEFAULT '',
					episode_num TEXT NOT NULL DEFAULT '',
					icon TEXT NOT NULL DEFAULT '',
					FOREIGN KEY (epg_data_id) REFERENCES epg_data(id) ON DELETE CASCADE
				)`,
				`CREATE INDEX idx_program_data_epg_data_id ON program_data(epg_data_id)`,
				`CREATE INDEX idx_program_data_start_stop ON program_data(start, stop)`,

				`CREATE TABLE hdhr_devices (
					id TEXT PRIMARY KEY,
					name TEXT NOT NULL,
					device_id TEXT NOT NULL UNIQUE,
					device_auth TEXT NOT NULL,
					firmware_version TEXT NOT NULL DEFAULT '20240101',
					tuner_count INTEGER NOT NULL DEFAULT 2,
					port INTEGER NOT NULL DEFAULT 0,
					channel_profile_id TEXT,
					is_enabled INTEGER NOT NULL DEFAULT 1,
					created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
					updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
					FOREIGN KEY (channel_profile_id) REFERENCES channel_profiles(id) ON DELETE SET NULL
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
		{"Browser", "ffmpeg", "m3u", "none", "copy", "mp4", false, false},
		{"SAT>IP Copy", "ffmpeg", "satip", "none", "copy", "mpegts", false, false},
		{"M3U Copy", "ffmpeg", "m3u", "none", "copy", "mpegts", false, false},
		{"M3U → MP4", "ffmpeg", "m3u", "none", "copy", "mp4", false, false},
		{"M3U → Matroska", "ffmpeg", "m3u", "none", "copy", "matroska", false, false},
	}

	for _, p := range profiles {
		id := uuid.New().String()
		args := ffmpeg.ComposeStreamProfileArgs(p.sourceType, p.hwaccel, p.videoCodec, p.container)
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

	type clientSeed struct {
		name       string
		priority   int
		sourceType string
		container  string
		rules      []struct {
			headerName string
			matchType  string
			matchValue string
		}
	}

	clients := []clientSeed{
		{
			name:       "Plex",
			priority:   10,
			sourceType: "m3u",
			container:  "mpegts",
			rules: []struct {
				headerName string
				matchType  string
				matchValue string
			}{
				{"User-Agent", "contains", "Lavf/"},
				{"Icy-Metadata", "exists", ""},
			},
		},
		{
			name:       "VLC",
			priority:   20,
			sourceType: "m3u",
			container:  "matroska",
			rules: []struct {
				headerName string
				matchType  string
				matchValue string
			}{
				{"User-Agent", "contains", "VLC/"},
			},
		},
		{
			name:       "Browser",
			priority:   100,
			sourceType: "m3u",
			container:  "mp4",
			rules: []struct {
				headerName string
				matchType  string
				matchValue string
			}{
				{"User-Agent", "contains", "Mozilla/"},
			},
		},
	}

	for _, c := range clients {
		args := ffmpeg.ComposeStreamProfileArgs(c.sourceType, "none", "copy", c.container)
		profileID := uuid.New().String()
		if _, err := db.ExecContext(ctx,
			`INSERT INTO stream_profiles (id, name, stream_mode, source_type, hwaccel, video_codec, container, custom_args, command, args, is_default, is_system, is_client)
			 VALUES (?, ?, 'ffmpeg', ?, 'none', 'copy', ?, '', 'ffmpeg', ?, 0, 0, 1)`,
			profileID, c.name, c.sourceType, c.container, args); err != nil {
			return err
		}

		clientID := uuid.New().String()
		if _, err := db.ExecContext(ctx,
			`INSERT INTO clients (id, name, priority, stream_profile_id, is_enabled) VALUES (?, ?, ?, ?, 1)`,
			clientID, c.name, c.priority, profileID); err != nil {
			return err
		}

		for _, r := range c.rules {
			ruleID := uuid.New().String()
			if _, err := db.ExecContext(ctx,
				`INSERT INTO client_match_rules (id, client_id, header_name, match_type, match_value) VALUES (?, ?, ?, ?, ?)`,
				ruleID, clientID, r.headerName, r.matchType, r.matchValue); err != nil {
				return err
			}
		}
	}

	return nil
}

type execContext interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}
