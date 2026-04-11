package gstreamer

import (
	"fmt"
	"strings"

	"github.com/go-gst/go-gst/gst"

	"github.com/gavinmcnair/tvproxy/pkg/ffmpeg"
)

func init() {
	gst.Init(nil)
}

func BuildNativePipeline(name string, probe *ffmpeg.ProbeResult, opts PipelineOpts) (*gst.Pipeline, error) {
	if probe != nil {
		if probe.Video != nil {
			opts.VideoCodec = probe.Video.Codec
		}
		if len(probe.AudioTracks) > 0 {
			opts.AudioCodec = probe.AudioTracks[0].Codec
		}
		opts.Container = probe.FormatName
	}

	pipelineStr := buildPipelineString(opts)
	pipeline, err := gst.NewPipelineFromString(pipelineStr)
	if err != nil {
		return nil, fmt.Errorf("failed to create pipeline: %w", err)
	}
	return pipeline, nil
}

func buildPipelineString(opts PipelineOpts) string {
	var parts []string

	switch opts.InputType {
	case "rtsp":
		parts = append(parts, fmt.Sprintf("rtspsrc location=%s latency=0 protocols=tcp", opts.InputURL))
		parts = append(parts, "!", "rtpmp2tdepay")
	case "file":
		parts = append(parts, fmt.Sprintf("filesrc location=%s", opts.InputURL))
	default:
		src := fmt.Sprintf("souphttpsrc location=%s", opts.InputURL)
		if opts.IsLive {
			src += " do-timestamp=true is-live=true"
		}
		parts = append(parts, src)
	}

	parts = append(parts, "!", "tsparse", "set-timestamps=true", "!", "tsdemux", "name=demux")

	vcodec := normalizeCodec(opts.VideoCodec)
	acodec := normalizeCodec(opts.AudioCodec)

	outVideo := opts.OutputVideoCodec
	if outVideo == "" || outVideo == "default" {
		outVideo = "copy"
	}

	if opts.DualOutput {
		parts = append(parts, buildNativeDualPipeline(vcodec, acodec, outVideo, opts)...)
	} else {
		parts = append(parts, buildNativeVideoPipeline(vcodec, outVideo, opts)...)
		parts = append(parts, buildNativeAudioPipeline(acodec, opts)...)
		parts = append(parts, buildNativeOutputPipeline(opts)...)
	}

	return strings.Join(parts, " ")
}

func buildNativeVideoPipeline(vcodec, outVideo string, opts PipelineOpts) []string {
	var parts []string
	parts = append(parts, "demux.", "!", "queue")

	if outVideo == "copy" {
		switch vcodec {
		case "h264":
			parts = append(parts, "!", "h264parse")
		case "h265", "hevc":
			parts = append(parts, "!", "h265parse")
		case "mpeg2video":
			parts = append(parts, "!", "mpegvideoparse")
		}
	} else {
		parts = append(parts, nativeDecodeChain(vcodec, opts.HWAccel)...)
		parts = append(parts, nativeEncodeChain(outVideo, opts.HWAccel, opts.OutputBitrate)...)
		parts = append(parts, "!", "h264parse")
	}
	return parts
}

func buildNativeAudioPipeline(acodec string, opts PipelineOpts) []string {
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

	return []string{"demux.", "!", "queue", "!", dec, "!", "audioconvert", "!", "faac", "!", "aacparse"}
}

func buildNativeOutputPipeline(opts PipelineOpts) []string {
	var parts []string

	switch opts.OutputFormat {
	case OutputHLS:
		seg := opts.HLSSegmentTime
		if seg <= 0 {
			seg = 6
		}
		dir := opts.HLSDir
		parts = append(parts, "!", fmt.Sprintf("hlssink3 name=mux target-duration=%d playlist-location=%s/playlist.m3u8 location=%s/seg%%05d.ts", seg, dir, dir))
	case OutputMP4:
		parts = append(parts, "!", "mp4mux", "fragment-duration=1000", "streamable=true")
		if opts.RecordingPath != "" {
			parts = append(parts, "!", fmt.Sprintf("filesink location=%s", opts.RecordingPath))
		} else {
			parts = append(parts, "!", "fdsink", "fd=1")
		}
	default:
		parts = append(parts, "!", "mpegtsmux", "name=mux")
		if opts.RecordingPath != "" {
			parts = append(parts, "!", fmt.Sprintf("filesink location=%s", opts.RecordingPath))
		} else {
			parts = append(parts, "!", "fdsink", "fd=1")
		}
	}
	return parts
}

func buildNativeDualPipeline(vcodec, acodec, outVideo string, opts PipelineOpts) []string {
	var parts []string

	parts = append(parts, "demux.", "!", "queue")
	if outVideo == "copy" {
		switch vcodec {
		case "h264":
			parts = append(parts, "!", "h264parse")
		case "h265":
			parts = append(parts, "!", "h265parse")
		case "mpeg2video":
			parts = append(parts, "!", "mpegvideoparse")
		}
	} else {
		parts = append(parts, nativeDecodeChain(vcodec, opts.HWAccel)...)
		parts = append(parts, nativeEncodeChain(outVideo, opts.HWAccel, opts.OutputBitrate)...)
		parts = append(parts, "!", "h264parse")
	}
	parts = append(parts, "!", "tee", "name=vt")

	seg := opts.HLSSegmentTime
	if seg <= 0 {
		seg = 6
	}
	dir := opts.HLSDir

	parts = append(parts, "vt.", "!", "queue", "!", "mpegtsmux", "name=hlsmux",
		"!", fmt.Sprintf("hlssink3 target-duration=%d playlist-location=%s/playlist.m3u8 location=%s/seg%%05d.ts", seg, dir, dir))

	if opts.RecordingPath != "" {
		parts = append(parts, "vt.", "!", "queue", "!", "mpegtsmux", "name=recmux",
			"!", fmt.Sprintf("filesink location=%s", opts.RecordingPath))
	}

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

	parts = append(parts, "demux.", "!", "queue", "!", adec, "!", "audioconvert", "!", "tee", "name=at")
	parts = append(parts, "at.", "!", "queue", "!", "faac", "!", "aacparse", "!", "hlsmux.")
	if opts.RecordingPath != "" {
		parts = append(parts, "at.", "!", "queue", "!", "faac", "!", "aacparse", "!", "recmux.")
	}

	return parts
}

func nativeDecodeChain(codec string, hw HWAccel) []string {
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
		case "h265":
			return []string{"!", "h265parse", "!", "qsvh265dec"}
		}
	case HWVideoToolbox:
		if codec == "h264" {
			return []string{"!", "h264parse", "!", "vtdec"}
		}
	}
	switch codec {
	case "h264":
		return []string{"!", "h264parse", "!", "avdec_h264"}
	case "h265":
		return []string{"!", "h265parse", "!", "avdec_h265"}
	case "mpeg2video":
		return []string{"!", "mpegvideoparse", "!", "avdec_mpeg2video"}
	}
	return []string{"!", "decodebin"}
}

func nativeEncodeChain(codec string, hw HWAccel, bitrate int) []string {
	br := bitrate
	if br <= 0 {
		br = 4000
	}

	switch hw {
	case HWVAAPI:
		switch codec {
		case "h264":
			return []string{"!", "videoconvert", "!", fmt.Sprintf("vaapih264enc bitrate=%d tune=low-latency", br)}
		case "h265", "hevc":
			return []string{"!", "videoconvert", "!", fmt.Sprintf("vaapih265enc bitrate=%d", br)}
		case "av1":
			return []string{"!", "videoconvert", "!", fmt.Sprintf("vaapiav1enc bitrate=%d", br)}
		}
	case HWQSV:
		switch codec {
		case "h264":
			return []string{"!", fmt.Sprintf("qsvh264enc bitrate=%d target-usage=1", br)}
		case "h265":
			return []string{"!", fmt.Sprintf("qsvh265enc bitrate=%d", br)}
		}
	case HWVideoToolbox:
		return []string{"!", "videoconvert", "!", fmt.Sprintf("vtenc_h264 bitrate=%d realtime=true allow-frame-reordering=false", br)}
	}
	return []string{"!", "videoconvert", "!", fmt.Sprintf("x264enc bitrate=%d speed-preset=ultrafast tune=zerolatency", br)}
}
