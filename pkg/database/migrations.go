package database

import (
	"context"
	"database/sql"

	"github.com/gavinmcnair/tvproxy/pkg/ffmpeg"
)

type migration struct {
	name string
	sql  string
	fn   func(ctx context.Context, db *sql.DB) error // optional Go migration (used instead of sql if set)
}

var migrations = []migration{
	{
		name: "create_users",
		sql: `CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			is_admin INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
	},
	{
		name: "create_m3u_accounts",
		sql: `CREATE TABLE m3u_accounts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
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
	},
	{
		name: "create_streams",
		sql: `CREATE TABLE streams (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			m3u_account_id INTEGER NOT NULL,
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
		);
		CREATE INDEX idx_streams_m3u_account_id ON streams(m3u_account_id);
		CREATE INDEX idx_streams_content_hash ON streams(content_hash)`,
	},
	{
		name: "create_channel_groups",
		sql: `CREATE TABLE channel_groups (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			is_enabled INTEGER NOT NULL DEFAULT 1,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
	},
	{
		name: "create_channel_profiles",
		sql: `CREATE TABLE channel_profiles (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			stream_profile TEXT NOT NULL DEFAULT '',
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
	},
	{
		name: "create_channels",
		sql: `CREATE TABLE channels (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			channel_number INTEGER NOT NULL UNIQUE,
			name TEXT NOT NULL,
			logo TEXT NOT NULL DEFAULT '',
			tvg_id TEXT NOT NULL DEFAULT '',
			channel_group_id INTEGER,
			channel_profile_id INTEGER,
			is_enabled INTEGER NOT NULL DEFAULT 1,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (channel_group_id) REFERENCES channel_groups(id) ON DELETE SET NULL,
			FOREIGN KEY (channel_profile_id) REFERENCES channel_profiles(id) ON DELETE SET NULL
		);
		CREATE INDEX idx_channels_channel_number ON channels(channel_number)`,
	},
	{
		name: "create_channel_streams",
		sql: `CREATE TABLE channel_streams (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			channel_id INTEGER NOT NULL,
			stream_id INTEGER NOT NULL,
			priority INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY (channel_id) REFERENCES channels(id) ON DELETE CASCADE,
			FOREIGN KEY (stream_id) REFERENCES streams(id) ON DELETE CASCADE,
			UNIQUE(channel_id, stream_id)
		);
		CREATE INDEX idx_channel_streams_channel_id ON channel_streams(channel_id)`,
	},
	{
		name: "create_logos",
		sql: `CREATE TABLE logos (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			url TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
	},
	{
		name: "create_stream_profiles",
		sql: `CREATE TABLE stream_profiles (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			command TEXT NOT NULL DEFAULT '',
			args TEXT NOT NULL DEFAULT '',
			is_default INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
	},
	{
		name: "create_epg_sources",
		sql: `CREATE TABLE epg_sources (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			url TEXT NOT NULL,
			is_enabled INTEGER NOT NULL DEFAULT 1,
			last_refreshed DATETIME,
			channel_count INTEGER NOT NULL DEFAULT 0,
			program_count INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
	},
	{
		name: "create_epg_data",
		sql: `CREATE TABLE epg_data (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			epg_source_id INTEGER NOT NULL,
			channel_id TEXT NOT NULL,
			name TEXT NOT NULL,
			icon TEXT NOT NULL DEFAULT '',
			FOREIGN KEY (epg_source_id) REFERENCES epg_sources(id) ON DELETE CASCADE
		);
		CREATE INDEX idx_epg_data_channel_id ON epg_data(channel_id);
		CREATE INDEX idx_epg_data_epg_source_id ON epg_data(epg_source_id)`,
	},
	{
		name: "create_program_data",
		sql: `CREATE TABLE program_data (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			epg_data_id INTEGER NOT NULL,
			title TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			start DATETIME NOT NULL,
			stop DATETIME NOT NULL,
			category TEXT NOT NULL DEFAULT '',
			episode_num TEXT NOT NULL DEFAULT '',
			icon TEXT NOT NULL DEFAULT '',
			FOREIGN KEY (epg_data_id) REFERENCES epg_data(id) ON DELETE CASCADE
		);
		CREATE INDEX idx_program_data_epg_data_id ON program_data(epg_data_id);
		CREATE INDEX idx_program_data_start_stop ON program_data(start, stop)`,
	},
	{
		name: "create_hdhr_devices",
		sql: `CREATE TABLE hdhr_devices (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			device_id TEXT NOT NULL UNIQUE,
			device_auth TEXT NOT NULL,
			firmware_version TEXT NOT NULL DEFAULT '20240101',
			tuner_count INTEGER NOT NULL DEFAULT 2,
			channel_profile_id INTEGER,
			is_enabled INTEGER NOT NULL DEFAULT 1,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (channel_profile_id) REFERENCES channel_profiles(id) ON DELETE SET NULL
		)`,
	},
	{
		name: "create_core_settings",
		sql: `CREATE TABLE core_settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL DEFAULT ''
		)`,
	},
	{
		name: "create_user_agents",
		sql: `CREATE TABLE user_agents (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			user_agent TEXT NOT NULL,
			is_default INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		INSERT INTO user_agents (name, user_agent, is_default) VALUES ('VLC', 'VLC/3.0.20 LibVLC/3.0.20', 1)`,
	},
	{
		name: "insert_default_stream_profile",
		sql: `INSERT INTO stream_profiles (name, is_default) VALUES ('Direct (No Transcoding)', 1)`,
	},
	{
		name: "insert_stream_profiles_v2",
		sql: `INSERT INTO stream_profiles (name, command, args) VALUES
			('SAT>IP Direct', 'ffmpeg', '-hide_banner -loglevel fatal -i {input} -c copy -bsf:v dump_extra -f mpegts pipe:1 -rw_timeout 5000000'),
			('M3U Direct', 'ffmpeg', '-hide_banner -loglevel error -analyzeduration 1000000 -probesize 1000000 -i {input} -map 0:v -map 0:a:0 -c:v copy -c:a aac -b:a 192k -ac 2 -c:s copy -f mpegts -fflags +genpts -movflags +faststart -copyts pipe:1'),
			('SAT>IP Intel QSV', 'ffmpeg', '-hide_banner -loglevel fatal -hwaccel qsv -hwaccel_output_format qsv -i {input} -c:v h264_qsv -preset fast -c:a copy -bsf:v dump_extra -f mpegts pipe:1 -rw_timeout 5000000'),
			('SAT>IP NVIDIA NVENC', 'ffmpeg', '-hide_banner -loglevel fatal -hwaccel cuda -hwaccel_output_format cuda -i {input} -c:v h264_nvenc -preset p4 -c:a copy -bsf:v dump_extra -f mpegts pipe:1 -rw_timeout 5000000'),
			('M3U Intel QSV', 'ffmpeg', '-hide_banner -loglevel error -hwaccel qsv -hwaccel_output_format qsv -analyzeduration 1000000 -probesize 1000000 -i {input} -map 0:v -map 0:a:0 -c:v h264_qsv -preset fast -c:a aac -b:a 192k -ac 2 -c:s copy -f mpegts -fflags +genpts -movflags +faststart -copyts pipe:1'),
			('M3U NVIDIA NVENC', 'ffmpeg', '-hide_banner -loglevel error -hwaccel cuda -hwaccel_output_format cuda -analyzeduration 1000000 -probesize 1000000 -i {input} -map 0:v -map 0:a:0 -c:v h264_nvenc -preset p4 -c:a aac -b:a 192k -ac 2 -c:s copy -f mpegts -fflags +genpts -movflags +faststart -copyts pipe:1'),
			('SAT>IP AV1 Intel QSV', 'ffmpeg', '-hide_banner -loglevel fatal -hwaccel qsv -hwaccel_output_format qsv -i {input} -c:v av1_qsv -preset fast -c:a copy -bsf:v dump_extra -f mpegts pipe:1 -rw_timeout 5000000'),
			('SAT>IP AV1 NVIDIA NVENC', 'ffmpeg', '-hide_banner -loglevel fatal -hwaccel cuda -hwaccel_output_format cuda -i {input} -c:v av1_nvenc -preset p4 -c:a copy -bsf:v dump_extra -f mpegts pipe:1 -rw_timeout 5000000'),
			('M3U AV1 Intel QSV', 'ffmpeg', '-hide_banner -loglevel error -hwaccel qsv -hwaccel_output_format qsv -analyzeduration 1000000 -probesize 1000000 -i {input} -map 0:v -map 0:a:0 -c:v av1_qsv -preset fast -c:a aac -b:a 192k -ac 2 -c:s copy -f mpegts -fflags +genpts -movflags +faststart -copyts pipe:1'),
			('M3U AV1 NVIDIA NVENC', 'ffmpeg', '-hide_banner -loglevel error -hwaccel cuda -hwaccel_output_format cuda -analyzeduration 1000000 -probesize 1000000 -i {input} -map 0:v -map 0:a:0 -c:v av1_nvenc -preset p4 -c:a aac -b:a 192k -ac 2 -c:s copy -f mpegts -fflags +genpts -movflags +faststart -copyts pipe:1')`,
	},
	{
		name: "add_stream_profile_dropdown_columns",
		sql: `ALTER TABLE stream_profiles ADD COLUMN source_type TEXT NOT NULL DEFAULT 'direct';
		ALTER TABLE stream_profiles ADD COLUMN hwaccel TEXT NOT NULL DEFAULT 'none';
		ALTER TABLE stream_profiles ADD COLUMN video_codec TEXT NOT NULL DEFAULT 'copy';
		ALTER TABLE stream_profiles ADD COLUMN custom_args TEXT NOT NULL DEFAULT '';
		UPDATE stream_profiles SET source_type = 'direct', hwaccel = 'none', video_codec = 'copy' WHERE name = 'Direct (No Transcoding)';
		UPDATE stream_profiles SET source_type = 'satip', hwaccel = 'none', video_codec = 'copy' WHERE name = 'SAT>IP Direct';
		UPDATE stream_profiles SET source_type = 'm3u', hwaccel = 'none', video_codec = 'copy' WHERE name = 'M3U Direct';
		DELETE FROM stream_profiles WHERE name IN ('SAT>IP Intel QSV', 'SAT>IP NVIDIA NVENC', 'M3U Intel QSV', 'M3U NVIDIA NVENC', 'SAT>IP AV1 Intel QSV', 'SAT>IP AV1 NVIDIA NVENC', 'M3U AV1 Intel QSV', 'M3U AV1 NVIDIA NVENC')`,
	},
	{
		name: "recompose_stream_profile_args",
		fn: func(ctx context.Context, db *sql.DB) error {
			rows, err := db.QueryContext(ctx,
				`SELECT id, source_type, hwaccel, video_codec, custom_args FROM stream_profiles WHERE custom_args = ''`)
			if err != nil {
				return err
			}
			defer rows.Close()

			type profile struct {
				id         int64
				sourceType string
				hwaccel    string
				videoCodec string
			}
			var profiles []profile
			for rows.Next() {
				var p profile
				var customArgs string
				if err := rows.Scan(&p.id, &p.sourceType, &p.hwaccel, &p.videoCodec, &customArgs); err != nil {
					return err
				}
				profiles = append(profiles, p)
			}
			if err := rows.Err(); err != nil {
				return err
			}

			for _, p := range profiles {
				container := ffmpeg.DefaultContainer(p.videoCodec)
				args := ffmpeg.ComposeStreamProfileArgs(p.sourceType, p.hwaccel, p.videoCodec, container)
				command := "ffmpeg"
				if args == "" {
					command = ""
				}
				if _, err := db.ExecContext(ctx,
					`UPDATE stream_profiles SET command = ?, args = ? WHERE id = ?`,
					command, args, p.id); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		name: "add_container_column",
		sql:  `ALTER TABLE stream_profiles ADD COLUMN container TEXT NOT NULL DEFAULT 'mpegts'`,
	},
	{
		name: "unify_custom_args_and_container",
		fn: func(ctx context.Context, db *sql.DB) error {
			rows, err := db.QueryContext(ctx,
				`SELECT id, source_type, hwaccel, video_codec, custom_args, args FROM stream_profiles`)
			if err != nil {
				return err
			}
			defer rows.Close()

			type profile struct {
				id         int64
				sourceType string
				hwaccel    string
				videoCodec string
				customArgs string
				args       string
			}
			var profiles []profile
			for rows.Next() {
				var p profile
				if err := rows.Scan(&p.id, &p.sourceType, &p.hwaccel, &p.videoCodec, &p.customArgs, &p.args); err != nil {
					return err
				}
				profiles = append(profiles, p)
			}
			if err := rows.Err(); err != nil {
				return err
			}

			for _, p := range profiles {
				container := ffmpeg.DefaultContainer(p.videoCodec)
				var customArgs string
				if p.customArgs != "" {
					customArgs = p.customArgs
				} else if p.args != "" {
					customArgs = p.args
				} else {
					customArgs = ffmpeg.ComposeStreamProfileArgs(p.sourceType, p.hwaccel, p.videoCodec, container)
				}
				command := "ffmpeg"
				if _, err := db.ExecContext(ctx,
					`UPDATE stream_profiles SET container = ?, custom_args = ?, command = ?, args = ? WHERE id = ?`,
					container, customArgs, command, customArgs, p.id); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		name: "seed_container_profiles",
		sql: `INSERT INTO stream_profiles (name, source_type, hwaccel, video_codec, container, custom_args, command, args, is_default, created_at, updated_at)
		VALUES
			('M3U → MP4 (Browser/Plex)', 'm3u', 'none', 'copy', 'mp4',
			 '',
			 'ffmpeg',
			 '-hide_banner -loglevel warning -analyzeduration 1000000 -probesize 1000000 -i {input} -map 0:v:0 -map 0:a:0 -c:v copy -c:a aac -b:a 128k -ac 2 -f mp4 -movflags frag_keyframe+empty_moov+default_base_moof -fflags +genpts pipe:1',
			 0, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
			('M3U → Matroska (VLC)', 'm3u', 'none', 'copy', 'matroska',
			 '',
			 'ffmpeg',
			 '-hide_banner -loglevel warning -analyzeduration 1000000 -probesize 1000000 -i {input} -map 0:v:0 -map 0:a:0 -c:v copy -c:a aac -b:a 128k -ac 2 -f matroska -fflags +genpts -copyts pipe:1',
			 0, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
	},
	{
		name: "clear_custom_args_extras",
		sql:  `UPDATE stream_profiles SET custom_args = '' WHERE custom_args = args AND custom_args != ''`,
	},
	{
		name: "reset_stream_profiles_to_defaults",
		fn: func(ctx context.Context, db *sql.DB) error {
			// Wipe all existing stream profiles
			if _, err := db.ExecContext(ctx, `DELETE FROM stream_profiles`); err != nil {
				return err
			}
			// Reset autoincrement so IDs start fresh
			if _, err := db.ExecContext(ctx, `DELETE FROM sqlite_sequence WHERE name = 'stream_profiles'`); err != nil {
				return err
			}

			type seed struct {
				name       string
				sourceType string
				hwaccel    string
				videoCodec string
				container  string
				isDefault  bool
			}

			seeds := []seed{
				{"Direct (No Transcoding)", "direct", "none", "copy", "mpegts", true},
				{"SAT>IP Direct", "satip", "none", "copy", "mpegts", false},
				{"M3U Direct", "m3u", "none", "copy", "mpegts", false},
				{"M3U → MP4 (Browser/Plex)", "m3u", "none", "copy", "mp4", false},
				{"M3U → Matroska (VLC)", "m3u", "none", "copy", "matroska", false},
			}

			for _, s := range seeds {
				args := ffmpeg.ComposeStreamProfileArgs(s.sourceType, s.hwaccel, s.videoCodec, s.container)
				command := "ffmpeg"
				if args == "" {
					command = ""
				}
				if _, err := db.ExecContext(ctx,
					`INSERT INTO stream_profiles (name, source_type, hwaccel, video_codec, container, custom_args, command, args, is_default)
					 VALUES (?, ?, ?, ?, ?, '', ?, ?, ?)`,
					s.name, s.sourceType, s.hwaccel, s.videoCodec, s.container, command, args, s.isDefault); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		name: "reset_stream_profiles_v2",
		fn: func(ctx context.Context, db *sql.DB) error {
			if _, err := db.ExecContext(ctx, `DELETE FROM stream_profiles`); err != nil {
				return err
			}
			if _, err := db.ExecContext(ctx, `DELETE FROM sqlite_sequence WHERE name = 'stream_profiles'`); err != nil {
				return err
			}

			type seed struct {
				name       string
				sourceType string
				hwaccel    string
				videoCodec string
				container  string
				isDefault  bool
			}

			seeds := []seed{
				{"Direct (No Transcoding)", "direct", "none", "copy", "mpegts", true},
				{"SAT>IP Direct", "satip", "none", "copy", "mpegts", false},
				{"M3U Direct", "m3u", "none", "copy", "mpegts", false},
				{"M3U → MP4 (Browser/Plex)", "m3u", "none", "copy", "mp4", false},
				{"M3U → Matroska (VLC)", "m3u", "none", "copy", "matroska", false},
			}

			for _, s := range seeds {
				args := ffmpeg.ComposeStreamProfileArgs(s.sourceType, s.hwaccel, s.videoCodec, s.container)
				command := "ffmpeg"
				if args == "" {
					command = ""
				}
				if _, err := db.ExecContext(ctx,
					`INSERT INTO stream_profiles (name, source_type, hwaccel, video_codec, container, custom_args, command, args, is_default)
					 VALUES (?, ?, ?, ?, ?, '', ?, ?, ?)`,
					s.name, s.sourceType, s.hwaccel, s.videoCodec, s.container, command, args, s.isDefault); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		name: "add_use_custom_args_column",
		sql:  `ALTER TABLE stream_profiles ADD COLUMN use_custom_args INTEGER NOT NULL DEFAULT 0`,
	},
	{
		name: "add_stream_mode_columns",
		fn: func(ctx context.Context, db *sql.DB) error {
			if _, err := db.ExecContext(ctx, `ALTER TABLE stream_profiles ADD COLUMN stream_mode TEXT NOT NULL DEFAULT 'ffmpeg'`); err != nil {
				return err
			}
			// Backfill: "Direct (No Transcoding)" gets stream_mode='direct', all others stay 'ffmpeg'
			if _, err := db.ExecContext(ctx, `UPDATE stream_profiles SET stream_mode = 'direct' WHERE source_type = 'direct'`); err != nil {
				return err
			}
			return nil
		},
	},
	{
		name: "reset_stream_profiles_v3",
		fn: func(ctx context.Context, db *sql.DB) error {
			if _, err := db.ExecContext(ctx, `DELETE FROM stream_profiles`); err != nil {
				return err
			}
			if _, err := db.ExecContext(ctx, `DELETE FROM sqlite_sequence WHERE name = 'stream_profiles'`); err != nil {
				return err
			}

			type seed struct {
				name       string
				streamMode string
				sourceType string
				hwaccel    string
				videoCodec string
				container  string
				isDefault  bool
			}

			seeds := []seed{
				{"Direct", "direct", "m3u", "none", "copy", "mpegts", true},
				{"Proxy", "proxy", "m3u", "none", "copy", "mpegts", false},
				{"SAT>IP Copy", "ffmpeg", "satip", "none", "copy", "mpegts", false},
				{"M3U Copy", "ffmpeg", "m3u", "none", "copy", "mpegts", false},
				{"M3U → MP4 (Browser/Plex)", "ffmpeg", "m3u", "none", "copy", "mp4", false},
				{"M3U → Matroska (VLC)", "ffmpeg", "m3u", "none", "copy", "matroska", false},
			}

			for _, s := range seeds {
				args := ffmpeg.ComposeStreamProfileArgs(s.sourceType, s.hwaccel, s.videoCodec, s.container)
				command := "ffmpeg"
				// Direct and proxy modes don't use ffmpeg
				if s.streamMode == "direct" || s.streamMode == "proxy" {
					command = ""
					args = ""
				}
				if _, err := db.ExecContext(ctx,
					`INSERT INTO stream_profiles (name, stream_mode, source_type, hwaccel, video_codec, container, custom_args, command, args, is_default)
					 VALUES (?, ?, ?, ?, ?, ?, '', ?, ?, ?)`,
					s.name, s.streamMode, s.sourceType, s.hwaccel, s.videoCodec, s.container, command, args, s.isDefault); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		name: "add_is_system_and_browser_profile",
		fn: func(ctx context.Context, db *sql.DB) error {
			// Add is_system column
			if _, err := db.ExecContext(ctx, `ALTER TABLE stream_profiles ADD COLUMN is_system INTEGER NOT NULL DEFAULT 0`); err != nil {
				return err
			}

			// Wipe and re-seed with system profiles
			if _, err := db.ExecContext(ctx, `DELETE FROM stream_profiles`); err != nil {
				return err
			}
			if _, err := db.ExecContext(ctx, `DELETE FROM sqlite_sequence WHERE name = 'stream_profiles'`); err != nil {
				return err
			}

			type seed struct {
				name       string
				streamMode string
				sourceType string
				hwaccel    string
				videoCodec string
				container  string
				isDefault  bool
				isSystem   bool
			}

			seeds := []seed{
				{"Direct", "direct", "m3u", "none", "copy", "mpegts", true, true},
				{"Proxy", "proxy", "m3u", "none", "copy", "mpegts", false, true},
				{"Browser", "ffmpeg", "m3u", "none", "copy", "mp4", false, true},
				{"SAT>IP Copy", "ffmpeg", "satip", "none", "copy", "mpegts", false, false},
				{"M3U Copy", "ffmpeg", "m3u", "none", "copy", "mpegts", false, false},
				{"M3U → MP4", "ffmpeg", "m3u", "none", "copy", "mp4", false, false},
				{"M3U → Matroska", "ffmpeg", "m3u", "none", "copy", "matroska", false, false},
			}

			for _, s := range seeds {
				args := ffmpeg.ComposeStreamProfileArgs(s.sourceType, s.hwaccel, s.videoCodec, s.container)
				command := "ffmpeg"
				if s.streamMode == "direct" || s.streamMode == "proxy" {
					command = ""
					args = ""
				}
				if _, err := db.ExecContext(ctx,
					`INSERT INTO stream_profiles (name, stream_mode, source_type, hwaccel, video_codec, container, custom_args, command, args, is_default, is_system)
					 VALUES (?, ?, ?, ?, ?, ?, '', ?, ?, ?, ?)`,
					s.name, s.streamMode, s.sourceType, s.hwaccel, s.videoCodec, s.container, command, args, s.isDefault, s.isSystem); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		name: "add_port_to_hdhr_devices",
		fn: func(ctx context.Context, db *sql.DB) error {
			// Idempotent: check if column already exists (may have been added by a prior broken migration run)
			var hasPort bool
			rows2, err := db.QueryContext(ctx, `PRAGMA table_info(hdhr_devices)`)
			if err != nil {
				return err
			}
			for rows2.Next() {
				var cid int
				var name, typ string
				var notnull int
				var dflt sql.NullString
				var pk int
				if err := rows2.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
					rows2.Close()
					return err
				}
				if name == "port" {
					hasPort = true
				}
			}
			rows2.Close()

			if !hasPort {
				if _, err := db.ExecContext(ctx, `ALTER TABLE hdhr_devices ADD COLUMN port INTEGER NOT NULL DEFAULT 0`); err != nil {
					return err
				}
			}
			// Backfill existing devices with sequential ports starting at 47601.
			// Collect IDs first to avoid holding an open cursor while writing (SQLITE_BUSY).
			rows, err := db.QueryContext(ctx, `SELECT id FROM hdhr_devices WHERE port = 0 ORDER BY id`)
			if err != nil {
				return err
			}
			var ids []int64
			for rows.Next() {
				var id int64
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return err
				}
				ids = append(ids, id)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}

			// Get the next available port (in case some devices already have ports)
			var maxPort int
			if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(port), 47600) FROM hdhr_devices`).Scan(&maxPort); err != nil {
				return err
			}
			port := maxPort + 1
			if port < 47601 {
				port = 47601
			}

			for _, id := range ids {
				if _, err := db.ExecContext(ctx, `UPDATE hdhr_devices SET port = ? WHERE id = ?`, port, id); err != nil {
					return err
				}
				port++
			}
			return nil
		},
	},
	{
		name: "add_channel_group_to_hdhr_devices",
		sql:  `ALTER TABLE hdhr_devices ADD COLUMN channel_group_id INTEGER`,
	},
	{
		name: "create_hdhr_device_channel_groups",
		sql: `CREATE TABLE hdhr_device_channel_groups (
			hdhr_device_id INTEGER NOT NULL,
			channel_group_id INTEGER NOT NULL,
			PRIMARY KEY (hdhr_device_id, channel_group_id),
			FOREIGN KEY (hdhr_device_id) REFERENCES hdhr_devices(id) ON DELETE CASCADE,
			FOREIGN KEY (channel_group_id) REFERENCES channel_groups(id) ON DELETE CASCADE
		)`,
	},
	{
		name: "migrate_hdhr_device_channel_group_data",
		fn: func(ctx context.Context, db *sql.DB) error {
			// Copy existing channel_group_id values into the junction table
			rows, err := db.QueryContext(ctx, `SELECT id, channel_group_id FROM hdhr_devices WHERE channel_group_id IS NOT NULL`)
			if err != nil {
				return err
			}
			var pairs []struct {
				deviceID int64
				groupID  int64
			}
			for rows.Next() {
				var deviceID, groupID int64
				if err := rows.Scan(&deviceID, &groupID); err != nil {
					rows.Close()
					return err
				}
				pairs = append(pairs, struct {
					deviceID int64
					groupID  int64
				}{deviceID, groupID})
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}

			for _, p := range pairs {
				if _, err := db.ExecContext(ctx,
					`INSERT OR IGNORE INTO hdhr_device_channel_groups (hdhr_device_id, channel_group_id) VALUES (?, ?)`,
					p.deviceID, p.groupID); err != nil {
					return err
				}
			}

			// Drop the old column (SQLite 3.35+)
			if _, err := db.ExecContext(ctx, `ALTER TABLE hdhr_devices DROP COLUMN channel_group_id`); err != nil {
				return err
			}
			return nil
		},
	},
	{
		name: "channels_logo_id_fk",
		fn: func(ctx context.Context, db *sql.DB) error {
			// 1. Add logo_id FK column
			if _, err := db.ExecContext(ctx,
				`ALTER TABLE channels ADD COLUMN logo_id INTEGER REFERENCES logos(id) ON DELETE SET NULL`); err != nil {
				return err
			}

			// 2. Migrate existing logo text → logo_id
			rows, err := db.QueryContext(ctx, `SELECT id, name, logo FROM channels WHERE logo != ''`)
			if err != nil {
				return err
			}
			type chanLogo struct {
				id   int64
				name string
				url  string
			}
			var items []chanLogo
			for rows.Next() {
				var cl chanLogo
				if err := rows.Scan(&cl.id, &cl.name, &cl.url); err != nil {
					rows.Close()
					return err
				}
				items = append(items, cl)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}

			// Deduplicate: map URL → logo ID
			urlToLogoID := make(map[string]int64)
			for _, cl := range items {
				if logoID, ok := urlToLogoID[cl.url]; ok {
					// Reuse existing logo
					if _, err := db.ExecContext(ctx, `UPDATE channels SET logo_id = ? WHERE id = ?`, logoID, cl.id); err != nil {
						return err
					}
					continue
				}

				// Check if logo with this URL already exists
				var existingID int64
				err := db.QueryRowContext(ctx, `SELECT id FROM logos WHERE url = ?`, cl.url).Scan(&existingID)
				if err == nil {
					urlToLogoID[cl.url] = existingID
					if _, err := db.ExecContext(ctx, `UPDATE channels SET logo_id = ? WHERE id = ?`, existingID, cl.id); err != nil {
						return err
					}
					continue
				}

				// Create new logo
				res, err := db.ExecContext(ctx, `INSERT INTO logos (name, url, created_at) VALUES (?, ?, CURRENT_TIMESTAMP)`, cl.name, cl.url)
				if err != nil {
					return err
				}
				newID, err := res.LastInsertId()
				if err != nil {
					return err
				}
				urlToLogoID[cl.url] = newID
				if _, err := db.ExecContext(ctx, `UPDATE channels SET logo_id = ? WHERE id = ?`, newID, cl.id); err != nil {
					return err
				}
			}

			// 3. Drop the old logo text column
			if _, err := db.ExecContext(ctx, `ALTER TABLE channels DROP COLUMN logo`); err != nil {
				return err
			}

			// 4. Create index
			if _, err := db.ExecContext(ctx, `CREATE INDEX idx_channels_logo_id ON channels(logo_id)`); err != nil {
				return err
			}

			return nil
		},
	},
	{
		name: "create_clients_and_match_rules",
		fn: func(ctx context.Context, db *sql.DB) error {
			// Create clients table
			if _, err := db.ExecContext(ctx, `CREATE TABLE clients (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				name TEXT NOT NULL,
				priority INTEGER NOT NULL DEFAULT 0,
				stream_profile_id INTEGER NOT NULL,
				is_enabled INTEGER NOT NULL DEFAULT 1,
				created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				FOREIGN KEY (stream_profile_id) REFERENCES stream_profiles(id)
			)`); err != nil {
				return err
			}
			if _, err := db.ExecContext(ctx, `CREATE INDEX idx_clients_priority ON clients(priority)`); err != nil {
				return err
			}

			// Create client_match_rules table
			if _, err := db.ExecContext(ctx, `CREATE TABLE client_match_rules (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				client_id INTEGER NOT NULL,
				header_name TEXT NOT NULL,
				match_type TEXT NOT NULL,
				match_value TEXT NOT NULL DEFAULT '',
				FOREIGN KEY (client_id) REFERENCES clients(id) ON DELETE CASCADE
			)`); err != nil {
				return err
			}
			if _, err := db.ExecContext(ctx, `CREATE INDEX idx_client_match_rules_client_id ON client_match_rules(client_id)`); err != nil {
				return err
			}

			// Seed default clients with auto-created stream profiles
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

			seeds := []clientSeed{
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

			for _, s := range seeds {
				// Create a stream profile for this client
				args := ffmpeg.ComposeStreamProfileArgs(s.sourceType, "none", "copy", s.container)
				res, err := db.ExecContext(ctx,
					`INSERT INTO stream_profiles (name, stream_mode, source_type, hwaccel, video_codec, container, custom_args, command, args, is_default, is_system)
					 VALUES (?, 'ffmpeg', ?, 'none', 'copy', ?, '', 'ffmpeg', ?, 0, 0)`,
					s.name, s.sourceType, s.container, args)
				if err != nil {
					return err
				}
				profileID, err := res.LastInsertId()
				if err != nil {
					return err
				}

				// Create the client
				res, err = db.ExecContext(ctx,
					`INSERT INTO clients (name, priority, stream_profile_id, is_enabled) VALUES (?, ?, ?, 1)`,
					s.name, s.priority, profileID)
				if err != nil {
					return err
				}
				clientID, err := res.LastInsertId()
				if err != nil {
					return err
				}

				// Create match rules
				for _, r := range s.rules {
					if _, err := db.ExecContext(ctx,
						`INSERT INTO client_match_rules (client_id, header_name, match_type, match_value) VALUES (?, ?, ?, ?)`,
						clientID, r.headerName, r.matchType, r.matchValue); err != nil {
						return err
					}
				}
			}

			return nil
		},
	},
}
