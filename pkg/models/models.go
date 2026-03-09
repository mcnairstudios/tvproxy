package models

import "time"

type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	IsAdmin      bool      `json:"is_admin"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type M3UAccount struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	URL            string    `json:"url"`
	Type           string    `json:"type"` // "m3u" or "xtream"
	Username       string    `json:"username,omitempty"`
	Password       string    `json:"password,omitempty"`
	MaxStreams      int       `json:"max_streams"`
	IsEnabled      bool      `json:"is_enabled"`
	LastRefreshed  *time.Time `json:"last_refreshed,omitempty"`
	StreamCount    int       `json:"stream_count"`
	RefreshInterval int      `json:"refresh_interval"` // seconds, 0 = use default
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Stream struct {
	ID           int64     `json:"id"`
	M3UAccountID int64     `json:"m3u_account_id"`
	Name         string    `json:"name"`
	URL          string    `json:"url"`
	Group        string    `json:"group"`
	Logo         string    `json:"logo,omitempty"`
	TvgID        string    `json:"tvg_id,omitempty"`
	TvgName      string    `json:"tvg_name,omitempty"`
	ContentHash  string    `json:"content_hash"`
	IsActive     bool      `json:"is_active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Channel struct {
	ID               int64     `json:"id"`
	ChannelNumber    int       `json:"channel_number"`
	Name             string    `json:"name"`
	Logo             string    `json:"logo,omitempty"`
	TvgID            string    `json:"tvg_id,omitempty"`
	ChannelGroupID   *int64    `json:"channel_group_id,omitempty"`
	ChannelProfileID *int64    `json:"channel_profile_id,omitempty"`
	IsEnabled        bool      `json:"is_enabled"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type ChannelStream struct {
	ID        int64 `json:"id"`
	ChannelID int64 `json:"channel_id"`
	StreamID  int64 `json:"stream_id"`
	Priority  int   `json:"priority"`
}

type ChannelGroup struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	IsEnabled bool      `json:"is_enabled"`
	SortOrder int       `json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ChannelProfile struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	StreamProfile  string    `json:"stream_profile,omitempty"`
	SortOrder      int       `json:"sort_order"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Logo struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"created_at"`
}

type StreamProfile struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Command     string    `json:"command,omitempty"`
	Args        string    `json:"args,omitempty"`
	IsDefault   bool      `json:"is_default"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type EPGSource struct {
	ID            int64      `json:"id"`
	Name          string     `json:"name"`
	URL           string     `json:"url"`
	IsEnabled     bool       `json:"is_enabled"`
	LastRefreshed *time.Time `json:"last_refreshed,omitempty"`
	ChannelCount  int        `json:"channel_count"`
	ProgramCount  int        `json:"program_count"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type EPGData struct {
	ID          int64  `json:"id"`
	EPGSourceID int64  `json:"epg_source_id"`
	ChannelID   string `json:"channel_id"`
	Name        string `json:"name"`
	Icon        string `json:"icon,omitempty"`
}

type ProgramData struct {
	ID          int64     `json:"id"`
	EPGDataID   int64     `json:"epg_data_id"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	Start       time.Time `json:"start"`
	Stop        time.Time `json:"stop"`
	Category    string    `json:"category,omitempty"`
	EpisodeNum  string    `json:"episode_num,omitempty"`
	Icon        string    `json:"icon,omitempty"`
}

type HDHRDevice struct {
	ID              int64     `json:"id"`
	Name            string    `json:"name"`
	DeviceID        string    `json:"device_id"`
	DeviceAuth      string    `json:"device_auth"`
	FirmwareVersion string    `json:"firmware_version"`
	TunerCount      int       `json:"tuner_count"`
	ChannelProfileID *int64   `json:"channel_profile_id,omitempty"`
	IsEnabled       bool      `json:"is_enabled"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type CoreSetting struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type UserAgent struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	UserAgent string    `json:"user_agent"`
	IsDefault bool      `json:"is_default"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
