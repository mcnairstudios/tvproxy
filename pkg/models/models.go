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
	Type            string     `json:"type"`
	Username        string     `json:"username,omitempty"`
	Password        string     `json:"password,omitempty"`
	MaxStreams      int        `json:"max_streams"`
	IsEnabled       bool       `json:"is_enabled"`
	LastRefreshed   *time.Time `json:"last_refreshed,omitempty"`
	StreamCount     int        `json:"stream_count"`
	RefreshInterval int        `json:"refresh_interval"`
	LastError       string     `json:"last_error"`
	UseWireGuard      bool       `json:"use_wireguard"`
	UseXtreamMetadata bool       `json:"use_xtream_metadata"`
	SourceProfileID   string     `json:"source_profile_id,omitempty"`
	TLSEnrolled     bool       `json:"tls_enrolled"`
	ETag            string     `json:"etag,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type SatIPSource struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	Host             string     `json:"host"`
	HTTPPort         int        `json:"http_port"`
	IsEnabled        bool       `json:"is_enabled"`
	TransmitterFile  string     `json:"transmitter_file"`
	LastScanned      *time.Time `json:"last_scanned,omitempty"`
	StreamCount      int        `json:"stream_count"`
	LastError        string     `json:"last_error"`
	SourceProfileID  string     `json:"source_profile_id,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type HDHRTuner struct {
	Host            string `json:"host"`
	DeviceID        string `json:"device_id"`
	DeviceModel     string `json:"device_model"`
	FirmwareVersion string `json:"firmware_version"`
	TunerCount      int    `json:"tuner_count"`
}

type HDHRSource struct {
	ID              string       `json:"id"`
	Name            string       `json:"name"`
	Devices         []HDHRTuner  `json:"devices"`
	IsEnabled       bool         `json:"is_enabled"`
	TunerCount      int          `json:"tuner_count"`
	LastScanned     *time.Time   `json:"last_scanned,omitempty"`
	StreamCount     int          `json:"stream_count"`
	LastError       string       `json:"last_error"`
	SourceProfileID string       `json:"source_profile_id,omitempty"`
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
}

type StreamTrack struct {
	PID       uint16 `json:"pid"`
	Type      string `json:"type"`
	Category  string `json:"category,omitempty"`
	Language  string `json:"language,omitempty"`
	AudioType uint8  `json:"audio_type,omitempty"`
	Label     string `json:"label,omitempty"`
}

type Stream struct {
	ID            string        `json:"id"`
	M3UAccountID  string        `json:"m3u_account_id"`
	SatIPSourceID string        `json:"satip_source_id,omitempty"`
	HDHRSourceID  string        `json:"hdhr_source_id,omitempty"`
	Name          string        `json:"name"`
	URL           string        `json:"url"`
	Group         string        `json:"group"`
	Logo          string        `json:"logo,omitempty"`
	TvgID         string        `json:"tvg_id,omitempty"`
	TvgName       string        `json:"tvg_name,omitempty"`
	ContentHash   string        `json:"content_hash"`
	VODType       string        `json:"vod_type,omitempty"`
	VODSeries     string        `json:"vod_series,omitempty"`
	VODCollection string        `json:"vod_collection,omitempty"`
	VODSeason     int           `json:"vod_season,omitempty"`
	VODSeasonName string        `json:"vod_season_name,omitempty"`
	VODEpisode    int           `json:"vod_episode,omitempty"`
	VODYear       int           `json:"vod_year,omitempty"`
	VODVCodec     string        `json:"vod_vcodec,omitempty"`
	VODACodec     string        `json:"vod_acodec,omitempty"`
	VODRes        string        `json:"vod_resolution,omitempty"`
	VODAudio      string        `json:"vod_audio,omitempty"`
	VODDuration   float64       `json:"vod_duration,omitempty"`
	TMDBID        int           `json:"tmdb_id,omitempty"`
	TMDBManual    bool          `json:"tmdb_manual,omitempty"`
	CacheType     string        `json:"cache_type,omitempty"`
	CacheKey      int           `json:"cache_key,omitempty"`
	Language      string        `json:"language,omitempty"`
	UseWireGuard  bool          `json:"use_wireguard,omitempty"`
	IsActive      bool          `json:"is_active"`
	Tracks        []StreamTrack `json:"tracks,omitempty"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

type Channel struct {
	ID              string    `json:"id"`
	UserID          string    `json:"user_id"`
	Name            string    `json:"name"`
	LogoID          *string   `json:"logo_id,omitempty"`
	Logo            string    `json:"logo,omitempty"`
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
	ID              string    `json:"id"`
	UserID          string    `json:"user_id"`
	Name            string    `json:"name"`
	ImageURL        string    `json:"image_url,omitempty"`
	IsEnabled       bool      `json:"is_enabled"`
	JellyfinEnabled bool      `json:"jellyfin_enabled"`
	JellyfinType    string    `json:"jellyfin_type,omitempty"`
	SortOrder       int       `json:"sort_order"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type Logo struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"created_at"`
}

type SourceProfile struct {
	ID   string `json:"id"`
	Name string `json:"name"`

	Deinterlace       bool   `json:"deinterlace"`
	DeinterlaceMethod string `json:"deinterlace_method,omitempty"`

	AudioDelayMs   int    `json:"audio_delay_ms"`
	AudioChannels  int    `json:"audio_channels"`
	AudioLanguage  string `json:"audio_language,omitempty"`

	VideoQueueMs   int `json:"video_queue_ms"`
	AudioQueueMs   int `json:"audio_queue_ms"`

	RTSPLatency    int    `json:"rtsp_latency"`
	RTSPProtocols  string `json:"rtsp_protocols,omitempty"`
	RTSPBufferMode int    `json:"rtsp_buffer_mode"`

	HTTPTimeoutSec int    `json:"http_timeout_sec"`
	HTTPRetries    int    `json:"http_retries"`
	HTTPUserAgent  string `json:"http_user_agent,omitempty"`

	TSSetTimestamps bool `json:"ts_set_timestamps"`

	EncoderBitrateKbps int `json:"encoder_bitrate_kbps"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type StreamProfile struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	StreamMode    string    `json:"stream_mode"`
	HWAccel       string    `json:"hwaccel"`
	VideoCodec    string    `json:"video_codec"`
	Container     string    `json:"container"`
	Delivery      string    `json:"delivery"`
	AudioCodec    string    `json:"audio_codec"`
	Deinterlace   bool      `json:"deinterlace"`
	FPSMode       string    `json:"fps_mode"`
	UseCustomArgs bool      `json:"use_custom_args"`
	AutoDetect    bool      `json:"auto_detect"`
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
	ETag          string     `json:"etag,omitempty"`
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
	ListenPort      int               `json:"listen_port"`
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
	Username     string  `json:"username,omitempty"`
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
	ID            string `json:"id"`
	M3UAccountID  string `json:"m3u_account_id"`
	SatIPSourceID string `json:"satip_source_id,omitempty"`
	HDHRSourceID  string `json:"hdhr_source_id,omitempty"`
	Name          string `json:"name"`
	Group         string `json:"group"`
	Logo          string `json:"logo,omitempty"`
	VODType       string `json:"vod_type,omitempty"`
	VODSeries     string `json:"vod_series,omitempty"`
	VODSeason     int    `json:"vod_season,omitempty"`
	VODSeasonName string `json:"vod_season_name,omitempty"`
	VODEpisode    int    `json:"vod_episode,omitempty"`
	VODYear       int    `json:"vod_year,omitempty"`
	TMDBID        int    `json:"tmdb_id,omitempty"`
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

type WireGuardProfile struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	PrivateKey         string    `json:"private_key,omitempty"`
	Address            string    `json:"address"`
	DNS                string    `json:"dns"`
	PeerPublicKey      string    `json:"peer_public_key"`
	PeerEndpoint       string    `json:"peer_endpoint"`
	RouteHosts         string    `json:"route_hosts"`
	HealthcheckURL     string    `json:"healthcheck_url,omitempty"`
	HealthcheckMethod  string    `json:"healthcheck_method,omitempty"`
	HealthcheckInterval int      `json:"healthcheck_interval,omitempty"`
	IsEnabled          bool      `json:"is_enabled"`
	Priority           int       `json:"priority"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}
