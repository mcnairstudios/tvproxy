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

	"github.com/gavinmcnair/tvproxy/pkg/avprobe"
	"github.com/gavinmcnair/tvproxy/pkg/config"
	"github.com/gavinmcnair/tvproxy/pkg/media"
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
	satipSourceStore   store.SatIPSourceStore
	settingsService    *SettingsService
	sessionMgr         *session.Manager
	recordingStore     store.RecordingStore
	probeCache         store.ProbeCache
	activity           *ActivityService
	log                zerolog.Logger

	mu         sync.RWMutex
	recordings map[string]*recordingState
}

func (s *VODService) SetProbeCache(pc store.ProbeCache) { s.probeCache = pc }

func NewVODService(
	channelStore store.ChannelStore,
	streamStore store.StreamReader,
	streamProfileRepo store.ProfileStore,
	sourceProfileStore store.SourceProfileStore,
	m3uAccountStore store.M3UAccountStore,
	satipSourceStore store.SatIPSourceStore,
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
		satipSourceStore:   satipSourceStore,
		settingsService:    settingsService,
		sessionMgr:        sessionMgr,
		recordingStore:    recordingStore,
		activity:          activity,
		log:               log.With().Str("service", "vod").Logger(),
		recordings:        make(map[string]*recordingState),
	}
}

type resolvedStream struct {
	URL            string
	StreamName     string
	ChannelName    string
	StreamID       string
	StreamGroup    string
	UseWireGuard   bool
	M3UAccountID   string
	SatIPSourceID  string
}

func (s *VODService) resolveStreamForChannel(ctx context.Context, channelID string) (*resolvedStream, error) {
	channel, err := s.channelStore.GetByID(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("channel not found: %w", err)
	}
	if !channel.IsEnabled {
		return nil, fmt.Errorf("channel %s is disabled", channelID)
	}

	channelStreams, err := s.channelStore.GetStreams(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("getting channel streams: %w", err)
	}

	for _, cs := range channelStreams {
		stream, err := s.streamStore.GetByID(ctx, cs.StreamID)
		if err != nil || !stream.IsActive {
			continue
		}
		return &resolvedStream{
			URL:           stream.URL,
			StreamName:    stream.Name,
			ChannelName:   channel.Name,
			StreamID:      cs.StreamID,
			StreamGroup:   stream.Group,
			UseWireGuard:  stream.UseWireGuard,
			M3UAccountID:  stream.M3UAccountID,
			SatIPSourceID: stream.SatIPSourceID,
		}, nil
	}

	return nil, fmt.Errorf("no active streams for channel %s", channelID)
}

func applySourceProfile(opts *session.StartOpts, sp *models.SourceProfile) {
	if sp == nil {
		return
	}
	opts.Deinterlace = sp.Deinterlace
	opts.DeinterlaceMethod = sp.DeinterlaceMethod
	opts.AudioDelayMs = sp.AudioDelayMs
	opts.AudioLanguage = sp.AudioLanguage
	opts.VideoQueueMs = sp.VideoQueueMs
	opts.AudioQueueMs = sp.AudioQueueMs
	opts.RTSPLatency = sp.RTSPLatency
	opts.RTSPProtocols = sp.RTSPProtocols
	opts.RTSPBufferMode = sp.RTSPBufferMode
	opts.HTTPTimeoutSec = sp.HTTPTimeoutSec
	opts.HTTPRetries = sp.HTTPRetries
	opts.HTTPUserAgent = sp.HTTPUserAgent
	opts.TSSetTimestamps = sp.TSSetTimestamps
	opts.EncoderBitrateKbps = sp.EncoderBitrateKbps
}

func (s *VODService) lookupSourceProfile(ctx context.Context, m3uAccountID, satipSourceID, streamURL string) *models.SourceProfile {
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
	if satipSourceID != "" && s.satipSourceStore != nil {
		if src, err := s.satipSourceStore.GetByID(ctx, satipSourceID); err == nil && src.SourceProfileID != "" {
			if sp, err := s.sourceProfileStore.GetByID(ctx, src.SourceProfileID); err == nil {
				return sp
			}
		}
	}
	return s.autoSelectSourceProfile(ctx, streamURL)
}

func (s *VODService) resolveEncoderElement(ctx context.Context, videoCodec string) string {
	codec := strings.ToLower(videoCodec)
	switch codec {
	case "h264", "h265", "av1":
		return s.settingsService.ResolveEncoderElement(ctx, codec)
	}
	return ""
}

func (s *VODService) resolveDecoderElement(ctx context.Context, sourceVideoCodec string) string {
	codec := strings.ToLower(sourceVideoCodec)
	switch codec {
	case "h264", "h265", "av1":
		return s.settingsService.ResolveDecoderElement(ctx, codec)
	case "mpeg2", "mpeg2video":
		return s.settingsService.ResolveDecoderElement(ctx, "mpeg2")
	}
	return ""
}

func (s *VODService) resolveDecodeHWAccel(ctx context.Context) string {
	return s.settingsService.ResolveDecodeHWAccel(ctx)
}

func (s *VODService) autoSelectSourceProfile(ctx context.Context, streamURL string) *models.SourceProfile {
	if streamURL == "" {
		return nil
	}

	var names []string
	switch {
	case strings.HasPrefix(streamURL, "rtsp://"):
		names = []string{"SAT>IP"}
	case strings.Contains(streamURL, ":5004/"):
		names = []string{"HDHomeRun", "IPTV"}
	case hasVideoFileExtension(streamURL):
		names = []string{"VOD", "TVProxy-streams"}
	default:
		names = []string{"IPTV"}
	}

	for _, name := range names {
		if sp, err := s.sourceProfileStore.GetByName(ctx, name); err == nil {
			return sp
		}
	}
	return nil
}

func hasVideoFileExtension(u string) bool {
	lower := strings.ToLower(u)
	if idx := strings.IndexByte(lower, '?'); idx != -1 {
		lower = lower[:idx]
	}
	for _, ext := range []string{".mp4", ".mkv", ".webm", ".mov", ".avi"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

func (s *VODService) transcoderPreference(ctx context.Context) string {
	if val, _ := s.settingsService.Get(ctx, "transcoder"); val != "" {
		return val
	}
	return "auto"
}

type sessionArgs struct {
	Container        string
	OutputVideoCodec string
	OutputAudioCodec string
	OutputHWAccel    string
	Delivery         string
	OutputHeight     int
}

func (s *VODService) composeSessionArgs(ctx context.Context, profileName, streamURL, streamGroup string) sessionArgs {
	if profileName == "" {
		return sessionArgs{Container: "mp4"}
	}
	sp, err := s.streamProfileRepo.GetByName(ctx, profileName)
	if err != nil {
		return sessionArgs{Container: "mp4"}
	}

	globalHW, _ := s.settingsService.ResolveGlobalDefaults(ctx)
	hwaccel := sp.HWAccel
	if hwaccel == "default" || hwaccel == "" {
		hwaccel = globalHW
	}
	videoCodec := sp.VideoCodec
	audioCodec := sp.AudioCodec
	if audioCodec == "default" || audioCodec == "" {
		audioCodec = "aac"
	}

	delivery := sp.Delivery
	if delivery == "" {
		delivery = "stream"
	}

	container := sp.Container
	if (videoCodec == "av1" || videoCodec == "AV1") && (container == "mpegts" || container == "ts") {
		container = "mp4"
	}

	return sessionArgs{
		Container:        container,
		OutputVideoCodec: videoCodec,
		OutputAudioCodec: audioCodec,
		OutputHWAccel:    hwaccel,
		Delivery:         delivery,
		OutputHeight:     sp.OutputHeight,
	}
}

func (s *VODService) StartWatching(ctx context.Context, channelID string, profileName string, userAgent string, remoteAddr string) (string, string, string, bool, error) {
	rs, err := s.resolveStreamForChannel(ctx, channelID)
	if err != nil {
		return "", "", "", false, err
	}

	streamURL := rs.URL
	streamName := rs.StreamName
	channelName := rs.ChannelName
	streamID := rs.StreamID
	streamGroup := rs.StreamGroup
	useWG := rs.UseWireGuard

	audioOnly := strings.EqualFold(streamGroup, "radio")
	sa := s.composeSessionArgs(ctx, profileName, streamURL, streamGroup)

	var probeVCodec, probeACodec string
	if s.probeCache != nil {
		if pr, err := s.probeCache.GetProbe(streamID); err == nil && pr != nil {
			if pr.Video != nil {
				probeVCodec = pr.Video.Codec
			}
			if len(pr.AudioTracks) > 0 {
				probeACodec = pr.AudioTracks[0].Codec
			}
		}
	}

	strategy := resolveSessionStrategy(
		StrategyInput{
			StreamURL:     streamURL,
			VODType:       "",
			UseWireGuard:  useWG,
			SatIPSource:   strings.HasPrefix(streamURL, "rtsp://"),
			StreamGroup:   streamGroup,
			StreamID:      streamID,
			StreamVCodec:  probeVCodec,
			StreamACodec:  probeACodec,
			SourceProfile: s.lookupSourceProfile(ctx, rs.M3UAccountID, rs.SatIPSourceID, streamURL),
		},
		StrategyOutput{
			VideoCodec:   sa.OutputVideoCodec,
			AudioCodec:   sa.OutputAudioCodec,
			HWAccel:      sa.OutputHWAccel,
			Container:    sa.Container,
			OutputHeight: sa.OutputHeight,
		},
	)

	if sa.Delivery == "mse" && strategy.VideoCodec == "copy" {
		codec := probeVCodec
		if codec == "" {
			codec = "h265"
		}
		strategy.VideoCodec = normalizeVideoCodecName(codec)
	}

	startOpts := session.StartOpts{
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
		OutputDir:        s.config.VODOutputDir,
		SkipProbe:         strategy.SkipProbe,
		MetadataOnly:     strategy.MetadataOnly,
		Delivery:         sa.Delivery,
		OutputHeight:     sa.OutputHeight,
	}
	startOpts.VideoEncoderElement = s.resolveEncoderElement(ctx, strategy.VideoCodec)
	startOpts.VideoDecoderElement = s.resolveDecoderElement(ctx, probeVCodec)
	startOpts.DecodeHWAccel = s.resolveDecodeHWAccel(ctx)
	sp := s.lookupSourceProfile(ctx, rs.M3UAccountID, rs.SatIPSourceID, streamURL)
	applySourceProfile(&startOpts, sp)

	_, consumerID, err := s.sessionMgr.GetOrCreateWithConsumer(ctx, startOpts, session.ConsumerViewer)
	if err != nil {
		return "", "", "", false, err
	}

	spName := "none"
	if sp != nil {
		spName = sp.Name
	}
	s.log.Info().Str("channel_id", channelID).Str("profile", profileName).Str("source_profile", spName).Str("container", sa.Container).Str("user_agent", userAgent).Str("remote", remoteAddr).Msg("viewer started")

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
			SourceProfile: s.lookupSourceProfile(ctx, stream.M3UAccountID, stream.SatIPSourceID, streamURL),
		},
		StrategyOutput{
			VideoCodec:   sa.OutputVideoCodec,
			AudioCodec:   sa.OutputAudioCodec,
			HWAccel:      sa.OutputHWAccel,
			Container:    sa.Container,
			OutputHeight: sa.OutputHeight,
		},
	)

	if sa.Delivery == "mse" && strategy.VideoCodec == "copy" {
		codec := stream.VODVCodec
		if codec == "" {
			codec = "h265"
		}
		strategy.VideoCodec = normalizeVideoCodecName(codec)
	}

	startOpts2 := session.StartOpts{
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
		OutputDir:        s.config.VODOutputDir,
		SkipProbe:         strategy.SkipProbe,
		KnownDuration:    stream.VODDuration,
		MetadataOnly:     strategy.MetadataOnly,
		Delivery:         sa.Delivery,
		OutputHeight:     sa.OutputHeight,
	}
	startOpts2.VideoEncoderElement = s.resolveEncoderElement(ctx, strategy.VideoCodec)
	startOpts2.VideoDecoderElement = s.resolveDecoderElement(ctx, stream.VODVCodec)
	startOpts2.DecodeHWAccel = s.resolveDecodeHWAccel(ctx)
	applySourceProfile(&startOpts2, s.lookupSourceProfile(ctx, stream.M3UAccountID, stream.SatIPSourceID, streamURL))

	_, consumerID, err := s.sessionMgr.GetOrCreateWithConsumer(ctx, startOpts2, session.ConsumerViewer)
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
		ChannelID:            sessionKey,
		StreamID:             sessionKey,
		StreamURL:            filePath,
		StreamName:           name,
		ChannelName:          name,
		ProfileName:          profileName,
		OutputVideoCodec:     sa.OutputVideoCodec,
		OutputAudioCodec:     sa.OutputAudioCodec,
		OutputContainer:      sa.Container,
		OutputHWAccel:        sa.OutputHWAccel,
		VideoEncoderElement:  s.resolveEncoderElement(ctx, sa.OutputVideoCodec),
		DecodeHWAccel:        s.resolveDecodeHWAccel(ctx),
		OutputDir:            s.config.VODOutputDir,
		MetadataOnly:         false,
		Delivery:             sa.Delivery,
		OutputHeight:         sa.OutputHeight,
	}, session.ConsumerViewer)
	if err != nil {
		return "", "", "", 0, false, err
	}

	var duration float64
	var audioOnly bool
	probe, _ := avprobe.Probe(ctx, filePath, "")
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

func (s *VODService) GetFileSize(channelID string) int64 {
	return s.sessionMgr.GetFileSize(channelID)
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

func (s *VODService) GetProbeInfo(channelID string) (*media.VideoInfo, []media.AudioTrack, float64) {
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
