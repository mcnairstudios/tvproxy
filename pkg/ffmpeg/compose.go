package ffmpeg

import (
	"github.com/gavinmcnair/tvproxy/pkg/defaults"
)

var defaultFFmpegSettings = &defaults.FFmpegSettings{
	LogLevel:           "warning",
	AnalyzeDuration:    1000000,
	ProbeSize:          1000000,
	AudioBitrate:       "192k",
	AudioChannels:      2,
	WebMAudioCodec:     "libopus",
	MP4Movflags:        "frag_keyframe+empty_moov+default_base_moof",
	MaxMuxingQueueSize: 4096,
	FFlags:             "+genpts+discardcorrupt",
	Encoders: map[string]defaults.EncoderCodecSettings{
		"h264": {
			Software:     defaults.EncoderHWSettings{Preset: "fast"},
			QSV:          defaults.EncoderHWSettings{Preset: "veryslow", GlobalQuality: 20},
			NVENC:        defaults.EncoderHWSettings{Preset: "p4"},
			VAAPI:        defaults.EncoderHWSettings{},
			VideoToolbox: defaults.EncoderHWSettings{},
		},
		"h265": {
			Software:     defaults.EncoderHWSettings{Preset: "fast"},
			QSV:          defaults.EncoderHWSettings{Preset: "veryslow", GlobalQuality: 22},
			NVENC:        defaults.EncoderHWSettings{Preset: "p4"},
			VAAPI:        defaults.EncoderHWSettings{},
			VideoToolbox: defaults.EncoderHWSettings{},
		},
		"av1": {
			Software: defaults.EncoderHWSettings{Preset: "6", CRF: 24, PixFmt: "yuv420p10le"},
			QSV:      defaults.EncoderHWSettings{Preset: "veryslow", GlobalQuality: 25},
			NVENC:    defaults.EncoderHWSettings{Preset: "p4", CQ: 24, PixFmt: "p010le"},
			VAAPI:    defaults.EncoderHWSettings{RCMode: "ICQ", GlobalQuality: 25},
		},
	},
}

var cfgSettings *defaults.FFmpegSettings

func SetSettings(s *defaults.FFmpegSettings) {
	cfgSettings = s
}

func settings() *defaults.FFmpegSettings {
	if cfgSettings == nil {
		return defaultFFmpegSettings
	}
	merged := *defaultFFmpegSettings
	if cfgSettings.LogLevel != "" {
		merged.LogLevel = cfgSettings.LogLevel
	}
	if cfgSettings.AnalyzeDuration != 0 {
		merged.AnalyzeDuration = cfgSettings.AnalyzeDuration
	}
	if cfgSettings.ProbeSize != 0 {
		merged.ProbeSize = cfgSettings.ProbeSize
	}
	if cfgSettings.AudioBitrate != "" {
		merged.AudioBitrate = cfgSettings.AudioBitrate
	}
	if cfgSettings.AudioChannels != 0 {
		merged.AudioChannels = cfgSettings.AudioChannels
	}
	if cfgSettings.WebMAudioCodec != "" {
		merged.WebMAudioCodec = cfgSettings.WebMAudioCodec
	}
	if cfgSettings.MP4Movflags != "" {
		merged.MP4Movflags = cfgSettings.MP4Movflags
	}
	if cfgSettings.MaxMuxingQueueSize != 0 {
		merged.MaxMuxingQueueSize = cfgSettings.MaxMuxingQueueSize
	}
	if cfgSettings.FFlags != "" {
		merged.FFlags = cfgSettings.FFlags
	}
	if cfgSettings.ProbeTimeoutStr != "" {
		merged.ProbeTimeoutStr = cfgSettings.ProbeTimeoutStr
	}
	if cfgSettings.WaitDelayStr != "" {
		merged.WaitDelayStr = cfgSettings.WaitDelayStr
	}
	if cfgSettings.StartupTimeoutStr != "" {
		merged.StartupTimeoutStr = cfgSettings.StartupTimeoutStr
	}
	if cfgSettings.ProbeTimeout != 0 {
		merged.ProbeTimeout = cfgSettings.ProbeTimeout
	}
	if cfgSettings.WaitDelay != 0 {
		merged.WaitDelay = cfgSettings.WaitDelay
	}
	if cfgSettings.StartupTimeout != 0 {
		merged.StartupTimeout = cfgSettings.StartupTimeout
	}
	if len(cfgSettings.Encoders) > 0 {
		merged.Encoders = cfgSettings.Encoders
	}
	return &merged
}

func DefaultContainer(videoCodec string) string {
	switch videoCodec {
	case "av1":
		return "matroska"
	default:
		return "mpegts"
	}
}

func buildVFChain(hwaccel, videoCodec string, deinterlace bool) []string {
	if videoCodec == "copy" {
		return nil
	}

	var filters []string

	needsHWUpload := hwaccel == "vaapi"

	switch {
	case hwaccel == "qsv" && deinterlace:
		filters = append(filters, "vpp_qsv=deinterlace_mode=advanced")
	case hwaccel == "nvenc" && deinterlace:
		filters = append(filters, "yadif_cuda")
	case needsHWUpload && deinterlace:
		filters = append(filters, "yadif", "format=nv12", "hwupload")
	case needsHWUpload:
		filters = append(filters, "format=nv12", "hwupload")
	case deinterlace:
		filters = append(filters, "yadif")
	}

	return filters
}


func encoderFlags(hwaccel, videoCodec string, s *defaults.FFmpegSettings) []string {
	switch videoCodec {
	case "copy":
		return []string{"-c:v", "copy"}
	case "h264":
		return h264Flags(hwaccel, s)
	case "h265":
		return h265Flags(hwaccel, s)
	case "av1":
		return av1Flags(hwaccel, s)
	default:
		return []string{"-c:v", "copy"}
	}
}

func getEncoderHW(s *defaults.FFmpegSettings, codec, hwaccel string) defaults.EncoderHWSettings {
	codecSettings, ok := s.Encoders[codec]
	if !ok {
		return defaults.EncoderHWSettings{}
	}
	switch hwaccel {
	case "qsv":
		return codecSettings.QSV
	case "nvenc":
		return codecSettings.NVENC
	case "vaapi":
		return codecSettings.VAAPI
	case "videotoolbox":
		return codecSettings.VideoToolbox
	default:
		return codecSettings.Software
	}
}

func h264Flags(hwaccel string, s *defaults.FFmpegSettings) []string {
	hw := getEncoderHW(s, "h264", hwaccel)
	switch hwaccel {
	case "qsv":
		return append([]string{"-c:v", "h264_qsv"}, hw.Flags()...)
	case "nvenc":
		return append([]string{"-c:v", "h264_nvenc"}, hw.Flags()...)
	case "vaapi":
		return append([]string{"-c:v", "h264_vaapi"}, hw.Flags()...)
	case "videotoolbox":
		flags := append([]string{"-c:v", "h264_videotoolbox", "-realtime", "1"}, hw.Flags()...)
		return flags
	default:
		sw := getEncoderHW(s, "h264", "none")
		return append([]string{"-c:v", "libx264", "-tune", "zerolatency"}, sw.Flags()...)
	}
}

func h265Flags(hwaccel string, s *defaults.FFmpegSettings) []string {
	hw := getEncoderHW(s, "h265", hwaccel)
	switch hwaccel {
	case "qsv":
		return append([]string{"-c:v", "hevc_qsv"}, hw.Flags()...)
	case "nvenc":
		return append([]string{"-c:v", "hevc_nvenc"}, hw.Flags()...)
	case "vaapi":
		return append([]string{"-c:v", "hevc_vaapi"}, hw.Flags()...)
	case "videotoolbox":
		return append([]string{"-c:v", "hevc_videotoolbox"}, hw.Flags()...)
	default:
		sw := getEncoderHW(s, "h265", "none")
		return append([]string{"-c:v", "libx265"}, sw.Flags()...)
	}
}

func av1Flags(hwaccel string, s *defaults.FFmpegSettings) []string {
	hw := getEncoderHW(s, "av1", hwaccel)
	switch hwaccel {
	case "qsv":
		return append([]string{"-c:v", "av1_qsv"}, hw.Flags()...)
	case "nvenc":
		return append([]string{"-c:v", "av1_nvenc"}, hw.Flags()...)
	case "vaapi":
		return append([]string{"-c:v", "av1_vaapi"}, hw.Flags()...)
	default:
		sw := getEncoderHW(s, "av1", "none")
		return append([]string{"-c:v", "libsvtav1"}, sw.Flags()...)
	}
}

