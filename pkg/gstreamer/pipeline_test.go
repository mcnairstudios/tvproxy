package gstreamer

import (
	"strings"
	"testing"

	"github.com/gavinmcnair/tvproxy/pkg/media"
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

	cmd := p.PipelineStr
	if !strings.Contains(cmd, "h264parse") {
		t.Error("expected h264parse for copy mode")
	}
	if !strings.Contains(cmd, "aacparse") {
		t.Error("expected aacparse before decoder")
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
	if !strings.Contains(cmd, "config-interval=-1") {
		t.Error("expected config-interval=-1 on parser")
	}
}

func TestBuildPipeline_VaapiTranscode(t *testing.T) {
	p := BuildPipeline(PipelineOpts{
		InputURL:         "http://provider.com/stream",
		InputType:        "http",
		IsLive:           true,
		VideoCodec:       "h264",
		AudioCodec:       "aac",
		OutputVideoCodec: "h265",
		OutputAudioCodec: "aac",
		OutputBitrate:    4000,
		OutputFormat:     OutputMPEGTS,
		HWAccel:          HWVAAPI,
		RecordingPath:    "/tmp/transcode.ts",
	})

	cmd := p.PipelineStr
	if !strings.Contains(cmd, "vaapih264dec") {
		t.Errorf("expected vaapih264dec, got: %s", cmd)
	}
	if !strings.Contains(cmd, "vaapih265enc") {
		t.Errorf("expected vaapih265enc, got: %s", cmd)
	}
	if !strings.Contains(cmd, "h265parse") {
		t.Error("expected h265parse for h265 output")
	}
	if strings.Contains(cmd, "videoconvert") {
		t.Error("vaapi transcode should NOT have videoconvert")
	}
}

func TestBuildPipeline_VideoToolboxH265(t *testing.T) {
	p := BuildPipeline(PipelineOpts{
		InputURL:         "http://192.168.1.186:5004/auto/v101",
		InputType:        "http",
		IsLive:           true,
		VideoCodec:       "h264",
		AudioCodec:       "aac_latm",
		OutputVideoCodec: "h265",
		OutputAudioCodec: "aac",
		OutputFormat:     OutputMPEGTS,
		HWAccel:          HWVideoToolbox,
		RecordingPath:    "/tmp/h265.ts",
	})

	cmd := p.PipelineStr
	if !strings.Contains(cmd, "vtdec") {
		t.Errorf("expected vtdec, got: %s", cmd)
	}
	if !strings.Contains(cmd, "vtenc_h265") {
		t.Errorf("expected vtenc_h265, got: %s", cmd)
	}
	if strings.Contains(cmd, "videoconvert") {
		t.Error("VideoToolbox h265 should NOT have videoconvert")
	}
	if !strings.Contains(cmd, "h265parse config-interval=-1") {
		t.Error("expected h265parse config-interval=-1")
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
		RecordingPath:    "/tmp/satip.ts",
	})

	cmd := p.PipelineStr
	if !strings.Contains(cmd, "rtspsrc") {
		t.Error("expected rtspsrc")
	}
	if !strings.Contains(cmd, "rtpmp2tdepay") {
		t.Error("expected rtpmp2tdepay")
	}
	if !strings.Contains(cmd, "mpegvideoparse") {
		t.Error("expected mpegvideoparse for mpeg2 decode")
	}
}

func TestBuildPipeline_AV1ForcesMp4Mux(t *testing.T) {
	p := BuildPipeline(PipelineOpts{
		InputURL:         "http://device/stream",
		InputType:        "http",
		IsLive:           true,
		VideoCodec:       "h264",
		AudioCodec:       "aac_latm",
		OutputVideoCodec: "av1",
		OutputAudioCodec: "aac",
		OutputFormat:     OutputMPEGTS,
		HWAccel:          HWVAAPI,
		RecordingPath:    "/tmp/av1.mp4",
	})

	cmd := p.PipelineStr
	if strings.Contains(cmd, "mpegtsmux") {
		t.Error("AV1 must NOT use mpegtsmux (AV1 not supported in MPEG-TS)")
	}
	if !strings.Contains(cmd, "mp4mux") {
		t.Error("AV1 must use mp4mux")
	}
	if !strings.Contains(cmd, "av1parse") {
		t.Error("expected av1parse")
	}
}

func TestBuildFromProbe(t *testing.T) {
	probe := &media.ProbeResult{
		HasVideo:    true,
		Video:       &media.VideoInfo{Codec: "h264"},
		AudioTracks: []media.AudioTrack{{Codec: "aac_latm"}},
		FormatName:  "mpegts",
	}

	p := BuildFromProbe(probe, "http://device/stream", PipelineOpts{
		InputType:        "http",
		IsLive:           true,
		OutputVideoCodec: "copy",
		OutputFormat:     OutputMPEGTS,
		RecordingPath:    "/tmp/probe.ts",
	})

	cmd := p.PipelineStr
	if !strings.Contains(cmd, "h264parse") {
		t.Error("expected h264parse from probe video codec")
	}
	if !strings.Contains(cmd, "avdec_aac_latm") {
		t.Error("expected aac_latm decoder from probe audio codec")
	}
	if !strings.Contains(cmd, "aacparse") {
		t.Error("expected aacparse before decoder")
	}
}

func TestBuildPipeline_AudioCopy(t *testing.T) {
	p := BuildPipeline(PipelineOpts{
		InputURL:         "http://device/stream",
		InputType:        "http",
		IsLive:           true,
		VideoCodec:       "h264",
		AudioCodec:       "aac",
		OutputVideoCodec: "copy",
		OutputAudioCodec: "copy",
		OutputFormat:     OutputMPEGTS,
		RecordingPath:    "/tmp/copy.ts",
	})

	cmd := p.PipelineStr
	if strings.Contains(cmd, "avdec_aac") {
		t.Error("audio copy should not decode")
	}
	if !strings.Contains(cmd, "aacparse") {
		t.Error("audio copy should still have aacparse")
	}
}
