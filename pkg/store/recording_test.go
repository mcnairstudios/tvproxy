package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testRecordingStore(t *testing.T) (*RecordingStoreImpl, string) {
	t.Helper()
	dir := t.TempDir()
	log := zerolog.New(os.Stderr).Level(zerolog.Disabled)
	return NewRecordingStore(dir, log), dir
}

func TestActiveDir(t *testing.T) {
	s, dir := testRecordingStore(t)
	assert.Equal(t, filepath.Join(dir, "stream1", "active"), s.ActiveDir("stream1"))
}

func TestRecordedDir(t *testing.T) {
	s, dir := testRecordingStore(t)
	assert.Equal(t, filepath.Join(dir, "stream1", "recorded"), s.RecordedDir("stream1"))
}

func TestWriteAndReadSessionMeta(t *testing.T) {
	s, _ := testRecordingStore(t)

	meta := SessionMeta{
		Status:      SessionActive,
		SessionID:   "sess-1",
		StreamID:    "stream-1",
		StreamName:  "BBC One",
		ChannelID:   "chan-1",
		ChannelName: "BBC One HD",
		FileName:    "bbc_one.mp4",
		StartedAt:   time.Now().Truncate(time.Second),
	}

	require.NoError(t, s.WriteSessionMeta("stream-1", meta))

	got, err := s.ReadSessionMeta("stream-1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, SessionActive, got.Status)
	assert.Equal(t, "sess-1", got.SessionID)
	assert.Equal(t, "BBC One", got.StreamName)
	assert.Equal(t, "bbc_one.mp4", got.FileName)
}

func TestReadSessionMeta_NotFound(t *testing.T) {
	s, _ := testRecordingStore(t)
	got, err := s.ReadSessionMeta("nonexistent")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestRemoveActiveSession(t *testing.T) {
	s, _ := testRecordingStore(t)

	meta := SessionMeta{Status: SessionActive, SessionID: "s1", StreamID: "stream-1", FileName: "test.mp4"}
	require.NoError(t, s.WriteSessionMeta("stream-1", meta))

	mp4Path := filepath.Join(s.ActiveDir("stream-1"), "test.mp4")
	require.NoError(t, os.WriteFile(mp4Path, []byte("video"), 0644))

	require.NoError(t, s.RemoveActiveSession("stream-1"))

	_, err := os.Stat(s.ActiveDir("stream-1"))
	assert.True(t, os.IsNotExist(err))
}

func TestCompleteRecording(t *testing.T) {
	s, _ := testRecordingStore(t)

	activeDir := s.ActiveDir("stream-1")
	require.NoError(t, os.MkdirAll(activeDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(activeDir, "bbc_one.mp4"), []byte("video data"), 0644))

	meta := SessionMeta{
		Status:       SessionRecording,
		SessionID:    "sess-1",
		StreamID:     "stream-1",
		StreamName:   "BBC One",
		ChannelID:    "chan-1",
		ChannelName:  "BBC One HD",
		FileName:     "bbc_one.mp4",
		StartedAt:    time.Now().Add(-30 * time.Minute),
		ProgramTitle: "News at Ten",
		UserID:       "user-1",
		StoppedAt:    time.Now(),
	}

	filename, err := s.CompleteRecording("stream-1", meta)
	require.NoError(t, err)
	assert.Contains(t, filename, "News_at_Ten")
	assert.True(t, filepath.Ext(filename) == ".mp4")

	mp4Path := filepath.Join(s.RecordedDir("stream-1"), filename)
	data, err := os.ReadFile(mp4Path)
	require.NoError(t, err)
	assert.Equal(t, "video data", string(data))

	jsonName := filename[:len(filename)-4] + ".json"
	jsonPath := filepath.Join(s.RecordedDir("stream-1"), jsonName)
	_, err = os.Stat(jsonPath)
	assert.NoError(t, err)

	_, err = os.Stat(activeDir)
	assert.True(t, os.IsNotExist(err))
}

func TestCompleteRecording_FilenameCollision(t *testing.T) {
	s, _ := testRecordingStore(t)

	activeDir := s.ActiveDir("stream-1")
	recordedDir := s.RecordedDir("stream-1")
	require.NoError(t, os.MkdirAll(activeDir, 0755))
	require.NoError(t, os.MkdirAll(recordedDir, 0755))

	now := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	meta := SessionMeta{
		Status:       SessionRecording,
		StreamID:     "stream-1",
		FileName:     "source.mp4",
		ProgramTitle: "Test Show",
		StoppedAt:    now,
	}

	require.NoError(t, os.WriteFile(filepath.Join(activeDir, "source.mp4"), []byte("first"), 0644))
	existingName := "Test_Show_20260402_1200.mp4"
	require.NoError(t, os.WriteFile(filepath.Join(recordedDir, existingName), []byte("existing"), 0644))

	filename, err := s.CompleteRecording("stream-1", meta)
	require.NoError(t, err)
	assert.Contains(t, filename, "_1.mp4")
}

func TestListActiveRecordings(t *testing.T) {
	s, _ := testRecordingStore(t)

	activeMeta := SessionMeta{Status: SessionRecording, StreamID: "stream-1", ProgramTitle: "News"}
	require.NoError(t, s.WriteSessionMeta("stream-1", activeMeta))

	viewingMeta := SessionMeta{Status: SessionActive, StreamID: "stream-2"}
	require.NoError(t, s.WriteSessionMeta("stream-2", viewingMeta))

	recordings, err := s.ListActiveRecordings()
	require.NoError(t, err)
	assert.Len(t, recordings, 1)
	assert.Equal(t, "News", recordings[0].ProgramTitle)
}

func TestList_RecordedDir(t *testing.T) {
	s, dir := testRecordingStore(t)

	recordedDir := filepath.Join(dir, "stream-1", "recorded")
	require.NoError(t, os.MkdirAll(recordedDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(recordedDir, "show.mp4"), []byte("video"), 0644))

	completedMeta := SessionMeta{
		Status:       SessionCompleted,
		StreamID:     "stream-1",
		ChannelName:  "BBC One",
		ProgramTitle: "Show",
		UserID:       "admin",
		StartedAt:    time.Now().Add(-time.Hour),
		StoppedAt:    time.Now(),
	}
	require.NoError(t, os.WriteFile(filepath.Join(recordedDir, "show.json"), mustMarshal(t, completedMeta), 0644))

	entries, err := s.List("admin", true)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
	assert.Equal(t, "show.mp4", entries[0].Filename)
	assert.Equal(t, "stream-1", entries[0].StreamID)
	require.NotNil(t, entries[0].Meta)
	assert.Equal(t, "Show", entries[0].Meta.ProgramTitle)
}

func TestList_LegacyRecordingDir(t *testing.T) {
	s, dir := testRecordingStore(t)

	legacyDir := filepath.Join(dir, "stream-2", "recording")
	require.NoError(t, os.MkdirAll(legacyDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(legacyDir, "old.mp4"), []byte("old video"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(legacyDir, "old.json"), []byte(`{"stream_id":"stream-2","program_title":"Legacy Show","user_id":"admin"}`), 0644))

	entries, err := s.List("admin", true)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
	assert.Equal(t, "old.mp4", entries[0].Filename)
	require.NotNil(t, entries[0].Meta)
	assert.Equal(t, "Legacy Show", entries[0].Meta.ProgramTitle)
}

func TestDelete_RecordedDir(t *testing.T) {
	s, dir := testRecordingStore(t)

	recordedDir := filepath.Join(dir, "stream-1", "recorded")
	require.NoError(t, os.MkdirAll(recordedDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(recordedDir, "show.mp4"), []byte("video"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(recordedDir, "show.json"), []byte("{}"), 0644))

	require.NoError(t, s.Delete("stream-1", "show.mp4"))

	_, err := os.Stat(filepath.Join(recordedDir, "show.mp4"))
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(recordedDir, "show.json"))
	assert.True(t, os.IsNotExist(err))
}

func TestFilePath_RecordedDir(t *testing.T) {
	s, dir := testRecordingStore(t)

	recordedDir := filepath.Join(dir, "stream-1", "recorded")
	require.NoError(t, os.MkdirAll(recordedDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(recordedDir, "show.mp4"), []byte("video"), 0644))

	path, err := s.FilePath("stream-1", "show.mp4")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(recordedDir, "show.mp4"), path)
}

func TestFilePath_LegacyDir(t *testing.T) {
	s, dir := testRecordingStore(t)

	legacyDir := filepath.Join(dir, "stream-1", "recording")
	require.NoError(t, os.MkdirAll(legacyDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(legacyDir, "old.mp4"), []byte("video"), 0644))

	path, err := s.FilePath("stream-1", "old.mp4")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(legacyDir, "old.mp4"), path)
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return data
}
