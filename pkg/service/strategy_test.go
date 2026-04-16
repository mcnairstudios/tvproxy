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
		VideoCodec: "copy",
		AudioCodec: "aac",
		Container:  "mp4",
	}

	s := resolveSessionStrategy(in, out)

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
		VideoCodec: "copy",
		AudioCodec: "aac",
		Container:  "mp4",
	}

	s := resolveSessionStrategy(in, out)

	if s.Category != CategoryVODRemote {
		t.Errorf("category = %d, want CategoryVODRemote", s.Category)
	}
	if s.MetadataOnly {
		t.Error("MetadataOnly should be false for VOD (session manager starts transcoder)")
	}
}

func TestResolveSessionStrategy_VODLocal(t *testing.T) {
	in := StrategyInput{
		StreamURL: "http://192.168.1.149:8090/stream/movies/test.mkv",
		VODType:   "movie",
		StreamID:  "test-local-id",
	}
	out := StrategyOutput{
		VideoCodec: "copy",
		AudioCodec: "aac",
		Container:  "mp4",
	}

	s := resolveSessionStrategy(in, out)

	if s.Category != CategoryVODLocal {
		t.Errorf("category = %d, want CategoryVODLocal", s.Category)
	}
	if s.MetadataOnly {
		t.Error("MetadataOnly should be false for VOD (session manager starts transcoder)")
	}
}

func TestResolveSessionStrategy_LiveWithTranscode(t *testing.T) {
	in := StrategyInput{
		StreamURL: "http://provider.com/live/user/pass/123.ts",
		VODType:   "",
		StreamID:  "test-transcode-id",
	}
	out := StrategyOutput{
		VideoCodec: "av1",
		AudioCodec: "aac",
		HWAccel:    "qsv",
		Container:  "mp4",
	}

	s := resolveSessionStrategy(in, out)

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
		StreamURL:    "http://provider.com/live/123.ts",
		VODType:      "",
		StreamID:     "test-sp",
		StreamVCodec: "mpeg2video",
		StreamACodec: "mp2",
		SourceProfile: &models.SourceProfile{
			Deinterlace:    true,
			AudioDelayMs:   200,
			VideoQueueMs:   15000,
		},
	}
	out := StrategyOutput{
		VideoCodec: "default",
		AudioCodec: "default",
		Container:  "mp4",
	}

	s := resolveSessionStrategy(in, out)

	if s.VideoCodec != "copy" {
		t.Errorf("VideoCodec = %q, want copy (mpeg2 source, default client = copy)", s.VideoCodec)
	}
	if s.AudioCodec != "aac" {
		t.Errorf("AudioCodec = %q, want aac (mp2 source needs transcode for mp4)", s.AudioCodec)
	}
}

func TestResolveSessionStrategy_SkipProbe(t *testing.T) {
	in := StrategyInput{
		StreamURL: "http://provider.com/live/123.ts",
		StreamID:  "test-skip",
	}
	out := StrategyOutput{Container: "mp4"}
	s := resolveSessionStrategy(in, out)
	if s.SkipProbe {
		t.Error("SkipProbe should be false for probe_mode=auto")
	}
}

func TestLiveStrategy_ForcesAudioTranscode_IPTV(t *testing.T) {
	in := StrategyInput{
		StreamURL:    "http://provider.com/live/123.ts",
		StreamACodec: "ac3",
	}
	out := StrategyOutput{VideoCodec: "copy", AudioCodec: "copy", Container: "mp4"}
	s := resolveSessionStrategy(in, out)
	if s.AudioCodec != "aac" {
		t.Errorf("live IPTV with ac3 source: AudioCodec = %q, want aac (MSE can't play ac3)", s.AudioCodec)
	}
}

func TestLiveStrategy_ForcesAudioTranscode_SatIP(t *testing.T) {
	in := StrategyInput{
		StreamURL:    "rtsp://192.168.1.148/?freq=545",
		SatIPSource:  true,
		StreamACodec: "aac_latm",
	}
	out := StrategyOutput{VideoCodec: "h265", AudioCodec: "copy", Container: "mp4"}
	s := resolveSessionStrategy(in, out)
	if s.AudioCodec != "aac" {
		t.Errorf("SAT>IP with aac_latm: AudioCodec = %q, want aac (LATM breaks MSE)", s.AudioCodec)
	}
}

func TestLiveStrategy_ForcesAudioTranscode_SatIP_MP2(t *testing.T) {
	in := StrategyInput{
		StreamURL:    "rtsp://192.168.1.148/?freq=545",
		SatIPSource:  true,
		StreamACodec: "mp2",
	}
	out := StrategyOutput{VideoCodec: "h265", AudioCodec: "default", Container: "mp4"}
	s := resolveSessionStrategy(in, out)
	if s.AudioCodec != "aac" {
		t.Errorf("SAT>IP with mp2: AudioCodec = %q, want aac", s.AudioCodec)
	}
}

func TestLiveStrategy_AlwaysTranscodesAudio(t *testing.T) {
	in := StrategyInput{
		StreamURL:    "rtsp://192.168.1.148/?freq=545",
		SatIPSource:  true,
		StreamACodec: "aac",
	}
	out := StrategyOutput{VideoCodec: "h265", AudioCodec: "copy", Container: "mp4"}
	s := resolveSessionStrategy(in, out)
	if s.AudioCodec != "aac" {
		t.Errorf("live SAT>IP even with aac source: AudioCodec = %q, want aac (all live forces transcode)", s.AudioCodec)
	}
}

func TestVODRemoteStrategy_TranscodesNonAAC(t *testing.T) {
	in := StrategyInput{
		StreamURL:    "http://provider.com/movie/user/pass/456.avi",
		VODType:      "movie",
		StreamACodec: "ac3",
	}
	out := StrategyOutput{VideoCodec: "copy", AudioCodec: "copy", Container: "mp4"}
	s := resolveSessionStrategy(in, out)
	if s.AudioCodec != "aac" {
		t.Errorf("VOD with ac3: AudioCodec = %q, want aac", s.AudioCodec)
	}
}

func TestVODRemoteStrategy_CopiesAAC(t *testing.T) {
	in := StrategyInput{
		StreamURL:    "http://provider.com/movie/user/pass/456.mp4",
		VODType:      "movie",
		StreamACodec: "aac",
	}
	out := StrategyOutput{VideoCodec: "copy", AudioCodec: "copy", Container: "mp4"}
	s := resolveSessionStrategy(in, out)
	if s.AudioCodec != "copy" {
		t.Errorf("VOD with aac source and copy: AudioCodec = %q, want copy", s.AudioCodec)
	}
}

func TestVODLocalStrategy_TranscodesDTS(t *testing.T) {
	in := StrategyInput{
		StreamURL:    "http://192.168.1.149:8090/stream/movies/test.mkv",
		VODType:      "movie",
		StreamACodec: "dts",
	}
	out := StrategyOutput{VideoCodec: "copy", AudioCodec: "default", Container: "mp4"}
	s := resolveSessionStrategy(in, out)
	if s.AudioCodec != "aac" {
		t.Errorf("local VOD with dts: AudioCodec = %q, want aac", s.AudioCodec)
	}
}

func TestHDHR_ClassifiesAsIPTV(t *testing.T) {
	in := StrategyInput{
		StreamURL: "http://192.168.1.148:5004/auto/v100",
	}
	got := classifyStream(in)
	if got != CategoryLiveIPTV {
		t.Errorf("HDHR stream classified as %d, want CategoryLiveIPTV", got)
	}
}

func TestHDHR_LiveWithH264Copy(t *testing.T) {
	in := StrategyInput{
		StreamURL:    "http://192.168.1.148:5004/auto/v100",
		StreamVCodec: "h264",
		StreamACodec: "aac_latm",
	}
	out := StrategyOutput{VideoCodec: "copy", AudioCodec: "copy", Container: "mp4"}
	s := resolveSessionStrategy(in, out)
	if s.VideoCodec != "copy" {
		t.Errorf("HDHR h264 with copy: VideoCodec = %q, want copy", s.VideoCodec)
	}
	if s.AudioCodec != "aac" {
		t.Errorf("HDHR with aac_latm: AudioCodec = %q, want aac (live forces transcode)", s.AudioCodec)
	}
}

func TestSatIP_H264toAV1(t *testing.T) {
	in := StrategyInput{
		StreamURL:    "rtsp://192.168.1.148/?freq=545",
		SatIPSource:  true,
		StreamVCodec: "h264",
		StreamACodec: "aac_latm",
	}
	out := StrategyOutput{VideoCodec: "av1", AudioCodec: "aac", HWAccel: "vaapi", Container: "mp4"}
	s := resolveSessionStrategy(in, out)
	if s.VideoCodec != "av1" {
		t.Errorf("SAT>IP h264→av1: VideoCodec = %q, want av1", s.VideoCodec)
	}
	if s.HWAccel != "vaapi" {
		t.Errorf("SAT>IP: HWAccel = %q, want vaapi", s.HWAccel)
	}
	if s.AudioCodec != "aac" {
		t.Errorf("SAT>IP: AudioCodec = %q, want aac", s.AudioCodec)
	}
}

func TestSatIP_MPEG2toH265(t *testing.T) {
	in := StrategyInput{
		StreamURL:    "rtsp://192.168.1.148/?freq=545",
		SatIPSource:  true,
		StreamVCodec: "mpeg2video",
		StreamACodec: "mp2",
	}
	out := StrategyOutput{VideoCodec: "h265", AudioCodec: "aac", HWAccel: "vaapi", Container: "mp4"}
	s := resolveSessionStrategy(in, out)
	if s.VideoCodec != "h265" {
		t.Errorf("SAT>IP mpeg2→h265: VideoCodec = %q, want h265", s.VideoCodec)
	}
	if s.AudioCodec != "aac" {
		t.Errorf("SAT>IP mp2 audio: AudioCodec = %q, want aac", s.AudioCodec)
	}
}

func TestTVProxyStreams_H265Copy(t *testing.T) {
	in := StrategyInput{
		StreamURL:    "http://192.168.1.149:8090/stream/movies/test.mp4",
		VODType:      "movie",
		StreamVCodec: "h265",
		StreamACodec: "aac",
	}
	out := StrategyOutput{VideoCodec: "copy", AudioCodec: "copy", Container: "mp4"}
	s := resolveSessionStrategy(in, out)
	if s.Category != CategoryVODLocal {
		t.Errorf("category = %d, want CategoryVODLocal", s.Category)
	}
	if s.VideoCodec != "copy" {
		t.Errorf("VideoCodec = %q, want copy (h265 matches)", s.VideoCodec)
	}
	if s.AudioCodec != "copy" {
		t.Errorf("AudioCodec = %q, want copy (aac source)", s.AudioCodec)
	}
}

func TestTVProxyStreams_H265toAV1(t *testing.T) {
	in := StrategyInput{
		StreamURL:    "http://192.168.1.149:8090/stream/movies/test.mkv",
		VODType:      "movie",
		StreamVCodec: "h265",
		StreamACodec: "aac",
	}
	out := StrategyOutput{VideoCodec: "av1", AudioCodec: "aac", HWAccel: "vaapi", Container: "mp4"}
	s := resolveSessionStrategy(in, out)
	if s.VideoCodec != "av1" {
		t.Errorf("tvproxy-streams h265→av1: VideoCodec = %q, want av1", s.VideoCodec)
	}
}

func TestIPTV_VOD_Movie(t *testing.T) {
	in := StrategyInput{
		StreamURL:    "http://provider.com/movie/user/pass/456.avi",
		VODType:      "movie",
		StreamVCodec: "h264",
		StreamACodec: "aac",
	}
	out := StrategyOutput{VideoCodec: "h265", AudioCodec: "aac", Container: "mp4"}
	s := resolveSessionStrategy(in, out)
	if s.Category != CategoryVODRemote {
		t.Errorf("category = %d, want CategoryVODRemote", s.Category)
	}
	if s.VideoCodec != "h265" {
		t.Errorf("IPTV VOD h264→h265: VideoCodec = %q, want h265", s.VideoCodec)
	}
}

func TestIPTV_LiveChannel(t *testing.T) {
	in := StrategyInput{
		StreamURL:    "http://provider.com/live/user/pass/123.ts",
		StreamVCodec: "h264",
		StreamACodec: "aac",
	}
	out := StrategyOutput{VideoCodec: "h265", AudioCodec: "aac", Container: "mp4"}
	s := resolveSessionStrategy(in, out)
	if s.Category != CategoryLiveIPTV {
		t.Errorf("category = %d, want CategoryLiveIPTV", s.Category)
	}
	if s.VideoCodec != "h265" {
		t.Errorf("IPTV live h264→h265: VideoCodec = %q, want h265", s.VideoCodec)
	}
}

func TestLiveStrategy_AV1ForcesMp4(t *testing.T) {
	in := StrategyInput{
		StreamURL:    "http://provider.com/live/123.ts",
		StreamVCodec: "h264",
		StreamACodec: "aac",
	}
	out := StrategyOutput{VideoCodec: "av1", AudioCodec: "aac", Container: "mpegts"}
	s := resolveSessionStrategy(in, out)
	if s.VideoCodec != "av1" {
		t.Errorf("VideoCodec = %q, want av1", s.VideoCodec)
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
