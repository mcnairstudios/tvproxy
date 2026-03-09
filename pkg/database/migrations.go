package database

type migration struct {
	name string
	sql  string
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
}
