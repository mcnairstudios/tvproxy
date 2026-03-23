package models

import "time"

type User struct {
	ID              string     `json:"id"`
	Username        string     `json:"username"`
	PasswordHash    string     `json:"-"`
	IsAdmin         bool       `json:"is_admin"`
	InviteToken     *string    `json:"invite_token,omitempty"`
	InviteExpiresAt *time.Time `json:"invite_expires_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type M3UAccount struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	URL             string     `json:"url"`
	Type            string     `json:"type"` // "m3u" or "xtream"
	Username        string     `json:"username,omitempty"`
	Password        string     `json:"password,omitempty"`
	MaxStreams      int        `json:"max_streams"`
	IsEnabled       bool       `json:"is_enabled"`
	LastRefreshed   *time.Time `json:"last_refreshed,omitempty"`
	StreamCount     int        `json:"stream_count"`
	RefreshInterval int        `json:"refresh_interval"` // seconds, 0 = use default
	LastError       string     `json:"last_error"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type Stream struct {
	ID           string    `json:"id"`
	M3UAccountID string    `json:"m3u_account_id"`
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
	ID              string    `json:"id"`
	UserID          string    `json:"user_id"`
	Name            string    `json:"name"`
	LogoID          *string   `json:"logo_id,omitempty"`
	Logo            string    `json:"logo,omitempty"`
	LogoCached      string    `json:"logo_cached,omitempty"`
	TvgID           string    `json:"tvg_id,omitempty"`
	ChannelGroupID  *string   `json:"channel_group_id,omitempty"`
	StreamProfileID *string   `json:"stream_profile_id,omitempty"`
	FailCount       int       `json:"fail_count"`
	IsEnabled       bool      `json:"is_enabled"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type ChannelStream struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
	StreamID  string `json:"stream_id"`
	Priority  int    `json:"priority"`
}

type ChannelGroup struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Name      string    `json:"name"`
	IsEnabled bool      `json:"is_enabled"`
	SortOrder int       `json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Logo struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	URL            string    `json:"url"`
	CachedFilename string    `json:"cached_filename,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type StreamProfile struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	StreamMode    string    `json:"stream_mode"`
	SourceType    string    `json:"source_type"`
	HWAccel       string    `json:"hwaccel"`
	VideoCodec    string    `json:"video_codec"`
	Container     string    `json:"container"`
	Deinterlace   bool      `json:"deinterlace"`
	FPSMode       string    `json:"fps_mode"`
	UseCustomArgs bool      `json:"use_custom_args"`
	CustomArgs    string    `json:"custom_args,omitempty"`
	Command       string    `json:"command,omitempty"`
	Args          string    `json:"args,omitempty"`
	IsDefault     bool      `json:"is_default"`
	IsSystem      bool      `json:"is_system"`
	IsClient      bool      `json:"is_client"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type EPGSource struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	URL           string     `json:"url"`
	IsEnabled     bool       `json:"is_enabled"`
	LastRefreshed *time.Time `json:"last_refreshed,omitempty"`
	ChannelCount  int        `json:"channel_count"`
	ProgramCount  int        `json:"program_count"`
	LastError     string     `json:"last_error"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type EPGData struct {
	ID          string `json:"id"`
	EPGSourceID string `json:"epg_source_id"`
	ChannelID   string `json:"channel_id"`
	Name        string `json:"name"`
	Icon        string `json:"icon,omitempty"`
}

type ProgramData struct {
	ID               string    `json:"id"`
	EPGDataID        string    `json:"epg_data_id"`
	Title            string    `json:"title"`
	Description      string    `json:"description,omitempty"`
	Start            time.Time `json:"start"`
	Stop             time.Time `json:"stop"`
	Category         string    `json:"category,omitempty"`
	EpisodeNum       string    `json:"episode_num,omitempty"`
	Icon             string    `json:"icon,omitempty"`
	Subtitle         string    `json:"subtitle,omitempty"`
	Date             string    `json:"date,omitempty"`
	Language         string    `json:"language,omitempty"`
	IsNew            bool      `json:"is_new,omitempty"`
	IsPreviouslyShown bool    `json:"is_previously_shown,omitempty"`
	Credits          string    `json:"credits,omitempty"`
	Rating           string    `json:"rating,omitempty"`
	RatingIcon       string    `json:"rating_icon,omitempty"`
	StarRating       string    `json:"star_rating,omitempty"`
	SubCategories    string    `json:"sub_categories,omitempty"`
	EpisodeNumSystem string    `json:"episode_num_system,omitempty"`
}

type HDHRDevice struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	DeviceID        string    `json:"device_id"`
	DeviceAuth      string    `json:"device_auth"`
	FirmwareVersion string    `json:"firmware_version"`
	TunerCount      int       `json:"tuner_count"`
	Port            int       `json:"port"`
	ChannelGroupIDs []string  `json:"channel_group_ids,omitempty"`
	IsEnabled       bool      `json:"is_enabled"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type CoreSetting struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type Client struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Priority        int               `json:"priority"`
	StreamProfileID string            `json:"stream_profile_id"`
	IsEnabled       bool              `json:"is_enabled"`
	MatchRules      []ClientMatchRule `json:"match_rules"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

type ClientMatchRule struct {
	ID         string `json:"id"`
	ClientID   string `json:"client_id"`
	HeaderName string `json:"header_name"`
	MatchType  string `json:"match_type"`
	MatchValue string `json:"match_value"`
}

type ActiveViewer struct {
	ID           string  `json:"id"`
	ChannelID    string  `json:"channel_id,omitempty"`
	ChannelName  string  `json:"channel_name,omitempty"`
	StreamID     string  `json:"stream_id,omitempty"`
	StreamName   string  `json:"stream_name,omitempty"`
	M3UAccountID string  `json:"m3u_account_id,omitempty"`
	ProfileName  string  `json:"profile_name"`
	UserAgent    string  `json:"user_agent"`
	ClientName   string  `json:"client_name,omitempty"`
	RemoteAddr   string  `json:"remote_addr"`
	StartedAt    string  `json:"started_at"`
	LastActive   string  `json:"last_active"`
	IdleSecs     float64 `json:"idle_secs"`
	Type         string  `json:"type"`
}

type StreamSummary struct {
	ID           string `json:"id"`
	M3UAccountID string `json:"m3u_account_id"`
	Name         string `json:"name"`
	Group        string `json:"group"`
	Logo         string `json:"logo,omitempty"`
}

type GuideProgram struct {
	ChannelID   string    `json:"channel_id"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	Start       time.Time `json:"start"`
	Stop        time.Time `json:"stop"`
	Category    string    `json:"category,omitempty"`
}

type ScheduledRecording struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id"`
	ChannelID    string    `json:"channel_id"`
	ChannelName  string    `json:"channel_name"`
	ProgramTitle string    `json:"program_title"`
	StartAt      time.Time `json:"start_at"`
	StopAt       time.Time `json:"stop_at"`
	Status       string    `json:"status"`
	SessionID    string    `json:"session_id,omitempty"`
	SegmentID    string    `json:"segment_id,omitempty"`
	FilePath     string    `json:"file_path,omitempty"`
	LastError    string    `json:"last_error,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
