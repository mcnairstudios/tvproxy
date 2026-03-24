package ffmpeg

import (
	"strconv"
	"strings"

	"github.com/gavinmcnair/tvproxy/pkg/defaults"
)

type ComposeOptions struct {
	SourceType  string
	HWAccel     string
	VideoCodec  string
	Container   string
	Deinterlace bool
	FPSMode     string
}

var defaultFFmpegSettings = &defaults.FFmpegSettings{
	LogLevel:           "warning",
	AnalyzeDuration:    5000000,
	ProbeSize:          5000000,
	SatIPRWTimeout:     5000000,
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
	if cfgSettings != nil {
		return cfgSettings
	}
	return defaultFFmpegSettings
}

func ComposeStreamProfileArgs(opts ComposeOptions) string {
	s := settings()
	var parts []string

	parts = append(parts, "-hide_banner", "-loglevel", s.LogLevel, "-nostdin")

	switch opts.HWAccel {
	case "qsv":
		parts = append(parts, "-init_hw_device", "vaapi=va:/dev/dri/renderD128", "-init_hw_device", "qsv=qs@va", "-hwaccel", "qsv", "-hwaccel_output_format", "qsv")
	case "nvenc":
		parts = append(parts, "-hwaccel", "cuda", "-hwaccel_output_format", "cuda")
	case "vaapi":
		if opts.VideoCodec == "av1" {
			parts = append(parts, "-hwaccel", "vaapi", "-hwaccel_output_format", "vaapi", "-vaapi_device", "/dev/dri/renderD128")
		} else {
			parts = append(parts, "-init_hw_device", "vaapi=va:/dev/dri/renderD128", "-filter_hw_device", "va")
		}
	case "videotoolbox":
		parts = append(parts, "-hwaccel", "videotoolbox", "-hwaccel_output_format", "videotoolbox_vld")
	}

	if opts.SourceType == "m3u" {
		parts = append(parts, "-analyzeduration", strconv.Itoa(s.AnalyzeDuration), "-probesize", strconv.Itoa(s.ProbeSize))
	}

	if opts.SourceType == "satip" {
		parts = append(parts, "-rw_timeout", strconv.Itoa(s.SatIPRWTimeout))
	}

	parts = append(parts, "-err_detect", "ignore_err")

	parts = append(parts, "-i", "{input}")

	if opts.SourceType == "m3u" {
		if opts.VideoCodec == "copy" {
			parts = append(parts, "-map", "0:v", "-map", "0:a:0")
		} else {
			parts = append(parts, "-map", "0:v:0", "-map", "0:a:0")
		}
	}

	parts = append(parts, "-max_muxing_queue_size", strconv.Itoa(s.MaxMuxingQueueSize))

	if opts.FPSMode == "cfr" && opts.VideoCodec != "copy" {
		parts = append(parts, "-fps_mode", "cfr")
	}

	vfFilters := buildVFChain(opts)
	if len(vfFilters) > 0 {
		parts = append(parts, "-vf", strings.Join(vfFilters, ","))
	}

	parts = append(parts, encoderFlags(opts.HWAccel, opts.VideoCodec, s)...)

	audioBitrate := s.AudioBitrate
	audioChannels := strconv.Itoa(s.AudioChannels)

	switch opts.SourceType {
	case "satip":
		parts = append(parts, "-c:a", "copy")
		if opts.Container == "mpegts" {
			parts = append(parts, "-bsf:v", "dump_extra")
		}
	case "m3u":
		if opts.Container == "webm" {
			parts = append(parts, "-c:a", s.WebMAudioCodec, "-b:a", audioBitrate, "-ac", audioChannels)
		} else {
			parts = append(parts, "-c:a", "aac", "-b:a", audioBitrate, "-ac", audioChannels)
		}
		parts = append(parts, "-c:s", "copy")
	}

	switch opts.Container {
	case "mp4":
		parts = append(parts, "-f", "mp4", "-movflags", s.MP4Movflags)
	default:
		parts = append(parts, "-f", opts.Container)
	}

	if opts.SourceType == "m3u" {
		parts = append(parts, "-fflags", s.FFlags)
		if opts.Container == "mpegts" || opts.Container == "matroska" {
			parts = append(parts, "-copyts")
		}
	}

	parts = append(parts, "pipe:1")

	return strings.Join(parts, " ")
}

func buildVFChain(opts ComposeOptions) []string {
	if opts.VideoCodec == "copy" {
		return nil
	}

	var filters []string

	needsDeinterlace := opts.Deinterlace
	needsHWUpload := opts.HWAccel == "vaapi" && opts.VideoCodec != "av1"
	needsHWDownload := opts.HWAccel == "videotoolbox" && !isVideoToolboxEncoder(opts.VideoCodec)

	switch {
	case opts.HWAccel == "qsv" && needsDeinterlace:
		filters = append(filters, "vpp_qsv=deinterlace_mode=advanced")
	case opts.HWAccel == "nvenc" && needsDeinterlace:
		filters = append(filters, "yadif_cuda")
	case needsHWDownload && needsDeinterlace:
		filters = append(filters, "hwdownload", "format=nv12", "yadif")
	case needsHWDownload:
		filters = append(filters, "hwdownload", "format=nv12")
	case needsHWUpload && needsDeinterlace:
		filters = append(filters, "yadif", "format=nv12", "hwupload")
	case needsHWUpload:
		filters = append(filters, "format=nv12", "hwupload")
	case needsDeinterlace:
		filters = append(filters, "yadif")
	}

	return filters
}

func DefaultContainer(videoCodec string) string {
	switch videoCodec {
	case "av1":
		return "matroska"
	default:
		return "mpegts"
	}
}

func isVideoToolboxEncoder(videoCodec string) bool {
	return videoCodec == "h264" || videoCodec == "h265"
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
		return append([]string{"-c:v", "h264_videotoolbox"}, hw.Flags()...)
	default:
		sw := getEncoderHW(s, "h264", "none")
		return append([]string{"-c:v", "libx264"}, sw.Flags()...)
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
