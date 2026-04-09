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
	config             *config.Config
	channelStore       store.ChannelStore
	streamStore        store.StreamReader
	streamProfileRepo  store.ProfileStore
	sourceProfileStore store.SourceProfileStore
	m3uAccountStore    store.M3UAccountStore
	settingsService    *SettingsService
	sessionMgr         *session.Manager
	recordingStore     store.RecordingStore
	activity           *ActivityService
	log                zerolog.Logger

	mu         sync.RWMutex
	recordings map[string]*recordingState
}

func NewVODService(
	channelStore store.ChannelStore,
	streamStore store.StreamReader,
	streamProfileRepo store.ProfileStore,
	sourceProfileStore store.SourceProfileStore,
	m3uAccountStore store.M3UAccountStore,
	settingsService *SettingsService,
	sessionMgr *session.Manager,
	recordingStore store.RecordingStore,
	activity *ActivityService,
	cfg *config.Config,
	log zerolog.Logger,
) *VODService {
	return &VODService{
		config:             cfg,
		channelStore:       channelStore,
		streamStore:        streamStore,
		streamProfileRepo:  streamProfileRepo,
		sourceProfileStore: sourceProfileStore,
		m3uAccountStore:    m3uAccountStore,
		settingsService:    settingsService,
		sessionMgr:        sessionMgr,
		recordingStore:    recordingStore,
		activity:          activity,
		log:               log.With().Str("service", "vod").Logger(),
		recordings:        make(map[string]*recordingState),
	}
}

func (s *VODService) resolveStreamForChannel(ctx context.Context, channelID string) (streamURL, streamName, channelName, streamID, streamGroup string, useWireGuard bool, err error) {
	channel, err := s.channelStore.GetByID(ctx, channelID)
	if err != nil {
		return "", "", "", "", "", false, fmt.Errorf("channel not found: %w", err)
	}
	if !channel.IsEnabled {
		return "", "", "", "", "", false, fmt.Errorf("channel %s is disabled", channelID)
	}

	channelStreams, err := s.channelStore.GetStreams(ctx, channelID)
	if err != nil {
		return "", "", "", "", "", false, fmt.Errorf("getting channel streams: %w", err)
	}

	for _, cs := range channelStreams {
		stream, err := s.streamStore.GetByID(ctx, cs.StreamID)
		if err != nil || !stream.IsActive {
			continue
		}
		return stream.URL, stream.Name, channel.Name, cs.StreamID, stream.Group, stream.UseWireGuard, nil
	}

	return "", "", "", "", "", false, fmt.Errorf("no active streams for channel %s", channelID)
}

func (s *VODService) lookupSourceProfile(ctx context.Context, m3uAccountID, satipSourceID string) *models.SourceProfile {
	if s.sourceProfileStore == nil {
		return nil
	}
	if m3uAccountID != "" && s.m3uAccountStore != nil {
		if acct, err := s.m3uAccountStore.GetByID(ctx, m3uAccountID); err == nil && acct.SourceProfileID != "" {
			if sp, err := s.sourceProfileStore.GetByID(ctx, acct.SourceProfileID); err == nil {
				return sp
			}
		}
	}
	return nil
}

type sessionArgs struct {
	Command          string
	Args             string
	Container        string
	Delivery         string
	OutputVideoCodec string
	OutputAudioCodec string
	OutputHWAccel    string
}

func (s *VODService) composeSessionArgs(ctx context.Context, profileName, streamURL, streamGroup string) sessionArgs {
	if profileName == "" {
		return sessionArgs{Command: "ffmpeg", Container: "mp4"}
	}
	sp, err := s.streamProfileRepo.GetByName(ctx, profileName)
	if err != nil {
		return sessionArgs{Command: "ffmpeg", Container: "mp4"}
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
	audioCodec := sp.AudioCodec
	if audioCodec == "default" || audioCodec == "" {
		audioCodec = "aac"
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
	return sessionArgs{
		Command:          command,
		Args:             args,
		Container:        sp.Container,
		Delivery:         sp.Delivery,
		OutputVideoCodec: videoCodec,
		OutputAudioCodec: audioCodec,
		OutputHWAccel:    hwaccel,
	}
}

func (s *VODService) StartWatching(ctx context.Context, channelID string, profileName string, userAgent string, remoteAddr string) (string, string, string, bool, error) {
	streamURL, streamName, channelName, streamID, streamGroup, useWG, err := s.resolveStreamForChannel(ctx, channelID)
	if err != nil {
		return "", "", "", false, err
	}

	audioOnly := strings.EqualFold(streamGroup, "radio")
	sa := s.composeSessionArgs(ctx, profileName, streamURL, streamGroup)

	strategy := resolveSessionStrategy(
		StrategyInput{
			StreamURL:     streamURL,
			VODType:       "",
			UseWireGuard:  useWG,
			SatIPSource:   strings.HasPrefix(streamURL, "rtsp://"),
			StreamGroup:   streamGroup,
			StreamID:      streamID,
			SourceProfile: s.lookupSourceProfile(ctx, streamID, ""),
		},
		StrategyOutput{
			Delivery:   sa.Delivery,
			VideoCodec: sa.OutputVideoCodec,
			AudioCodec: sa.OutputAudioCodec,
			HWAccel:    sa.OutputHWAccel,
			Container:  sa.Container,
			Command:    sa.Command,
			Args:       sa.Args,
		},
		s.config.VODOutputDir,
	)

	_, consumerID, err := s.sessionMgr.GetOrCreateWithConsumer(ctx, session.StartOpts{
		ChannelID:        channelID,
		StreamID:         streamID,
		StreamURL:        streamURL,
		StreamName:       streamName,
		ChannelName:      channelName,
		ProfileName:      profileName,
		OutputVideoCodec: strategy.VideoCodec,
		OutputAudioCodec: strategy.AudioCodec,
		OutputContainer:  strategy.Container,
		OutputHWAccel:    strategy.HWAccel,
		UseWireGuard:     useWG,
		Command:          strategy.Command,
		Args:             strategy.FFmpegArgs,
		OutputDir:        s.config.VODOutputDir,
		HLSOutputDir:     strategy.HLSOutputDir,
		SourceInputArgs:   strategy.SourceInputArgs,
		SourceDeinterlace: strategy.SourceDeinterlace,
		SourceAudioResync: strategy.SourceAudioResync,
		SourceFPSMode:     strategy.SourceFPSMode,
		MetadataOnly:     strategy.MetadataOnly,
	}, session.ConsumerViewer)
	if err != nil {
		return "", "", "", false, err
	}

	s.log.Info().Str("channel_id", channelID).Str("profile", profileName).Str("container", sa.Container).Str("user_agent", userAgent).Str("remote", remoteAddr).Msg("viewer started")

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

	return channelID, consumerID, sa.Container, audioOnly, nil
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

	sa := s.composeSessionArgs(ctx, profileName, streamURL, stream.Group)

	strategy := resolveSessionStrategy(
		StrategyInput{
			StreamURL:     streamURL,
			VODType:       stream.VODType,
			VODDuration:   stream.VODDuration,
			UseWireGuard:  stream.UseWireGuard,
			SatIPSource:   stream.SatIPSourceID != "",
			StreamGroup:   stream.Group,
			StreamID:      streamID,
			StreamVCodec:  stream.VODVCodec,
			StreamACodec:  stream.VODACodec,
			SourceProfile: s.lookupSourceProfile(ctx, stream.M3UAccountID, stream.SatIPSourceID),
		},
		StrategyOutput{
			Delivery:   sa.Delivery,
			VideoCodec: sa.OutputVideoCodec,
			AudioCodec: sa.OutputAudioCodec,
			HWAccel:    sa.OutputHWAccel,
			Container:  sa.Container,
			Command:    sa.Command,
			Args:       sa.Args,
		},
		s.config.VODOutputDir,
	)

	_, consumerID, err := s.sessionMgr.GetOrCreateWithConsumer(ctx, session.StartOpts{
		ChannelID:        streamID,
		StreamID:         streamID,
		StreamURL:        streamURL,
		StreamName:       stream.Name,
		ChannelName:      stream.Name,
		ProfileName:      profileName,
		OutputVideoCodec: strategy.VideoCodec,
		OutputAudioCodec: strategy.AudioCodec,
		OutputContainer:  strategy.Container,
		OutputHWAccel:    strategy.HWAccel,
		UseWireGuard:     stream.UseWireGuard,
		Command:          strategy.Command,
		Args:             strategy.FFmpegArgs,
		OutputDir:        s.config.VODOutputDir,
		HLSOutputDir:     strategy.HLSOutputDir,
		SourceInputArgs:   strategy.SourceInputArgs,
		SourceDeinterlace: strategy.SourceDeinterlace,
		SourceAudioResync: strategy.SourceAudioResync,
		SourceFPSMode:     strategy.SourceFPSMode,
		KnownDuration:    stream.VODDuration,
		MetadataOnly:     strategy.MetadataOnly,
	}, session.ConsumerViewer)
	if err != nil {
		return "", "", "", err
	}

	s.log.Info().Str("stream_id", streamID).Str("profile", profileName).Str("container", sa.Container).Str("user_agent", userAgent).Str("remote", remoteAddr).Msg("viewer started")

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

	return streamID, consumerID, sa.Container, nil
}

func (s *VODService) StartWatchingFile(ctx context.Context, filePath, name, profileName, userAgent, remoteAddr string) (string, string, string, float64, bool, error) {
	sa := s.composeSessionArgs(ctx, profileName, filePath, "")

	sessionKey := "file:" + filepath.Base(filePath)

	_, consumerID, err := s.sessionMgr.GetOrCreateWithConsumer(ctx, session.StartOpts{
		ChannelID:        sessionKey,
		StreamID:         sessionKey,
		StreamURL:        filePath,
		StreamName:       name,
		ChannelName:      name,
		ProfileName:      profileName,
		OutputVideoCodec: sa.OutputVideoCodec,
		OutputAudioCodec: sa.OutputAudioCodec,
		OutputContainer:  sa.Container,
		OutputHWAccel:    sa.OutputHWAccel,
		Command:          sa.Command,
		Args:             sa.Args,
		OutputDir:        s.config.VODOutputDir,
		MetadataOnly:     sa.Delivery == "hls",
	}, session.ConsumerViewer)
	if err != nil {
		return "", "", "", 0, false, err
	}

	var duration float64
	var audioOnly bool
	probe, _ := ffmpeg.Probe(ctx, filePath, "")
	if probe != nil {
		duration = probe.Duration
		audioOnly = probe.Video == nil && len(probe.AudioTracks) > 0
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

	return sessionKey, consumerID, sa.Container, duration, audioOnly, nil
}

func (s *VODService) SeekSession(ctx context.Context, channelID string, position float64) error {
	sess := s.sessionMgr.Get(channelID)
	if sess == nil {
		return fmt.Errorf("session not found")
	}

	s.sessionMgr.RestartWithSeek(ctx, channelID, position)
	return nil
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
