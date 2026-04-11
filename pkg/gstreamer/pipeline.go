package gstreamer

import (
	"fmt"
	"strings"

	"github.com/gavinmcnair/tvproxy/pkg/ffmpeg"
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

	DualOutput bool // HLS + recording simultaneously
}

type Pipeline struct {
	Elements []string
	Cmd      string
	Args     []string
}

func BuildPipeline(opts PipelineOpts) *Pipeline {
	var elements []string

	elements = append(elements, buildSource(opts)...)
	elements = append(elements, buildDemux(opts)...)

	if opts.DualOutput {
		elements = append(elements, buildDualOutput(opts)...)
	} else {
		elements = append(elements, buildVideoChain(opts)...)
		elements = append(elements, buildAudioChain(opts)...)
		elements = append(elements, buildMux(opts)...)
		elements = append(elements, buildSink(opts)...)
	}

	pipeline := strings.Join(elements, " ")
	return &Pipeline{
		Elements: elements,
		Cmd:      "gst-launch-1.0",
		Args:     []string{"-q", "-e", pipeline},
	}
}

func BuildFromProbe(probe *ffmpeg.ProbeResult, inputURL string, opts PipelineOpts) *Pipeline {
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

func buildSource(opts PipelineOpts) []string {
	switch opts.InputType {
	case "rtsp":
		return []string{
			fmt.Sprintf("rtspsrc location=%q latency=0 protocols=tcp", opts.InputURL),
			"!", "rtpmp2tdepay",
		}
	case "file":
		return []string{
			fmt.Sprintf("filesrc location=%q", opts.InputURL),
		}
	default:
		src := fmt.Sprintf("souphttpsrc location=%q", opts.InputURL)
		if opts.IsLive {
			src += " do-timestamp=true is-live=true"
		}
		return []string{src}
	}
}

func buildDemux(opts PipelineOpts) []string {
	elements := []string{"!", "tsparse", "set-timestamps=true"}

	if opts.DualOutput {
		elements = append(elements, "!", "tsdemux", "name=demux")
	} else {
		elements = append(elements, "!", "tsdemux")
	}
	return elements
}

func buildVideoChain(opts PipelineOpts) []string {
	var elements []string

	vcodec := normalizeCodec(opts.VideoCodec)
	outCodec := opts.OutputVideoCodec
	if outCodec == "" || outCodec == "default" {
		outCodec = "copy"
	}

	if outCodec == "copy" {
		switch vcodec {
		case "h264":
			elements = append(elements, "!", "queue", "!", "h264parse")
		case "h265", "hevc":
			elements = append(elements, "!", "queue", "!", "h265parse")
		case "mpeg2video":
			elements = append(elements, "!", "queue", "!", "mpegvideoparse")
		default:
			elements = append(elements, "!", "queue")
		}
	} else {
		elements = append(elements, "!", "queue")
		elements = append(elements, buildDecoder(vcodec, opts.HWAccel)...)
		elements = append(elements, buildEncoder(outCodec, opts.HWAccel, opts.OutputBitrate)...)
	}
	return elements
}

func buildDecoder(codec string, hw HWAccel) []string {
	switch hw {
	case HWVAAPI:
		switch codec {
		case "h264":
			return []string{"!", "h264parse", "!", "vaapih264dec"}
		case "h265", "hevc":
			return []string{"!", "h265parse", "!", "vaapih265dec"}
		case "mpeg2video":
			return []string{"!", "mpegvideoparse", "!", "vaapidecode"}
		}
	case HWQSV:
		switch codec {
		case "h264":
			return []string{"!", "h264parse", "!", "qsvh264dec"}
		case "h265", "hevc":
			return []string{"!", "h265parse", "!", "qsvh265dec"}
		}
	case HWVideoToolbox:
		switch codec {
		case "h264":
			return []string{"!", "h264parse", "!", "vtdec"}
		}
	}
	switch codec {
	case "h264":
		return []string{"!", "h264parse", "!", "avdec_h264"}
	case "h265", "hevc":
		return []string{"!", "h265parse", "!", "avdec_h265"}
	case "mpeg2video":
		return []string{"!", "mpegvideoparse", "!", "avdec_mpeg2video"}
	}
	return []string{"!", "decodebin"}
}

func buildEncoder(codec string, hw HWAccel, bitrate int) []string {
	br := bitrate
	if br <= 0 {
		br = 4000
	}

	switch hw {
	case HWVAAPI:
		var enc string
		switch codec {
		case "h264":
			enc = fmt.Sprintf("vaapih264enc bitrate=%d tune=low-latency", br)
		case "h265", "hevc":
			enc = fmt.Sprintf("vaapih265enc bitrate=%d", br)
		case "av1":
			enc = fmt.Sprintf("vaapiav1enc bitrate=%d", br)
		default:
			enc = fmt.Sprintf("vaapih264enc bitrate=%d tune=low-latency", br)
		}
		return []string{"!", "videoconvert", "!", enc}
	case HWQSV:
		var enc string
		switch codec {
		case "h264":
			enc = fmt.Sprintf("qsvh264enc bitrate=%d target-usage=1", br)
		case "h265", "hevc":
			enc = fmt.Sprintf("qsvh265enc bitrate=%d", br)
		default:
			enc = fmt.Sprintf("qsvh264enc bitrate=%d target-usage=1", br)
		}
		return []string{"!", enc}
	case HWVideoToolbox:
		return []string{
			"!", "videoconvert",
			"!", fmt.Sprintf("vtenc_h264 bitrate=%d realtime=true allow-frame-reordering=false", br),
		}
	}
	return []string{
		"!", "videoconvert",
		"!", fmt.Sprintf("x264enc bitrate=%d speed-preset=ultrafast tune=zerolatency", br),
	}
}

func buildAudioChain(opts PipelineOpts) []string {
	acodec := normalizeCodec(opts.AudioCodec)
	outAudio := opts.OutputAudioCodec
	if outAudio == "" || outAudio == "default" {
		outAudio = "aac"
	}

	if outAudio == "copy" && canCopyAudio(acodec, opts.OutputFormat) {
		return nil
	}

	var dec string
	switch acodec {
	case "aac_latm":
		dec = "avdec_aac_latm"
	case "aac":
		dec = "avdec_aac"
	case "mp2", "mp3":
		dec = "mpg123audiodec"
	case "ac3":
		dec = "avdec_ac3"
	default:
		dec = "decodebin"
	}

	return []string{
		"!", "queue", "!", dec, "!", "audioconvert",
		"!", "faac", "!", "aacparse",
	}
}

func buildMux(opts PipelineOpts) []string {
	switch opts.OutputFormat {
	case OutputHLS:
		seg := opts.HLSSegmentTime
		if seg <= 0 {
			seg = 6
		}
		dir := opts.HLSDir
		if dir == "" {
			dir = "/tmp/hls"
		}
		return []string{
			"!", fmt.Sprintf("hlssink3 target-duration=%d playlist-location=%s/playlist.m3u8 location=%s/seg%%05d.ts", seg, dir, dir),
		}
	case OutputMP4:
		return []string{"!", "mp4mux", "fragment-duration=1000", "streamable=true"}
	default:
		return []string{"!", "mpegtsmux"}
	}
}

func buildSink(opts PipelineOpts) []string {
	if opts.RecordingPath != "" {
		return []string{"!", fmt.Sprintf("filesink location=%q", opts.RecordingPath)}
	}
	return []string{"!", "fdsink", "fd=1"}
}

func buildDualOutput(opts PipelineOpts) []string {
	var elements []string

	elements = append(elements, "demux.", "!", "queue")
	elements = append(elements, buildDecoder(normalizeCodec(opts.VideoCodec), opts.HWAccel)...)
	elements = append(elements, "!", "tee", "name=vt")

	seg := opts.HLSSegmentTime
	if seg <= 0 {
		seg = 6
	}
	dir := opts.HLSDir
	if dir == "" {
		dir = "/tmp/hls"
	}

	elements = append(elements,
		"vt.", "!", "queue",
	)
	elements = append(elements, buildEncoder(opts.OutputVideoCodec, opts.HWAccel, opts.OutputBitrate)...)
	elements = append(elements,
		"!", "h264parse",
		"!", "mpegtsmux", "name=hlsmux",
		"!", fmt.Sprintf("hlssink3 target-duration=%d playlist-location=%s/playlist.m3u8 location=%s/seg%%05d.ts", seg, dir, dir),
	)

	if opts.RecordingPath != "" {
		elements = append(elements,
			"vt.", "!", "queue",
		)
		elements = append(elements, buildEncoder(opts.OutputVideoCodec, opts.HWAccel, opts.OutputBitrate)...)
		elements = append(elements,
			"!", "h264parse",
			"!", "mpegtsmux", "name=recmux",
			"!", fmt.Sprintf("filesink location=%q", opts.RecordingPath),
		)
	}

	acodec := normalizeCodec(opts.AudioCodec)
	var adec string
	switch acodec {
	case "aac_latm":
		adec = "avdec_aac_latm"
	case "aac":
		adec = "avdec_aac"
	case "mp2", "mp3":
		adec = "mpg123audiodec"
	default:
		adec = "decodebin"
	}

	elements = append(elements,
		"demux.", "!", "queue", "!", adec, "!", "audioconvert", "!", "tee", "name=at",
		"at.", "!", "queue", "!", "faac", "!", "aacparse", "!", "hlsmux.",
	)
	if opts.RecordingPath != "" {
		elements = append(elements,
			"at.", "!", "queue", "!", "faac", "!", "aacparse", "!", "recmux.",
		)
	}

	return elements
}

func canCopyAudio(codec string, format OutputFormat) bool {
	if format == OutputMPEGTS {
		return codec == "aac" || codec == "mp2" || codec == "ac3"
	}
	return codec == "aac"
}

func normalizeCodec(codec string) string {
	c := strings.ToLower(codec)
	switch c {
	case "hevc":
		return "h265"
	case "mpeg2", "mpeg2video":
		return "mpeg2video"
	case "aac_latm", "mp4a-latm":
		return "aac_latm"
	}
	return c
}
