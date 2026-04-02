package ffmpeg

import (
	"strconv"
	"strings"

	"github.com/gavinmcnair/tvproxy/pkg/defaults"
)

type BuildOptions struct {
	StreamURL     string
	UserAgent     string
	Probe         *ProbeResult
	Container     string
	Delivery      string
	HWAccel       string
	VideoCodec    string
	AudioCodec    string
	AudioOnly     bool
	CustomCommand string
}

func Build(opts BuildOptions) (command, args string) {
	if opts.CustomCommand != "" {
		return "ffmpeg", opts.CustomCommand
	}
	return "ffmpeg", composeBuildArgs(opts)
}

func composeBuildArgs(opts BuildOptions) string {
	s := settings()
	hasVideo := !opts.AudioOnly && (opts.Probe == nil || opts.Probe.HasVideo)
	outputCodec := resolveOutputCodec(opts.Probe, opts.VideoCodec)
	interlaced := opts.Probe != nil && opts.Probe.Video != nil && isInterlaced(opts.Probe.Video.FieldOrder)

	var parts []string
	parts = append(parts, "-hide_banner", "-loglevel", s.LogLevel, "-nostdin")

	if outputCodec != "copy" && hasVideo {
		parts = append(parts, hwInitFlags(opts.HWAccel, outputCodec)...)
	}

	if isRTSPURL(opts.StreamURL) {
		if opts.AudioOnly {
			parts = append(parts,
				"-rtsp_transport", "tcp",
				"-analyzeduration", "0",
				"-probesize", "32",
				"-max_delay", "500000")
		} else {
			parts = append(parts,
				"-rtsp_transport", "tcp",
				"-analyzeduration", "3000000",
				"-probesize", "2000000",
				"-max_delay", "500000")
		}
	} else if isHTTPURL(opts.StreamURL) {
		parts = append(parts,
			"-analyzeduration", strconv.Itoa(s.AnalyzeDuration),
			"-probesize", strconv.Itoa(s.ProbeSize))
	}

	if opts.UserAgent != "" {
		parts = append(parts, "-user_agent", opts.UserAgent)
	}

	parts = append(parts, "-err_detect", "ignore_err", "-fflags", s.FFlags, "-i", "{input}")

	if hasVideo {
		parts = append(parts, "-map", "0:v:0?")
	}
	parts = append(parts, "-map", "0:a:0?")

	parts = append(parts, "-max_muxing_queue_size", strconv.Itoa(s.MaxMuxingQueueSize))

	if !hasVideo {
		parts = append(parts, "-c:a", "aac", "-ac", "2", "-b:a", s.AudioBitrate)
		parts = append(parts, "-output_ts_offset", "0", "-f", "adts", "pipe:1")
		return strings.Join(parts, " ")
	}

	if vf := buildVFChain(opts.HWAccel, outputCodec, interlaced); len(vf) > 0 {
		parts = append(parts, "-vf", strings.Join(vf, ","))
	}
	parts = append(parts, encoderFlags(opts.HWAccel, outputCodec, s)...)
	parts = append(parts, "-g", "50", "-keyint_min", "50")
	parts = append(parts, buildAudioFlags(opts.Probe, opts.Container, opts.Delivery, opts.AudioCodec, s)...)

	parts = append(parts, "-output_ts_offset", "0")

	switch opts.Container {
	case "mp4", "":
		parts = append(parts, "-f", "mp4", "-movflags", s.MP4Movflags)
	case "mpegts":
		parts = append(parts, "-f", "mpegts")
	case "matroska":
		parts = append(parts, "-f", "matroska")
	case "webm":
		parts = append(parts, "-f", "webm")
	default:
		parts = append(parts, "-f", opts.Container)
	}

	parts = append(parts, "pipe:1")

	return strings.Join(parts, " ")
}

func resolveOutputCodec(probe *ProbeResult, videoCodec string) string {
	if videoCodec != "" && videoCodec != "copy" {
		return videoCodec
	}
	if probe == nil || probe.Video == nil {
		return "copy"
	}
	switch probe.Video.Codec {
	case "mpeg2video":
		return "h264"
	case "h264":
		if isInterlaced(probe.Video.FieldOrder) {
			return "h264"
		}
		return "copy"
	case "hevc":
		if isInterlaced(probe.Video.FieldOrder) {
			return "h265"
		}
		return "copy"
	default:
		return "copy"
	}
}

func hwInitFlags(hwaccel, outputCodec string) []string {
	switch hwaccel {
	case "qsv":
		return []string{"-init_hw_device", "vaapi=va:/dev/dri/renderD128", "-init_hw_device", "qsv=qs@va", "-hwaccel", "qsv", "-hwaccel_output_format", "qsv"}
	case "nvenc":
		return []string{"-hwaccel", "cuda", "-hwaccel_output_format", "cuda"}
	case "vaapi":
		return []string{"-init_hw_device", "vaapi=va:/dev/dri/renderD128", "-filter_hw_device", "va"}
	}
	return nil
}

func buildAudioFlags(probe *ProbeResult, container, delivery, audioCodec string, s *defaults.FFmpegSettings) []string {
	switch audioCodec {
	case "copy":
		return []string{"-c:a", "copy"}
	case "aac":
		return []string{"-c:a", "aac", "-ac", "2", "-b:a", s.AudioBitrate}
	case "opus":
		return []string{"-c:a", "libopus", "-b:a", s.AudioBitrate}
	default:
		if container == "webm" {
			return []string{"-c:a", "libopus", "-b:a", s.AudioBitrate}
		}
		if delivery == "dash" {
			return []string{"-c:a", "aac", "-ac", "2", "-b:a", s.AudioBitrate}
		}
		return audioEncoder(probe)
	}
}
