package defaults

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"
)

type MatchRule struct {
	HeaderName string `json:"header_name"`
	MatchType  string `json:"match_type"`
	MatchValue string `json:"match_value"`
}

type ClientDefault struct {
	Name       string      `json:"name"`
	Priority   int         `json:"priority"`
	ListenPort int         `json:"listen_port"`
	AutoDetect bool        `json:"auto_detect"`
	HWAccel    string      `json:"hwaccel"`
	Container  string      `json:"container"`
	Delivery   string      `json:"delivery,omitempty"`
	MatchRules []MatchRule `json:"match_rules"`
}

type ClientDefaults struct {
	Clients []ClientDefault `json:"clients"`
}

func LoadClientDefaults(externalPath string) (*ClientDefaults, error) {
	if data, err := os.ReadFile(externalPath); err == nil {
		var defs ClientDefaults
		if err := json.Unmarshal(data, &defs); err != nil {
			return nil, fmt.Errorf("parsing external %s: %w", externalPath, err)
		}
		return &defs, nil
	}

	data, err := Assets.ReadFile("clients.json")
	if err != nil {
		return nil, fmt.Errorf("reading embedded clients.json: %w", err)
	}
	var defs ClientDefaults
	if err := json.Unmarshal(data, &defs); err != nil {
		return nil, fmt.Errorf("parsing embedded clients.json: %w", err)
	}
	return &defs, nil
}

type EncoderHWSettings struct {
	Preset        string `json:"preset,omitempty"`
	GlobalQuality int    `json:"global_quality,omitempty"`
	PixFmt        string `json:"pix_fmt,omitempty"`
	LookAhead     int    `json:"look_ahead,omitempty"`
	CRF           int    `json:"crf,omitempty"`
	CQ            int    `json:"cq,omitempty"`
	RCMode        string `json:"rc_mode,omitempty"`
}

type EncoderCodecSettings struct {
	Software     EncoderHWSettings `json:"software"`
	QSV          EncoderHWSettings `json:"qsv"`
	NVENC        EncoderHWSettings `json:"nvenc"`
	VAAPI        EncoderHWSettings `json:"vaapi"`
	VideoToolbox EncoderHWSettings `json:"videotoolbox"`
}

type FFmpegSettings struct {
	LogLevel           string                          `json:"log_level"`
	AnalyzeDuration    int                             `json:"analyzeduration"`
	ProbeSize          int                             `json:"probesize"`
	AudioBitrate       string                          `json:"audio_bitrate"`
	AudioChannels      int                             `json:"audio_channels"`
	WebMAudioCodec     string                          `json:"webm_audio_codec"`
	MP4Movflags        string                          `json:"mp4_movflags"`
	MaxMuxingQueueSize int                             `json:"max_muxing_queue_size"`
	FFlags             string                          `json:"fflags"`
	ProbeTimeoutStr    string                          `json:"probe_timeout"`
	WaitDelayStr       string                          `json:"wait_delay"`
	StartupTimeoutStr  string                          `json:"startup_timeout"`
	Encoders           map[string]EncoderCodecSettings `json:"encoders"`

	ProbeTimeout   time.Duration `json:"-"`
	WaitDelay      time.Duration `json:"-"`
	StartupTimeout time.Duration `json:"-"`
}

type NetworkSettings struct {
	ReconnectDelayMax     int    `json:"reconnect_delay_max"`
	ReconnectRWTimeout    int    `json:"reconnect_rw_timeout"`
	LogoDownloadTimeoutStr string `json:"logo_download_timeout"`
	XtreamAPITimeoutStr    string `json:"xtream_api_timeout"`

	LogoDownloadTimeout time.Duration `json:"-"`
	XtreamAPITimeout    time.Duration `json:"-"`
}

type VODSettings struct {
	ProbeTimeoutStr    string `json:"probe_timeout"`
	FileRetryCount     int    `json:"file_retry_count"`
	FileRetryDelayStr  string `json:"file_retry_delay"`

	ProbeTimeout   time.Duration `json:"-"`
	FileRetryDelay time.Duration `json:"-"`
}

type WorkerSettings struct {
	SSDPAnnounceIntervalStr  string `json:"ssdp_announce_interval"`
	DLNAAnnounceIntervalStr  string `json:"dlna_announce_interval"`
	HDHRDiscoverIntervalStr  string `json:"hdhr_discover_interval"`
	HDHRBroadcastIntervalStr string `json:"hdhr_broadcast_interval"`
	RetryDelayStr            string `json:"retry_delay"`

	SSDPAnnounceInterval  time.Duration `json:"-"`
	DLNAAnnounceInterval  time.Duration `json:"-"`
	HDHRDiscoverInterval  time.Duration `json:"-"`
	HDHRBroadcastInterval time.Duration `json:"-"`
	RetryDelay            time.Duration `json:"-"`
}

type ServerSettings struct {
	HTTPReadTimeoutStr    string `json:"http_read_timeout"`
	HTTPIdleTimeoutStr    string `json:"http_idle_timeout"`
	RequestBodyLimitBytes int64  `json:"request_body_limit_bytes"`
	HDHRReadTimeoutStr    string `json:"hdhr_read_timeout"`
	HDHRIdleTimeoutStr    string `json:"hdhr_idle_timeout"`

	HTTPReadTimeout time.Duration `json:"-"`
	HTTPIdleTimeout time.Duration `json:"-"`
	HDHRReadTimeout time.Duration `json:"-"`
	HDHRIdleTimeout time.Duration `json:"-"`
}

type EPGSettings struct {
	BatchSize int `json:"batch_size"`
}

type RecordingSettings struct {
	StartCutoffStr string `json:"start_cutoff"`
	LeadTimeStr    string `json:"lead_time"`

	StartCutoff time.Duration `json:"-"`
	LeadTime    time.Duration `json:"-"`
}

type AuthSettings struct {
	InviteTokenExpiryStr string `json:"invite_token_expiry"`

	InviteTokenExpiry time.Duration `json:"-"`
}

type Settings struct {
	FFmpeg    FFmpegSettings    `json:"ffmpeg"`
	Network   NetworkSettings   `json:"network"`
	VOD       VODSettings       `json:"vod"`
	Workers   WorkerSettings    `json:"workers"`
	Server    ServerSettings    `json:"server"`
	EPG       EPGSettings       `json:"epg"`
	Recording RecordingSettings `json:"recording"`
	Auth      AuthSettings      `json:"auth"`
}

func (s *Settings) parseDurations() {
	s.FFmpeg.ProbeTimeout = parseDur(s.FFmpeg.ProbeTimeoutStr, 10*time.Second)
	s.FFmpeg.WaitDelay = parseDur(s.FFmpeg.WaitDelayStr, 5*time.Second)
	s.FFmpeg.StartupTimeout = parseDur(s.FFmpeg.StartupTimeoutStr, 30*time.Second)

	s.Network.LogoDownloadTimeout = parseDur(s.Network.LogoDownloadTimeoutStr, 10*time.Second)
	s.Network.XtreamAPITimeout = parseDur(s.Network.XtreamAPITimeoutStr, 30*time.Second)

	s.VOD.ProbeTimeout = parseDur(s.VOD.ProbeTimeoutStr, 15*time.Second)
	s.VOD.FileRetryDelay = parseDur(s.VOD.FileRetryDelayStr, 200*time.Millisecond)

	s.Workers.SSDPAnnounceInterval = parseDur(s.Workers.SSDPAnnounceIntervalStr, 30*time.Second)
	s.Workers.DLNAAnnounceInterval = parseDur(s.Workers.DLNAAnnounceIntervalStr, 30*time.Second)
	s.Workers.HDHRDiscoverInterval = parseDur(s.Workers.HDHRDiscoverIntervalStr, 10*time.Second)
	s.Workers.HDHRBroadcastInterval = parseDur(s.Workers.HDHRBroadcastIntervalStr, 10*time.Second)
	s.Workers.RetryDelay = parseDur(s.Workers.RetryDelayStr, 2*time.Second)

	s.Server.HTTPReadTimeout = parseDur(s.Server.HTTPReadTimeoutStr, 15*time.Second)
	s.Server.HTTPIdleTimeout = parseDur(s.Server.HTTPIdleTimeoutStr, 60*time.Second)
	s.Server.HDHRReadTimeout = parseDur(s.Server.HDHRReadTimeoutStr, 15*time.Second)
	s.Server.HDHRIdleTimeout = parseDur(s.Server.HDHRIdleTimeoutStr, 60*time.Second)

	s.Recording.StartCutoff = parseDur(s.Recording.StartCutoffStr, 5*time.Minute)
	s.Recording.LeadTime = parseDur(s.Recording.LeadTimeStr, 30*time.Second)

	s.Auth.InviteTokenExpiry = parseDur(s.Auth.InviteTokenExpiryStr, 7*24*time.Hour)
}

func parseDur(s string, fallback time.Duration) time.Duration {
	if s == "" {
		return fallback
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}

func (hw EncoderHWSettings) Flags() []string {
	var flags []string
	if hw.Preset != "" {
		flags = append(flags, "-preset", hw.Preset)
	}
	if hw.GlobalQuality > 0 {
		flags = append(flags, "-global_quality", strconv.Itoa(hw.GlobalQuality))
	}
	if hw.CRF > 0 {
		flags = append(flags, "-crf", strconv.Itoa(hw.CRF))
	}
	if hw.CQ > 0 {
		flags = append(flags, "-cq", strconv.Itoa(hw.CQ))
	}
	if hw.LookAhead > 0 {
		flags = append(flags, "-look_ahead", strconv.Itoa(hw.LookAhead))
	}
	if hw.RCMode != "" {
		flags = append(flags, "-rc_mode", hw.RCMode)
	}
	if hw.PixFmt != "" {
		flags = append(flags, "-pix_fmt", hw.PixFmt)
	}
	return flags
}

func LoadSettings(externalPath string) (*Settings, error) {
	var s Settings

	if data, err := os.ReadFile(externalPath); err == nil {
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("parsing external %s: %w", externalPath, err)
		}
		s.parseDurations()
		return &s, nil
	}

	data, err := Assets.ReadFile("settings.json")
	if err != nil {
		return nil, fmt.Errorf("reading embedded settings.json: %w", err)
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing embedded settings.json: %w", err)
	}
	s.parseDurations()
	return &s, nil
}
