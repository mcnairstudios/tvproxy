package service

import (
	"testing"
)

func TestClassifyStream(t *testing.T) {
	tests := []struct {
		name string
		in   StrategyInput
		want StreamCategory
	}{
		{
			name: "xtream live channel",
			in:   StrategyInput{StreamURL: "http://provider.com/live/user/pass/123.ts", VODType: ""},
			want: CategoryLiveIPTV,
		},
		{
			name: "xtream movie",
			in:   StrategyInput{StreamURL: "http://provider.com/movie/user/pass/456.avi", VODType: "movie"},
			want: CategoryVODRemote,
		},
		{
			name: "xtream series episode",
			in:   StrategyInput{StreamURL: "http://provider.com/series/user/pass/789.mp4", VODType: "series"},
			want: CategoryVODRemote,
		},
		{
			name: "tvproxy-streams movie (local)",
			in:   StrategyInput{StreamURL: "http://192.168.1.149:8090/stream/movies/test.mkv", VODType: "movie"},
			want: CategoryVODLocal,
		},
		{
			name: "satip stream",
			in:   StrategyInput{StreamURL: "rtsp://192.168.1.148/?freq=545", SatIPSource: true},
			want: CategoryLiveSatIP,
		},
		{
			name: "live with explicit type",
			in:   StrategyInput{StreamURL: "http://provider.com/live/123.ts", VODType: "live"},
			want: CategoryLiveIPTV,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyStream(tt.in)
			if got != tt.want {
				t.Errorf("classifyStream() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestResolveSessionStrategy_LiveIPTV(t *testing.T) {
	in := StrategyInput{
		StreamURL: "http://provider.com/live/user/pass/123.ts",
		VODType:   "",
		StreamID:  "test-stream-id",
	}
	out := StrategyOutput{
		Delivery:   "hls",
		VideoCodec: "copy",
		AudioCodec: "aac",
		Container:  "mp4",
	}

	s := resolveSessionStrategy(in, out, "/tmp/recordings")

	if s.Category != CategoryLiveIPTV {
		t.Errorf("category = %d, want CategoryLiveIPTV", s.Category)
	}
	if s.MetadataOnly {
		t.Error("MetadataOnly should be false for live")
	}
	if s.HLSOutputDir == "" {
		t.Error("HLSOutputDir should be set for live HLS")
	}
	if s.FFmpegArgs != "" {
		t.Errorf("FFmpegArgs should be empty for live HLS, got %q", s.FFmpegArgs)
	}
}

func TestResolveSessionStrategy_VODRemote(t *testing.T) {
	in := StrategyInput{
		StreamURL: "http://provider.com/movie/user/pass/456.avi",
		VODType:   "movie",
		StreamID:  "test-movie-id",
	}
	out := StrategyOutput{
		Delivery:   "hls",
		VideoCodec: "copy",
		AudioCodec: "aac",
		Container:  "mp4",
		Args:       "-c:v copy -c:a aac",
	}

	s := resolveSessionStrategy(in, out, "/tmp/recordings")

	if s.Category != CategoryVODRemote {
		t.Errorf("category = %d, want CategoryVODRemote", s.Category)
	}
	if !s.MetadataOnly {
		t.Error("MetadataOnly should be true for remote VOD with HLS delivery")
	}
	if s.HLSOutputDir != "" {
		t.Error("HLSOutputDir should be empty for VOD")
	}
}

func TestResolveSessionStrategy_VODLocal(t *testing.T) {
	in := StrategyInput{
		StreamURL: "http://192.168.1.149:8090/stream/movies/test.mkv",
		VODType:   "movie",
		StreamID:  "test-local-id",
	}
	out := StrategyOutput{
		Delivery:   "hls",
		VideoCodec: "copy",
		AudioCodec: "aac",
		Container:  "mp4",
	}

	s := resolveSessionStrategy(in, out, "/tmp/recordings")

	if s.Category != CategoryVODLocal {
		t.Errorf("category = %d, want CategoryVODLocal", s.Category)
	}
	if !s.MetadataOnly {
		t.Error("MetadataOnly should be true for local VOD with HLS delivery")
	}
}

func TestResolveSessionStrategy_LiveWithTranscode(t *testing.T) {
	in := StrategyInput{
		StreamURL: "http://provider.com/live/user/pass/123.ts",
		VODType:   "",
		StreamID:  "test-transcode-id",
	}
	out := StrategyOutput{
		Delivery:   "hls",
		VideoCodec: "av1",
		AudioCodec: "aac",
		HWAccel:    "qsv",
		Container:  "mp4",
	}

	s := resolveSessionStrategy(in, out, "/tmp/recordings")

	if s.VideoCodec != "av1" {
		t.Errorf("VideoCodec = %q, want av1", s.VideoCodec)
	}
	if s.HWAccel != "qsv" {
		t.Errorf("HWAccel = %q, want qsv", s.HWAccel)
	}
	if s.MetadataOnly {
		t.Error("MetadataOnly should be false for live")
	}
}

func TestIsLocalURL(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"http://192.168.1.149:8090/stream/test.mkv", true},
		{"http://10.0.0.1/stream", true},
		{"http://localhost:8080/test", true},
		{"http://127.0.0.1/test", true},
		{"http://172.16.0.1/test", true},
		{"http://t.41rpa.uk:8880/live/123.ts", false},
		{"http://provider.com/movie/456.avi", false},
		{"/local/file.mp4", true},
		{"rtsp://192.168.1.148/?freq=545", true},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			if got := isLocalURL(tt.url); got != tt.want {
				t.Errorf("isLocalURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}
