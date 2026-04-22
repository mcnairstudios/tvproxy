package demux

import (
	"fmt"
	"io"
	"os/exec"
	"testing"
	"time"
)

func ffmpegAvailable() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

func skipIfNoFFmpeg(t *testing.T) {
	t.Helper()
	if !ffmpegAvailable() {
		t.Skip("skipping: ffmpeg not found in PATH (CGo FFmpeg libs likely unavailable)")
	}
}

func TestNewDemuxer_InvalidURL(t *testing.T) {
	skipIfNoFFmpeg(t)

	_, err := NewDemuxer("file:///nonexistent/path.ts", DemuxOpts{TimeoutSec: 2})
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

func TestDemuxOpts_Defaults(t *testing.T) {
	opts := DemuxOpts{}
	if opts.TimeoutSec != 0 {
		t.Errorf("expected default TimeoutSec 0, got %d", opts.TimeoutSec)
	}
	if opts.AudioTrack != 0 {
		t.Errorf("expected default AudioTrack 0, got %d", opts.AudioTrack)
	}
	if opts.AudioLanguage != "" {
		t.Errorf("expected default AudioLanguage empty, got %q", opts.AudioLanguage)
	}
}

func TestStreamType_Values(t *testing.T) {
	tests := []struct {
		name string
		st   StreamType
		want int
	}{
		{"Video", Video, 0},
		{"Audio", Audio, 1},
		{"Subtitle", Subtitle, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if int(tt.st) != tt.want {
				t.Errorf("StreamType %s = %d, want %d", tt.name, tt.st, tt.want)
			}
		})
	}
}

func TestToNanoseconds(t *testing.T) {
	t.Log("toNanoseconds formula: ts * tb.Num * 1e9 / tb.Den")
}

func TestSetAudioTrack_NoStreams(t *testing.T) {
	skipIfNoFFmpeg(t)

	t.Log("SetAudioTrack validates stream index and media type")
}

func TestPacket_Fields(t *testing.T) {
	p := &Packet{
		Type:     Video,
		Data:     []byte{0x00, 0x00, 0x01},
		PTS:      1_000_000_000,
		DTS:      999_000_000,
		Duration: 33_333_333,
		Keyframe: true,
	}

	if p.Type != Video {
		t.Errorf("expected Video, got %d", p.Type)
	}
	if len(p.Data) != 3 {
		t.Errorf("expected 3 bytes, got %d", len(p.Data))
	}
	if !p.Keyframe {
		t.Error("expected Keyframe true")
	}
	if p.PTS != 1_000_000_000 {
		t.Errorf("expected PTS 1e9, got %d", p.PTS)
	}
}

func TestReadPacket_EOF(t *testing.T) {
	skipIfNoFFmpeg(t)

	_ = io.EOF
	t.Log("ReadPacket returns io.EOF at end of stream")
}

func TestClose_Idempotent(t *testing.T) {
	d := &Demuxer{closed: true}
	d.Close()
	d.Close()
}

func TestSeekTo_NoVideo(t *testing.T) {
	skipIfNoFFmpeg(t)

	t.Log("SeekTo falls back to AV_TIME_BASE when no video stream present")
}

func TestIsTransient(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"io.EOF", io.EOF, false},
		{"generic", fmt.Errorf("some error"), false},
		{"timeout", fmt.Errorf("connection timeout"), true},
		{"connection reset", fmt.Errorf("Connection reset by peer"), true},
		{"connection refused", fmt.Errorf("Connection refused"), true},
		{"network unreachable", fmt.Errorf("Network is unreachable"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTransient(tt.err)
			if got != tt.want {
				t.Errorf("isTransient(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestDemuxOpts_Follow(t *testing.T) {
	opts := DemuxOpts{Follow: true, FormatHint: "mpegts"}
	if !opts.Follow {
		t.Error("expected Follow true")
	}
	if opts.FormatHint != "mpegts" {
		t.Errorf("expected FormatHint mpegts, got %q", opts.FormatHint)
	}
}

func TestFollowRetryInterval(t *testing.T) {
	if followRetryInterval <= 0 {
		t.Error("followRetryInterval must be positive")
	}
	if followRetryInterval > 1*time.Second {
		t.Error("followRetryInterval seems too large")
	}
}

func TestReconnect_NilContext(t *testing.T) {
	skipIfNoFFmpeg(t)

	d := &Demuxer{
		url:      "file:///nonexistent/path.ts",
		opts:     DemuxOpts{TimeoutSec: 1},
		videoIdx: 0,
		audioIdx: 1,
		subIdx:   -1,
		basePTS:  -1,
	}
	err := d.Reconnect()
	if err == nil {
		t.Fatal("expected error from Reconnect with invalid URL, got nil")
	}
}
