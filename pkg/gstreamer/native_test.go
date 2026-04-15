package gstreamer

import (
	"testing"

	"github.com/gavinmcnair/tvproxy/pkg/media"
)

func TestBuildNativePipeline_HDHR(t *testing.T) {
	probe := &media.ProbeResult{
		HasVideo:    true,
		Video:       &media.VideoInfo{Codec: "h264"},
		AudioTracks: []media.AudioTrack{{Codec: "aac_latm"}},
		FormatName:  "mpegts",
	}

	pipeline, err := BuildNativePipeline("test-hdhr", probe, PipelineOpts{
		InputURL:         "http://192.168.1.186:5004/auto/v101",
		InputType:        "http",
		IsLive:           true,
		OutputVideoCodec: "copy",
		OutputAudioCodec: "aac",
		OutputFormat:     OutputMPEGTS,
		RecordingPath:    "/tmp/test_native.ts",
	})
	if err != nil {
		t.Fatalf("failed to build pipeline: %v", err)
	}
	if pipeline == nil {
		t.Fatal("pipeline is nil")
	}
	t.Logf("pipeline created successfully")
}

func TestBuildNativePipeline_Transcode(t *testing.T) {
	probe := &media.ProbeResult{
		HasVideo:    true,
		Video:       &media.VideoInfo{Codec: "h264"},
		AudioTracks: []media.AudioTrack{{Codec: "aac_latm"}},
		FormatName:  "mpegts",
	}

	pipeline, err := BuildNativePipeline("test-transcode", probe, PipelineOpts{
		InputURL:         "http://192.168.1.186:5004/auto/v101",
		InputType:        "http",
		IsLive:           true,
		OutputVideoCodec: "h264",
		OutputAudioCodec: "aac",
		OutputBitrate:    4000,
		OutputFormat:     OutputMP4,
		HWAccel:          HWVideoToolbox,
		RecordingPath:    "/tmp/test_transcode.mp4",
	})
	if err != nil {
		t.Fatalf("failed to build pipeline: %v", err)
	}
	if pipeline == nil {
		t.Fatal("pipeline is nil")
	}
	t.Logf("transcode pipeline created successfully")
}
