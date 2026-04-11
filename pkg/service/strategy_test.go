package service

import (
	"testing"

	"github.com/gavinmcnair/tvproxy/pkg/models"
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

func TestResolveVideoAction(t *testing.T) {
	tests := []struct {
		source, client, want string
	}{
		{"h264", "default", "copy"},
		{"h264", "", "copy"},
		{"h264", "h264", "copy"},
		{"h264", "av1", "av1"},
		{"mpeg2video", "h264", "h264"},
		{"hevc", "default", "copy"},
		{"hevc", "h264", "h264"},
	}
	for _, tt := range tests {
		got := resolveVideoAction(tt.source, tt.client)
		if got != tt.want {
			t.Errorf("resolveVideoAction(%q, %q) = %q, want %q", tt.source, tt.client, got, tt.want)
		}
	}
}

func TestResolveAudioAction(t *testing.T) {
	tests := []struct {
		source, client, container, want string
	}{
		{"aac", "default", "mp4", "copy"},
		{"aac", "default", "webm", "opus"},
		{"ac3", "default", "mp4", "aac"},
		{"mp2", "default", "mp4", "aac"},
		{"aac", "aac", "mp4", "copy"},
		{"ac3", "aac", "mp4", "aac"},
		{"aac", "opus", "webm", "opus"},
	}
	for _, tt := range tests {
		got := resolveAudioAction(tt.source, tt.client, tt.container)
		if got != tt.want {
			t.Errorf("resolveAudioAction(%q, %q, %q) = %q, want %q", tt.source, tt.client, tt.container, got, tt.want)
		}
	}
}

func TestResolveSessionStrategy_LiveWithSourceProfile(t *testing.T) {
	in := StrategyInput{
		StreamURL: "http://provider.com/live/123.ts",
		VODType:   "",
		StreamID:  "test-sp",
		SourceProfile: &models.SourceProfile{
			VideoCodec:      "mpeg2video",
			AudioCodec:      "mp2",
			Deinterlace:     true,
			AudioResync:     true,
			FPSMode:         "cfr",
			AnalyzeDuration: 3000000,
			ProbeSize:       10000000,
		},
	}
	out := StrategyOutput{
		Delivery:   "hls",
		VideoCodec: "default",
		AudioCodec: "default",
		Container:  "mp4",
	}

	s := resolveSessionStrategy(in, out, "/tmp/recordings")

	if s.VideoCodec != "copy" {
		t.Errorf("VideoCodec = %q, want copy (mpeg2 source, default client = copy)", s.VideoCodec)
	}
	if s.AudioCodec != "aac" {
		t.Errorf("AudioCodec = %q, want aac (mp2 source needs transcode for mp4)", s.AudioCodec)
	}
	if !s.SourceDeinterlace {
		t.Error("SourceDeinterlace should be true")
	}
	if !s.SourceAudioResync {
		t.Error("SourceAudioResync should be true")
	}
	if s.SourceFPSMode != "cfr" {
		t.Errorf("SourceFPSMode = %q, want cfr", s.SourceFPSMode)
	}
	if s.SourceInputArgs == "" {
		t.Error("SourceInputArgs should be set from profile")
	}
}

func TestResolveSessionStrategy_SkipProbe(t *testing.T) {
	in := StrategyInput{
		StreamURL: "http://provider.com/live/123.ts",
		StreamID:  "test-skip",
		SourceProfile: &models.SourceProfile{
			ProbeMode:  "none",
			VideoCodec: "h264",
			AudioCodec: "aac",
		},
	}
	out := StrategyOutput{Delivery: "hls", Container: "mp4"}
	s := resolveSessionStrategy(in, out, "/tmp")
	if !s.SkipProbe {
		t.Error("SkipProbe should be true for probe_mode=none")
	}

	in.SourceProfile.ProbeMode = "declared"
	s = resolveSessionStrategy(in, out, "/tmp")
	if !s.SkipProbe {
		t.Error("SkipProbe should be true for probe_mode=declared")
	}

	in.SourceProfile.ProbeMode = "auto"
	s = resolveSessionStrategy(in, out, "/tmp")
	if s.SkipProbe {
		t.Error("SkipProbe should be false for probe_mode=auto")
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
