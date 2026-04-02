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

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"time"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/config"
	"github.com/gavinmcnair/tvproxy/pkg/ffmpeg"
	"github.com/gavinmcnair/tvproxy/pkg/session"
	"github.com/gavinmcnair/tvproxy/pkg/store"
)

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrNotAuthorized   = errors.New("not authorized")
	ErrInvalidFilename = errors.New("invalid filename")
	ErrFileNotFound    = errors.New("file not found")
	ErrStreamNotFound  = errors.New("stream not found")
	ErrAlreadyRecording = errors.New("channel already recording")
)

type recordingIntent struct {
	ChannelID    string    `json:"channel_id"`
	StreamID     string    `json:"stream_id"`
	ChannelName  string    `json:"channel_name"`
	ProgramTitle string    `json:"program_title"`
	UserID       string    `json:"user_id"`
	StopAt       time.Time `json:"stop_at"`
}

const recordingIntentFile = "recording.json"

type recordingState struct {
	ConsumerID string
	Title      string
	UserID     string
	StartedAt  time.Time
	StopAt     time.Time
	Timer      *time.Timer
}

type VODService struct {
	config            *config.Config
	channelStore      store.ChannelStore
	streamStore       store.StreamReader
	streamProfileRepo store.ProfileStore
	settingsService   *SettingsService
	sessionMgr        *session.Manager
	recordingStore    store.RecordingStore
	activity          *ActivityService
	log               zerolog.Logger

	mu         sync.RWMutex
	recordings map[string]*recordingState
}

func NewVODService(
	channelStore store.ChannelStore,
	streamStore store.StreamReader,
	streamProfileRepo store.ProfileStore,
	settingsService *SettingsService,
	sessionMgr *session.Manager,
	recordingStore store.RecordingStore,
	activity *ActivityService,
	cfg *config.Config,
	log zerolog.Logger,
) *VODService {
	return &VODService{
		config:            cfg,
		channelStore:      channelStore,
		streamStore:       streamStore,
		streamProfileRepo: streamProfileRepo,
		settingsService:   settingsService,
		sessionMgr:        sessionMgr,
		recordingStore:    recordingStore,
		activity:          activity,
		log:               log.With().Str("service", "vod").Logger(),
		recordings:        make(map[string]*recordingState),
	}
}

func (s *VODService) resolveStreamForChannel(ctx context.Context, channelID string) (streamURL, streamName, channelName, streamID, streamGroup string, err error) {
	channel, err := s.channelStore.GetByID(ctx, channelID)
	if err != nil {
		return "", "", "", "", "", fmt.Errorf("channel not found: %w", err)
	}
	if !channel.IsEnabled {
		return "", "", "", "", "", fmt.Errorf("channel %s is disabled", channelID)
	}

	channelStreams, err := s.channelStore.GetStreams(ctx, channelID)
	if err != nil {
		return "", "", "", "", "", fmt.Errorf("getting channel streams: %w", err)
	}

	for _, cs := range channelStreams {
		stream, err := s.streamStore.GetByID(ctx, cs.StreamID)
		if err != nil || !stream.IsActive {
			continue
		}
		return stream.URL, stream.Name, channel.Name, cs.StreamID, stream.Group, nil
	}

	return "", "", "", "", "", fmt.Errorf("no active streams for channel %s", channelID)
}

func (s *VODService) composeSessionArgs(ctx context.Context, profileName, streamURL, streamGroup string) (string, string, string) {
	if profileName == "" {
		return "ffmpeg", "", "mp4"
	}
	sp, err := s.streamProfileRepo.GetByName(ctx, profileName)
	if err != nil {
		return "ffmpeg", "", "mp4"
	}

	globalHW, globalCodec := s.settingsService.ResolveGlobalDefaults(ctx)
	hwaccel := sp.HWAccel
	if hwaccel == "default" || hwaccel == "" {
		hwaccel = globalHW
	}
	videoCodec := sp.VideoCodec
	if videoCodec == "default" || videoCodec == "" {
		videoCodec = globalCodec
	}

	var probe *ffmpeg.ProbeResult
	if streamURL != "" {
		probe, _ = s.recordingStore.GetProbe(ffmpeg.StreamHash(streamURL))
	}

	audioOnly := strings.EqualFold(streamGroup, "radio")
	command, args := ffmpeg.Build(ffmpeg.BuildOptions{
		StreamURL:     streamURL,
		Probe:         probe,
		Container:     sp.Container,
		Delivery:      sp.Delivery,
		HWAccel:       hwaccel,
		VideoCodec:    videoCodec,
		AudioCodec:    sp.AudioCodec,
		AudioOnly:     audioOnly,
		CustomCommand: sp.Args,
	})
	return command, args, sp.Container
}

func (s *VODService) StartWatching(ctx context.Context, channelID string, profileName string, userAgent string, remoteAddr string) (string, string, string, bool, error) {
	streamURL, streamName, channelName, streamID, streamGroup, err := s.resolveStreamForChannel(ctx, channelID)
	if err != nil {
		return "", "", "", false, err
	}

	audioOnly := strings.EqualFold(streamGroup, "radio")
	command, args, container := s.composeSessionArgs(ctx, profileName, streamURL, streamGroup)

	_, consumerID, err := s.sessionMgr.GetOrCreateWithConsumer(ctx, session.StartOpts{
		ChannelID:   channelID,
		StreamID:    streamID,
		StreamURL:   streamURL,
		StreamName:  streamName,
		ChannelName: channelName,
		ProfileName: profileName,
		Command:     command,
		Args:        args,
		OutputDir:   s.config.VODOutputDir,
	}, session.ConsumerViewer)
	if err != nil {
		return "", "", "", false, err
	}

	s.log.Info().Str("channel_id", channelID).Str("profile", profileName).Str("container", container).Str("user_agent", userAgent).Str("remote", remoteAddr).Msg("viewer started")

	if s.activity != nil {
		s.activity.Add(ViewerOpts{
			ID:          consumerID,
			ChannelID:   channelID,
			ChannelName: channelName,
			ProfileName: profileName,
			UserAgent:   userAgent,
			RemoteAddr:  remoteAddr,
			Type:        "vod",
		})
	}

	return channelID, consumerID, container, audioOnly, nil
}

func (s *VODService) StartWatchingStream(ctx context.Context, streamID string, profileName string, userAgent string, remoteAddr string) (string, string, string, error) {
	stream, err := s.streamStore.GetByID(ctx, streamID)
	if err != nil {
		return "", "", "", fmt.Errorf("%w: %w", ErrStreamNotFound, err)
	}
	if !stream.IsActive {
		return "", "", "", fmt.Errorf("stream %s is inactive", streamID)
	}

	streamURL := stream.URL
	if strings.EqualFold(stream.Group, "radio") {
		streamURL = audioOnlyURL(*stream)
	}

	command, args, container := s.composeSessionArgs(ctx, profileName, streamURL, stream.Group)

	_, consumerID, err := s.sessionMgr.GetOrCreateWithConsumer(ctx, session.StartOpts{
		ChannelID:   streamID,
		StreamID:    streamID,
		StreamURL:   streamURL,
		StreamName:  stream.Name,
		ChannelName: stream.Name,
		ProfileName: profileName,
		Command:     command,
		Args:        args,
		OutputDir:   s.config.VODOutputDir,
	}, session.ConsumerViewer)
	if err != nil {
		return "", "", "", err
	}

	s.log.Info().Str("stream_id", streamID).Str("profile", profileName).Str("container", container).Str("user_agent", userAgent).Str("remote", remoteAddr).Msg("viewer started")

	if s.activity != nil {
		s.activity.Add(ViewerOpts{
			ID:           consumerID,
			StreamID:     streamID,
			StreamName:   stream.Name,
			M3UAccountID: stream.M3UAccountID,
			ProfileName:  profileName,
			UserAgent:    userAgent,
			RemoteAddr:   remoteAddr,
			Type:         "vod",
		})
	}

	return streamID, consumerID, container, nil
}

func (s *VODService) StopWatching(channelID string, consumerID string) {
	if s.activity != nil {
		s.activity.Remove(consumerID)
	}
	s.sessionMgr.RemoveConsumer(channelID, consumerID)
}

func (s *VODService) TailSession(ctx context.Context, channelID string) (io.ReadCloser, error) {
	return s.sessionMgr.TailFile(ctx, channelID)
}

func (s *VODService) StartRecording(ctx context.Context, channelID, title, channelName, userID string, stopAt time.Time) error {
	if !stopAt.IsZero() {
		stopAt = stopAt.Add(s.config.RecordStopBuffer)
	} else {
		stopAt = time.Now().Add(s.config.RecordDefaultDuration)
	}
	return s.startRecordingInternal(ctx, channelID, title, channelName, userID, stopAt)
}

func (s *VODService) startRecordingInternal(ctx context.Context, channelID, title, channelName, userID string, stopAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.recordings[channelID]; exists {
		return ErrAlreadyRecording
	}

	streamURL, streamName, resolvedChannelName, streamID, _, err := s.resolveStreamForChannel(ctx, channelID)
	if err != nil {
		return err
	}
	if channelName == "" {
		channelName = resolvedChannelName
	}

	defaultHWAccel, defaultCodec := s.settingsService.ResolveGlobalDefaults(ctx)

	var probe *ffmpeg.ProbeResult
	if streamURL != "" {
		probe, _ = s.recordingStore.GetProbe(ffmpeg.StreamHash(streamURL))
	}

	command, args := ffmpeg.Build(ffmpeg.BuildOptions{
		StreamURL:  streamURL,
		Probe:      probe,
		Container:  "mp4",
		HWAccel:    defaultHWAccel,
		VideoCodec: defaultCodec,
	})

	sess, consumerID, err := s.sessionMgr.GetOrCreateWithConsumer(ctx, session.StartOpts{
		ChannelID:   channelID,
		StreamID:    streamID,
		StreamURL:   streamURL,
		StreamName:  streamName,
		ChannelName: channelName,
		ProfileName: session.ConsumerRecording,
		Command:     command,
		Args:        args,
		OutputDir:   s.config.VODOutputDir,
	}, session.ConsumerRecording)
	if err != nil {
		return err
	}

	rs := &recordingState{
		ConsumerID: consumerID,
		Title:      title,
		UserID:     userID,
		StartedAt:  time.Now(),
		StopAt:     stopAt,
	}

	rs.Timer = time.AfterFunc(time.Until(stopAt), func() {
		s.log.Info().Str("channel_id", channelID).Msg("recording deadline reached, auto-stopping")
		s.stopRecordingInternal(channelID)
	})

	s.recordings[channelID] = rs

	if sess != nil {
		s.writeRecordingIntent(sess.TempDir, recordingIntent{
			ChannelID:    channelID,
			StreamID:     streamID,
			ChannelName:  channelName,
			ProgramTitle: title,
			UserID:       userID,
			StopAt:       stopAt,
		})
	}

	if s.activity != nil {
		s.activity.Add(ViewerOpts{
			ID:          consumerID,
			ChannelID:   channelID,
			ChannelName: channelName,
			ProfileName: title,
			Type: session.ConsumerRecording,
		})
	}

	s.log.Info().Str("channel_id", channelID).Str("title", title).Str("user_id", userID).Time("stop_at", stopAt).Msg("recording started")
	return nil
}

func (s *VODService) StopRecording(channelID string) error {
	return s.stopRecordingInternal(channelID)
}

func (s *VODService) stopRecordingInternal(channelID string) error {
	s.mu.Lock()
	rs, exists := s.recordings[channelID]
	if !exists {
		s.mu.Unlock()
		return fmt.Errorf("no recording for channel %s", channelID)
	}
	delete(s.recordings, channelID)
	s.mu.Unlock()

	if rs.Timer != nil {
		rs.Timer.Stop()
	}

	sess := s.sessionMgr.Get(channelID)
	if sess != nil {
		s.saveRecording(sess, rs)
		s.removeRecordingIntent(sess.TempDir)
	}

	if s.activity != nil {
		s.activity.Remove(rs.ConsumerID)
	}

	s.sessionMgr.RemoveConsumer(channelID, rs.ConsumerID)

	s.log.Info().Str("channel_id", channelID).Str("title", rs.Title).Msg("recording stopped")
	return nil
}

func (s *VODService) saveRecording(sess *session.Session, rs *recordingState) {
	tempPath := filepath.Join(os.TempDir(), fmt.Sprintf("tvproxy-remux-%d.mp4", time.Now().UnixNano()))
	args := []string{
		"-hide_banner", "-loglevel", "warning",
		"-i", sess.FilePath,
		"-c", "copy",
		"-f", "mp4",
		tempPath,
	}

	cmd := exec.Command("ffmpeg", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		s.log.Error().Err(err).Str("output", string(output)).Msg("failed to remux recording")
		os.Remove(tempPath)
		return
	}
	defer os.Remove(tempPath)

	meta := store.RecordingMeta{
		StreamID:     sess.StreamID,
		StreamName:   sess.StreamName,
		ChannelID:    sess.ChannelID,
		ChannelName:  sess.ChannelName,
		ProgramTitle: rs.Title,
		UserID:       rs.UserID,
		StartedAt:    rs.StartedAt,
		StoppedAt:    time.Now(),
	}

	streamID := sess.StreamID
	if streamID == "" {
		streamID = sess.ChannelID
	}

	filename, err := s.recordingStore.Save(streamID, tempPath, meta)
	if err != nil {
		s.log.Error().Err(err).Msg("failed to save recording")
		return
	}

	s.log.Info().Str("stream_id", streamID).Str("filename", filename).Msg("recording saved")
}

func (s *VODService) IsRecording(channelID string) bool {
	s.mu.RLock()
	_, exists := s.recordings[channelID]
	s.mu.RUnlock()
	return exists
}

func (s *VODService) GetSession(channelID string) *session.Session {
	return s.sessionMgr.Get(channelID)
}

func (s *VODService) GetBufferedSecs(channelID string) float64 {
	return s.sessionMgr.GetBufferedSecs(channelID)
}

func (s *VODService) IsDone(channelID string) bool {
	return s.sessionMgr.IsDone(channelID)
}

func (s *VODService) ConsumerCount(channelID string) int {
	return s.sessionMgr.ConsumerCount(channelID)
}

func (s *VODService) GetError(channelID string) error {
	return s.sessionMgr.GetError(channelID)
}

func (s *VODService) GetProbeInfo(channelID string) (*ffmpeg.VideoInfo, []ffmpeg.AudioTrack, float64) {
	sess := s.sessionMgr.Get(channelID)
	if sess == nil {
		return nil, nil, 0
	}
	return sess.GetProbeInfo()
}

func (s *VODService) ProbeFile(ctx context.Context, streamURL, filePath string) (*ffmpeg.ProbeResult, error) {
	if streamURL != "" {
		cached, _ := s.recordingStore.GetProbe(ffmpeg.StreamHash(streamURL))
		if cached != nil {
			return cached, nil
		}
	}
	result, err := ffmpeg.Probe(ctx, filePath, "")
	if err != nil {
		return nil, err
	}
	if streamURL != "" && result != nil {
		s.recordingStore.SaveProbe(ffmpeg.StreamHash(streamURL), result)
	}
	return result, nil
}

func (s *VODService) ProbeStream(ctx context.Context, streamID string) (*ffmpeg.ProbeResult, error) {
	stream, err := s.streamStore.GetByID(ctx, streamID)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrStreamNotFound, err)
	}
	return ffmpeg.Probe(ctx, stream.URL, s.config.UserAgent)
}

func (s *VODService) TranscodeFile(ctx context.Context, filePath, profileName string) (io.ReadCloser, string, error) {
	sp, err := s.streamProfileRepo.GetByName(ctx, profileName)
	if err != nil {
		return nil, "", fmt.Errorf("profile %q not found: %w", profileName, err)
	}

	if sp.Args == "" {
		f, err := os.Open(filePath)
		if err != nil {
			return nil, "", err
		}
		return f, "video/mp4", nil
	}

	probe, probeErr := ffmpeg.Probe(ctx, filePath, "")
	if probeErr == nil && probe.Video != nil {
		fileCodec := ffmpeg.NormalizeVideoCodec(probe.Video.Codec)
		videoMatch := sp.VideoCodec == "copy" || sp.VideoCodec == fileCodec
		containerMatch := sp.Container == probe.FormatName
		if videoMatch && containerMatch {
			f, err := os.Open(filePath)
			if err != nil {
				return nil, "", err
			}
			return f, containerContentType(probe.FormatName), nil
		}
	}

	args := ffmpeg.ShellSplit(sp.Args)
	for i, arg := range args {
		if arg == "{input}" {
			args[i] = filePath
		}
	}
	args = append([]string{"-y"}, args...)

	cmd := exec.CommandContext(ctx, sp.Command, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, "", fmt.Errorf("creating stdout pipe: %w", err)
	}
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return nil, "", fmt.Errorf("starting ffmpeg transcode: %w", err)
	}

	contentType := containerContentType(sp.Container)
	return &cmdReadCloser{ReadCloser: stdout, cmd: cmd}, contentType, nil
}

func containerContentType(container string) string {
	switch container {
	case "mp4":
		return "video/mp4"
	case "mpegts":
		return "video/MP2T"
	case "matroska":
		return "video/x-matroska"
	case "webm":
		return "video/webm"
	default:
		return "video/mp4"
	}
}

type cmdReadCloser struct {
	io.ReadCloser
	cmd *exec.Cmd
}

func (c *cmdReadCloser) Close() error {
	c.ReadCloser.Close()
	return c.cmd.Wait()
}

func (s *VODService) ListCompletedRecordings(userID string, isAdmin bool) ([]store.RecordingEntry, error) {
	return s.recordingStore.List(userID, isAdmin)
}

func (s *VODService) GetCompletedRecordingPath(streamID, filename, userID string, isAdmin bool) (string, error) {
	if !isAdmin {
		meta, err := s.recordingStore.GetMeta(streamID, filename)
		if err != nil {
			return "", err
		}
		if meta == nil || (meta.UserID != "" && meta.UserID != userID) {
			return "", ErrNotAuthorized
		}
	}
	return s.recordingStore.FilePath(streamID, filename)
}

func (s *VODService) DeleteCompletedRecording(streamID, filename, userID string, isAdmin bool) error {
	if !isAdmin {
		meta, err := s.recordingStore.GetMeta(streamID, filename)
		if err != nil {
			return err
		}
		if meta == nil || (meta.UserID != "" && meta.UserID != userID) {
			return ErrNotAuthorized
		}
	}
	return s.recordingStore.Delete(streamID, filename)
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
	vodDir := s.config.VODOutputDir
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
		if err := s.startRecordingInternal(ctx, intent.ChannelID, intent.ProgramTitle, intent.ChannelName, intent.UserID, intent.StopAt); err != nil {
			s.log.Error().Err(err).Str("channel", intent.ChannelName).Msg("failed to recover recording")
			os.RemoveAll(filepath.Join(vodDir, entry.Name()))
		}
	}
}

func (s *VODService) Shutdown() {
	s.mu.Lock()
	for channelID, rs := range s.recordings {
		if rs.Timer != nil {
			rs.Timer.Stop()
		}
		delete(s.recordings, channelID)
	}
	s.mu.Unlock()

	s.sessionMgr.Shutdown()
	s.log.Info().Msg("VOD service shutdown complete")
}

func audioOnlyURL(stream models.Stream) string {
	if len(stream.Tracks) == 0 {
		return stream.URL
	}
	var audioPIDs []string
	for _, t := range stream.Tracks {
		if t.Category == "audio" {
			audioPIDs = append(audioPIDs, fmt.Sprintf("%d", t.PID))
		}
	}
	if len(audioPIDs) == 0 {
		return stream.URL
	}
	// Replace pids= in the URL with just PAT (0) + audio PIDs
	idx := strings.Index(stream.URL, "pids=")
	if idx < 0 {
		return stream.URL
	}
	end := strings.Index(stream.URL[idx:], "&")
	var base string
	if end < 0 {
		base = stream.URL[:idx]
	} else {
		base = stream.URL[:idx] + stream.URL[idx+end+1:]
		base += "&"
	}
	return base + "pids=0," + strings.Join(audioPIDs, ",")
}
