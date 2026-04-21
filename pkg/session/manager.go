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

	"github.com/gavinmcnair/tvproxy/pkg/config"
	"github.com/gavinmcnair/tvproxy/pkg/gstreamer"
	"github.com/gavinmcnair/tvproxy/pkg/media"
	"github.com/gavinmcnair/tvproxy/pkg/store"
	"github.com/gavinmcnair/tvproxy/pkg/tvsatipscan"
)

const maxBufferedSecs = 172800.0

// StartOpts configures a new session. Fields are used by two pipeline paths:
//
// Native pipeline (executor + PipelineSpec): SourceVideoCodec, OutputVideoCodec,
// OutputAudioCodec, OutputContainer, OutputHWAccel, DecodeHWAccel, EncoderBitrateKbps,
// OutputHeight, AudioLanguage, Delivery.
//
// String pipeline (proxy.go subprocess): all fields including Deinterlace, AudioDelayMs,
// VideoQueueMs, AudioQueueMs, RTSP*, HTTP*, TSSetTimestamps, VideoEncoderElement,
// VideoDecoderElement.
//
// Fields marked "deferred" below are populated by source profiles but not yet wired
// into the native pipeline builder. They remain for future use.
type StartOpts struct {
	ChannelID        string
	StreamID         string
	StreamURL        string
	StreamName       string
	ChannelName      string
	ProfileName      string
	SourceVideoCodec string
	OutputVideoCodec string
	OutputAudioCodec string
	OutputContainer  string
	OutputHWAccel        string
	DecodeHWAccel        string
	VideoEncoderElement  string // string pipeline only; native uses tvproxyencode
	VideoDecoderElement  string // string pipeline only; native uses tvproxydecode
	UseWireGuard         bool
	KnownDuration    float64
	SeekOffset       float64
	OutputDir         string

	MetadataOnly      bool

	Deinterlace       bool   // deferred: needs vapostproc/deinterlace element
	DeinterlaceMethod string // deferred
	AudioLanguage     string
	RTSPLatency       int    // wired to tvproxysrc rtsp-latency
	RTSPProtocols     string // wired to tvproxysrc rtsp-transport
	HTTPTimeoutSec    int    // wired to tvproxysrc timeout
	HTTPUserAgent     string // wired to tvproxysrc user-agent
	EncoderBitrateKbps int
	Delivery           string
	OutputHeight       int   // deferred: needs videoscale element
}

type Manager struct {
	sessions      map[string]*Session
	config        *config.Config
	httpClient    *http.Client
	wgClient      *http.Client
	wgProxyMgr    *WGProxyManager
	executor      *gstreamer.Executor
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
		executor:    gstreamer.NewExecutor(log),
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
	if s.SessionWatcher != nil {
		s.SessionWatcher.Close()
	}
	if !s.Recorded {
		os.RemoveAll(s.TempDir)
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
	s.Record()
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

	if s.SessionWatcher != nil {
		s.SessionWatcher.Close()
	}
	if !s.Recorded && !s.wasRecording {
		os.RemoveAll(s.TempDir)
	}

	m.log.Info().
		Str("channel_id", channelID).
		Str("session_id", s.ID).
		Bool("preserved", s.Recorded || s.wasRecording).
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
			info, statErr := f.Stat()
			if statErr == nil && info.Size() > 0 {
				break
			}
			f.Close()
			f = nil
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
		return nil, fmt.Errorf("timed out waiting for session data after %d retries", retryCount)
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
	m.mu.RLock()
	s, ok := m.sessions[channelID]
	m.mu.RUnlock()
	if !ok {
		return
	}

	if s.Seek(position) {
		m.log.Info().Str("channel_id", channelID).Float64("position", position).Msg("CGO seek (pipeline stays running)")
		return
	}

	m.log.Info().Str("channel_id", channelID).Float64("position", position).Msg("restarting session with seek")
	m.mu.Lock()
	opts := s.startOpts
	oldFile := s.FilePath
	s.cancel()
	delete(m.sessions, channelID)
	m.mu.Unlock()

	select {
	case <-s.done:
	case <-time.After(3 * time.Second):
	}

	if oldFile != "" {
		os.Remove(oldFile)
	}

	opts.KnownDuration = s.Duration
	opts.SeekOffset = position
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
		Delivery:         opts.Delivery,
		UseWireGuard:     opts.UseWireGuard,
		Duration:         opts.KnownDuration,
		SeekOffset:       opts.SeekOffset,
		FilePath:         filePath,
		TempDir:          tempDir,
		OutputDir:        tempDir,
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

	if m.probeCache != nil && opts.StreamID != "" {
		if cached, _ := m.probeCache.GetProbe(opts.StreamID); cached != nil {
			s.SetProbeInfo(cached.Video, cached.AudioTracks, cached.Duration)
		}
	}

	m.sessions[opts.ChannelID] = s
	m.mu.Unlock()

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
		if s.SessionWatcher != nil {
			s.SessionWatcher.Close()
		}
		if !s.Recorded && !s.HasRecordingConsumer() {
			os.RemoveAll(s.TempDir)
		}
	}

	m.log.Info().Int("sessions", len(sessions)).Msg("session manager shutdown complete")
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

	isMSE := s.startOpts.Delivery == "mse"
	isLive := s.Duration == 0

	pipelineURL := s.StreamURL
	if s.UseWireGuard && strings.HasPrefix(pipelineURL, "http") && m.wgProxyMgr != nil {
		if proxy := m.wgProxyMgr.GetAny(); proxy != nil {
			pipelineURL = proxy.ProxyURL(s.StreamURL)
			m.log.Info().Str("session_id", s.ID).Str("proxy_url", pipelineURL).Msg("routing through WG proxy")
		}
	}

	outCodec := gstreamer.NormalizeCodec(s.startOpts.OutputVideoCodec)
	srcCodec := gstreamer.NormalizeCodec(s.startOpts.SourceVideoCodec)
	needsTranscode := resolveNeedsTranscode(outCodec, srcCodec)

	containerHint := ""
	if s.startOpts.OutputContainer == "mpegts" || isLive {
		containerHint = "mpegts"
	}

	// StartOpts fields intentionally NOT mapped to SessionOpts:
	//
	// Deferred (no native builder element yet):
	//   Deinterlace, DeinterlaceMethod — needs vapostproc/deinterlace in pipeline builder
	//   OutputHeight — needs videoscale + capsfilter in pipeline builder
	//
	// These fields remain in StartOpts for the string pipeline builder (proxy.go)
	// and the source profile UI. They are not dead code.
	maxBitDepth := 0
	if !gstreamer.Decode10BitSupported() {
		maxBitDepth = 8
	}

	userAgent := m.config.UserAgent
	if s.startOpts.HTTPUserAgent != "" {
		userAgent = s.startOpts.HTTPUserAgent
	}

	sessionOpts := gstreamer.SessionOpts{
		SourceURL:     pipelineURL,
		IsLive:        isLive,
		IsFileSource:  !strings.HasPrefix(pipelineURL, "http") && !strings.HasPrefix(pipelineURL, "rtsp"),
		UserAgent:     userAgent,
		HTTPTimeout:   s.startOpts.HTTPTimeoutSec,
		RTSPLatency:   s.startOpts.RTSPLatency,
		RTSPTransport: s.startOpts.RTSPProtocols,
		VideoCodec:    srcCodec,
		ContainerHint: containerHint,
		NeedsTranscode:      needsTranscode,
		HWAccel:             s.startOpts.OutputHWAccel,
		DecodeHWAccel:       s.startOpts.DecodeHWAccel,
		OutputCodec:         outCodec,
		Bitrate:             s.startOpts.EncoderBitrateKbps,
		OutputHeight:        s.startOpts.OutputHeight,
		MaxBitDepth:         maxBitDepth,
		VideoDecoderElement: s.startOpts.VideoDecoderElement,
		VideoEncoderElement: s.startOpts.VideoEncoderElement,
		AudioChannels:  2,
		AudioLanguage:  s.startOpts.AudioLanguage,
		OutputDir:      s.OutputDir,
		MuxOutputPath:  s.FilePath,
	}

	m.log.Info().
		Str("session_id", s.ID).
		Str("channel", s.ChannelName).
		Str("stream_url", pipelineURL).
		Str("src_video", srcCodec).
		Str("out_video", outCodec).
		Str("hw", s.startOpts.OutputHWAccel).
		Bool("transcode", needsTranscode).
		Bool("is_live", isLive).
		Bool("mse", isMSE).
		Str("output_dir", s.OutputDir).
		Str("profile", s.startOpts.ProfileName).
		Msg("building pipeline")

	var spec gstreamer.PipelineSpec
	if isMSE {
		spec = BuildMSEPipeline(sessionOpts)
	} else if needsTranscode {
		spec = BuildStreamTranscodePipeline(sessionOpts)
	} else {
		spec = BuildStreamPipeline(sessionOpts)
	}

	pipeline, buildErr := m.executor.Build(spec)
	if buildErr != nil {
		m.log.Error().Err(buildErr).Str("session_id", s.ID).Msg("pipeline build failed")
		s.setError(fmt.Errorf("pipeline build failed: %w", buildErr))
		return
	}
	s.SetStopPipeline(func() { pipeline.SetState(gst.StateNull) })

	if isMSE {
		w, wErr := NewWatcher(s.OutputDir, m.log)
		if wErr != nil {
			m.log.Error().Err(wErr).Str("session_id", s.ID).Msg("failed to create watcher")
			s.setError(fmt.Errorf("watcher creation failed: %w", wErr))
			pipeline.SetState(gst.StateNull)
			return
		}
		s.SessionWatcher = w

		s.SetSeekFunc(func(position float64) {
			m.log.Info().Str("session_id", s.ID).Float64("position", position).Msg("seek — restarting pipeline")
			w.Reset()
			pipeline.SetState(gst.StateNull)
		})
	}

	if err := pipeline.SetState(gst.StatePlaying); err != nil {
		m.log.Error().Err(err).Str("session_id", s.ID).Msg("pipeline SetState PLAYING failed")
	} else {
		m.log.Info().Str("session_id", s.ID).Msg("pipeline PLAYING")
	}

	if s.startOpts.SeekOffset > 0 && !isLive {
		go func() {
			time.Sleep(3 * time.Second)
			seekNs := int64(s.startOpts.SeekOffset * 1e9)
			m.log.Info().Str("session_id", s.ID).Float64("seek_to", s.startOpts.SeekOffset).Msg("deferred seek after preroll")
			seekEvt := gst.NewSeekEvent(1.0, gst.FormatTime, gst.SeekFlagFlush|gst.SeekFlagKeyUnit, gst.SeekTypeSet, seekNs, gst.SeekTypeNone, 0)
			pipeline.SendEvent(seekEvt)
		}()
	}

	go m.pollFileProgress(ctx, s)

	if m.probeCache != nil && s.StreamID != "" {
		go m.cachePassiveMetadata(ctx, s)
	}

	if isMSE {
		<-ctx.Done()
		m.log.Info().Str("session_id", s.ID).Msg("MSE session ended (context cancelled)")
		pipeline.SetState(gst.StateNull)
		return
	}

	if !isLive {
		result := m.executor.RunBusLoop(ctx, pipeline, false)
		pipeline.SetState(gst.StateNull)
		if result.Err != nil {
			s.setError(fmt.Errorf("%s", friendlyGstError(result.Err.Error())))
		}
		m.log.Info().Str("session_id", s.ID).Str("reason", result.ExitReason).Msg("VOD pipeline stopped")
		return
	}

	const maxRetries = 3
	const retryBaseDelay = 2 * time.Second
	retryCount := 0

	for {
		result := m.executor.RunBusLoop(ctx, pipeline, true)
		pipeline.SetState(gst.StateNull)

		if ctx.Err() != nil {
			m.log.Info().Str("session_id", s.ID).Msg("pipeline stopped (context cancelled)")
			break
		}

		if result.ExitReason == "vod_complete" {
			m.log.Info().Str("session_id", s.ID).Msg("pipeline stopped (complete)")
			break
		}

		if result.Err != nil {
			s.setError(fmt.Errorf("%s", friendlyGstError(result.Err.Error())))
			s.setLastStderr(result.Err.Error())
		}

		retryCount++
		if retryCount > maxRetries {
			m.log.Error().Str("session_id", s.ID).Int("retries", retryCount-1).Msg("live pipeline failed — max retries exceeded")
			s.setError(fmt.Errorf("live stream failed after %d retries", maxRetries))
			break
		}

		delay := retryBaseDelay * time.Duration(retryCount)
		m.log.Warn().Str("session_id", s.ID).Str("reason", result.ExitReason).Int("retry", retryCount).Dur("delay", delay).Msg("live pipeline dropped — restarting")

		select {
		case <-ctx.Done():
			break
		case <-time.After(delay):
		}
		if ctx.Err() != nil {
			break
		}

		newPipeline, buildErr := m.executor.Build(spec)
		if buildErr != nil {
			m.log.Error().Err(buildErr).Str("session_id", s.ID).Msg("pipeline rebuild failed")
			s.setError(fmt.Errorf("pipeline rebuild failed: %w", buildErr))
			break
		}

		s.SetStopPipeline(func() { newPipeline.SetState(gst.StateNull) })
		pipeline = newPipeline

		if s.SessionWatcher != nil {
			s.SessionWatcher.Reset()
		}

		if stateErr := pipeline.SetState(gst.StatePlaying); stateErr != nil {
			m.log.Error().Err(stateErr).Str("session_id", s.ID).Msg("pipeline restart SetState PLAYING failed")
			break
		}

		m.log.Info().Str("session_id", s.ID).Int("retry", retryCount).Msg("live pipeline restarted — PLAYING")
	}
}

func (m *Manager) cachePassiveMetadata(ctx context.Context, s *Session) {
	time.Sleep(5 * time.Second)
	if ctx.Err() != nil {
		return
	}
	video, audioTracks, duration := s.GetProbeInfo()
	if video != nil || len(audioTracks) > 0 || duration > 0 {
		result := &media.ProbeResult{
			Duration:    duration,
			IsVOD:       duration > 0,
			HasVideo:    video != nil,
			Video:       video,
			AudioTracks: audioTracks,
		}
		m.probeCache.SaveProbe(s.StreamID, result)
		m.log.Debug().Str("stream_id", s.StreamID).Msg("passive metadata cached from playback")
	}
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

func resolveNeedsTranscode(outCodec, srcCodec string) bool {
	if outCodec == "" || outCodec == "copy" || outCodec == "default" {
		return false
	}
	if srcCodec == "" {
		return true
	}
	return outCodec != srcCodec
}

func (m *Manager) pollFileProgress(ctx context.Context, s *Session) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.SessionWatcher != nil {
				vCount := s.SessionWatcher.VideoSegmentCount()
				if vCount > 0 {
					s.setBuffered(float64(vCount) * 2.0)
					continue
				}
			}
			info, err := os.Stat(s.FilePath)
			if err == nil && info.Size() > 0 {
				secs := float64(info.Size()) / 500000.0
				s.setBuffered(secs)
			}
		}
	}
}

