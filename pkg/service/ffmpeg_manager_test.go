package service

import (
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"

	"github.com/gavinmcnair/tvproxy/pkg/config"
	"github.com/gavinmcnair/tvproxy/pkg/ffmpeg"
)

func newTestFFmpegManager(t *testing.T) *FFmpegManager {
	t.Helper()
	cfg := &config.Config{UserAgent: "test"}
	log := zerolog.Nop()
	return NewFFmpegManager(cfg, nil, log)
}

func TestParseProgress_NormalValues(t *testing.T) {
	mgr := newTestFFmpegManager(t)
	proc := &ManagedProcess{ID: "test-proc"}

	input := "out_time_us=5000000\nout_time_us=10000000\n"
	mgr.parseProgress(proc, strings.NewReader(input))

	proc.mu.Lock()
	assert.Equal(t, 10.0, proc.BufferedSecs)
	proc.mu.Unlock()
}

func TestParseProgress_CapsAt48Hours(t *testing.T) {
	mgr := newTestFFmpegManager(t)
	proc := &ManagedProcess{ID: "test-proc"}

	input := "out_time_us=200000000000\n"
	mgr.parseProgress(proc, strings.NewReader(input))

	proc.mu.Lock()
	assert.Equal(t, maxBufferedSecs, proc.BufferedSecs, "should be capped at 48 hours")
	proc.mu.Unlock()
}

func TestParseProgress_IgnoresNegativeValues(t *testing.T) {
	mgr := newTestFFmpegManager(t)
	proc := &ManagedProcess{ID: "test-proc"}

	input := "out_time_us=-1000000\n"
	mgr.parseProgress(proc, strings.NewReader(input))

	proc.mu.Lock()
	assert.Equal(t, 0.0, proc.BufferedSecs, "negative values should be ignored")
	proc.mu.Unlock()
}

func TestParseProgress_IgnoresInvalidLines(t *testing.T) {
	mgr := newTestFFmpegManager(t)
	proc := &ManagedProcess{ID: "test-proc"}

	input := "out_time_us=not_a_number\nframe=100\nfps=25.0\nbitrate=1000kbit/s\nspeed=1.0x\n"
	mgr.parseProgress(proc, strings.NewReader(input))

	proc.mu.Lock()
	assert.Equal(t, 0.0, proc.BufferedSecs)
	proc.mu.Unlock()
}

func TestGetBufferedSecs_UnknownProcess(t *testing.T) {
	mgr := newTestFFmpegManager(t)
	assert.Equal(t, 0.0, mgr.GetBufferedSecs("nonexistent"))
}

func TestIsReady_UnknownProcess(t *testing.T) {
	mgr := newTestFFmpegManager(t)
	assert.True(t, mgr.IsReady("nonexistent"), "unknown process should be considered ready")
}

func TestGetError_UnknownProcess(t *testing.T) {
	mgr := newTestFFmpegManager(t)
	assert.Nil(t, mgr.GetError("nonexistent"))
}

func TestRemove_Process(t *testing.T) {
	mgr := newTestFFmpegManager(t)

	proc := &ManagedProcess{ID: "test-proc", Ready: make(chan struct{})}
	mgr.mu.Lock()
	mgr.processes["test-proc"] = proc
	mgr.mu.Unlock()

	assert.False(t, mgr.IsReady("test-proc"))

	mgr.Remove("test-proc")

	assert.True(t, mgr.IsReady("test-proc"), "removed process should appear ready")
}

func TestSanitizeFilename(t *testing.T) {
	ts := time.Date(2025, 1, 15, 14, 30, 0, 0, time.UTC)
	suffix := "_20250115_1430"

	tests := []struct {
		title    string
		expected string
	}{
		{"My Recording", "My_Recording" + suffix},
		{"", "recording" + suffix},
		{"a/b\\c:d", "a_b_c_d" + suffix},
		{"   ", "recording" + suffix},
	}

	for _, tt := range tests {
		name := ffmpeg.SanitizeFilename(tt.title, ts)
		assert.Equal(t, tt.expected, name, "for title: %q", tt.title)
	}
}

func TestSanitizeFilename_Truncation(t *testing.T) {
	ts := time.Date(2025, 1, 15, 14, 30, 0, 0, time.UTC)
	longTitle := strings.Repeat("a", 100)
	name := ffmpeg.SanitizeFilename(longTitle, ts)
	assert.LessOrEqual(t, len(name), 74, "should truncate long names")
}
