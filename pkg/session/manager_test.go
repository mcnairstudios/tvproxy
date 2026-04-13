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
