package gstreamer

import (
	"testing"
)

func TestBuild_ContainerDetection(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		want    string
	}{
		{"MP4 file", "http://server/video.mp4", "mp4"},
		{"MKV file", "http://server/video.mkv", "matroska"},
		{"TS file", "http://server/stream.ts", "mpegts"},
		{"WebM file", "http://server/video.webm", "webm"},
		{"No extension", "http://server/stream/123", ""},
		{"HDHR port 5004", "http://192.168.1.186:5004/auto/v101", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containerFromURL(tt.url)
			if got != tt.want {
				t.Errorf("containerFromURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestNormalizeCodec_AllProbeNames(t *testing.T) {
	tests := []struct{ in, want string }{
		{"h264", "h264"},
		{"hevc", "h265"},
		{"mpeg2video", "mpeg2video"},
		{"aac", "aac"},
		{"mp2", "mp2"},
		{"ac3", "ac3"},
		{"eac3", "eac3"},
		{"H.264 Video", "h264"},
		{"H.265 Video", "h265"},
		{"H.265/HEVC Video", "h265"},
		{"MPEG-2 Video", "mpeg2video"},
		{"AV1 Video", "av1"},
		{"AAC Audio (LATM)", "aac_latm"},
		{"AAC Audio", "aac"},
		{"MPEG-1 Audio", "mp2"},
		{"MP2 (MPEG audio layer 2)", "mp2"},
		{"AC-3", "ac3"},
		{"E-AC-3", "eac3"},
		{"copy", "copy"},
		{"", ""},
		{"default", "default"},
		{"TV(HEVC)", "h265"},
		{"dts", "dts"},
		{"dca", "dts"},
		{"DTS-HD", "dts"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := NormalizeCodec(tt.in)
			if got != tt.want {
				t.Errorf("NormalizeCodec(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestBuildAudioChain(t *testing.T) {
	tests := []struct {
		name     string
		codec    string
		wantLen  int
	}{
		{"AAC LATM", "aac_latm", 7},
		{"AAC copy", "aac", 1},
		{"MP2", "mp2", 7},
		{"AC3", "ac3", 6},
		{"EAC3", "eac3", 6},
		{"DTS", "dts", 6},
		{"Empty (default)", "", 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chain := buildAudioChain(tt.codec)
			if len(chain) != tt.wantLen {
				t.Errorf("buildAudioChain(%q) length = %d, want %d", tt.codec, len(chain), tt.wantLen)
			}
			for i, el := range chain {
				if el == nil {
					t.Errorf("buildAudioChain(%q) element %d is nil", tt.codec, i)
				}
			}
		})
	}
}

func TestBuild_PathSelection(t *testing.T) {
	tests := []struct {
		name string
		opts PipelineOpts
		want string
	}{
		{
			"RTSP transcode",
			PipelineOpts{InputURL: "rtsp://server/?freq=545", OutputVideoCodec: "av1", VideoCodec: "h264", RecordingPath: "/tmp/test.mp4"},
			"rtsp-transcode",
		},
		{
			"RTSP copy",
			PipelineOpts{InputURL: "rtsp://server/?freq=545", OutputVideoCodec: "copy", VideoCodec: "h264", RecordingPath: "/tmp/test.ts"},
			"rtsp-copy",
		},
		{
			"VOD MP4 transcode",
			PipelineOpts{InputURL: "http://server/movie.mp4", OutputVideoCodec: "av1", VideoCodec: "hevc", Container: "mp4", RecordingPath: "/tmp/test.mp4"},
			"vod-mp4-transcode",
		},
		{
			"VOD MP4 copy",
			PipelineOpts{InputURL: "http://server/movie.mp4", OutputVideoCodec: "copy", VideoCodec: "hevc", Container: "mp4", RecordingPath: "/tmp/test.mp4"},
			"vod-mp4-copy",
		},
		{
			"HDHR HTTP transcode",
			PipelineOpts{InputURL: "http://192.168.1.186:5004/auto/v101", OutputVideoCodec: "av1", VideoCodec: "h264", RecordingPath: "/tmp/test.mp4"},
			"mpegts-transcode",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, path, err := Build(tt.opts)
			if err != nil {
				t.Skipf("Build error (expected on CI without GStreamer): %v", err)
			}
			if path != tt.want {
				t.Errorf("Build path = %q, want %q", path, tt.want)
			}
		})
	}
}

func TestBuild_CopyDetection(t *testing.T) {
	tests := []struct {
		name     string
		srcCodec string
		outCodec string
		wantCopy bool
	}{
		{"explicit copy", "h264", "copy", true},
		{"empty output = copy", "h264", "", true},
		{"default = copy", "h264", "default", true},
		{"same codec = copy", "h264", "h264", true},
		{"different codec = transcode", "h264", "av1", false},
		{"h265 to av1 = transcode", "h265", "av1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outCodec := NormalizeCodec(tt.outCodec)
			srcCodec := NormalizeCodec(tt.srcCodec)
			isCopy := outCodec == "" || outCodec == "default" || outCodec == "copy" || outCodec == srcCodec
			if isCopy != tt.wantCopy {
				t.Errorf("isCopy(src=%q, out=%q) = %v, want %v", tt.srcCodec, tt.outCodec, isCopy, tt.wantCopy)
			}
		})
	}
}

func TestBitrate(t *testing.T) {
	if got := bitrate(PipelineOpts{OutputBitrate: 8000}); got != 8000 {
		t.Errorf("bitrate with explicit = %d, want 8000", got)
	}
	if got := bitrate(PipelineOpts{OutputBitrate: 0}); got != 6000 {
		t.Errorf("bitrate default = %d, want 6000", got)
	}
}

func TestBuild_MPEGTSDetection(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		container string
		wantMPEG  bool
	}{
		{"RTSP SAT>IP", "rtsp://192.168.1.149/?freq=545", "", true},
		{"HDHR HTTP", "http://192.168.1.186:5004/auto/v101", "", true},
		{"IPTV .ts", "http://provider/stream.ts", "mpegts", true},
		{"VOD .mp4", "http://server/movie.mp4", "mp4", false},
		{"VOD .mkv", "http://server/movie.mkv", "matroska", false},
		{"Unknown URL no container", "http://provider/live/123", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			container := tt.container
			if container == "" {
				container = containerFromURL(tt.url)
			}
			isRTSP := len(tt.url) > 7 && tt.url[:7] == "rtsp://"
			isMPEGTS := isRTSP || container == "mpegts" || container == "mpeg-ts" || container == "ts" || container == ""
			if isMPEGTS != tt.wantMPEG {
				t.Errorf("isMPEGTS for %q (container=%q) = %v, want %v", tt.url, container, isMPEGTS, tt.wantMPEG)
			}
		})
	}
}
