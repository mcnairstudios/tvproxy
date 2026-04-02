package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/gavinmcnair/tvproxy/pkg/ffmpeg"
	"github.com/gavinmcnair/tvproxy/pkg/session"
	"github.com/gavinmcnair/tvproxy/pkg/store"
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
			Type:        session.ConsumerRecording,
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
