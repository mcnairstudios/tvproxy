package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/config"
	"github.com/gavinmcnair/tvproxy/pkg/ffmpeg"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
	"github.com/gavinmcnair/tvproxy/pkg/store"
)

type SegmentStatus string

const (
	SegmentRecording  SegmentStatus = "recording"
	SegmentDefined    SegmentStatus = "defined"
	SegmentExtracting SegmentStatus = "extracting"
	SegmentCompleted  SegmentStatus = "completed"
	SegmentFailed     SegmentStatus = "failed"
)

var (
	ErrSessionNotFound     = errors.New("session not found")
	ErrNotAuthorized       = errors.New("not authorized")
	ErrNoActiveSegment     = errors.New("no active segment")
	ErrInvalidFilename     = errors.New("invalid filename")
	ErrFileNotFound        = errors.New("file not found")
	ErrActiveSegmentExists = errors.New("session already has an active segment")
	ErrStreamNotFound      = errors.New("stream not found")
	ErrSegmentNotFound     = errors.New("segment not found")
)

type RecordingSegment struct {
	ID          string
	StartOffset float64
	EndOffset   *float64
	Title       string
	ChannelName string
	UserID      string
	CreatedAt   time.Time
	Status      SegmentStatus
	StopAt      time.Time
	FilePath    string
	mu          sync.Mutex
}

type VODSession struct {
	ID          string
	ProcessID   string
	StreamURL   string
	StreamName  string
	ChannelName string
	ProfileName string
	Duration    float64
	Video       *ffmpeg.VideoInfo
	AudioTracks []ffmpeg.AudioTrack
	AudioIndex  int
	FilePath    string
	TempDir     string
	LastAccess  time.Time
	Detached    bool
	Segments    []*RecordingSegment
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.Mutex
}

func (session *VODSession) HasActiveSegment() bool {
	for _, seg := range session.Segments {
		seg.mu.Lock()
		active := seg.Status == SegmentRecording
		seg.mu.Unlock()
		if active {
			return true
		}
	}
	return false
}

func (session *VODSession) ActiveSegment() *RecordingSegment {
	for _, seg := range session.Segments {
		seg.mu.Lock()
		active := seg.Status == SegmentRecording
		seg.mu.Unlock()
		if active {
			return seg
		}
	}
	return nil
}

func (session *VODSession) HasPendingWork() bool {
	for _, seg := range session.Segments {
		seg.mu.Lock()
		pending := seg.Status == SegmentRecording || seg.Status == SegmentDefined || seg.Status == SegmentExtracting
		seg.mu.Unlock()
		if pending {
			return true
		}
	}
	return false
}

type SegmentInfo struct {
	ID          string   `json:"id"`
	StartOffset float64  `json:"start_offset"`
	EndOffset   *float64 `json:"end_offset,omitempty"`
	Status      string   `json:"status"`
	Title       string   `json:"title"`
}

type RecordingInfo struct {
	SessionID    string        `json:"session_id"`
	ChannelName  string        `json:"channel_name"`
	ProgramTitle string        `json:"program_title"`
	BufferedSecs float64       `json:"buffered_secs"`
	StopAt       string        `json:"stop_at,omitempty"`
	UserID       string        `json:"user_id"`
	Segments     []SegmentInfo `json:"segments"`
}

type CompletedRecording struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	ModTime  string `json:"mod_time"`
	UserID   string `json:"user_id"`
}

type VODService struct {
	config            *config.Config
	channelRepo       *repository.ChannelRepository
	streamStore       store.StreamReader
	streamProfileRepo *repository.StreamProfileRepository
	ffmpegMgr         *FFmpegManager
	activity          *ActivityService
	log               zerolog.Logger
	mu                sync.RWMutex
	sessions          map[string]*VODSession
}

type recordingIntent struct {
	ChannelID    string    `json:"channel_id"`
	ChannelName  string    `json:"channel_name"`
	ProgramTitle string    `json:"program_title"`
	UserID       string    `json:"user_id"`
	StopAt       time.Time `json:"stop_at"`
}

const recordingIntentFile = "recording.json"

func NewVODService(
	channelRepo *repository.ChannelRepository,
	streamStore store.StreamReader,
	streamProfileRepo *repository.StreamProfileRepository,
	ffmpegMgr *FFmpegManager,
	activity *ActivityService,
	cfg *config.Config,
	log zerolog.Logger,
) *VODService {
	return &VODService{
		config:            cfg,
		channelRepo:       channelRepo,
		streamStore:       streamStore,
		streamProfileRepo: streamProfileRepo,
		ffmpegMgr:         ffmpegMgr,
		activity:          activity,
		log:               log.With().Str("service", "vod").Logger(),
		sessions:          make(map[string]*VODSession),
	}
}

func (s *VODService) writeRecordingIntent(tempDir string, intent recordingIntent) {
	data, err := json.Marshal(intent)
	if err != nil {
		s.log.Error().Err(err).Msg("failed to marshal recording intent")
		return
	}
	path := filepath.Join(tempDir, recordingIntentFile)
	if err := os.WriteFile(path, data, 0644); err != nil {
		s.log.Error().Err(err).Str("path", path).Msg("failed to write recording intent")
	}
}

func (s *VODService) removeRecordingIntent(tempDir string) {
	path := filepath.Join(tempDir, recordingIntentFile)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		s.log.Warn().Err(err).Str("path", path).Msg("failed to remove recording intent")
	}
}

func (s *VODService) RecoverRecordings(ctx context.Context) {
	vodDir := s.config.VODTempDir
	if vodDir == "" {
		return
	}

	entries, err := os.ReadDir(vodDir)
	if err != nil {
		if !os.IsNotExist(err) {
			s.log.Error().Err(err).Msg("failed to read VOD temp dir for recovery")
		}
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		intentPath := filepath.Join(vodDir, entry.Name(), recordingIntentFile)
		data, err := os.ReadFile(intentPath)
		if err != nil {
			continue
		}

		var intent recordingIntent
		if err := json.Unmarshal(data, &intent); err != nil {
			s.log.Warn().Str("path", intentPath).Err(err).Msg("invalid recording intent, removing")
			os.RemoveAll(filepath.Join(vodDir, entry.Name()))
			continue
		}

		if intent.StopAt.Before(time.Now()) {
			s.log.Info().Str("channel", intent.ChannelName).Str("program", intent.ProgramTitle).Msg("recording intent expired, cleaning up")
			os.RemoveAll(filepath.Join(vodDir, entry.Name()))
			continue
		}

		s.log.Info().Str("channel", intent.ChannelName).Str("program", intent.ProgramTitle).Time("stop_at", intent.StopAt).Msg("recovering recording")
		_, _, err = s.CreateRecordingSession(ctx, intent.ChannelID, intent.ProgramTitle, intent.ChannelName, intent.UserID, intent.StopAt)
		if err != nil {
			s.log.Error().Err(err).Str("channel", intent.ChannelName).Msg("failed to recover recording")
			os.RemoveAll(filepath.Join(vodDir, entry.Name()))
		}
	}
}

func (s *VODService) ProbeStream(ctx context.Context, streamID string) (*ffmpeg.ProbeResult, error) {
	stream, err := s.streamStore.GetByID(ctx, streamID)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrStreamNotFound, err)
	}
	return ffmpeg.Probe(ctx, stream.URL, s.config.UserAgent)
}

func (s *VODService) CreateSession(ctx context.Context, streamID string, profileName string, audioIndex int, userAgent string, remoteAddr string) (*VODSession, error) {
	stream, err := s.streamStore.GetByID(ctx, streamID)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrStreamNotFound, err)
	}
	if !stream.IsActive {
		return nil, fmt.Errorf("stream %s is inactive", streamID)
	}
	session, err := s.createSessionForURL(ctx, uuid.New().String(), stream.URL, stream.ID, profileName, audioIndex)
	if err != nil {
		return nil, err
	}
	session.StreamName = stream.Name
	if s.activity != nil {
		s.activity.Add(ViewerOpts{
			ID:           session.ID,
			StreamID:     streamID,
			StreamName:   stream.Name,
			M3UAccountID: stream.M3UAccountID,
			ProfileName:  profileName,
			UserAgent:    userAgent,
			RemoteAddr:   remoteAddr,
			Type:     "vod",
		})
	}
	return session, nil
}

func (s *VODService) CreateSessionForChannel(ctx context.Context, channelID string, profileName string, audioIndex int, userAgent string, remoteAddr string) (*VODSession, error) {
	if session := s.reattachSession(channelID); session != nil {
		return session, nil
	}

	channel, err := s.channelRepo.GetByID(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("channel not found: %w", err)
	}
	if !channel.IsEnabled {
		return nil, fmt.Errorf("channel %s is disabled", channelID)
	}

	channelStreams, err := s.channelRepo.GetStreams(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("getting channel streams: %w", err)
	}

	for _, cs := range channelStreams {
		stream, err := s.streamStore.GetByID(ctx, cs.StreamID)
		if err != nil || !stream.IsActive {
			continue
		}
		session, err := s.createSessionForURL(ctx, channelID, stream.URL, stream.ID, profileName, audioIndex)
		if err != nil {
			return nil, err
		}
		session.ChannelName = channel.Name
		if s.activity != nil {
			s.activity.Add(ViewerOpts{
				ID:           session.ID,
				ChannelID:    channelID,
				ChannelName:  channel.Name,
				M3UAccountID: stream.M3UAccountID,
				ProfileName:  profileName,
				UserAgent:    userAgent,
				RemoteAddr:   remoteAddr,
				Type:     "vod",
			})
		}
		return session, nil
	}

	return nil, fmt.Errorf("no active streams for channel %s", channelID)
}

func (s *VODService) reattachSession(channelID string) *VODSession {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.sessions[channelID]
	if !ok {
		return nil
	}

	existing.mu.Lock()
	hasPending := existing.HasPendingWork()
	hasSegments := len(existing.Segments) > 0
	existing.mu.Unlock()

	if hasSegments && (!s.ffmpegMgr.IsReady(existing.ProcessID) || hasPending) {
		existing.mu.Lock()
		existing.Detached = false
		existing.LastAccess = time.Now()
		existing.mu.Unlock()
		s.log.Info().Str("session_id", channelID).Bool("has_pending", hasPending).Msg("reattached player to existing session")
		return existing
	}

	existing.cancel()
	s.ffmpegMgr.Stop(existing.ProcessID)
	s.ffmpegMgr.Remove(existing.ProcessID)
	os.RemoveAll(existing.TempDir)
	delete(s.sessions, channelID)
	return nil
}

func (s *VODService) createSessionForURL(ctx context.Context, id string, streamURL string, streamID string, profileName string, audioIndex int) (*VODSession, error) {
	var profileArgs []string
	command := "ffmpeg"
	if profileName != "" {
		sp, err := s.streamProfileRepo.GetByName(ctx, profileName)
		if err != nil {
			return nil, fmt.Errorf("profile %q not found: %w", profileName, err)
		}
		if sp.Args != "" {
			profileArgs = ffmpeg.ShellSplit(sp.Args)
			if audioIndex > 0 {
				profileArgs = ffmpeg.ReplaceAudioMap(profileArgs, audioIndex)
			}
			command = sp.Command
		}
	}

	tempDir := filepath.Join(s.config.VODTempDir, id)
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}

	filePath := filepath.Join(tempDir, "video.mp4")
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		s.log.Warn().Err(err).Str("path", filePath).Msg("failed to remove existing VOD file")
	}

	processID := s.ffmpegMgr.Start(streamURL, filePath, tempDir, command, profileArgs)

	sessionCtx, sessionCancel := context.WithCancel(context.Background())
	session := &VODSession{
		ID:          id,
		ProcessID:   processID,
		StreamURL:   streamURL,
		ProfileName: profileName,
		AudioIndex:  audioIndex,
		FilePath:    filePath,
		TempDir:     tempDir,
		LastAccess:  time.Now(),
		ctx:         sessionCtx,
		cancel:      sessionCancel,
	}

	s.mu.Lock()
	if existing, ok := s.sessions[id]; ok {
		s.mu.Unlock()
		sessionCancel()
		s.ffmpegMgr.Stop(processID)
		s.ffmpegMgr.Remove(processID)
		existing.mu.Lock()
		existing.LastAccess = time.Now()
		existing.mu.Unlock()
		s.log.Info().Str("session_id", id).Msg("concurrent session creation detected, reusing existing")
		return existing, nil
	}
	s.sessions[id] = session
	s.mu.Unlock()

	go s.probeLocalFile(session)

	s.log.Info().Str("session_id", id).Str("stream_id", streamID).Msg("VOD session created")
	return session, nil
}

func (s *VODService) probeLocalFile(session *VODSession) {
	timeout := 30 * time.Second
	delay := 500 * time.Millisecond
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if s.ffmpegMgr.GetBufferedSecs(session.ProcessID) > 0 {
			break
		}
		if s.ffmpegMgr.IsReady(session.ProcessID) {
			return
		}
		select {
		case <-session.ctx.Done():
			return
		case <-time.After(delay):
		}
	}

	if s.ffmpegMgr.GetBufferedSecs(session.ProcessID) == 0 {
		return
	}

	probeDur := 15 * time.Second
	if s.config.Settings != nil {
		probeDur = s.config.Settings.VOD.ProbeTimeout
	}
	ctx, cancel := context.WithTimeout(session.ctx, probeDur)
	defer cancel()

	probe, err := ffmpeg.Probe(ctx, session.FilePath, "")
	if err != nil {
		return
	}

	if !probe.IsVOD {
		upstreamProbe, err := s.ffmpegMgr.ProbeURL(ctx, session.StreamURL)
		if err == nil && upstreamProbe.IsVOD {
			probe.Duration = upstreamProbe.Duration
			probe.IsVOD = true
		}
	}

	session.mu.Lock()
	if probe.IsVOD {
		session.Duration = probe.Duration
	}
	if probe.Video != nil {
		session.Video = probe.Video
	}
	if len(probe.AudioTracks) > 0 {
		session.AudioTracks = probe.AudioTracks
	}
	session.mu.Unlock()

	s.log.Info().Str("session_id", session.ID).Float64("duration", probe.Duration).Int("audio_tracks", len(probe.AudioTracks)).Msg("probe complete")
}

func (s *VODService) StreamSeek(ctx context.Context, session *VODSession, offsetSecs float64) (io.ReadCloser, error) {
	buffered := s.ffmpegMgr.GetBufferedSecs(session.ProcessID)

	if offsetSecs > buffered {
		return nil, fmt.Errorf("offset %.1fs exceeds buffered %.1fs", offsetSecs, buffered)
	}

	args := []string{
		"-hide_banner", "-loglevel", "warning",
		"-ss", fmt.Sprintf("%.6f", offsetSecs),
		"-i", session.FilePath,
		"-c", "copy",
		"-f", "mp4",
		"-movflags", "frag_keyframe+empty_moov+default_base_moof",
		"-fflags", "+genpts",
		"pipe:1",
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdout pipe: %w", err)
	}
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting seek ffmpeg: %w", err)
	}

	s.log.Info().Str("session_id", session.ID).Float64("offset", offsetSecs).Msg("serving seek stream")

	return &seekReadCloser{ReadCloser: stdout, cmd: cmd}, nil
}

type seekReadCloser struct {
	io.ReadCloser
	cmd *exec.Cmd
}

func (s *seekReadCloser) Close() error {
	s.ReadCloser.Close()
	return s.cmd.Wait()
}

func (s *VODService) StreamFile(ctx context.Context, session *VODSession) (io.ReadCloser, error) {
	retryCount := 50
	retryDelay := 200 * time.Millisecond
	if s.config.Settings != nil {
		retryCount = s.config.Settings.VOD.FileRetryCount
		retryDelay = s.config.Settings.VOD.FileRetryDelay
	}
	var f *os.File
	for i := 0; i < retryCount; i++ {
		var err error
		f, err = os.Open(session.FilePath)
		if err == nil {
			break
		}
		if s.ffmpegMgr.IsReady(session.ProcessID) {
			if procErr := s.ffmpegMgr.GetError(session.ProcessID); procErr != nil {
				return nil, procErr
			}
			return nil, fmt.Errorf("ffmpeg exited before creating file")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(retryDelay):
		}
	}
	if f == nil {
		return nil, fmt.Errorf("timed out waiting for VOD file")
	}

	isLive := session.Duration == 0

	return &tailFollowReader{
		file:      f,
		ctx:       ctx,
		isLive:    isLive,
		processID: session.ProcessID,
		ffmpegMgr: s.ffmpegMgr,
	}, nil
}

type tailFollowReader struct {
	file      *os.File
	ctx       context.Context
	isLive    bool
	processID string
	ffmpegMgr *FFmpegManager
}

func (r *tailFollowReader) Read(p []byte) (int, error) {
	for {
		n, err := r.file.Read(p)
		if n > 0 {
			return n, nil
		}
		if err != io.EOF {
			return 0, err
		}

		if !r.isLive || r.ffmpegMgr.IsReady(r.processID) {
			return 0, io.EOF
		}

		select {
		case <-r.ctx.Done():
			return 0, r.ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (r *tailFollowReader) Close() error {
	return r.file.Close()
}

func (s *VODService) getSession(id string) (*VODSession, bool) {
	s.mu.RLock()
	session, ok := s.sessions[id]
	s.mu.RUnlock()
	return session, ok
}

func (s *VODService) GetSession(id string) (*VODSession, bool) {
	session, ok := s.getSession(id)
	if ok {
		session.mu.Lock()
		session.LastAccess = time.Now()
		session.mu.Unlock()
		if s.activity != nil {
			s.activity.Touch(id)
		}
	}
	return session, ok
}

func checkSegmentAuth(seg *RecordingSegment, userID string, isAdmin bool) error {
	if !isAdmin && seg.UserID != userID {
		return ErrNotAuthorized
	}
	return nil
}

func findSegmentByID(session *VODSession, segID string) (*RecordingSegment, int) {
	for i, seg := range session.Segments {
		if seg.ID == segID {
			return seg, i
		}
	}
	return nil, -1
}

func (s *VODService) DeleteSession(id string) {
	if s.activity != nil {
		s.activity.Remove(id)
	}

	s.mu.Lock()
	session, ok := s.sessions[id]
	if !ok {
		s.mu.Unlock()
		return
	}

	session.mu.Lock()
	hasPending := session.HasPendingWork()
	hasRecording := session.HasActiveSegment()
	processID := session.ProcessID
	if hasPending {
		session.Detached = true
		session.mu.Unlock()
		s.mu.Unlock()
		if !hasRecording {
			s.ffmpegMgr.Stop(processID)
			s.log.Info().Str("session_id", id).Msg("player closed, defined segments pending (detached, ffmpeg stopped)")
		} else {
			s.log.Info().Str("session_id", id).Msg("player closed, still recording (detached, ffmpeg running)")
		}
		return
	}
	session.mu.Unlock()

	delete(s.sessions, id)
	s.mu.Unlock()

	session.cancel()
	s.ffmpegMgr.Stop(processID)
	s.ffmpegMgr.Remove(processID)
	os.RemoveAll(session.TempDir)
	s.log.Info().Str("session_id", id).Msg("VOD session deleted")
}

func (s *VODService) CreateSegment(sessionID, title, channelName, userID string, startOffset, endOffset float64, stopAt time.Time) (*RecordingSegment, error) {
	session, ok := s.GetSession(sessionID)
	if !ok {
		return nil, ErrSessionNotFound
	}

	session.mu.Lock()
	if session.HasActiveSegment() {
		session.mu.Unlock()
		return nil, ErrActiveSegmentExists
	}

	buffered := s.ffmpegMgr.GetBufferedSecs(session.ProcessID)
	if startOffset < 0 {
		startOffset = 0
	}
	if startOffset > buffered {
		startOffset = buffered
	}
	if endOffset < startOffset {
		endOffset = startOffset
	}
	if endOffset > buffered {
		endOffset = buffered
	}

	if !stopAt.IsZero() {
		stopAt = stopAt.Add(s.config.RecordStopBuffer)
		s.log.Info().Str("session_id", sessionID).Dur("buffer", s.config.RecordStopBuffer).Time("stop_at", stopAt).Msg("applied stop buffer to recording deadline")
	} else {
		stopAt = time.Now().Add(s.config.RecordDefaultDuration)
	}

	seg := &RecordingSegment{
		ID:          uuid.New().String(),
		StartOffset: startOffset,
		EndOffset:   &endOffset,
		Title:       title,
		ChannelName: channelName,
		UserID:      userID,
		CreatedAt:   time.Now(),
		Status:      SegmentRecording,
		StopAt:      stopAt,
	}
	session.Segments = append(session.Segments, seg)
	session.mu.Unlock()

	go s.startSegmentDeadline(session, seg)

	s.log.Info().Str("session_id", sessionID).Str("segment_id", seg.ID).Float64("start", startOffset).Float64("end", endOffset).Str("user_id", userID).Time("stop_at", stopAt).Msg("segment created")
	return seg, nil
}

func (s *VODService) CloseSegment(sessionID, userID string, isAdmin bool) error {
	session, ok := s.getSession(sessionID)
	if !ok {
		return ErrSessionNotFound
	}

	session.mu.Lock()
	seg := session.ActiveSegment()
	if seg == nil {
		session.mu.Unlock()
		return ErrNoActiveSegment
	}

	seg.mu.Lock()
	if err := checkSegmentAuth(seg, userID, isAdmin); err != nil {
		seg.mu.Unlock()
		session.mu.Unlock()
		return err
	}

	buffered := s.ffmpegMgr.GetBufferedSecs(session.ProcessID)
	seg.EndOffset = &buffered
	seg.Status = SegmentDefined
	segID := seg.ID
	seg.mu.Unlock()
	session.mu.Unlock()

	s.log.Info().Str("session_id", sessionID).Str("segment_id", segID).Float64("end", buffered).Msg("segment closed to defined")
	return nil
}

func (s *VODService) CloseAndExtract(sessionID, userID string, isAdmin bool) error {
	if err := s.CloseSegment(sessionID, userID, isAdmin); err != nil {
		return err
	}
	if session, ok := s.getSession(sessionID); ok {
		go s.extractAllDefined(session)
	}
	return nil
}

func (s *VODService) CancelSegment(sessionID, userID string, isAdmin bool) error {
	session, ok := s.getSession(sessionID)
	if !ok {
		return ErrSessionNotFound
	}

	session.mu.Lock()
	seg := session.ActiveSegment()
	if seg == nil {
		session.mu.Unlock()
		return ErrNoActiveSegment
	}

	seg.mu.Lock()
	if err := checkSegmentAuth(seg, userID, isAdmin); err != nil {
		seg.mu.Unlock()
		session.mu.Unlock()
		return err
	}
	segID := seg.ID
	seg.mu.Unlock()

	filtered := make([]*RecordingSegment, 0, len(session.Segments))
	for _, existing := range session.Segments {
		if existing.ID != segID {
			filtered = append(filtered, existing)
		}
	}
	session.Segments = filtered
	session.mu.Unlock()

	s.log.Info().Str("session_id", sessionID).Str("segment_id", segID).Msg("segment cancelled")
	return nil
}

func (s *VODService) UpdateSegment(sessionID, segmentID, userID string, isAdmin bool, startOffset, endOffset *float64) error {
	session, ok := s.getSession(sessionID)
	if !ok {
		return ErrSessionNotFound
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	seg, _ := findSegmentByID(session, segmentID)
	if seg == nil {
		return ErrSegmentNotFound
	}

	seg.mu.Lock()
	defer seg.mu.Unlock()

	if err := checkSegmentAuth(seg, userID, isAdmin); err != nil {
		return err
	}
	if seg.Status != SegmentRecording && seg.Status != SegmentDefined {
		return fmt.Errorf("segment is not editable")
	}

	buffered := s.ffmpegMgr.GetBufferedSecs(session.ProcessID)
	if startOffset != nil {
		v := *startOffset
		if v < 0 {
			v = 0
		}
		if v > buffered {
			v = buffered
		}
		seg.StartOffset = v
	}
	if endOffset != nil {
		v := *endOffset
		if v < 0 {
			seg.EndOffset = nil
		} else {
			if v > buffered {
				v = buffered
			}
			seg.EndOffset = &v
		}
	}

	s.log.Info().Str("session_id", sessionID).Str("segment_id", segmentID).Msg("segment updated")
	return nil
}

func (s *VODService) DeleteSegment(sessionID, segmentID, userID string, isAdmin bool) error {
	session, ok := s.getSession(sessionID)
	if !ok {
		return ErrSessionNotFound
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	seg, idx := findSegmentByID(session, segmentID)
	if seg == nil {
		return ErrSegmentNotFound
	}

	seg.mu.Lock()
	if err := checkSegmentAuth(seg, userID, isAdmin); err != nil {
		seg.mu.Unlock()
		return err
	}
	if seg.Status != SegmentRecording && seg.Status != SegmentDefined {
		seg.mu.Unlock()
		return fmt.Errorf("segment cannot be deleted in current state")
	}
	seg.mu.Unlock()

	session.Segments = append(session.Segments[:idx], session.Segments[idx+1:]...)
	s.log.Info().Str("session_id", sessionID).Str("segment_id", segmentID).Msg("segment deleted")
	return nil
}

func (s *VODService) extractAllDefined(session *VODSession) {
	session.mu.Lock()
	for _, seg := range session.Segments {
		seg.mu.Lock()
		if seg.Status == SegmentRecording {
			seg.mu.Unlock()
			session.mu.Unlock()
			return
		}
		seg.mu.Unlock()
	}

	type extractJob struct {
		id, title, userID string
		start, end        float64
	}
	var jobs []extractJob
	for _, seg := range session.Segments {
		seg.mu.Lock()
		if seg.Status == SegmentDefined {
			seg.Status = SegmentExtracting
			end := 0.0
			if seg.EndOffset != nil {
				end = *seg.EndOffset
			}
			jobs = append(jobs, extractJob{
				id:     seg.ID,
				title:  seg.Title,
				userID: seg.UserID,
				start:  seg.StartOffset,
				end:    end,
			})
		}
		seg.mu.Unlock()
	}
	session.mu.Unlock()

	for _, j := range jobs {
		go s.extractSegment(session, j.id, j.start, j.end, j.title, j.userID)
	}
}

func (s *VODService) startSegmentDeadline(session *VODSession, seg *RecordingSegment) {
	seg.mu.Lock()
	stopAt := seg.StopAt
	segID := seg.ID
	seg.mu.Unlock()

	if stopAt.IsZero() {
		return
	}

	timer := time.NewTimer(time.Until(stopAt))
	defer timer.Stop()

	select {
	case <-timer.C:
	case <-session.ctx.Done():
		return
	}

	s.log.Info().Str("session_id", session.ID).Str("segment_id", segID).Msg("segment deadline reached, auto-closing")

	session.mu.Lock()
	seg.mu.Lock()
	if seg.Status != SegmentRecording {
		seg.mu.Unlock()
		session.mu.Unlock()
		return
	}

	buffered := s.ffmpegMgr.GetBufferedSecs(session.ProcessID)
	seg.EndOffset = &buffered
	seg.Status = SegmentDefined
	seg.mu.Unlock()
	session.mu.Unlock()
}

func (s *VODService) extractSegment(session *VODSession, segID string, start, end float64, title, userID string) {
	select {
	case <-session.ctx.Done():
		return
	default:
	}

	destDir := s.userRecordDir(userID)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		s.log.Error().Err(err).Str("segment_id", segID).Msg("failed to create record directory")
		s.updateSegmentStatus(session, segID, SegmentFailed)
		s.maybeCleanupSession(session)
		return
	}

	name := ffmpeg.SanitizeFilename(title, time.Now())
	outputPath := filepath.Join(destDir, name+".mp4")
	for i := 1; ; i++ {
		if _, err := os.Stat(outputPath); os.IsNotExist(err) {
			break
		}
		outputPath = filepath.Join(destDir, fmt.Sprintf("%s_%d.mp4", name, i))
	}

	err := s.ffmpegMgr.ExtractSegment(session.FilePath, outputPath, start, end)
	if err != nil {
		s.log.Error().Err(err).Str("segment_id", segID).Msg("segment extraction failed")
		s.updateSegmentStatus(session, segID, SegmentFailed)
	} else {
		s.log.Info().Str("segment_id", segID).Str("output", outputPath).Msg("segment saved")
		s.updateSegmentStatusWithPath(session, segID, SegmentCompleted, outputPath)
	}

	s.maybeCleanupSession(session)
}

func (s *VODService) updateSegmentStatus(session *VODSession, segID string, status SegmentStatus) {
	s.updateSegmentStatusWithPath(session, segID, status, "")
}

func (s *VODService) updateSegmentStatusWithPath(session *VODSession, segID string, status SegmentStatus, filePath string) {
	session.mu.Lock()
	defer session.mu.Unlock()
	seg, _ := findSegmentByID(session, segID)
	if seg == nil {
		return
	}
	seg.mu.Lock()
	seg.Status = status
	if filePath != "" {
		seg.FilePath = filePath
	}
	seg.mu.Unlock()
}

func (s *VODService) GetSegmentByID(sessionID, segmentID string) (*RecordingSegment, bool) {
	session, ok := s.getSession(sessionID)
	if !ok {
		return nil, false
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	seg, _ := findSegmentByID(session, segmentID)
	if seg == nil {
		return nil, false
	}
	seg.mu.Lock()
	snapshot := RecordingSegment{
		ID:          seg.ID,
		StartOffset: seg.StartOffset,
		EndOffset:   seg.EndOffset,
		Title:       seg.Title,
		ChannelName: seg.ChannelName,
		UserID:      seg.UserID,
		CreatedAt:   seg.CreatedAt,
		Status:      seg.Status,
		StopAt:      seg.StopAt,
		FilePath:    seg.FilePath,
	}
	seg.mu.Unlock()
	return &snapshot, true
}

func (s *VODService) maybeCleanupSession(session *VODSession) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session.mu.Lock()
	detached := session.Detached
	hasPending := session.HasPendingWork()
	processID := session.ProcessID
	sessionID := session.ID
	session.mu.Unlock()

	if !detached || hasPending {
		return
	}

	delete(s.sessions, sessionID)

	if s.activity != nil {
		s.activity.Remove(sessionID)
	}

	session.cancel()
	s.ffmpegMgr.Stop(processID)
	s.ffmpegMgr.Remove(processID)
	os.RemoveAll(session.TempDir)
	s.log.Info().Str("session_id", sessionID).Msg("detached session cleaned up after all segments completed")
}

func (s *VODService) CleanupExpired() {
	s.mu.Lock()
	var expired []string
	var sessionsToExtract []*VODSession
	for id, session := range s.sessions {
		session.mu.Lock()
		hasPending := session.HasPendingWork()
		isDetached := session.Detached
		lastAccess := session.LastAccess
		processID := session.ProcessID
		ffmpegDone := s.ffmpegMgr.IsReady(processID)

		if ffmpegDone && hasPending {
			buffered := s.ffmpegMgr.GetBufferedSecs(processID)
			for _, seg := range session.Segments {
				seg.mu.Lock()
				if seg.Status == SegmentRecording {
					seg.EndOffset = &buffered
					seg.Status = SegmentDefined
				}
				seg.mu.Unlock()
			}
			session.Detached = true
			session.mu.Unlock()
			sessionsToExtract = append(sessionsToExtract, session)
			continue
		}

		shouldExpire := false
		if isDetached {
			shouldExpire = !hasPending && ffmpegDone
		} else if !hasPending && time.Since(lastAccess) > s.config.VODSessionTimeout {
			shouldExpire = true
		}
		session.mu.Unlock()

		if shouldExpire {
			expired = append(expired, id)
		}
	}
	for _, id := range expired {
		session := s.sessions[id]
		delete(s.sessions, id)
		if s.activity != nil {
			s.activity.Remove(id)
		}

		session.mu.Lock()
		processID := session.ProcessID
		isDetached := session.Detached
		session.mu.Unlock()

		session.cancel()
		if !isDetached {
			s.ffmpegMgr.Stop(processID)
		}
		s.ffmpegMgr.Remove(processID)
		os.RemoveAll(session.TempDir)
		s.log.Info().Str("session_id", id).Bool("detached", isDetached).Msg("expired VOD session cleaned up")
	}
	s.mu.Unlock()

	for _, session := range sessionsToExtract {
		s.log.Info().Str("session_id", session.ID).Msg("ffmpeg terminated, extracting defined segments")
		s.extractAllDefined(session)
	}
}

func (s *VODService) userRecordDir(userID string) string {
	return filepath.Join(s.config.RecordDir, userID)
}

func (s *VODService) CreateRecordingSession(ctx context.Context, channelID, programTitle, channelName, userID string, stopAt time.Time) (*VODSession, *RecordingSegment, error) {
	existing, hasExisting := s.getSession(channelID)

	if hasExisting {
		seg, err := s.CreateSegment(channelID, programTitle, channelName, userID, 0, 0, stopAt)
		if err != nil && !errors.Is(err, ErrActiveSegmentExists) {
			return nil, nil, err
		}
		return existing, seg, nil
	}

	channel, err := s.channelRepo.GetByID(ctx, channelID)
	if err != nil {
		return nil, nil, fmt.Errorf("channel not found: %w", err)
	}
	if !channel.IsEnabled {
		return nil, nil, fmt.Errorf("channel %s is disabled", channelID)
	}

	channelStreams, err := s.channelRepo.GetStreams(ctx, channelID)
	if err != nil {
		return nil, nil, fmt.Errorf("getting channel streams: %w", err)
	}

	var streamURL, streamID string
	for _, cs := range channelStreams {
		stream, err := s.streamStore.GetByID(ctx, cs.StreamID)
		if err != nil || !stream.IsActive {
			continue
		}
		streamURL = stream.URL
		streamID = stream.ID
		break
	}
	if streamURL == "" {
		return nil, nil, fmt.Errorf("no active streams for channel %s", channelID)
	}

	tempDir := filepath.Join(s.config.VODTempDir, channelID)
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return nil, nil, fmt.Errorf("creating temp dir: %w", err)
	}

	filePath := filepath.Join(tempDir, "video.mp4")

	var profileArgs []string
	command := "ffmpeg"
	sp, err := s.streamProfileRepo.GetByName(ctx, "Recording")
	if err == nil && sp.Args != "" {
		profileArgs = ffmpeg.ShellSplit(sp.Args)
		command = sp.Command
	}

	processID := s.ffmpegMgr.Start(streamURL, filePath, tempDir, command, profileArgs)

	sessionCtx, sessionCancel := context.WithCancel(context.Background())
	session := &VODSession{
		ID:         channelID,
		ProcessID:  processID,
		StreamURL:  streamURL,
		FilePath:   filePath,
		TempDir:    tempDir,
		LastAccess: time.Now(),
		ctx:        sessionCtx,
		cancel:     sessionCancel,
	}

	s.mu.Lock()
	s.sessions[channelID] = session
	s.mu.Unlock()

	if s.activity != nil {
		s.activity.Add(ViewerOpts{
			ID:          channelID,
			ChannelID:   channelID,
			ChannelName: channelName,
			ProfileName: programTitle,
			Type:        "recording",
		})
	}

	s.writeRecordingIntent(tempDir, recordingIntent{
		ChannelID:    channelID,
		ChannelName:  channelName,
		ProgramTitle: programTitle,
		UserID:       userID,
		StopAt:       stopAt,
	})

	seg, _ := s.CreateSegment(channelID, programTitle, channelName, userID, 0, 0, stopAt)

	s.log.Info().Str("session_id", channelID).Str("stream_id", streamID).Str("program", programTitle).Str("user_id", userID).Msg("recording session created")
	return session, seg, nil
}

func (s *VODService) GetProfileName(sessionID string) string {
	session, ok := s.getSession(sessionID)
	if !ok {
		return ""
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.ProfileName
}

func (s *VODService) GetVideoInfo(sessionID string) *ffmpeg.VideoInfo {
	session, ok := s.getSession(sessionID)
	if !ok {
		return nil
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.Video
}

func (s *VODService) GetAudioTracks(sessionID string) []ffmpeg.AudioTrack {
	session, ok := s.getSession(sessionID)
	if !ok {
		return nil
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.AudioTracks
}

func (s *VODService) GetAudioIndex(sessionID string) int {
	session, ok := s.getSession(sessionID)
	if !ok {
		return 0
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.AudioIndex
}

func (s *VODService) GetDuration(sessionID string) float64 {
	session, ok := s.getSession(sessionID)
	if !ok {
		return 0
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.Duration
}

func (s *VODService) GetBufferedSecs(sessionID string) float64 {
	session, ok := s.getSession(sessionID)
	if !ok {
		return 0
	}
	return s.ffmpegMgr.GetBufferedSecs(session.ProcessID)
}

func (s *VODService) IsProcessReady(sessionID string) bool {
	session, ok := s.getSession(sessionID)
	if !ok {
		return true
	}
	return s.ffmpegMgr.IsReady(session.ProcessID)
}

func (s *VODService) GetProcessError(sessionID string) error {
	session, ok := s.getSession(sessionID)
	if !ok {
		return nil
	}
	return s.ffmpegMgr.GetError(session.ProcessID)
}

func (s *VODService) ListRecordings(userID string, isAdmin bool) []RecordingInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var list []RecordingInfo
	for _, session := range s.sessions {
		session.mu.Lock()
		if len(session.Segments) == 0 {
			session.mu.Unlock()
			continue
		}

		var firstTitle, firstChannel, firstUserID string
		var firstStopAt time.Time
		var segInfos []SegmentInfo

		for _, seg := range session.Segments {
			seg.mu.Lock()
			if !isAdmin && seg.UserID != userID {
				seg.mu.Unlock()
				continue
			}
			if firstTitle == "" {
				firstTitle = seg.Title
				firstChannel = seg.ChannelName
				firstUserID = seg.UserID
				firstStopAt = seg.StopAt
			}
			si := SegmentInfo{
				ID:          seg.ID,
				StartOffset: seg.StartOffset,
				EndOffset:   seg.EndOffset,
				Status:      string(seg.Status),
				Title:       seg.Title,
			}
			segInfos = append(segInfos, si)
			seg.mu.Unlock()
		}
		session.mu.Unlock()

		if len(segInfos) == 0 {
			continue
		}

		info := RecordingInfo{
			SessionID:    session.ID,
			ChannelName:  firstChannel,
			ProgramTitle: firstTitle,
			BufferedSecs: s.ffmpegMgr.GetBufferedSecs(session.ProcessID),
			UserID:       firstUserID,
			Segments:     segInfos,
		}
		if !firstStopAt.IsZero() {
			info.StopAt = firstStopAt.Format(time.RFC3339)
		}
		list = append(list, info)
	}
	return list
}

func (s *VODService) ListCompletedRecordings(userID string, isAdmin bool) ([]CompletedRecording, error) {
	if isAdmin {
		return s.listAllCompletedRecordings()
	}
	return s.listUserCompletedRecordings(userID)
}

func (s *VODService) listUserCompletedRecordings(userID string) ([]CompletedRecording, error) {
	dir := s.userRecordDir(userID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []CompletedRecording{}, nil
		}
		return nil, err
	}

	var list []CompletedRecording
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		list = append(list, CompletedRecording{
			Filename: e.Name(),
			Size:     info.Size(),
			ModTime:  info.ModTime().Format(time.RFC3339),
			UserID:   userID,
		})
	}
	return list, nil
}

func (s *VODService) listAllCompletedRecordings() ([]CompletedRecording, error) {
	entries, err := os.ReadDir(s.config.RecordDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []CompletedRecording{}, nil
		}
		return nil, err
	}

	var list []CompletedRecording

	for _, e := range entries {
		if e.IsDir() {
			subEntries, err := os.ReadDir(filepath.Join(s.config.RecordDir, e.Name()))
			if err != nil {
				continue
			}
			for _, se := range subEntries {
				if se.IsDir() {
					continue
				}
				info, err := se.Info()
				if err != nil {
					continue
				}
				list = append(list, CompletedRecording{
					Filename: se.Name(),
					Size:     info.Size(),
					ModTime:  info.ModTime().Format(time.RFC3339),
					UserID:   e.Name(),
				})
			}
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		list = append(list, CompletedRecording{
			Filename: e.Name(),
			Size:     info.Size(),
			ModTime:  info.ModTime().Format(time.RFC3339),
		})
	}
	return list, nil
}

func (s *VODService) GetCompletedRecordingPath(filename, userID string) (string, error) {
	if strings.Contains(filename, "/") || strings.Contains(filename, "\\") || filename == ".." || filename == "." {
		return "", ErrInvalidFilename
	}
	var dir string
	if userID == "" {
		dir = s.config.RecordDir
	} else {
		dir = s.userRecordDir(userID)
	}
	fullPath := filepath.Join(dir, filename)
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return "", ErrFileNotFound
	}
	return fullPath, nil
}

func (s *VODService) DeleteCompletedRecording(filename, userID string) error {
	fullPath, err := s.GetCompletedRecordingPath(filename, userID)
	if err != nil {
		return err
	}
	return os.Remove(fullPath)
}

func (s *VODService) IsRecording(sessionID string) bool {
	session, ok := s.getSession(sessionID)
	if !ok {
		return false
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.HasActiveSegment()
}

func (s *VODService) GetSegments(sessionID string) []SegmentInfo {
	session, ok := s.getSession(sessionID)
	if !ok {
		return nil
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	var infos []SegmentInfo
	for _, seg := range session.Segments {
		seg.mu.Lock()
		infos = append(infos, SegmentInfo{
			ID:          seg.ID,
			StartOffset: seg.StartOffset,
			EndOffset:   seg.EndOffset,
			Status:      string(seg.Status),
			Title:       seg.Title,
		})
		seg.mu.Unlock()
	}
	return infos
}

func (s *VODService) Shutdown() {
	s.mu.Lock()
	sessions := make([]*VODSession, 0, len(s.sessions))
	for _, session := range s.sessions {
		sessions = append(sessions, session)
	}
	s.sessions = make(map[string]*VODSession)
	s.mu.Unlock()

	for _, session := range sessions {
		session.cancel()
		s.ffmpegMgr.Stop(session.ProcessID)
		s.ffmpegMgr.Wait(session.ProcessID)
		s.ffmpegMgr.Remove(session.ProcessID)
	}

	s.log.Info().Int("sessions", len(sessions)).Msg("VOD service shutdown complete")
}
