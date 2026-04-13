package gstreamer

import (
	"fmt"
	"strings"

	"github.com/go-gst/go-gst/gst"

	"github.com/gavinmcnair/tvproxy/pkg/media"
)

type OutputFormat string

const (
	OutputHLS    OutputFormat = "hls"
	OutputMPEGTS OutputFormat = "mpegts"
	OutputMP4    OutputFormat = "mp4"
)

type HWAccel string

const (
	HWNone         HWAccel = "none"
	HWVAAPI        HWAccel = "vaapi"
	HWQSV          HWAccel = "qsv"
	HWVideoToolbox HWAccel = "videotoolbox"
	HWNVENC        HWAccel = "nvenc"
)

type PipelineOpts struct {
	InputURL      string
	InputType     string // "http", "rtsp", "file"
	IsLive        bool

	VideoCodec    string // source video codec from probe
	AudioCodec    string // source audio codec from probe
	Container     string // source container from probe

	OutputVideoCodec string // target: "copy", "h264", "h265", "av1"
	OutputAudioCodec string // target: "copy", "aac"
	OutputBitrate    int    // kbps, 0 = auto
	OutputFormat     OutputFormat

	HWAccel HWAccel

	HLSDir          string
	HLSSegmentTime  int
	RecordingPath   string

	UserAgent    string
	ExtraHeaders map[string]string
	DualOutput   bool
}

type Pipeline struct {
	Elements    []string
	PipelineStr string
	Cmd         string
	Args        []string
}

func BuildPipeline(opts PipelineOpts) *Pipeline {
	pstr := buildPipelineStr(opts)

	var args []string
	args = append(args, "-q", "-e")
	args = append(args, strings.Fields(pstr)...)

	return &Pipeline{
		PipelineStr: pstr,
		Cmd:         "gst-launch-1.0",
		Args:        args,
	}
}

func BuildFromProbe(probe *media.ProbeResult, inputURL string, opts PipelineOpts) *Pipeline {
	if probe != nil {
		if probe.Video != nil {
			opts.VideoCodec = probe.Video.Codec
		}
		if len(probe.AudioTracks) > 0 {
			opts.AudioCodec = probe.AudioTracks[0].Codec
		}
		opts.Container = probe.FormatName
	}
	opts.InputURL = inputURL
	return BuildPipeline(opts)
}

func buildPipelineStr(opts PipelineOpts) string {
	outCodec := NormalizeCodec(opts.OutputVideoCodec)
	srcCodec := NormalizeCodec(opts.VideoCodec)
	isCopy := outCodec == "" || outCodec == "default" || outCodec == "copy" || outCodec == srcCodec

	if PluginsAvailable() && isCopy {
		return buildPluginPipelineStr(opts)
	}
	return buildManualPipelineStr(opts)
}

func buildPluginPipelineStr(opts PipelineOpts) string {
	var parts []string

	parts = append(parts, fmt.Sprintf("tvproxysrc location=%s", opts.InputURL))
	parts = append(parts, "! tvproxydemux name=d")

	outCodec := NormalizeCodec(opts.OutputVideoCodec)
	if outCodec == "" || outCodec == "default" {
		outCodec = "copy"
	}

	if outCodec == "copy" || outCodec == NormalizeCodec(opts.VideoCodec) {
		parts = append(parts, "d.video ! m.video")
	} else {
		dec := hwDecoder(NormalizeCodec(opts.VideoCodec), opts.HWAccel)
		enc := hwEncoder(outCodec, opts.HWAccel, opts.OutputBitrate)
		parts = append(parts, fmt.Sprintf("d.video ! %s ! %s ! m.video", dec, enc))
	}

	parts = append(parts, "d.audio ! m.audio")

	muxFormat := "mp4"
	if opts.OutputFormat == OutputMPEGTS {
		muxFormat = "mpegts"
	}
	parts = append(parts, fmt.Sprintf("tvproxymux name=m output-format=%s", muxFormat))

	if opts.RecordingPath != "" {
		parts = append(parts, fmt.Sprintf("! filesink location=%s", opts.RecordingPath))
	} else {
		parts = append(parts, "! fdsink fd=1")
	}

	return strings.Join(parts, " ")
}

func buildManualPipelineStr(opts PipelineOpts) string {
	var parts []string

	parts = append(parts, buildSource(opts))
	parts = append(parts, "! tsparse set-timestamps=true ! tsdemux name=demux")
	parts = append(parts, buildVideoStr(opts))
	parts = append(parts, buildSinkStr(opts))
	parts = append(parts, buildAudioStr(opts))

	return strings.Join(parts, " ")
}

func IsMPEGTS(container, inputURL string) bool {
	c := strings.ToLower(container)
	if c == "mpegts" || c == "mpeg-ts" || c == "ts" {
		return true
	}
	url := strings.ToLower(inputURL)
	if strings.HasSuffix(url, ".ts") || strings.Contains(url, ".ts?") {
		return true
	}
	if strings.Contains(url, ":5004/") {
		return true
	}
	if strings.HasSuffix(url, ".mp4") || strings.HasSuffix(url, ".mkv") ||
		strings.HasSuffix(url, ".webm") || strings.HasSuffix(url, ".mov") ||
		strings.HasSuffix(url, ".avi") {
		return false
	}
	return true
}

func buildSource(opts PipelineOpts) string {
	switch opts.InputType {
	case "rtsp":
		return fmt.Sprintf("rtspsrc location=%s latency=0 protocols=tcp ! rtpmp2tdepay", opts.InputURL)
	case "file":
		return fmt.Sprintf("filesrc location=%s", opts.InputURL)
	default:
		src := fmt.Sprintf("souphttpsrc location=%s", opts.InputURL)
		if opts.IsLive {
			src += " do-timestamp=true is-live=true"
		}
		return src
	}
}

func buildVideoStr(opts PipelineOpts) string {
	vcodec := NormalizeCodec(opts.VideoCodec)
	outCodec := NormalizeCodec(opts.OutputVideoCodec)
	if outCodec == "" || outCodec == "default" {
		outCodec = "copy"
	}

	if outCodec == "copy" || outCodec == vcodec {
		parser := videoParserStr(outCodec, vcodec)
		return fmt.Sprintf("demux. ! queue ! %s ! mux.", parser)
	}

	dec := hwDecoder(vcodec, opts.HWAccel)
	enc := hwEncoder(outCodec, opts.HWAccel, opts.OutputBitrate)
	parser := videoParserStr(outCodec, "")

	return fmt.Sprintf("demux. ! queue ! %s ! %s ! %s ! mux.", dec, enc, parser)
}

func buildSinkStr(opts PipelineOpts) string {
	outCodec := NormalizeCodec(opts.OutputVideoCodec)

	if opts.OutputFormat == OutputMP4 || outCodec == "av1" {
		if opts.RecordingPath != "" {
			return fmt.Sprintf("mp4mux name=mux fragment-duration=500 streamable=true ! filesink location=%s", opts.RecordingPath)
		}
		return "mp4mux name=mux fragment-duration=500 streamable=true ! fdsink fd=1"
	}
	if opts.RecordingPath != "" {
		return fmt.Sprintf("mpegtsmux name=mux ! filesink location=%s", opts.RecordingPath)
	}
	return "mpegtsmux name=mux ! fdsink fd=1"
}

func buildAudioStr(opts PipelineOpts) string {
	acodec := NormalizeCodec(opts.AudioCodec)
	outAudio := NormalizeCodec(opts.OutputAudioCodec)
	if outAudio == "" || outAudio == "default" {
		outAudio = "aac"
	}

	if outAudio == "copy" && canCopyAudio(acodec, opts.OutputFormat) {
		parser := audioParser(acodec)
		return fmt.Sprintf("demux. ! queue ! %s ! mux.", parser)
	}

	inputParser := audioInputParser(acodec)
	dec := audioDecoder(acodec)
	aacEnc := aacEncoderName()
	return fmt.Sprintf("demux. ! queue ! %s ! %s ! audioconvert ! audioresample ! audio/x-raw,channels=2 ! %s ! aacparse ! mux.", inputParser, dec, aacEnc)
}

func audioInputParser(codec string) string {
	switch codec {
	case "aac_latm":
		return "aacparse"
	case "aac":
		return "aacparse"
	case "ac3":
		return "ac3parse"
	case "eac3":
		return "identity"
	case "mp2", "mp3":
		return "mpegaudioparse"
	default:
		return "aacparse"
	}
}

func videoParserStr(outCodec, sourceCodec string) string {
	name, ci := videoParser(outCodec, sourceCodec)
	if ci != "" {
		return name + " " + ci
	}
	return name
}

func videoParser(outCodec, sourceCodec string) (string, string) {
	codec := outCodec
	if codec == "copy" {
		codec = sourceCodec
	}
	switch codec {
	case "h265":
		return "h265parse", "config-interval=-1"
	case "av1":
		return "av1parse", ""
	case "mpeg2video":
		return "mpegvideoparse", ""
	default:
		return "h264parse", "config-interval=-1"
	}
}

func hwDecoder(codec string, hw HWAccel) string {
	switch hw {
	case HWVAAPI:
		switch codec {
		case "h264":
			return "h264parse ! vaapih264dec"
		case "h265":
			return "h265parse ! vaapih265dec"
		case "av1":
			return "av1parse ! vaapidecode"
		case "mpeg2video":
			return "mpegvideoparse ! vaapidecode"
		}
	case HWQSV:
		switch codec {
		case "h264":
			return "h264parse ! qsvh264dec"
		case "h265":
			return "h265parse ! qsvh265dec"
		case "av1":
			return "av1parse ! qsvav1dec"
		}
	case HWVideoToolbox:
		switch codec {
		case "h264":
			return "h264parse ! vtdec"
		case "h265":
			return "h265parse ! vtdec"
		case "av1":
			return "av1parse ! vtdec"
		}
	}
	switch codec {
	case "h264":
		return "h264parse ! avdec_h264"
	case "h265":
		return "h265parse ! avdec_h265"
	case "av1":
		return "av1parse ! avdec_av1"
	case "mpeg2video":
		return "mpegvideoparse ! avdec_mpeg2video"
	}
	return "decodebin"
}

func hwEncoder(codec string, hw HWAccel, bitrate int) string {
	br := bitrate
	if br <= 0 {
		br = 6000
	}

	switch hw {
	case HWVAAPI:
		switch codec {
		case "h264":
			return fmt.Sprintf("vaapih264enc bitrate=%d tune=low-latency", br)
		case "h265":
			return fmt.Sprintf("vaapih265enc bitrate=%d", br)
		case "av1":
			return fmt.Sprintf("vaapiav1enc bitrate=%d", br)
		}
	case HWQSV:
		switch codec {
		case "h264":
			return fmt.Sprintf("qsvh264enc bitrate=%d target-usage=1", br)
		case "h265":
			return fmt.Sprintf("qsvh265enc bitrate=%d", br)
		case "av1":
			return fmt.Sprintf("qsvav1enc bitrate=%d", br)
		}
	case HWVideoToolbox:
		switch codec {
		case "h265":
			return fmt.Sprintf("vtenc_h265 bitrate=%d realtime=true allow-frame-reordering=false", br)
		case "av1":
			if gst.Find("vtenc_av1") != nil {
				return fmt.Sprintf("vtenc_av1 bitrate=%d realtime=true allow-frame-reordering=false", br)
			}
			return softwareAV1EncoderStr(br)
		default:
			return fmt.Sprintf("vtenc_h264 bitrate=%d realtime=true allow-frame-reordering=false", br)
		}
	}
	switch codec {
	case "h265":
		return fmt.Sprintf("videoconvert ! x265enc bitrate=%d speed-preset=ultrafast", br)
	case "av1":
		return softwareAV1EncoderStr(br)
	default:
		return fmt.Sprintf("videoconvert ! x264enc bitrate=%d speed-preset=ultrafast tune=zerolatency", br)
	}
}

func aacEncoderName() string {
	for _, name := range []string{"faac", "voaacenc", "avenc_aac"} {
		if gst.Find(name) != nil {
			return name
		}
	}
	return "faac"
}

func softwareAV1EncoderStr(bitrate int) string {
	if gst.Find("svtav1enc") != nil {
		return fmt.Sprintf("videoconvert ! video/x-raw,format=I420 ! svtav1enc preset=12 target-bitrate=%d", bitrate)
	}
	if gst.Find("rav1enc") != nil {
		return fmt.Sprintf("videoconvert ! video/x-raw,format=I420 ! rav1enc speed-preset=10 low-latency=true bitrate=%d", bitrate*1000)
	}
	return fmt.Sprintf("videoconvert ! video/x-raw,format=I420 ! av1enc cpu-used=8 usage-profile=realtime target-bitrate=%d", bitrate)
}

func audioDecoder(codec string) string {
	switch codec {
	case "aac_latm":
		return "avdec_aac_latm"
	case "aac":
		return "avdec_aac"
	case "mp2", "mp3":
		return "mpg123audiodec"
	case "ac3":
		return "avdec_ac3"
	case "eac3":
		return "avdec_eac3"
	default:
		return "avdec_aac_latm"
	}
}

func audioParser(codec string) string {
	switch codec {
	case "aac", "aac_latm":
		return "aacparse"
	case "ac3":
		return "ac3parse"
	case "mp2", "mp3":
		return "mpegaudioparse"
	default:
		return "aacparse"
	}
}

func canCopyAudio(codec string, format OutputFormat) bool {
	if format == OutputMPEGTS {
		return codec == "aac" || codec == "mp2" || codec == "ac3"
	}
	return codec == "aac"
}

func NormalizeCodec(codec string) string {
	c := strings.ToLower(strings.TrimSpace(codec))
	switch {
	case c == "hevc" || c == "h.265 video" || c == "h265 video" || c == "h.265/hevc video" || strings.Contains(c, "h265") || strings.Contains(c, "h.265") || strings.Contains(c, "hevc"):
		return "h265"
	case c == "h.264 video" || c == "h264 video" || strings.Contains(c, "h264") || strings.Contains(c, "h.264"):
		return "h264"
	case c == "mpeg2" || c == "mpeg2video" || c == "mpeg2 video" || c == "mpeg-2 video":
		return "mpeg2video"
	case c == "aac_latm" || c == "mp4a-latm" || c == "aac audio (latm)":
		return "aac_latm"
	case c == "aac" || c == "aac audio":
		return "aac"
	case c == "mp2" || c == "mp2 (mpeg audio layer 2)" || c == "mpeg audio layer 2" || c == "mpeg-1 audio" || c == "mp3" || c == "mpeg audio":
		return "mp2"
	case c == "ac3" || c == "ac-3" || c == "a_ac3":
		return "ac3"
	case c == "eac3" || c == "e-ac-3" || c == "a_eac3":
		return "eac3"
	case c == "dts" || c == "dca" || c == "dts-hd" || strings.Contains(c, "dts"):
		return "dts"
	case c == "opus" || c == "libopus":
		return "opus"
	case c == "flac":
		return "flac"
	case c == "vorbis" || c == "libvorbis":
		return "vorbis"
	case c == "av1 video":
		return "av1"
	}
	return c
}
