package service

import (
	"context"
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
)

type VODSession struct {
	ID           string
	ProcessID    string
	StreamURL    string
	Duration     float64
	FilePath     string
	TempDir      string
	LastAccess   time.Time
	Recording    bool
	Detached     bool
	RecordName   string
	ChannelName  string
	ProgramTitle string
	UserID       string
	mu           sync.Mutex
}

type RecordingInfo struct {
	SessionID    string  `json:"session_id"`
	ChannelName  string  `json:"channel_name"`
	ProgramTitle string  `json:"program_title"`
	BufferedSecs float64 `json:"buffered_secs"`
	StopAt       string  `json:"stop_at,omitempty"`
	UserID       string  `json:"user_id"`
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
	streamRepo        *repository.StreamRepository
	streamProfileRepo *repository.StreamProfileRepository
	ffmpegMgr         *FFmpegManager
	log               zerolog.Logger
	mu                sync.RWMutex
	sessions          map[string]*VODSession
}

func NewVODService(
	channelRepo *repository.ChannelRepository,
	streamRepo *repository.StreamRepository,
	streamProfileRepo *repository.StreamProfileRepository,
	ffmpegMgr *FFmpegManager,
	cfg *config.Config,
	log zerolog.Logger,
) *VODService {
	return &VODService{
		config:            cfg,
		channelRepo:       channelRepo,
		streamRepo:        streamRepo,
		streamProfileRepo: streamProfileRepo,
		ffmpegMgr:         ffmpegMgr,
		log:               log.With().Str("service", "vod").Logger(),
		sessions:          make(map[string]*VODSession),
	}
}

func (s *VODService) ProbeStream(ctx context.Context, streamID string) (*ffmpeg.ProbeResult, error) {
	stream, err := s.streamRepo.GetByID(ctx, streamID)
	if err != nil {
		return nil, fmt.Errorf("stream not found: %w", err)
	}
	return ffmpeg.Probe(ctx, stream.URL, s.config.UserAgent)
}

func (s *VODService) CreateSession(ctx context.Context, streamID string, profileName string) (*VODSession, error) {
	stream, err := s.streamRepo.GetByID(ctx, streamID)
	if err != nil {
		return nil, fmt.Errorf("stream not found: %w", err)
	}
	if !stream.IsActive {
		return nil, fmt.Errorf("stream %s is inactive", streamID)
	}
	return s.createSessionForURL(ctx, uuid.New().String(), stream.URL, stream.ID, profileName)
}

func (s *VODService) CreateSessionForChannel(ctx context.Context, channelID string, profileName string) (*VODSession, error) {
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
		stream, err := s.streamRepo.GetByID(ctx, cs.StreamID)
		if err != nil || !stream.IsActive {
			continue
		}
		return s.createSessionForURL(ctx, channelID, stream.URL, stream.ID, profileName)
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

	if !s.ffmpegMgr.IsReady(existing.ProcessID) {
		existing.mu.Lock()
		existing.Detached = false
		existing.LastAccess = time.Now()
		existing.mu.Unlock()
		s.log.Info().Str("session_id", channelID).Msg("reattached player to existing session")
		return existing
	}

	s.ffmpegMgr.Remove(existing.ProcessID)
	os.RemoveAll(existing.TempDir)
	delete(s.sessions, channelID)
	return nil
}

func (s *VODService) createSessionForURL(ctx context.Context, id string, streamURL string, streamID string, profileName string) (*VODSession, error) {
	var duration float64
	probe, err := ffmpeg.Probe(ctx, streamURL, s.config.UserAgent)
	if err == nil && probe.IsVOD {
		duration = probe.Duration
	}

	var profileArgs []string
	command := "ffmpeg"
	if profileName != "" {
		sp, err := s.streamProfileRepo.GetByName(ctx, profileName)
		if err != nil {
			return nil, fmt.Errorf("profile %q not found: %w", profileName, err)
		}
		if sp.Args != "" {
			profileArgs = ShellSplit(sp.Args)
			command = sp.Command
		}
	}

	tempDir := filepath.Join(s.config.VODTempDir, id)
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}

	filePath := filepath.Join(tempDir, "video.mp4")
	os.Remove(filePath)

	processID := s.ffmpegMgr.Start(streamURL, filePath, tempDir, command, profileArgs)

	session := &VODSession{
		ID:         id,
		ProcessID:  processID,
		StreamURL:  streamURL,
		Duration:   duration,
		FilePath:   filePath,
		TempDir:    tempDir,
		LastAccess: time.Now(),
	}

	s.mu.Lock()
	s.sessions[id] = session
	s.mu.Unlock()

	probeDur := 0.0
	if probe != nil {
		probeDur = probe.Duration
	}
	s.log.Info().Str("session_id", id).Str("stream_id", streamID).Float64("duration", probeDur).Msg("VOD session created")
	return session, nil
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
	var f *os.File
	for i := 0; i < 50; i++ {
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
		case <-time.After(200 * time.Millisecond):
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

func (s *VODService) GetSession(id string) (*VODSession, bool) {
	s.mu.RLock()
	session, ok := s.sessions[id]
	s.mu.RUnlock()
	if ok {
		session.mu.Lock()
		session.LastAccess = time.Now()
		session.mu.Unlock()
	}
	return session, ok
}

func (s *VODService) DeleteSession(id string) {
	s.mu.Lock()
	session, ok := s.sessions[id]
	if !ok {
		s.mu.Unlock()
		return
	}

	session.mu.Lock()
	isRecording := session.Recording
	processID := session.ProcessID
	session.mu.Unlock()

	if isRecording {
		session.mu.Lock()
		session.Detached = true
		session.mu.Unlock()
		s.mu.Unlock()
		s.log.Info().Str("session_id", id).Msg("player closed, recording continues (detached)")
		return
	}

	delete(s.sessions, id)
	s.mu.Unlock()

	s.ffmpegMgr.Stop(processID)
	s.ffmpegMgr.Remove(processID)
	os.RemoveAll(session.TempDir)
	s.log.Info().Str("session_id", id).Msg("VOD session deleted")
}

func (s *VODService) StopRecording(sessionID, userID string, isAdmin bool) error {
	s.mu.Lock()
	session, ok := s.sessions[sessionID]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("session not found")
	}

	session.mu.Lock()
	if !session.Recording {
		session.mu.Unlock()
		s.mu.Unlock()
		return fmt.Errorf("session is not recording")
	}
	if !isAdmin && session.UserID != userID {
		session.mu.Unlock()
		s.mu.Unlock()
		return fmt.Errorf("not authorized")
	}
	processID := session.ProcessID
	tempDir := session.TempDir
	session.mu.Unlock()

	delete(s.sessions, sessionID)
	s.mu.Unlock()

	s.log.Info().Str("session_id", sessionID).Msg("stopping recording, archiving file")

	go func() {
		s.ffmpegMgr.StopAndArchive(processID)
		s.ffmpegMgr.Wait(processID)
		s.ffmpegMgr.Remove(processID)
		os.RemoveAll(tempDir)
		s.log.Info().Str("session_id", sessionID).Msg("recording archived")
	}()

	return nil
}

func (s *VODService) CancelRecording(sessionID, userID string, isAdmin bool) error {
	s.mu.Lock()
	session, ok := s.sessions[sessionID]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("session not found")
	}

	session.mu.Lock()
	if !session.Recording {
		session.mu.Unlock()
		s.mu.Unlock()
		return fmt.Errorf("session is not recording")
	}
	if !isAdmin && session.UserID != userID {
		session.mu.Unlock()
		s.mu.Unlock()
		return fmt.Errorf("not authorized")
	}
	processID := session.ProcessID
	tempDir := session.TempDir
	session.mu.Unlock()

	delete(s.sessions, sessionID)
	s.mu.Unlock()

	s.log.Info().Str("session_id", sessionID).Msg("cancelling recording, discarding file")

	go func() {
		s.ffmpegMgr.Stop(processID)
		s.ffmpegMgr.Wait(processID)
		s.ffmpegMgr.Remove(processID)
		os.RemoveAll(tempDir)
		s.log.Info().Str("session_id", sessionID).Msg("recording cancelled and discarded")
	}()

	return nil
}

func (s *VODService) CleanupExpired() {
	s.mu.Lock()
	var expired []string
	for id, session := range s.sessions {
		session.mu.Lock()
		isRec := session.Recording
		isDetached := session.Detached
		lastAccess := session.LastAccess
		processID := session.ProcessID
		session.mu.Unlock()

		if isDetached {
			if s.ffmpegMgr.IsReady(processID) {
				expired = append(expired, id)
			}
			continue
		}
		if isRec {
			continue
		}
		if time.Since(lastAccess) > s.config.VODSessionTimeout {
			expired = append(expired, id)
		}
	}
	for _, id := range expired {
		session := s.sessions[id]
		delete(s.sessions, id)

		session.mu.Lock()
		processID := session.ProcessID
		isDetached := session.Detached
		session.mu.Unlock()

		if !isDetached {
			s.ffmpegMgr.Stop(processID)
		}
		s.ffmpegMgr.Remove(processID)
		os.RemoveAll(session.TempDir)
		s.log.Info().Str("session_id", id).Bool("detached", isDetached).Msg("expired VOD session cleaned up")
	}
	s.mu.Unlock()
}

func (s *VODService) userRecordDir(userID string) string {
	return filepath.Join(s.config.RecordDir, userID)
}

func (s *VODService) MarkRecording(sessionID, programTitle, channelName, userID string, stopAt time.Time) error {
	session, ok := s.GetSession(sessionID)
	if !ok {
		return fmt.Errorf("session not found")
	}

	session.mu.Lock()
	if session.Recording {
		session.mu.Unlock()
		return fmt.Errorf("session is already recording")
	}
	session.Recording = true
	session.ProgramTitle = programTitle
	session.ChannelName = channelName
	session.UserID = userID
	recordName := mgrSanitizeFilename(programTitle, time.Now())
	session.RecordName = recordName
	if stopAt.IsZero() {
		stopAt = time.Now().Add(s.config.RecordDefaultDuration)
	}
	processID := session.ProcessID
	session.mu.Unlock()

	archiveDir := s.userRecordDir(userID)
	s.ffmpegMgr.MarkForArchival(processID, recordName, archiveDir, stopAt)

	s.log.Info().Str("session_id", sessionID).Str("program", programTitle).Str("user_id", userID).Time("stop_at", stopAt).Msg("session marked for recording")
	return nil
}

func (s *VODService) CreateRecordingSession(ctx context.Context, channelID, programTitle, channelName, userID string, stopAt time.Time) (*VODSession, error) {
	s.mu.RLock()
	existing, hasExisting := s.sessions[channelID]
	s.mu.RUnlock()

	if hasExisting {
		err := s.MarkRecording(channelID, programTitle, channelName, userID, stopAt)
		if err != nil && err.Error() != "session is already recording" {
			return nil, err
		}
		return existing, nil
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

	var streamURL, streamID string
	for _, cs := range channelStreams {
		stream, err := s.streamRepo.GetByID(ctx, cs.StreamID)
		if err != nil || !stream.IsActive {
			continue
		}
		streamURL = stream.URL
		streamID = stream.ID
		break
	}
	if streamURL == "" {
		return nil, fmt.Errorf("no active streams for channel %s", channelID)
	}

	tempDir := filepath.Join(s.config.VODTempDir, channelID)
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}

	filePath := filepath.Join(tempDir, "video.mp4")

	if stopAt.IsZero() {
		stopAt = time.Now().Add(s.config.RecordDefaultDuration)
	}

	recordName := mgrSanitizeFilename(programTitle, time.Now())
	archiveDir := s.userRecordDir(userID)

	processID := s.ffmpegMgr.Start(streamURL, filePath, tempDir, "ffmpeg", nil)
	s.ffmpegMgr.MarkForArchival(processID, recordName, archiveDir, stopAt)

	session := &VODSession{
		ID:           channelID,
		ProcessID:    processID,
		StreamURL:    streamURL,
		FilePath:     filePath,
		TempDir:      tempDir,
		LastAccess:   time.Now(),
		Recording:    true,
		RecordName:   recordName,
		ChannelName:  channelName,
		ProgramTitle: programTitle,
		UserID:       userID,
	}

	s.mu.Lock()
	s.sessions[channelID] = session
	s.mu.Unlock()

	s.log.Info().Str("session_id", channelID).Str("stream_id", streamID).Str("program", programTitle).Str("user_id", userID).Time("stop_at", stopAt).Msg("recording session created")
	return session, nil
}

func (s *VODService) GetBufferedSecs(sessionID string) float64 {
	s.mu.RLock()
	session, ok := s.sessions[sessionID]
	s.mu.RUnlock()
	if !ok {
		return 0
	}
	return s.ffmpegMgr.GetBufferedSecs(session.ProcessID)
}

func (s *VODService) IsProcessReady(sessionID string) bool {
	s.mu.RLock()
	session, ok := s.sessions[sessionID]
	s.mu.RUnlock()
	if !ok {
		return true
	}
	return s.ffmpegMgr.IsReady(session.ProcessID)
}

func (s *VODService) GetProcessError(sessionID string) error {
	s.mu.RLock()
	session, ok := s.sessions[sessionID]
	s.mu.RUnlock()
	if !ok {
		return nil
	}
	return s.ffmpegMgr.GetError(session.ProcessID)
}

func (s *VODService) ListRecordings(userID string, isAdmin bool) []RecordingInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	archiveList := s.ffmpegMgr.ListArchiving()
	archiveByID := make(map[string]ArchiveInfo, len(archiveList))
	for _, a := range archiveList {
		archiveByID[a.ProcessID] = a
	}

	var list []RecordingInfo
	for _, session := range s.sessions {
		session.mu.Lock()
		if session.Recording {
			if !isAdmin && session.UserID != userID {
				session.mu.Unlock()
				continue
			}
			info := RecordingInfo{
				SessionID:    session.ID,
				ChannelName:  session.ChannelName,
				ProgramTitle: session.ProgramTitle,
				BufferedSecs: s.ffmpegMgr.GetBufferedSecs(session.ProcessID),
				UserID:       session.UserID,
			}
			if a, ok := archiveByID[session.ProcessID]; ok && a.StopAt != "" {
				info.StopAt = a.StopAt
			}
			list = append(list, info)
		}
		session.mu.Unlock()
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
		return "", fmt.Errorf("invalid filename")
	}
	var dir string
	if userID == "" {
		dir = s.config.RecordDir
	} else {
		dir = s.userRecordDir(userID)
	}
	fullPath := filepath.Join(dir, filename)
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return "", fmt.Errorf("file not found")
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
	s.mu.RLock()
	session, ok := s.sessions[sessionID]
	s.mu.RUnlock()
	if !ok {
		return false
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.Recording
}
