package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gavinmcnair/tvproxy/pkg/config"
)

func testManager(t *testing.T) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	log := zerolog.New(os.Stderr).Level(zerolog.Disabled)
	cfg := &config.Config{}
	m := NewManager(cfg, nil, nil, nil, log)
	t.Cleanup(func() { m.Shutdown() })
	return m, dir
}

func TestManager_GetReturnsNilForUnknown(t *testing.T) {
	m, _ := testManager(t)
	assert.Nil(t, m.Get("nonexistent"))
}

func TestManager_ConsumerCountZeroForUnknown(t *testing.T) {
	m, _ := testManager(t)
	assert.Equal(t, 0, m.ConsumerCount("nonexistent"))
}

func TestManager_IsDoneTrueForUnknown(t *testing.T) {
	m, _ := testManager(t)
	assert.True(t, m.IsDone("nonexistent"))
}

func TestManager_TailFileFailsNoSession(t *testing.T) {
	m, _ := testManager(t)
	_, err := m.TailFile(context.Background(), "nonexistent")
	assert.Error(t, err)
}

func TestSession_ConsumerLifecycle(t *testing.T) {
	s := &Session{
		ID:        "test",
		consumers: make(map[string]*Consumer),
		done:      make(chan struct{}),
	}

	c1 := &Consumer{ID: "c1", Type: "viewer", CreatedAt: time.Now()}
	c2 := &Consumer{ID: "c2", Type: "recording", CreatedAt: time.Now()}

	s.addConsumer(c1)
	assert.Equal(t, 1, s.consumerCount())

	s.addConsumer(c2)
	assert.Equal(t, 2, s.consumerCount())
	assert.True(t, s.HasRecordingConsumer())
	assert.Equal(t, "c2", s.RecordingConsumerID())

	remaining := s.removeConsumer("c1")
	assert.Equal(t, 1, remaining)
	assert.True(t, s.HasRecordingConsumer())

	remaining = s.removeConsumer("c2")
	assert.Equal(t, 0, remaining)
	assert.False(t, s.HasRecordingConsumer())
}

func TestSession_DoneState(t *testing.T) {
	s := &Session{done: make(chan struct{})}
	assert.False(t, s.isDone())
	s.markDone()
	assert.True(t, s.isDone())
	s.markDone()
}

func TestSession_ErrorState(t *testing.T) {
	s := &Session{done: make(chan struct{})}
	assert.Nil(t, s.getError())
	s.setError(assert.AnError)
	assert.Equal(t, assert.AnError, s.getError())
}

func TestSession_BufferedSecs(t *testing.T) {
	s := &Session{done: make(chan struct{})}
	assert.Equal(t, 0.0, s.getBuffered())
	s.setBuffered(42.5)
	assert.Equal(t, 42.5, s.getBuffered())
}

func TestTailReader_ReadsAvailableData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.mp4")
	require.NoError(t, os.WriteFile(path, []byte("hello"), 0644))

	s := &Session{done: make(chan struct{})}
	s.markDone()

	f, err := os.Open(path)
	require.NoError(t, err)

	ctx := context.Background()
	reader := newTailReader(ctx, f, s)
	defer reader.Close()

	buf := make([]byte, 10)
	n, err := reader.Read(buf)
	assert.Equal(t, 5, n)
	assert.Equal(t, "hello", string(buf[:n]))
}

func TestManager_Shutdown(t *testing.T) {
	m, _ := testManager(t)
	m.Shutdown()
	assert.Equal(t, 0, len(m.sessions))
}

func TestSession_RecordFlag(t *testing.T) {
	s := &Session{
		done:      make(chan struct{}),
		consumers: make(map[string]*Consumer),
	}
	assert.False(t, s.IsRecorded())

	s.Record()
	assert.True(t, s.IsRecorded())
	assert.True(t, s.Recorded)
}

func TestSession_PathHelpers(t *testing.T) {
	s := &Session{
		OutputDir: "/record/stream1/uuid1",
		done:      make(chan struct{}),
	}
	assert.Equal(t, "/record/stream1/uuid1/source.ts", s.SourceTSPath())
	assert.Equal(t, "/record/stream1/uuid1/probe.pb", s.ProbePBPath())
}

func TestSession_RecordPreservesOnCleanup(t *testing.T) {
	dir := t.TempDir()
	sessionDir := filepath.Join(dir, "session1")
	require.NoError(t, os.MkdirAll(sessionDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(sessionDir, "source.ts"), []byte("data"), 0644))

	log := zerolog.New(os.Stderr).Level(zerolog.Disabled)
	cfg := &config.Config{}
	m := NewManager(cfg, nil, nil, nil, log)
	defer m.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	s := &Session{
		ID:        "test-rec",
		ChannelID: "ch1",
		TempDir:   sessionDir,
		OutputDir: sessionDir,
		Recorded:  true,
		consumers: make(map[string]*Consumer),
		cancel:    cancel,
		done:      make(chan struct{}),
	}
	s.markDone()

	m.mu.Lock()
	m.sessions["ch1"] = s
	m.mu.Unlock()

	_ = ctx

	m.stopAndCleanup("ch1", s)

	_, err := os.Stat(sessionDir)
	assert.NoError(t, err, "recorded session directory should be preserved")
	_, err = os.Stat(filepath.Join(sessionDir, "source.ts"))
	assert.NoError(t, err, "source.ts should be preserved")
}

func TestSession_UnrecordedCleanup(t *testing.T) {
	dir := t.TempDir()
	sessionDir := filepath.Join(dir, "session2")
	require.NoError(t, os.MkdirAll(sessionDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(sessionDir, "source.ts"), []byte("data"), 0644))

	log := zerolog.New(os.Stderr).Level(zerolog.Disabled)
	cfg := &config.Config{}
	m := NewManager(cfg, nil, nil, nil, log)
	defer m.Shutdown()

	_, cancel := context.WithCancel(context.Background())
	s := &Session{
		ID:        "test-norec",
		ChannelID: "ch2",
		TempDir:   sessionDir,
		OutputDir: sessionDir,
		Recorded:  false,
		consumers: make(map[string]*Consumer),
		cancel:    cancel,
		done:      make(chan struct{}),
	}
	s.markDone()

	m.mu.Lock()
	m.sessions["ch2"] = s
	m.mu.Unlock()

	m.stopAndCleanup("ch2", s)

	_, err := os.Stat(sessionDir)
	assert.True(t, os.IsNotExist(err), "unrecorded session directory should be deleted")
}

func TestManager_AddRecordingConsumerSetsFlag(t *testing.T) {
	log := zerolog.New(os.Stderr).Level(zerolog.Disabled)
	cfg := &config.Config{}
	m := NewManager(cfg, nil, nil, nil, log)

	_, cancel := context.WithCancel(context.Background())
	s := &Session{
		ID:        "test-addrec",
		ChannelID: "ch3",
		consumers: make(map[string]*Consumer),
		cancel:    cancel,
		done:      make(chan struct{}),
	}
	s.markDone()

	m.mu.Lock()
	m.sessions["ch3"] = s
	m.mu.Unlock()

	consumerID := m.AddRecordingConsumer("ch3")
	assert.NotEmpty(t, consumerID)
	assert.True(t, s.IsRecorded(), "AddRecordingConsumer should set Recorded flag")
	assert.True(t, s.HasRecordingConsumer())

	m.Shutdown()
}

func TestResolveNeedsTranscode(t *testing.T) {
	tests := []struct {
		name     string
		outCodec string
		srcCodec string
		want     bool
	}{
		{"mpeg2 source, h264 output", "h264", "mpeg2video", true},
		{"h264 source, copy output", "copy", "h264", false},
		{"h265 source, copy output", "copy", "h265", false},
		{"empty output (default)", "", "h264", false},
		{"default output", "default", "h265", false},
		{"h264 source, h264 output (same)", "h264", "h264", false},
		{"h265 source, h264 output (different)", "h264", "h265", true},
		{"unknown source, h264 output (safe default)", "h264", "", true},
		{"unknown source, copy output", "copy", "", false},
		{"unknown source, empty output", "", "", false},
		{"av1 source, h265 output", "h265", "av1", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveNeedsTranscode(tt.outCodec, tt.srcCodec)
			assert.Equal(t, tt.want, got, "resolveNeedsTranscode(%q, %q)", tt.outCodec, tt.srcCodec)
		})
	}
}

func TestFriendlyGstError(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Could not multiplex stream.", "Stream encoding error — audio/video sync issue"},
		{"not-negotiated error from element", "Stream format not supported"},
		{"Service Unavailable", "Source stream unavailable (503)"},
		{"Server returned 404 Not Found", "Source stream not found (404)"},
		{"This file contains no valid or supported streams.", "Source contains no playable streams"},
		{"Internal data stream error.", "Internal pipeline error"},
		{"some unknown error", "some unknown error"},
	}
	for _, tt := range tests {
		got := friendlyGstError(tt.in)
		assert.Equal(t, tt.want, got, "for input: %s", tt.in)
	}
}
