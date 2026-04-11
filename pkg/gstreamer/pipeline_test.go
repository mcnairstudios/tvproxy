package gstreamer

import (
	"strings"
	"testing"

	"github.com/gavinmcnair/tvproxy/pkg/ffmpeg"
)

func TestBuildPipeline_HDHRCopy(t *testing.T) {
	p := BuildPipeline(PipelineOpts{
		InputURL:         "http://192.168.1.186:5004/auto/v101",
		InputType:        "http",
		IsLive:           true,
		VideoCodec:       "h264",
		AudioCodec:       "aac_latm",
		OutputVideoCodec: "copy",
		OutputAudioCodec: "aac",
		OutputFormat:     OutputMPEGTS,
		RecordingPath:    "/tmp/recording.ts",
	})

	cmd := strings.Join(p.Args, " ")
	if !strings.Contains(cmd, "souphttpsrc") {
		t.Error("expected souphttpsrc")
	}
	if !strings.Contains(cmd, "h264parse") {
		t.Error("expected h264parse for copy mode")
	}
	if !strings.Contains(cmd, "avdec_aac_latm") {
		t.Error("expected avdec_aac_latm")
	}
	if !strings.Contains(cmd, "mpegtsmux") {
		t.Error("expected mpegtsmux")
	}
	if !strings.Contains(cmd, "filesink") {
		t.Error("expected filesink")
	}
}

func TestBuildPipeline_VaapiTranscode(t *testing.T) {
	p := BuildPipeline(PipelineOpts{
		InputURL:         "http://provider.com/stream",
		InputType:        "http",
		IsLive:           true,
		VideoCodec:       "h264",
		AudioCodec:       "aac",
		OutputVideoCodec: "h264",
		OutputAudioCodec: "aac",
		OutputBitrate:    4000,
		OutputFormat:     OutputHLS,
		HWAccel:          HWVAAPI,
		HLSDir:           "/tmp/hls",
		HLSSegmentTime:   6,
	})

	cmd := strings.Join(p.Args, " ")
	if !strings.Contains(cmd, "vaapih264dec") {
		t.Error("expected vaapih264dec")
	}
	if !strings.Contains(cmd, "vaapih264enc") {
		t.Error("expected vaapih264enc")
	}
	if !strings.Contains(cmd, "hlssink3") {
		t.Error("expected hlssink3")
	}
}

func TestBuildPipeline_SatIP(t *testing.T) {
	p := BuildPipeline(PipelineOpts{
		InputURL:         "rtsp://192.168.1.100:554/stream",
		InputType:        "rtsp",
		IsLive:           true,
		VideoCodec:       "mpeg2video",
		AudioCodec:       "mp2",
		OutputVideoCodec: "h264",
		OutputAudioCodec: "aac",
		OutputBitrate:    3000,
		OutputFormat:     OutputMPEGTS,
		HWAccel:          HWVAAPI,
	})

	cmd := strings.Join(p.Args, " ")
	if !strings.Contains(cmd, "rtspsrc") {
		t.Error("expected rtspsrc")
	}
	if !strings.Contains(cmd, "rtpmp2tdepay") {
		t.Error("expected rtpmp2tdepay")
	}
	if !strings.Contains(cmd, "vaapidecode") || !strings.Contains(cmd, "mpegvideoparse") {
		t.Error("expected vaapi mpeg2 decode chain")
	}
}

func TestBuildPipeline_DualOutput(t *testing.T) {
	p := BuildPipeline(PipelineOpts{
		InputURL:         "http://device/auto/v101",
		InputType:        "http",
		IsLive:           true,
		VideoCodec:       "h264",
		AudioCodec:       "aac_latm",
		OutputVideoCodec: "h264",
		OutputBitrate:    4000,
		OutputFormat:     OutputHLS,
		HWAccel:          HWVAAPI,
		HLSDir:           "/tmp/hls",
		RecordingPath:    "/tmp/rec.ts",
		DualOutput:       true,
	})

	cmd := strings.Join(p.Args, " ")
	if !strings.Contains(cmd, "tee name=vt") {
		t.Error("expected tee for video fan-out")
	}
	if !strings.Contains(cmd, "hlssink3") {
		t.Error("expected hlssink3")
	}
	if !strings.Contains(cmd, "filesink") {
		t.Error("expected filesink for recording")
	}
	if !strings.Contains(cmd, "tee name=at") {
		t.Error("expected tee for audio fan-out")
	}
}

func TestBuildFromProbe(t *testing.T) {
	probe := &ffmpeg.ProbeResult{
		HasVideo:   true,
		Video:      &ffmpeg.VideoInfo{Codec: "h264"},
		AudioTracks: []ffmpeg.AudioTrack{{Codec: "aac_latm"}},
		FormatName: "mpegts",
	}

	p := BuildFromProbe(probe, "http://device/stream", PipelineOpts{
		InputType:        "http",
		IsLive:           true,
		OutputVideoCodec: "copy",
		OutputFormat:     OutputMPEGTS,
	})

	cmd := strings.Join(p.Args, " ")
	if !strings.Contains(cmd, "h264parse") {
		t.Error("expected h264parse from probe video codec")
	}
	if !strings.Contains(cmd, "avdec_aac_latm") {
		t.Error("expected aac_latm decoder from probe audio codec")
	}
}
