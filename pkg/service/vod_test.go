package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gavinmcnair/tvproxy/pkg/config"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
	"github.com/gavinmcnair/tvproxy/pkg/store"
)

func newTestVODService(t *testing.T) (*VODService, *config.Config) {
	t.Helper()
	db := setupTestDB(t)

	cfg := &config.Config{
		VODTempDir:            t.TempDir(),
		RecordDir:             t.TempDir(),
		VODSessionTimeout:     5 * time.Minute,
		RecordDefaultDuration: 4 * time.Hour,
		RecordStopBuffer:      5 * time.Minute,
		UserAgent:             "TVProxy-Test",
	}

	channelRepo := repository.NewChannelRepository(db)
	streamStore := store.NewStreamStore(filepath.Join(t.TempDir(), "streams.gob"), zerolog.New(os.Stderr).Level(zerolog.Disabled))
	streamProfileRepo := repository.NewStreamProfileRepository(db)
	log := zerolog.New(os.Stderr).Level(zerolog.Disabled)
	ffmpegMgr := NewFFmpegManager(cfg, nil, log)
	svc := NewVODService(channelRepo, streamStore, streamProfileRepo, ffmpegMgr, nil, cfg, log)
	return svc, cfg
}

func injectTestSession(svc *VODService, id string) *VODSession {
	ctx, cancel := context.WithCancel(context.Background())
	session := &VODSession{
		ID:         id,
		ProcessID:  "fake-process-id",
		StreamURL:  "http://example.com/stream",
		FilePath:   "/tmp/fake-video.mp4",
		TempDir:    "/tmp/fake-dir",
		LastAccess: time.Now(),
		ctx:        ctx,
		cancel:     cancel,
	}
	svc.mu.Lock()
	svc.sessions[id] = session
	svc.mu.Unlock()
	return session
}

// --- Sentinel Error Tests ---

func TestVOD_SentinelErrors_AreDistinct(t *testing.T) {
	sentinels := []error{
		ErrSessionNotFound,
		ErrNotAuthorized,
		ErrNoActiveSegment,
		ErrInvalidFilename,
		ErrFileNotFound,
		ErrActiveSegmentExists,
		ErrStreamNotFound,
		ErrSegmentNotFound,
	}

	for i, a := range sentinels {
		for j, b := range sentinels {
			if i == j {
				assert.True(t, errors.Is(a, b), "sentinel should match itself: %v", a)
			} else {
				assert.False(t, errors.Is(a, b), "sentinels should be distinct: %v vs %v", a, b)
			}
		}
	}
}

func TestVOD_CreateSegment_SessionNotFound(t *testing.T) {
	svc, _ := newTestVODService(t)

	_, err := svc.CreateSegment("nonexistent", "title", "ch", "user1", 0, 0, time.Time{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrSessionNotFound))
}

func TestVOD_CreateSegment_ActiveSegmentExists(t *testing.T) {
	svc, _ := newTestVODService(t)
	session := injectTestSession(svc, "sess1")

	seg, err := svc.CreateSegment("sess1", "first", "ch", "user1", 0, 0, time.Time{})
	require.NoError(t, err)
	require.NotNil(t, seg)

	assert.Equal(t, SegmentRecording, seg.Status)

	_, err = svc.CreateSegment("sess1", "second", "ch", "user1", 0, 0, time.Time{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrActiveSegmentExists))

	session.cancel()
}

func TestVOD_CloseSegment_SessionNotFound(t *testing.T) {
	svc, _ := newTestVODService(t)

	err := svc.CloseSegment("nonexistent", "user1", false)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrSessionNotFound))
}

func TestVOD_CloseSegment_NoActiveSegment(t *testing.T) {
	svc, _ := newTestVODService(t)
	session := injectTestSession(svc, "sess1")
	defer session.cancel()

	err := svc.CloseSegment("sess1", "user1", false)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoActiveSegment))
}

func TestVOD_CancelSegment_SessionNotFound(t *testing.T) {
	svc, _ := newTestVODService(t)

	err := svc.CancelSegment("nonexistent", "user1", false)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrSessionNotFound))
}

func TestVOD_CancelSegment_NoActiveSegment(t *testing.T) {
	svc, _ := newTestVODService(t)
	session := injectTestSession(svc, "sess1")
	defer session.cancel()

	err := svc.CancelSegment("sess1", "user1", false)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoActiveSegment))
}

func TestVOD_UpdateSegment_SessionNotFound(t *testing.T) {
	svc, _ := newTestVODService(t)
	v := 1.0

	err := svc.UpdateSegment("nonexistent", "seg1", "user1", false, &v, nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrSessionNotFound))
}

func TestVOD_UpdateSegment_SegmentNotFound(t *testing.T) {
	svc, _ := newTestVODService(t)
	session := injectTestSession(svc, "sess1")
	defer session.cancel()
	v := 1.0

	err := svc.UpdateSegment("sess1", "nonexistent-seg", "user1", false, &v, nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrSegmentNotFound))
}

func TestVOD_DeleteSegment_SessionNotFound(t *testing.T) {
	svc, _ := newTestVODService(t)

	err := svc.DeleteSegment("nonexistent", "seg1", "user1", false)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrSessionNotFound))
}

func TestVOD_DeleteSegment_SegmentNotFound(t *testing.T) {
	svc, _ := newTestVODService(t)
	session := injectTestSession(svc, "sess1")
	defer session.cancel()

	err := svc.DeleteSegment("sess1", "nonexistent-seg", "user1", false)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrSegmentNotFound))
}

// --- Auth Check Tests ---

func TestVOD_CheckSegmentAuth_OwnerAllowed(t *testing.T) {
	seg := &RecordingSegment{UserID: "user1"}
	assert.NoError(t, checkSegmentAuth(seg, "user1", false))
}

func TestVOD_CheckSegmentAuth_AdminAllowed(t *testing.T) {
	seg := &RecordingSegment{UserID: "user1"}
	assert.NoError(t, checkSegmentAuth(seg, "other-user", true))
}

func TestVOD_CheckSegmentAuth_OtherUserDenied(t *testing.T) {
	seg := &RecordingSegment{UserID: "user1"}
	err := checkSegmentAuth(seg, "user2", false)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotAuthorized))
}

func TestVOD_CloseSegment_AuthDenied(t *testing.T) {
	svc, _ := newTestVODService(t)
	session := injectTestSession(svc, "sess1")
	defer session.cancel()

	_, err := svc.CreateSegment("sess1", "title", "ch", "user1", 0, 0, time.Time{})
	require.NoError(t, err)

	err = svc.CloseSegment("sess1", "user2", false)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotAuthorized))
}

func TestVOD_CloseSegment_AdminOverride(t *testing.T) {
	svc, _ := newTestVODService(t)
	session := injectTestSession(svc, "sess1")
	defer session.cancel()

	_, err := svc.CreateSegment("sess1", "title", "ch", "user1", 0, 0, time.Time{})
	require.NoError(t, err)

	err = svc.CloseSegment("sess1", "admin-user", true)
	require.NoError(t, err)
}

func TestVOD_CancelSegment_AuthDenied(t *testing.T) {
	svc, _ := newTestVODService(t)
	session := injectTestSession(svc, "sess1")
	defer session.cancel()

	_, err := svc.CreateSegment("sess1", "title", "ch", "user1", 0, 0, time.Time{})
	require.NoError(t, err)

	err = svc.CancelSegment("sess1", "user2", false)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotAuthorized))
}

// --- Segment Lifecycle Tests ---

func TestVOD_SegmentLifecycle_CreateCloseDelete(t *testing.T) {
	svc, _ := newTestVODService(t)
	session := injectTestSession(svc, "sess1")
	defer session.cancel()

	seg, err := svc.CreateSegment("sess1", "my recording", "BBC One", "user1", 0, 0, time.Time{})
	require.NoError(t, err)
	assert.Equal(t, "my recording", seg.Title)
	assert.Equal(t, "BBC One", seg.ChannelName)
	assert.Equal(t, "user1", seg.UserID)
	assert.Equal(t, SegmentRecording, seg.Status)

	assert.True(t, session.HasActiveSegment())
	assert.True(t, session.HasPendingWork())

	err = svc.CloseSegment("sess1", "user1", false)
	require.NoError(t, err)

	seg.mu.Lock()
	assert.Equal(t, SegmentDefined, seg.Status)
	seg.mu.Unlock()

	assert.False(t, session.HasActiveSegment())
	assert.True(t, session.HasPendingWork())

	err = svc.DeleteSegment("sess1", seg.ID, "user1", false)
	require.NoError(t, err)

	session.mu.Lock()
	assert.Empty(t, session.Segments)
	session.mu.Unlock()
}

func TestVOD_SegmentLifecycle_CreateCancel(t *testing.T) {
	svc, _ := newTestVODService(t)
	session := injectTestSession(svc, "sess1")
	defer session.cancel()

	seg, err := svc.CreateSegment("sess1", "title", "ch", "user1", 0, 0, time.Time{})
	require.NoError(t, err)

	err = svc.CancelSegment("sess1", "user1", false)
	require.NoError(t, err)

	_ = seg
	session.mu.Lock()
	assert.Empty(t, session.Segments)
	session.mu.Unlock()
}

func TestVOD_UpdateSegment_Success(t *testing.T) {
	svc, _ := newTestVODService(t)
	session := injectTestSession(svc, "sess1")
	defer session.cancel()

	seg, err := svc.CreateSegment("sess1", "title", "ch", "user1", 0, 0, time.Time{})
	require.NoError(t, err)

	// Both offsets are capped to buffered (0.0 for fake process with no ffmpeg)
	newStart := 5.0
	newEnd := 10.0
	err = svc.UpdateSegment("sess1", seg.ID, "user1", false, &newStart, &newEnd)
	require.NoError(t, err)

	seg.mu.Lock()
	assert.Equal(t, 0.0, seg.StartOffset, "capped to buffered=0 for fake process")
	assert.NotNil(t, seg.EndOffset)
	assert.Equal(t, 0.0, *seg.EndOffset, "capped to buffered=0 for fake process")
	seg.mu.Unlock()
}

func TestVOD_UpdateSegment_AuthDenied(t *testing.T) {
	svc, _ := newTestVODService(t)
	session := injectTestSession(svc, "sess1")
	defer session.cancel()

	seg, err := svc.CreateSegment("sess1", "title", "ch", "user1", 0, 0, time.Time{})
	require.NoError(t, err)

	v := 5.0
	err = svc.UpdateSegment("sess1", seg.ID, "other-user", false, &v, nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotAuthorized))
}

func TestVOD_DeleteSegment_AuthDenied(t *testing.T) {
	svc, _ := newTestVODService(t)
	session := injectTestSession(svc, "sess1")
	defer session.cancel()

	seg, err := svc.CreateSegment("sess1", "title", "ch", "user1", 0, 0, time.Time{})
	require.NoError(t, err)

	err = svc.DeleteSegment("sess1", seg.ID, "other-user", false)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotAuthorized))
}

// --- Session Context Tests ---

func TestVOD_SessionContext_CancelledOnDelete(t *testing.T) {
	svc, _ := newTestVODService(t)
	session := injectTestSession(svc, "sess1")

	select {
	case <-session.ctx.Done():
		t.Fatal("context should not be cancelled before delete")
	default:
	}

	svc.DeleteSession("sess1")

	select {
	case <-session.ctx.Done():
		// expected
	case <-time.After(time.Second):
		t.Fatal("context should be cancelled after delete")
	}
}

func TestVOD_SessionContext_DeadlineRespectsCancel(t *testing.T) {
	svc, _ := newTestVODService(t)
	session := injectTestSession(svc, "sess1")

	seg, err := svc.CreateSegment("sess1", "title", "ch", "user1", 0, 0, time.Now().Add(10*time.Minute))
	require.NoError(t, err)
	_ = seg

	// Cancel the session context — the deadline goroutine should exit
	session.cancel()

	// Give goroutine time to notice
	time.Sleep(50 * time.Millisecond)

	// Segment should still be in recording state (deadline didn't fire)
	seg.mu.Lock()
	assert.Equal(t, SegmentRecording, seg.Status)
	seg.mu.Unlock()
}

// --- GetSession / getSession Tests ---

func TestVOD_GetSession_UpdatesLastAccess(t *testing.T) {
	svc, _ := newTestVODService(t)
	session := injectTestSession(svc, "sess1")
	defer session.cancel()

	oldAccess := session.LastAccess
	time.Sleep(10 * time.Millisecond)

	got, ok := svc.GetSession("sess1")
	assert.True(t, ok)
	assert.Same(t, session, got)
	assert.True(t, got.LastAccess.After(oldAccess))
}

func TestVOD_GetSession_NotFound(t *testing.T) {
	svc, _ := newTestVODService(t)

	_, ok := svc.GetSession("nonexistent")
	assert.False(t, ok)
}

// --- Recording Info Tests ---

func TestVOD_GetSegments_Empty(t *testing.T) {
	svc, _ := newTestVODService(t)
	session := injectTestSession(svc, "sess1")
	defer session.cancel()

	infos := svc.GetSegments("sess1")
	assert.Empty(t, infos)
}

func TestVOD_GetSegments_ReturnsAll(t *testing.T) {
	svc, _ := newTestVODService(t)
	session := injectTestSession(svc, "sess1")
	defer session.cancel()

	seg1, _ := svc.CreateSegment("sess1", "seg1", "ch", "user1", 0, 0, time.Time{})
	svc.CloseSegment("sess1", "user1", false)

	seg2, _ := svc.CreateSegment("sess1", "seg2", "ch", "user1", 0, 0, time.Time{})
	_ = seg2

	infos := svc.GetSegments("sess1")
	assert.Len(t, infos, 2)
	assert.Equal(t, seg1.ID, infos[0].ID)
	assert.Equal(t, "defined", infos[0].Status)
	assert.Equal(t, "recording", infos[1].Status)
}

func TestVOD_GetSegments_NonexistentSession(t *testing.T) {
	svc, _ := newTestVODService(t)

	infos := svc.GetSegments("nonexistent")
	assert.Nil(t, infos)
}

func TestVOD_IsRecording(t *testing.T) {
	svc, _ := newTestVODService(t)
	session := injectTestSession(svc, "sess1")
	defer session.cancel()

	assert.False(t, svc.IsRecording("sess1"))

	_, err := svc.CreateSegment("sess1", "title", "ch", "user1", 0, 0, time.Time{})
	require.NoError(t, err)
	assert.True(t, svc.IsRecording("sess1"))

	svc.CloseSegment("sess1", "user1", false)
	assert.False(t, svc.IsRecording("sess1"))
}

// --- Completed Recording Tests ---

func TestVOD_GetCompletedRecordingPath_InvalidFilename(t *testing.T) {
	svc, _ := newTestVODService(t)

	tests := []string{"../etc/passwd", "./config", "foo/bar", "foo\\bar", "..", "."}
	for _, fn := range tests {
		_, err := svc.GetCompletedRecordingPath(fn, "user1")
		require.Error(t, err, "should reject: %s", fn)
		assert.True(t, errors.Is(err, ErrInvalidFilename), "should be ErrInvalidFilename for: %s", fn)
	}
}

func TestVOD_GetCompletedRecordingPath_FileNotFound(t *testing.T) {
	svc, _ := newTestVODService(t)

	_, err := svc.GetCompletedRecordingPath("nonexistent.mp4", "user1")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrFileNotFound))
}

func TestVOD_GetCompletedRecordingPath_Success(t *testing.T) {
	svc, cfg := newTestVODService(t)
	userDir := filepath.Join(cfg.RecordDir, "user1")
	require.NoError(t, os.MkdirAll(userDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(userDir, "test.mp4"), []byte("data"), 0644))

	path, err := svc.GetCompletedRecordingPath("test.mp4", "user1")
	require.NoError(t, err)
	assert.Contains(t, path, "test.mp4")
}

func TestVOD_DeleteCompletedRecording_Success(t *testing.T) {
	svc, cfg := newTestVODService(t)
	userDir := filepath.Join(cfg.RecordDir, "user1")
	require.NoError(t, os.MkdirAll(userDir, 0755))
	filePath := filepath.Join(userDir, "delete-me.mp4")
	require.NoError(t, os.WriteFile(filePath, []byte("data"), 0644))

	err := svc.DeleteCompletedRecording("delete-me.mp4", "user1")
	require.NoError(t, err)

	_, err = os.Stat(filePath)
	assert.True(t, os.IsNotExist(err))
}

func TestVOD_ListCompletedRecordings_Empty(t *testing.T) {
	svc, _ := newTestVODService(t)

	list, err := svc.ListCompletedRecordings("user1", false)
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestVOD_ListCompletedRecordings_UserIsolation(t *testing.T) {
	svc, cfg := newTestVODService(t)

	for _, uid := range []string{"user1", "user2"} {
		dir := filepath.Join(cfg.RecordDir, uid)
		require.NoError(t, os.MkdirAll(dir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, uid+".mp4"), []byte("data"), 0644))
	}

	list, err := svc.ListCompletedRecordings("user1", false)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "user1.mp4", list[0].Filename)
	assert.Equal(t, "user1", list[0].UserID)
}

func TestVOD_ListCompletedRecordings_AdminSeesAll(t *testing.T) {
	svc, cfg := newTestVODService(t)

	for _, uid := range []string{"user1", "user2"} {
		dir := filepath.Join(cfg.RecordDir, uid)
		require.NoError(t, os.MkdirAll(dir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, uid+".mp4"), []byte("data"), 0644))
	}

	list, err := svc.ListCompletedRecordings("admin", true)
	require.NoError(t, err)
	assert.Len(t, list, 2)
}

// --- ListRecordings Tests ---

func TestVOD_ListRecordings_UserIsolation(t *testing.T) {
	svc, _ := newTestVODService(t)
	session := injectTestSession(svc, "sess1")
	defer session.cancel()

	_, err := svc.CreateSegment("sess1", "user1 recording", "ch", "user1", 0, 0, time.Time{})
	require.NoError(t, err)

	// user1 sees their recording
	list := svc.ListRecordings("user1", false)
	assert.Len(t, list, 1)

	// user2 doesn't
	list = svc.ListRecordings("user2", false)
	assert.Len(t, list, 0)

	// admin sees everything
	list = svc.ListRecordings("admin", true)
	assert.Len(t, list, 1)
}

// --- Shutdown Tests ---

func TestVOD_Shutdown_ClearsAllSessions(t *testing.T) {
	svc, _ := newTestVODService(t)

	s1 := injectTestSession(svc, "sess1")
	s2 := injectTestSession(svc, "sess2")

	svc.Shutdown()

	// All sessions cleared
	svc.mu.RLock()
	assert.Empty(t, svc.sessions)
	svc.mu.RUnlock()

	// Contexts cancelled
	select {
	case <-s1.ctx.Done():
	default:
		t.Fatal("session 1 context should be cancelled")
	}
	select {
	case <-s2.ctx.Done():
	default:
		t.Fatal("session 2 context should be cancelled")
	}
}

// --- CleanupExpired Tests ---

func TestVOD_CleanupExpired_ExpiresByTimeout(t *testing.T) {
	svc, cfg := newTestVODService(t)
	cfg.VODSessionTimeout = 10 * time.Millisecond

	session := injectTestSession(svc, "sess1")
	session.mu.Lock()
	session.LastAccess = time.Now().Add(-time.Minute)
	session.mu.Unlock()

	svc.CleanupExpired()

	svc.mu.RLock()
	_, exists := svc.sessions["sess1"]
	svc.mu.RUnlock()
	assert.False(t, exists, "expired session should be removed")
}

func TestVOD_CleanupExpired_SkipsActiveRecordings(t *testing.T) {
	svc, cfg := newTestVODService(t)
	cfg.VODSessionTimeout = 10 * time.Millisecond

	session := injectTestSession(svc, "sess1")
	session.mu.Lock()
	session.LastAccess = time.Now().Add(-time.Minute)
	session.mu.Unlock()

	_, err := svc.CreateSegment("sess1", "title", "ch", "user1", 0, 0, time.Time{})
	require.NoError(t, err)

	svc.CleanupExpired()

	svc.mu.RLock()
	_, exists := svc.sessions["sess1"]
	svc.mu.RUnlock()
	assert.True(t, exists, "session with active segment should not expire")
	session.cancel()
}

func TestVOD_CleanupExpired_CleansDetachedWithNoWork(t *testing.T) {
	svc, _ := newTestVODService(t)

	session := injectTestSession(svc, "sess1")
	session.mu.Lock()
	session.Detached = true
	session.mu.Unlock()

	// ffmpeg "done" for fake process = IsReady returns true (process not found)
	svc.CleanupExpired()

	svc.mu.RLock()
	_, exists := svc.sessions["sess1"]
	svc.mu.RUnlock()
	assert.False(t, exists, "detached session with no pending work should be cleaned")
}

// --- HasPendingWork Tests ---

func TestVOD_HasPendingWork(t *testing.T) {
	session := &VODSession{}
	assert.False(t, session.HasPendingWork())

	seg := &RecordingSegment{Status: SegmentRecording}
	session.Segments = append(session.Segments, seg)
	assert.True(t, session.HasPendingWork())

	seg.Status = SegmentDefined
	assert.True(t, session.HasPendingWork())

	seg.Status = SegmentExtracting
	assert.True(t, session.HasPendingWork())

	seg.Status = SegmentCompleted
	assert.False(t, session.HasPendingWork())

	seg.Status = SegmentFailed
	assert.False(t, session.HasPendingWork())
}

// --- HasActiveSegment Tests ---

func TestVOD_HasActiveSegment(t *testing.T) {
	session := &VODSession{}
	assert.False(t, session.HasActiveSegment())

	seg := &RecordingSegment{Status: SegmentDefined}
	session.Segments = append(session.Segments, seg)
	assert.False(t, session.HasActiveSegment())

	seg.Status = SegmentRecording
	assert.True(t, session.HasActiveSegment())
}

// --- StopAt Buffer Tests ---

func TestVOD_CreateSegment_AppliesStopBuffer(t *testing.T) {
	svc, cfg := newTestVODService(t)
	cfg.RecordStopBuffer = 10 * time.Minute
	session := injectTestSession(svc, "sess1")
	defer session.cancel()

	stopAt := time.Now().Add(30 * time.Minute)
	seg, err := svc.CreateSegment("sess1", "title", "ch", "user1", 0, 0, stopAt)
	require.NoError(t, err)

	// Stop time should be stopAt + 10min buffer
	expected := stopAt.Add(10 * time.Minute)
	assert.WithinDuration(t, expected, seg.StopAt, time.Second)
}

func TestVOD_CreateSegment_DefaultDurationWhenNoStopAt(t *testing.T) {
	svc, cfg := newTestVODService(t)
	cfg.RecordDefaultDuration = 2 * time.Hour
	session := injectTestSession(svc, "sess1")
	defer session.cancel()

	before := time.Now()
	seg, err := svc.CreateSegment("sess1", "title", "ch", "user1", 0, 0, time.Time{})
	require.NoError(t, err)

	expected := before.Add(2 * time.Hour)
	assert.WithinDuration(t, expected, seg.StopAt, 2*time.Second)
}

// --- DeleteSession Detach Tests ---

func TestVOD_DeleteSession_DetachesWhenPending(t *testing.T) {
	svc, _ := newTestVODService(t)
	session := injectTestSession(svc, "sess1")

	_, err := svc.CreateSegment("sess1", "title", "ch", "user1", 0, 0, time.Time{})
	require.NoError(t, err)

	svc.DeleteSession("sess1")

	session.mu.Lock()
	assert.True(t, session.Detached)
	session.mu.Unlock()

	// session should still exist
	svc.mu.RLock()
	_, exists := svc.sessions["sess1"]
	svc.mu.RUnlock()
	assert.True(t, exists)
	session.cancel()
}

func TestVOD_DeleteSession_RemovesWhenNoPending(t *testing.T) {
	svc, _ := newTestVODService(t)
	injectTestSession(svc, "sess1")

	svc.DeleteSession("sess1")

	svc.mu.RLock()
	_, exists := svc.sessions["sess1"]
	svc.mu.RUnlock()
	assert.False(t, exists)
}

// --- Concurrency Test ---

func TestVOD_ConcurrentSegmentOperations(t *testing.T) {
	svc, _ := newTestVODService(t)
	session := injectTestSession(svc, "sess1")
	defer session.cancel()

	var wg sync.WaitGroup
	errs := make([]error, 20)

	// 20 goroutines try to create a segment simultaneously
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = svc.CreateSegment("sess1", "title", "ch", "user1", 0, 0, time.Time{})
		}(i)
	}
	wg.Wait()

	successCount := 0
	activeExistsCount := 0
	for _, err := range errs {
		if err == nil {
			successCount++
		} else if errors.Is(err, ErrActiveSegmentExists) {
			activeExistsCount++
		}
	}
	assert.Equal(t, 1, successCount, "exactly one should succeed")
	assert.Equal(t, 19, activeExistsCount, "rest should get ErrActiveSegmentExists")
}
