package service

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/gavinmcnair/tvproxy/pkg/session"
	"github.com/gavinmcnair/tvproxy/pkg/store"
)

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

func (s *VODService) startRecordingInternal(ctx context.Context, sessionKey, title, channelName, userID string, stopAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.recordings[sessionKey]; exists {
		return ErrAlreadyRecording
	}

	sess := s.sessionMgr.Get(sessionKey)
	if sess != nil {
		consumerID := s.sessionMgr.AddRecordingConsumer(sessionKey)
		if consumerID == "" {
			return fmt.Errorf("failed to add recording consumer")
		}
		if channelName == "" {
			channelName = sess.ChannelName
		}
		return s.finalizeRecordingStart(sess, sessionKey, consumerID, title, channelName, userID, stopAt)
	}

	streamURL, streamName, resolvedChannelName, streamID, _, useWG, err := s.resolveStreamForChannel(ctx, sessionKey)
	if err != nil {
		return err
	}
	if channelName == "" {
		channelName = resolvedChannelName
	}

	defaultHWAccel, defaultCodec := s.settingsService.ResolveGlobalDefaults(ctx)

	_ = defaultHWAccel
	_ = defaultCodec

	newSess, consumerID, err := s.sessionMgr.GetOrCreateWithConsumer(ctx, session.StartOpts{
		ChannelID:   sessionKey,
		StreamID:    streamID,
		StreamURL:   streamURL,
		StreamName:  streamName,
		ChannelName: channelName,
		ProfileName:  session.ConsumerRecording,
		UseWireGuard: useWG,
		OutputDir:    s.config.VODOutputDir,
		Transcoder:   s.transcoderPreference(ctx),
	}, session.ConsumerRecording)
	if err != nil {
		return err
	}

	return s.finalizeRecordingStart(newSess, sessionKey, consumerID, title, channelName, userID, stopAt)
}

func (s *VODService) finalizeRecordingStart(sess *session.Session, sessionKey, consumerID, title, channelName, userID string, stopAt time.Time) error {
	rs := &recordingState{
		ConsumerID: consumerID,
		Title:      title,
		UserID:     userID,
		StartedAt:  time.Now(),
		StopAt:     stopAt,
	}

	rs.Timer = time.AfterFunc(time.Until(stopAt), func() {
		s.log.Info().Str("session_key", sessionKey).Msg("recording deadline reached, auto-stopping")
		s.stopRecordingInternal(sessionKey)
	})

	s.recordings[sessionKey] = rs

	if sess != nil {
		s.updateSessionMetaForRecording(sess, title, userID, stopAt)
	}

	if s.activity != nil {
		s.activity.Add(ViewerOpts{
			ID:          consumerID,
			ChannelID:   sessionKey,
			ChannelName: channelName,
			ProfileName: title,
			Type:        session.ConsumerRecording,
		})
	}

	s.log.Info().Str("session_key", sessionKey).Str("title", title).Str("user_id", userID).Time("stop_at", stopAt).Msg("recording started")
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
		s.completeRecording(sess, rs)
	}

	if s.activity != nil {
		s.activity.Remove(rs.ConsumerID)
	}

	s.sessionMgr.RemoveConsumer(channelID, rs.ConsumerID)

	s.log.Info().Str("channel_id", channelID).Str("title", rs.Title).Msg("recording stopped")
	return nil
}

func (s *VODService) completeRecording(sess *session.Session, rs *recordingState) {
	streamID := sess.StreamID
	if streamID == "" {
		streamID = sess.ChannelID
	}

	meta := store.SessionMeta{
		Status:       store.SessionRecording,
		SessionID:    sess.ID,
		StreamID:     sess.StreamID,
		StreamName:   sess.StreamName,
		StreamURL:    sess.StreamURL,
		ChannelID:    sess.ChannelID,
		ChannelName:  sess.ChannelName,
		ProfileName:  sess.ProfileName,
		FileName:     filepath.Base(sess.FilePath),
		StartedAt:    rs.StartedAt,
		ProgramTitle: rs.Title,
		UserID:       rs.UserID,
		StopAt:       rs.StopAt,
		StoppedAt:    time.Now(),
	}

	filename, err := s.recordingStore.CompleteRecording(streamID, meta)
	if err != nil {
		s.log.Error().Err(err).Msg("failed to complete recording")
		return
	}

	s.log.Info().Str("stream_id", streamID).Str("filename", filename).Msg("recording saved")
}

func (s *VODService) updateSessionMetaForRecording(sess *session.Session, title, userID string, stopAt time.Time) {
	streamID := sess.StreamID
	if streamID == "" {
		streamID = sess.ChannelID
	}
	meta := store.SessionMeta{
		Status:       store.SessionRecording,
		SessionID:    sess.ID,
		StreamID:     sess.StreamID,
		StreamName:   sess.StreamName,
		StreamURL:    sess.StreamURL,
		ChannelID:    sess.ChannelID,
		ChannelName:  sess.ChannelName,
		ProfileName:  sess.ProfileName,
		FileName:     filepath.Base(sess.FilePath),
		StartedAt:    time.Now(),
		ProgramTitle: title,
		UserID:       userID,
		StopAt:       stopAt,
	}
	if err := s.recordingStore.WriteSessionMeta(streamID, meta); err != nil {
		s.log.Error().Err(err).Msg("failed to write session meta for recording")
	}
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

func (s *VODService) RecoverRecordings(ctx context.Context) {
	activeRecordings, err := s.recordingStore.ListActiveRecordings()
	if err != nil {
		s.log.Error().Err(err).Msg("failed to scan for active recordings")
		return
	}

	for _, meta := range activeRecordings {
		if meta.StopAt.Before(time.Now()) {
			s.log.Info().Str("channel", meta.ChannelName).Str("program", meta.ProgramTitle).Msg("recording expired, cleaning up")
			s.recordingStore.RemoveActiveSession(meta.StreamID)
			continue
		}

		s.log.Info().Str("channel", meta.ChannelName).Str("program", meta.ProgramTitle).Time("stop_at", meta.StopAt).Msg("recovering recording")
		if err := s.startRecordingInternal(ctx, meta.ChannelID, meta.ProgramTitle, meta.ChannelName, meta.UserID, meta.StopAt); err != nil {
			s.log.Error().Err(err).Str("channel", meta.ChannelName).Msg("failed to recover recording")
			s.recordingStore.RemoveActiveSession(meta.StreamID)
		}
	}

}
