package session

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-gst/go-gst/gst"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/avprobe"
	"github.com/gavinmcnair/tvproxy/pkg/config"
	"github.com/gavinmcnair/tvproxy/pkg/gstreamer"
	"github.com/gavinmcnair/tvproxy/pkg/httputil"
	"github.com/gavinmcnair/tvproxy/pkg/media"
	"github.com/gavinmcnair/tvproxy/pkg/store"
	"github.com/gavinmcnair/tvproxy/pkg/tvsatipscan"
)

const maxBufferedSecs = 172800.0

type StartOpts struct {
	ChannelID        string
	StreamID         string
	StreamURL        string
	StreamName       string
	ChannelName      string
	ProfileName      string
	OutputVideoCodec string
	OutputAudioCodec string
	OutputContainer  string
	OutputHWAccel    string
	UseWireGuard     bool
	KnownDuration    float64
	SeekOffset       float64
	OutputDir         string
	HLSOutputDir      string
	SkipProbe         bool
	MetadataOnly      bool

	Deinterlace       bool
	DeinterlaceMethod string
	AudioDelayMs      int
	AudioChannels     int
	AudioLanguage     string
	VideoQueueMs      int
	AudioQueueMs      int
	RTSPLatency       int
	RTSPProtocols     string
	RTSPBufferMode    int
	HTTPTimeoutSec    int
	HTTPRetries       int
	HTTPUserAgent     string
	TSSetTimestamps   bool
	EncoderBitrateKbps int
}

type Manager struct {
	sessions      map[string]*Session
	config        *config.Config
	httpClient    *http.Client
	wgClient      *http.Client
	wgProxyMgr    *WGProxyManager
	probeCache    store.ProbeCache
	onCleanup     func(channelID string)
	log           zerolog.Logger
	mu            sync.RWMutex
}

func (m *Manager) clientForSession(s *Session) *http.Client {
	if s.UseWireGuard && m.wgClient != nil {
		return m.wgClient
	}
	return m.httpClient
}

func (m *Manager) WGProxy(profileID string, wgClient *http.Client, cfg *config.Config, log zerolog.Logger) (*WGProxy, error) {
	return m.wgProxyMgr.GetOrCreate(profileID, wgClient, cfg, log)
}

func (m *Manager) SetOnCleanup(fn func(channelID string)) {
	m.onCleanup = fn
}

func NewManager(cfg *config.Config, httpClient *http.Client, wgClient *http.Client, probeCache store.ProbeCache, log zerolog.Logger) *Manager {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Manager{
		sessions:    make(map[string]*Session),
		config:      cfg,
		httpClient:  httpClient,
		wgClient:    wgClient,
		wgProxyMgr:  NewWGProxyManager(),
		probeCache:  probeCache,
		log:         log.With().Str("component", "session_manager").Logger(),
	}
}

func (m *Manager) cleanupDoneSession(channelID string, s *Session) {
	s.mu.Lock()
	if s.lingerTimer != nil {
		s.lingerTimer.Stop()
		s.lingerTimer = nil
	}
	s.mu.Unlock()
	delete(m.sessions, channelID)
	s.cancel()
	<-s.done
	os.RemoveAll(s.TempDir)
	if s.HLSOutputDir != "" {
		os.RemoveAll(s.HLSOutputDir)
	}
	m.log.Info().Str("channel_id", channelID).Str("session_id", s.ID).Msg("replaced dead session")
}



const lingerDuration = 30 * time.Second


func (m *Manager) AddRecordingConsumer(sessionKey string) string {
	m.mu.RLock()
	s, ok := m.sessions[sessionKey]
	m.mu.RUnlock()
	if !ok {
		return ""
	}
	c := &Consumer{
		ID:        uuid.New().String(),
		Type:      ConsumerRecording,
		CreatedAt: time.Now(),
	}
	s.addConsumer(c)
	m.log.Info().Str("session_key", sessionKey).Str("consumer_id", c.ID).Msg("recording consumer added")
	return c.ID
}

func (m *Manager) RemoveConsumer(channelID string, consumerID string) {
	m.mu.RLock()
	s, ok := m.sessions[channelID]
	m.mu.RUnlock()
	if !ok {
		return
	}

	remaining := s.removeConsumer(consumerID)

	m.log.Info().
		Str("channel_id", channelID).
		Str("consumer_id", consumerID).
		Int("remaining", remaining).
		Msg("consumer removed")

	if remaining == 0 {
		if s.HasRecordingConsumer() {
			m.log.Info().Str("channel_id", channelID).Msg("viewer gone but recording active, keeping session")
		} else {
			m.log.Info().Str("channel_id", channelID).Msg("no consumers, cleaning up session")
			go m.stopAndCleanup(channelID, s)
		}
	}
}

func (m *Manager) stopAndCleanup(channelID string, s *Session) {
	m.mu.Lock()
	if current, ok := m.sessions[channelID]; !ok || current != s {
		m.mu.Unlock()
		return
	}
	delete(m.sessions, channelID)
	m.mu.Unlock()

	s.mu.Lock()
	if s.lingerTimer != nil {
		s.lingerTimer.Stop()
		s.lingerTimer = nil
	}
	s.mu.Unlock()

	s.cancel()

	<-s.done

	if m.onCleanup != nil {
		m.onCleanup(channelID)
	}

	if !s.wasRecording {
		os.RemoveAll(s.TempDir)
	}
	if s.HLSOutputDir != "" {
		os.RemoveAll(s.HLSOutputDir)
	}

	m.log.Info().
		Str("channel_id", channelID).
		Str("session_id", s.ID).
		Bool("preserved", s.HasRecordingConsumer()).
		Msg("session stopped and cleaned up")
}

func (m *Manager) TailFile(ctx context.Context, channelID string) (io.ReadCloser, error) {
	m.mu.RLock()
	s, ok := m.sessions[channelID]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("no session for channel %s", channelID)
	}
	return m.tailSession(ctx, s)
}

func (m *Manager) tailSession(ctx context.Context, s *Session) (io.ReadCloser, error) {
	retryCount := 50
	retryDelay := 200 * time.Millisecond
	if m.config.Settings != nil {
		retryCount = m.config.Settings.VOD.FileRetryCount
		retryDelay = m.config.Settings.VOD.FileRetryDelay
	}

	var f *os.File
	for i := 0; i < retryCount; i++ {
		var err error
		f, err = os.Open(s.FilePath)
		if err == nil {
			break
		}
		if s.isDone() {
			if procErr := s.getError(); procErr != nil {
				return nil, procErr
			}
			return nil, fmt.Errorf("transcoder exited before creating file")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(retryDelay):
		}
	}
	if f == nil {
		if procErr := s.getError(); procErr != nil {
			return nil, fmt.Errorf("transcoder failed: %w", procErr)
		}
		return nil, fmt.Errorf("timed out waiting for session file after %d retries", retryCount)
	}

	return newTailReader(ctx, f, s), nil
}

func (m *Manager) Get(channelID string) *Session {
	m.mu.RLock()
	s := m.sessions[channelID]
	m.mu.RUnlock()
	return s
}

func (m *Manager) ConsumerCount(channelID string) int {
	m.mu.RLock()
	s, ok := m.sessions[channelID]
	m.mu.RUnlock()
	if !ok {
		return 0
	}
	return s.consumerCount()
}

func (m *Manager) GetBufferedSecs(channelID string) float64 {
	m.mu.RLock()
	s, ok := m.sessions[channelID]
	m.mu.RUnlock()
	if !ok {
		return 0
	}
	return s.SeekOffset + s.getBuffered()
}

func (m *Manager) GetFileSize(channelID string) int64 {
	m.mu.RLock()
	s, ok := m.sessions[channelID]
	m.mu.RUnlock()
	if !ok {
		return 0
	}
	info, err := os.Stat(s.FilePath)
	if err != nil {
		return 0
	}
	return info.Size()
}

func (m *Manager) GetError(channelID string) error {
	m.mu.RLock()
	s, ok := m.sessions[channelID]
	m.mu.RUnlock()
	if !ok {
		return nil
	}
	return s.getError()
}

func (m *Manager) IsDone(channelID string) bool {
	m.mu.RLock()
	s, ok := m.sessions[channelID]
	m.mu.RUnlock()
	if !ok {
		return true
	}
	return s.isDone()
}

func (m *Manager) RestartWithSeek(ctx context.Context, channelID string, position float64) {
	m.mu.Lock()
	s, ok := m.sessions[channelID]
	if !ok {
		m.mu.Unlock()
		return
	}
	opts := s.startOpts
	s.cancel()
	delete(m.sessions, channelID)
	m.mu.Unlock()

	select {
	case <-s.done:
	case <-time.After(3 * time.Second):
	}

	opts.KnownDuration = s.Duration
	opts.SeekOffset = position

	m.log.Info().Str("channel_id", channelID).Float64("position", position).Msg("restarting session with seek")

	m.GetOrCreateWithConsumer(ctx, opts, ConsumerViewer)
}

func (m *Manager) GetOrCreateWithConsumer(ctx context.Context, opts StartOpts, consumerType string) (*Session, string, error) {
	m.mu.Lock()
	if s, ok := m.sessions[opts.ChannelID]; ok {
		if s.isDone() {
			m.cleanupDoneSession(opts.ChannelID, s)
		} else {
			s.mu.Lock()
			if s.lingerTimer != nil {
				s.lingerTimer.Stop()
				s.lingerTimer = nil
			}
			s.mu.Unlock()
			c := &Consumer{
				ID:        uuid.New().String(),
				Type:      consumerType,
				CreatedAt: time.Now(),
			}
			s.addConsumer(c)
			m.mu.Unlock()
			m.log.Info().
				Str("channel_id", opts.ChannelID).
				Str("consumer_id", c.ID).
				Str("type", consumerType).
				Int("total", s.consumerCount()).
				Msg("consumer added to existing session")
			return s, c.ID, nil
		}
	}

	streamID := opts.StreamID
	if streamID == "" {
		streamID = opts.ChannelID
	}

	var tempDir string
	if m.probeCache != nil {
		if rs, ok := m.probeCache.(interface{ ActiveDir(string) string }); ok {
			tempDir = rs.ActiveDir(streamID)
		}
	}
	if tempDir == "" {
		tempDir = filepath.Join(opts.OutputDir, streamID, "active")
	}
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		m.mu.Unlock()
		return nil, "", fmt.Errorf("creating session dir: %w", err)
	}

	ext := ".mp4"
	if opts.OutputContainer == "mpegts" {
		ext = ".ts"
	}
	fileName := media.SanitizeFilename(opts.StreamName, time.Now()) + ext
	filePath := filepath.Join(tempDir, fileName)

	sessionCtx, cancel := context.WithCancel(context.Background())

	sessionID := uuid.New().String()
	s := &Session{
		ID:          sessionID,
		ChannelID:   opts.ChannelID,
		StreamID:    opts.StreamID,
		StreamURL:   opts.StreamURL,
		StreamName:  opts.StreamName,
		ChannelName: opts.ChannelName,
		ProfileName:      opts.ProfileName,
		OutputVideoCodec: opts.OutputVideoCodec,
		OutputAudioCodec: opts.OutputAudioCodec,
		OutputContainer:  opts.OutputContainer,
		OutputHWAccel:    opts.OutputHWAccel,
		UseWireGuard:     opts.UseWireGuard,
		HLSOutputDir:    opts.HLSOutputDir,
		Duration:         opts.KnownDuration,
		SeekOffset:       opts.SeekOffset,
		FilePath:         filePath,
		TempDir:          tempDir,
		startOpts:   opts,
		consumers:   make(map[string]*Consumer),
		cancel:      cancel,
		done:        make(chan struct{}),
	}

	if m.probeCache != nil {
		if rs, ok := m.probeCache.(interface {
			WriteSessionMeta(string, store.SessionMeta) error
		}); ok {
			rs.WriteSessionMeta(streamID, store.SessionMeta{
				Status:      store.SessionActive,
				SessionID:   sessionID,
				StreamID:    opts.StreamID,
				StreamName:  opts.StreamName,
				StreamURL:   opts.StreamURL,
				ChannelID:   opts.ChannelID,
				ChannelName: opts.ChannelName,
				ProfileName: opts.ProfileName,
				FileName:    fileName,
				StartedAt:   time.Now(),
			})
		}
	}

	c := &Consumer{
		ID:        uuid.New().String(),
		Type:      consumerType,
		CreatedAt: time.Now(),
	}
	s.addConsumer(c)

	if m.probeCache != nil {
		var cached *media.ProbeResult
		if opts.StreamID != "" {
			cached, _ = m.probeCache.GetProbeByStreamID(opts.StreamID)
		}
		if cached == nil && opts.StreamURL != "" {
			cached, _ = m.probeCache.GetProbe(media.StreamHash(opts.StreamURL))
		}
		if cached != nil {
			s.SetProbeInfo(cached.Video, cached.AudioTracks, cached.Duration)
		}
	}

	m.sessions[opts.ChannelID] = s
	m.mu.Unlock()

	if opts.HLSOutputDir == "" && !opts.SkipProbe {
		go m.probeAsync(s, opts.StreamURL)
	}

	if !opts.MetadataOnly {
		go m.runPipeline(sessionCtx, s)
		if strings.HasPrefix(opts.StreamURL, "rtsp://") || strings.HasPrefix(opts.StreamURL, "rtsps://") {
			go m.logSignalAsync(s.ID, opts.ChannelID, opts.StreamURL)
		}
	}

	m.log.Info().
		Str("session_id", s.ID).
		Str("channel_id", opts.ChannelID).
		Str("stream_url", opts.StreamURL).
		Str("profile", opts.ProfileName).
		Str("consumer_id", c.ID).
		Msg("session created with consumer")

	return s, c.ID, nil
}


func (m *Manager) Shutdown() {
	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.sessions = make(map[string]*Session)
	m.mu.Unlock()

	for _, s := range sessions {
		s.mu.Lock()
		if s.lingerTimer != nil {
			s.lingerTimer.Stop()
			s.lingerTimer = nil
		}
		s.mu.Unlock()
		s.StopPipeline()
		s.cancel()
		<-s.done
		if !s.HasRecordingConsumer() {
			os.RemoveAll(s.TempDir)
		}
	}

	m.log.Info().Int("sessions", len(sessions)).Msg("session manager shutdown complete")
}

func (m *Manager) probeViaClient(ctx context.Context, client *http.Client, url string) (*media.ProbeResult, error) {
	resp, err := httputil.Fetch(ctx, client, m.config, url)
	if err != nil {
		return nil, fmt.Errorf("probe upstream: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("probe upstream returned %d", resp.StatusCode)
	}
	return avprobe.ProbeReader(ctx, resp.Body)
}

func (m *Manager) ProbeURL(ctx context.Context, url string) (*media.ProbeResult, error) {
	resp, err := httputil.Fetch(ctx, m.httpClient, m.config, url)
	if err != nil {
		return nil, fmt.Errorf("probe upstream: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("probe upstream returned %d", resp.StatusCode)
	}

	return avprobe.ProbeReader(ctx, resp.Body)
}

func (m *Manager) probeAsync(s *Session, streamURL string) {
	if m.probeCache != nil {
		var cached *media.ProbeResult
		if s.StreamID != "" {
			cached, _ = m.probeCache.GetProbeByStreamID(s.StreamID)
		}
		if cached == nil && streamURL != "" {
			cached, _ = m.probeCache.GetProbe(media.StreamHash(streamURL))
		}
		if cached != nil && (cached.Duration > 0 || !media.IsHTTPURL(streamURL)) {
			s.SetProbeInfo(cached.Video, cached.AudioTracks, cached.Duration)
			m.log.Debug().Str("session_id", s.ID).Float64("duration", cached.Duration).Msg("probe cache hit")
			return
		}
	}

	probeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var result *media.ProbeResult
	var err error
	client := m.clientForSession(s)

	var extraHeaders []string
	if m.config.BypassHeader != "" && m.config.BypassSecret != "" {
		extraHeaders = append(extraHeaders, m.config.BypassHeader+": "+m.config.BypassSecret)
	}
	result, err = avprobe.Probe(probeCtx, streamURL, m.config.UserAgent)
	if (err != nil || result == nil || result.Duration == 0) && media.IsHTTPURL(streamURL) {
		m.log.Debug().Str("session_id", s.ID).Msg("direct probe incomplete, trying HTTP pipe probe")
		pipeResult, pipeErr := m.probeViaClient(probeCtx, client, streamURL)
		if pipeErr == nil && pipeResult != nil {
			if result == nil || (pipeResult.Video != nil && result.Video == nil) {
				result = pipeResult
			} else if pipeResult.Duration > 0 && result.Duration == 0 {
				result.Duration = pipeResult.Duration
			}
		}
	}
	if err != nil || result == nil || (result.Video == nil && len(result.AudioTracks) == 0) {
		m.log.Warn().Err(err).Str("session_id", s.ID).Msg("source probe failed, falling back to output file")
		result = m.probeOutputFile(s)
	}

	if result == nil || (result.Video == nil && len(result.AudioTracks) == 0) {
		m.log.Warn().Str("session_id", s.ID).Msg("all probe methods failed")
		return
	}

	s.SetProbeInfo(result.Video, result.AudioTracks, result.Duration)

	if m.probeCache != nil {
		if s.StreamID != "" {
			m.probeCache.SaveProbeByStreamID(s.StreamID, result)
		}
		if streamURL != "" {
			m.probeCache.SaveProbe(media.StreamHash(streamURL), result)
		}
	}

	m.log.Debug().Str("session_id", s.ID).Bool("is_vod", result.IsVOD).Int("audio_tracks", len(result.AudioTracks)).Msg("async probe complete")
}

func (m *Manager) probeOutputFile(s *Session) *media.ProbeResult {
	for i := 0; i < 60; i++ {
		if s.isDone() {
			return nil
		}
		if s.getBuffered() > 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if s.getBuffered() == 0 {
		return nil
	}

	time.Sleep(time.Second)

	probeCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result, err := avprobe.Probe(probeCtx, s.FilePath, "")
	if err != nil {
		m.log.Warn().Err(err).Str("session_id", s.ID).Msg("output file probe failed")
		return nil
	}
	return result
}

func (m *Manager) logSignalAsync(sessionID, channelID, streamURL string) {
	info, err := tvsatipscan.QuerySignal(streamURL, 5*time.Second)
	if err != nil {
		m.log.Warn().Err(err).Str("session_id", sessionID).Str("channel_id", channelID).Msg("satip signal query failed")
		return
	}
	if info == nil {
		m.log.Warn().Str("session_id", sessionID).Str("channel_id", channelID).Msg("satip signal query returned no data")
		return
	}
	m.log.Info().
		Str("session_id", sessionID).
		Str("channel_id", channelID).
		Bool("lock", info.Lock).
		Int("level", info.Level).
		Int("level_pct", info.LevelPct()).
		Int("quality", info.Quality).
		Int("quality_pct", info.QualityPct()).
		Int("ber", info.BER).
		Int("fe_id", info.FeID).
		Float64("freq_mhz", info.FreqMHz).
		Int("bw_mhz", info.BwMHz).
		Str("msys", info.Msys).
		Str("mtype", info.Mtype).
		Str("plp_id", info.PLPID).
		Str("t2_id", info.T2ID).
		Int("bitrate_kbps", info.BitratKbps).
		Bool("active", info.Active).
		Str("server", info.Server).
		Msg("satip tuner signal at session start")
}

func (m *Manager) runPipeline(ctx context.Context, s *Session) {
	defer s.markDone()
	m.log.Info().Str("session_id", s.ID).Str("channel_id", s.ChannelID).Msg("runPipeline started")

	probe := m.ensureProbe(ctx, s.startOpts)
	if ctx.Err() != nil {
		return
	}
	if probe != nil {
		s.SetProbeInfo(probe.Video, probe.AudioTracks, probe.Duration)
	}

	srcVideo := ""
	srcAudio := ""
	container := ""
	srcWidth := 0
	srcHeight := 0
	if probe != nil {
		if probe.Video != nil {
			srcVideo = probe.Video.Codec
		}
		srcWidth = probe.Width
		srcHeight = probe.Height
		if len(probe.AudioTracks) > 0 {
			srcAudio = probe.AudioTracks[0].Codec
		}
		container = probe.FormatName
	}

	hwAccel := gstreamer.HWNone
	switch s.startOpts.OutputHWAccel {
	case "vaapi":
		hwAccel = gstreamer.HWVAAPI
	case "qsv":
		hwAccel = gstreamer.HWQSV
	case "videotoolbox":
		hwAccel = gstreamer.HWVideoToolbox
	case "nvenc":
		hwAccel = gstreamer.HWNVENC
	}

	outFormat := gstreamer.OutputMP4
	if s.startOpts.OutputContainer == "mpegts" {
		outFormat = gstreamer.OutputMPEGTS
	}

	var extraHeaders map[string]string
	if m.config.BypassHeader != "" && m.config.BypassSecret != "" {
		extraHeaders = map[string]string{m.config.BypassHeader: m.config.BypassSecret}
	}

	userAgent := m.config.UserAgent
	if s.startOpts.HTTPUserAgent != "" {
		userAgent = s.startOpts.HTTPUserAgent
	}

	opts := gstreamer.PipelineOpts{
		InputURL:         s.StreamURL,
		UserAgent:        userAgent,
		ExtraHeaders:     extraHeaders,
		VideoCodec:       srcVideo,
		AudioCodec:       srcAudio,
		Container:        container,
		OutputVideoCodec: s.startOpts.OutputVideoCodec,
		OutputAudioCodec: s.startOpts.OutputAudioCodec,
		OutputBitrate:    0,
		OutputFormat:     outFormat,
		HWAccel:          hwAccel,
		RecordingPath:    s.FilePath,
		IsLive:           probe == nil || probe.Duration == 0,

		Deinterlace:       s.startOpts.Deinterlace,
		DeinterlaceMethod: s.startOpts.DeinterlaceMethod,
		AudioDelayMs:      s.startOpts.AudioDelayMs,
		AudioChannels:     s.startOpts.AudioChannels,
		AudioLanguage:     s.startOpts.AudioLanguage,
		VideoQueueMs:      s.startOpts.VideoQueueMs,
		AudioQueueMs:      s.startOpts.AudioQueueMs,
		RTSPLatency:       s.startOpts.RTSPLatency,
		RTSPProtocols:     s.startOpts.RTSPProtocols,
		RTSPBufferMode:    s.startOpts.RTSPBufferMode,
		HTTPTimeoutSec:    s.startOpts.HTTPTimeoutSec,
		HTTPRetries:       s.startOpts.HTTPRetries,
		TSSetTimestamps:   s.startOpts.TSSetTimestamps,
		EncoderBitrateKbps: s.startOpts.EncoderBitrateKbps,
		SourceWidth:        srcWidth,
		SourceHeight:       srcHeight,
	}

	m.log.Info().
		Str("session_id", s.ID).
		Str("channel", s.ChannelName).
		Str("src_video", srcVideo).
		Str("src_audio", srcAudio).
		Str("container", container).
		Str("out_video", s.startOpts.OutputVideoCodec).
		Str("out_container", s.startOpts.OutputContainer).
		Str("hw", s.startOpts.OutputHWAccel).
		Bool("deinterlace", s.startOpts.Deinterlace).
		Int("audio_delay", s.startOpts.AudioDelayMs).
		Int("width", srcWidth).
		Int("height", srcHeight).
		Msg("building pipeline")

	pipeline, path, err := gstreamer.Build(opts)
	if err != nil {
		m.log.Error().Err(err).Str("session_id", s.ID).Msg("pipeline build failed")
		s.setError(fmt.Errorf("pipeline build failed: %w", err))
		return
	}

	m.log.Info().Str("session_id", s.ID).Str("path", path).Str("output", s.FilePath).Msg("pipeline built, setting to PLAYING")
	s.SetStopPipeline(func() { pipeline.SetState(gst.StateNull) })
	stateErr := pipeline.SetState(gst.StatePlaying)
	if stateErr != nil {
		m.log.Error().Err(stateErr).Str("session_id", s.ID).Msg("pipeline SetState PLAYING failed")
	} else {
		m.log.Info().Str("session_id", s.ID).Msg("pipeline PLAYING")
	}
	isLive := opts.IsLive
	go m.pollFileProgress(ctx, s)

	bus := pipeline.GetBus()
pipeloop:
	for {
		msg := bus.TimedPop(gst.ClockTime(500000000))
		if ctx.Err() != nil {
			break pipeloop
		}
		if msg == nil {
			continue
		}
		switch msg.Type() {
		case gst.MessageEOS:
			if isLive {
				m.log.Warn().Str("session_id", s.ID).Msg("gstreamer EOS on live stream (source dropped)")
			} else {
				m.log.Info().Str("session_id", s.ID).Msg("gstreamer EOS (VOD complete)")
			}
			break pipeloop
		case gst.MessageError:
			gstErr := msg.ParseError()
			errStr := gstErr.Error()
			if strings.Contains(errStr, "Could not multiplex") || strings.Contains(errStr, "clock problem") {
				m.log.Warn().Str("session_id", s.ID).Err(gstErr).Msg("gstreamer transient error (continuing)")
				continue
			}
			m.log.Error().Str("session_id", s.ID).Err(gstErr).Msg("gstreamer error")
			userErr := friendlyGstError(errStr)
			s.setError(fmt.Errorf("%s", userErr))
			s.setLastStderr(gstErr.Error())
			break pipeloop
		}
	}

	pipeline.SetState(gst.StateNull)
	m.log.Info().Str("session_id", s.ID).Msg("pipeline stopped")
}

func friendlyGstError(err string) string {
	switch {
	case strings.Contains(err, "Could not multiplex"):
		return "Stream encoding error — audio/video sync issue"
	case strings.Contains(err, "not-negotiated"):
		return "Stream format not supported"
	case strings.Contains(err, "Service Unavailable"):
		return "Source stream unavailable (503)"
	case strings.Contains(err, "Not Found"):
		return "Source stream not found (404)"
	case strings.Contains(err, "no valid or supported streams"):
		return "Source contains no playable streams"
	case strings.Contains(err, "Internal data stream"):
		return "Internal pipeline error"
	default:
		return err
	}
}

func isValidProbe(p *media.ProbeResult) bool {
	return p != nil && (p.Video != nil || len(p.AudioTracks) > 0 || p.Duration > 0)
}

func (m *Manager) ensureProbe(ctx context.Context, opts StartOpts) *media.ProbeResult {
	if m.probeCache != nil {
		if opts.StreamID != "" {
			if cached, _ := m.probeCache.GetProbeByStreamID(opts.StreamID); isValidProbe(cached) {
				m.log.Debug().Str("stream_id", opts.StreamID).Float64("duration", cached.Duration).Msg("probe cache hit (by ID)")
				return cached
			}
		}
		if opts.StreamURL != "" {
			if cached, _ := m.probeCache.GetProbe(media.StreamHash(opts.StreamURL)); isValidProbe(cached) {
				m.log.Debug().Str("stream_url", opts.StreamURL).Float64("duration", cached.Duration).Msg("probe cache hit (by URL)")
				return cached
			}
		}
	}

	m.log.Info().Str("stream_url", opts.StreamURL).Msg("no probe cache — probing now (blocking)")
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	result, err := avprobe.Probe(probeCtx, opts.StreamURL, m.config.UserAgent)
	if err != nil || result == nil {
		m.log.Warn().Err(err).Str("stream_url", opts.StreamURL).Msg("pre-probe failed")
		return nil
	}

	if m.probeCache != nil {
		if opts.StreamID != "" {
			m.probeCache.SaveProbeByStreamID(opts.StreamID, result)
		}
		m.probeCache.SaveProbe(media.StreamHash(opts.StreamURL), result)
	}

	videoCodec := ""
	if result.Video != nil {
		videoCodec = result.Video.Codec
	}
	m.log.Info().
		Str("video", videoCodec).
		Str("container", result.FormatName).
		Msg("pre-probe complete")
	return result
}

func (m *Manager) pollFileProgress(ctx context.Context, s *Session) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			info, err := os.Stat(s.FilePath)
			if err == nil && info.Size() > 0 {
				secs := float64(info.Size()) / 500000.0
				s.setBuffered(secs)
			}
		}
	}
}

