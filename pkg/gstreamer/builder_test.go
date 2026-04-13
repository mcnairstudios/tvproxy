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
