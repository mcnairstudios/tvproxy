package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gavinmcnair/tvproxy/pkg/models"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/config"
	"github.com/gavinmcnair/tvproxy/pkg/ffmpeg"
	"github.com/gavinmcnair/tvproxy/pkg/session"
	"github.com/gavinmcnair/tvproxy/pkg/store"
)

var (
	ErrSessionNotFound  = errors.New("session not found")
	ErrNotAuthorized    = errors.New("not authorized")
	ErrInvalidFilename  = errors.New("invalid filename")
	ErrFileNotFound     = errors.New("file not found")
	ErrStreamNotFound   = errors.New("stream not found")
	ErrAlreadyRecording = errors.New("channel already recording")
)

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

func (s *VODService) StartWatchingFile(ctx context.Context, filePath, name, profileName, userAgent, remoteAddr string) (string, string, string, float64, error) {
	command, args, container := s.composeSessionArgs(ctx, profileName, filePath, "")

	sessionKey := "file:" + filepath.Base(filePath)

	_, consumerID, err := s.sessionMgr.GetOrCreateWithConsumer(ctx, session.StartOpts{
		ChannelID:   sessionKey,
		StreamID:    sessionKey,
		StreamURL:   filePath,
		StreamName:  name,
		ChannelName: name,
		ProfileName: profileName,
		Command:     command,
		Args:        args,
		OutputDir:   s.config.VODOutputDir,
	}, session.ConsumerViewer)
	if err != nil {
		return "", "", "", 0, err
	}

	var duration float64
	probe, _ := ffmpeg.Probe(ctx, filePath, "")
	if probe != nil {
		duration = probe.Duration
		sess := s.sessionMgr.Get(sessionKey)
		if sess != nil {
			sess.SetProbeInfo(probe.Video, probe.AudioTracks, probe.Duration)
		}
	}

	if s.activity != nil {
		s.activity.Add(ViewerOpts{
			ID:          consumerID,
			ChannelID:   sessionKey,
			ChannelName: name,
			ProfileName: profileName,
			UserAgent:   userAgent,
			RemoteAddr:  remoteAddr,
			Type:        "vod",
		})
	}

	return sessionKey, consumerID, container, duration, nil
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
