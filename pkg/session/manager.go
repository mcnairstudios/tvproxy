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

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/lib/av/demuxloop"
	"github.com/gavinmcnair/tvproxy/pkg/lib/av/probe"

	"github.com/gavinmcnair/tvproxy/pkg/config"
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
	SourceVideoCodec string
	OutputVideoCodec string
	OutputAudioCodec string
	OutputContainer  string
	OutputHWAccel        string
	DecodeHWAccel        string
	VideoEncoderElement  string
	VideoDecoderElement  string
	UseWireGuard         bool
	KnownDuration    float64
	SeekOffset       float64
	OutputDir         string

	MetadataOnly      bool

	Deinterlace       bool
	DeinterlaceMethod string
	AudioLanguage     string
	RTSPLatency       int
	RTSPProtocols     string
	HTTPTimeoutSec    int
	HTTPUserAgent     string
	EncoderBitrateKbps int
	Delivery           string
	OutputHeight       int
}

type StreamProbeUpdater interface {
	UpdateStreamProbeData(ctx context.Context, id string, duration float64, vcodec, acodec string) error
}

type Manager struct {
	sessions      map[string]*Session
	config        *config.Config
	httpClient    *http.Client
	wgClient      *http.Client
	wgProxyMgr    *WGProxyManager
	wgProxyFunc   func(string) string
	probeCache    store.ProbeCache
	streamStore   StreamProbeUpdater
	onCleanup     func(channelID string)
	log           zerolog.Logger
	mu            sync.RWMutex
}

func (m *Manager) SetWGProxyFunc(fn func(string) string) {
	m.wgProxyFunc = fn
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

func NewManager(cfg *config.Config, httpClient *http.Client, wgClient *http.Client, probeCache store.ProbeCache, streamStore StreamProbeUpdater, log zerolog.Logger) *Manager {
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
		streamStore: streamStore,
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

	m.log.Info().Str("channel_id", channelID).Float64("position", position).Msg("restarting pipeline in-place with seek")

	s.StopPipeline()
	s.cancel()

	select {
	case <-s.done:
	case <-time.After(3 * time.Second):
	}

	if s.SessionWatcher != nil {
		s.SessionWatcher.Reset()
	}

	segDir := filepath.Join(s.OutputDir, "segments")
	os.RemoveAll(segDir)
	os.MkdirAll(segDir, 0755)
	os.Remove(filepath.Join(s.OutputDir, "probe.pb"))

	if s.FilePath != "" {
		os.Remove(s.FilePath)
	}
	sourceTSPath := filepath.Join(s.OutputDir, "source.ts")
	os.Remove(sourceTSPath)

	s.startOpts.KnownDuration = s.Duration
	s.startOpts.SeekOffset = position
	s.SeekOffset = position

	newCtx, newCancel := context.WithCancel(context.Background())
	s.cancel = newCancel
	s.done = make(chan struct{})
	s.doneOnce = sync.Once{}
	s.setError(nil)

	go m.runPipeline(newCtx, s)

	m.log.Info().Str("channel_id", channelID).Str("session_id", s.ID).Float64("position", position).Msg("pipeline restarted for seek")
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
	if s.UseWireGuard && strings.HasPrefix(pipelineURL, "http") && m.wgProxyFunc != nil {
		pipelineURL = m.wgProxyFunc(s.StreamURL)
		m.log.Info().Str("session_id", s.ID).Str("proxy_url", pipelineURL).Msg("routing through WG proxy")
	}

	isFileSource := !strings.HasPrefix(pipelineURL, "http") && !strings.HasPrefix(pipelineURL, "rtsp")

	userAgent := m.config.UserAgent
	if s.startOpts.HTTPUserAgent != "" {
		userAgent = s.startOpts.HTTPUserAgent
	}

	ds, err := NewDemuxSession(DemuxOpts{
		URL:           pipelineURL,
		OutputDir:     s.OutputDir,
		AudioLanguage: s.startOpts.AudioLanguage,
		IsFileSource:  isFileSource,
		TimeoutSec:    s.startOpts.HTTPTimeoutSec,
		UserAgent:     userAgent,
		RTSPLatency:   s.startOpts.RTSPLatency,
		Log:           m.log.With().Str("session_id", s.ID).Logger(),
	})
	if err != nil {
		m.log.Error().Err(err).Str("session_id", s.ID).Msg("demux session creation failed")
		s.setError(fmt.Errorf("demux session creation failed: %w", err))
		return
	}
	defer ds.Close()

	info := ds.Info()
	audioIdx := ds.AudioIndex()

	if info.Video != nil {
		fps := ""
		if info.Video.FramerateD > 0 {
			fps = fmt.Sprintf("%.2f", float64(info.Video.FramerateN)/float64(info.Video.FramerateD))
		}
		s.SetProbeInfo(&media.VideoInfo{
			Codec:      info.Video.Codec,
			BitDepth:   info.Video.BitDepth,
			Interlaced: info.Video.Interlaced,
			FPS:        fps,
			BitRate:    fmt.Sprintf("%d", info.Video.BitrateKbps*1000),
		}, probeAudioTracks(info), float64(info.DurationMs)/1000.0)
	}

	outCodec := media.NormalizeCodec(s.startOpts.OutputVideoCodec)
	srcCodec := ""
	if info.Video != nil {
		srcCodec = media.NormalizeCodec(info.Video.Codec)
	}
	needsTranscode := resolveNeedsTranscode(outCodec, srcCodec)
	if !needsTranscode && (outCodec == "" || outCodec == "default" || outCodec == "copy") {
		outCodec = srcCodec
	}

	encCodec := media.BaseCodec(outCodec)

	maxBitDepth := 0

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
		Int("audio_idx", audioIdx).
		Msg("building pipeline")

	if s.startOpts.Delivery == "hls" {
		m.runGoHLS(ctx, s, ds, info, audioIdx, isLive, needsTranscode, encCodec, srcCodec, maxBitDepth)
		return
	}

	if isMSE {
		m.runGoMSE(ctx, s, ds, info, audioIdx, isLive, needsTranscode, encCodec, srcCodec, maxBitDepth)
		return
	}

	if !needsTranscode {
		m.runGoStreamCopy(ctx, s, ds, info, audioIdx, isLive)
		return
	}

	if outCodec == srcCodec {
		m.runGoAudioTranscode(ctx, s, ds, info, audioIdx, isLive)
		return
	}

	m.runGoFullTranscode(ctx, s, ds, info, audioIdx, isLive, encCodec, maxBitDepth)
}

func (m *Manager) runGoStreamCopy(ctx context.Context, s *Session, ds *DemuxSession, info *probe.StreamInfo, audioIdx int, isLive bool) {
	format := "mpegts"
	if s.startOpts.OutputContainer == "mp4" {
		format = "mp4"
	}

	gp, err := NewStreamCopyPipeline(StreamCopyOpts{
		Info:             info,
		AudioIndex:       audioIdx,
		FilePath:         s.FilePath,
		Format:           format,
		OutputAudioCodec: s.startOpts.OutputAudioCodec,
		Log:              m.log.With().Str("session_id", s.ID).Logger(),
	})
	if err != nil {
		m.log.Error().Err(err).Str("session_id", s.ID).Msg("Go stream copy pipeline creation failed")
		s.setError(fmt.Errorf("stream copy pipeline failed: %w", err))
		return
	}
	s.SetStopPipeline(func() { gp.Stop() })

	if s.startOpts.SeekOffset > 0 && !isLive {
		seekMs := int64(s.startOpts.SeekOffset * 1000)
		if seekErr := ds.Demuxer().SeekTo(seekMs); seekErr != nil {
			m.log.Warn().Err(seekErr).Str("session_id", s.ID).Msg("initial seek failed")
		}
	}

	ds.Demuxer().SetOnSeek(func() {
		m.log.Info().Str("session_id", s.ID).Msg("seek: stream copy reset")
	})

	s.SetSeekFunc(func(position float64) {
		seekMs := int64(position * 1000)
		if err := ds.Demuxer().RequestSeek(seekMs); err != nil {
			m.log.Warn().Err(err).Str("session_id", s.ID).Float64("position", position).Msg("seek request failed")
		} else {
			m.log.Info().Str("session_id", s.ID).Float64("position", position).Msg("seek requested")
		}
	})

	go m.pollFileProgress(ctx, s)

	if m.probeCache != nil && s.StreamID != "" {
		go m.cachePassiveMetadata(ctx, s)
	}

	m.log.Info().Str("session_id", s.ID).Str("format", format).Msg("Go stream copy pipeline started")

	demuxErr := ds.RunWithSink(ctx, gp)

	gp.Stop()

	if ctx.Err() != nil {
		m.log.Info().Str("session_id", s.ID).Msg("stream copy ended (context cancelled)")
		return
	}

	if demuxErr != nil {
		if !isLive {
			s.setError(fmt.Errorf("stream copy error: %w", demuxErr))
			return
		}
		m.log.Warn().Err(demuxErr).Str("session_id", s.ID).Msg("live stream copy failed")
		s.setError(fmt.Errorf("live stream copy failed: %w", demuxErr))
	} else {
		m.log.Info().Str("session_id", s.ID).Msg("stream copy completed")
	}
}

func (m *Manager) runGoMSE(ctx context.Context, s *Session, ds *DemuxSession, info *probe.StreamInfo, audioIdx int, isLive, needsTranscode bool, outCodec, srcCodec string, maxBitDepth int) {
	forceDecode := needsTranscode || s.startOpts.EncoderBitrateKbps > 0 ||
		(s.startOpts.OutputHeight > 0 && info.Video != nil && s.startOpts.OutputHeight < info.Video.Height)

	var sink demuxloop.PacketSink
	var stopFn func()

	if !forceDecode {
		gp, err := NewMSECopyPipeline(MSECopyOpts{
			Info:             info,
			AudioIndex:       audioIdx,
			OutputDir:        s.OutputDir,
			IsLive:           isLive,
			OutputAudioCodec: s.startOpts.OutputAudioCodec,
			VideoCodecParams: ds.Demuxer().VideoCodecParameters(), AudioCodecParams: ds.Demuxer().AudioCodecParameters(),
			Log:              m.log.With().Str("session_id", s.ID).Logger(),
		})
		if err != nil {
			m.log.Error().Err(err).Str("session_id", s.ID).Msg("Go MSE copy pipeline failed")
			s.setError(fmt.Errorf("MSE copy pipeline failed: %w", err))
			return
		}
		sink = gp
		stopFn = func() { gp.Stop() }
		m.log.Info().Str("session_id", s.ID).Str("codec", srcCodec).Msg("MSE copy mode — no decode/encode")
	} else {
		gp, err := NewMSETranscodePipeline(MSETranscodeOpts{
			Info:             info,
			AudioIndex:       audioIdx,
			OutputDir:        s.OutputDir,
			IsLive:           isLive,
			HWAccel:          s.startOpts.OutputHWAccel,
			DecodeHWAccel:    s.startOpts.DecodeHWAccel,
			OutputCodec:      outCodec,
			OutputAudioCodec: s.startOpts.OutputAudioCodec,
			Bitrate:          s.startOpts.EncoderBitrateKbps,
			OutputHeight:     s.startOpts.OutputHeight,
			Deinterlace:      s.startOpts.Deinterlace,
			MaxBitDepth:      maxBitDepth,
			VideoCodecParams: ds.Demuxer().VideoCodecParameters(), AudioCodecParams: ds.Demuxer().AudioCodecParameters(),
			Log:              m.log.With().Str("session_id", s.ID).Logger(),
		})
		if err != nil {
			m.log.Error().Err(err).Str("session_id", s.ID).Msg("Go MSE transcode pipeline failed")
			s.setError(fmt.Errorf("MSE pipeline failed: %w", err))
			return
		}
		sink = gp
		stopFn = func() { gp.Stop() }
	}
	s.SetStopPipeline(stopFn)

	w, wErr := NewWatcher(s.OutputDir, m.log)
	if wErr != nil {
		m.log.Error().Err(wErr).Str("session_id", s.ID).Msg("failed to create watcher")
		s.setError(fmt.Errorf("watcher creation failed: %w", wErr))
		stopFn()
		return
	}
	s.SessionWatcher = w

	if s.startOpts.SeekOffset > 0 && !isLive {
		seekMs := int64(s.startOpts.SeekOffset * 1000)
		if seekErr := ds.Demuxer().SeekTo(seekMs); seekErr != nil {
			m.log.Warn().Err(seekErr).Str("session_id", s.ID).Msg("initial seek failed")
		}
	}

	type seekResetter interface {
		ResetForSeek()
	}
	ds.Demuxer().SetOnSeek(func() {
		if w != nil {
			w.Reset()
		}
		if sr, ok := sink.(seekResetter); ok {
			sr.ResetForSeek()
		}
		m.log.Info().Str("session_id", s.ID).Msg("seek: watcher+muxer reset")
	})

	s.SetSeekFunc(func(position float64) {
		seekMs := int64(position * 1000)
		if err := ds.Demuxer().RequestSeek(seekMs); err != nil {
			m.log.Warn().Err(err).Str("session_id", s.ID).Float64("position", position).Msg("seek request failed")
		} else {
			m.log.Info().Str("session_id", s.ID).Float64("position", position).Msg("seek requested")
		}
	})

	go m.pollFileProgress(ctx, s)

	if m.probeCache != nil && s.StreamID != "" {
		go m.cachePassiveMetadata(ctx, s)
	}

	m.log.Info().Str("session_id", s.ID).Bool("transcode", forceDecode).Msg("Go MSE pipeline started")

	demuxErr := ds.RunWithSink(ctx, sink)

	stopFn()

	if ctx.Err() != nil {
		m.log.Info().Str("session_id", s.ID).Msg("MSE session ended (context cancelled)")
		return
	}

	if demuxErr == nil {
		m.log.Info().Str("session_id", s.ID).Msg("MSE session completed")
		return
	}

	if !isLive {
		s.setError(fmt.Errorf("MSE error: %w", demuxErr))
		return
	}

	m.log.Warn().Err(demuxErr).Str("session_id", s.ID).Msg("live MSE failed")
	s.setError(fmt.Errorf("live MSE failed: %w", demuxErr))
}

func (m *Manager) runGoHLS(ctx context.Context, s *Session, ds *DemuxSession, info *probe.StreamInfo, audioIdx int, isLive, needsTranscode bool, outCodec, srcCodec string, maxBitDepth int) {
	forceDecode := needsTranscode || s.startOpts.EncoderBitrateKbps > 0 ||
		(s.startOpts.OutputHeight > 0 && info.Video != nil && s.startOpts.OutputHeight < info.Video.Height)

	segDuration := 6

	var sink demuxloop.PacketSink
	var stopFn func()

	if !forceDecode {
		gp, err := NewHLSCopyPipeline(HLSCopyOpts{
			Info:             info,
			AudioIndex:       audioIdx,
			OutputDir:        s.OutputDir,
			IsLive:           isLive,
			SegmentDuration:  segDuration,
			OutputAudioCodec: s.startOpts.OutputAudioCodec,
			VideoCodecParams: ds.Demuxer().VideoCodecParameters(), AudioCodecParams: ds.Demuxer().AudioCodecParameters(),
			Log:              m.log.With().Str("session_id", s.ID).Logger(),
		})
		if err != nil {
			m.log.Error().Err(err).Str("session_id", s.ID).Msg("Go HLS copy pipeline failed")
			s.setError(fmt.Errorf("HLS copy pipeline failed: %w", err))
			return
		}
		sink = gp
		stopFn = func() { gp.Stop() }
		m.log.Info().Str("session_id", s.ID).Str("codec", srcCodec).Msg("HLS copy mode — no decode/encode")
	} else {
		gp, err := NewHLSTranscodePipeline(HLSTranscodeOpts{
			Info:             info,
			AudioIndex:       audioIdx,
			OutputDir:        s.OutputDir,
			IsLive:           isLive,
			SegmentDuration:  segDuration,
			HWAccel:          s.startOpts.OutputHWAccel,
			DecodeHWAccel:    s.startOpts.DecodeHWAccel,
			OutputCodec:      outCodec,
			OutputAudioCodec: s.startOpts.OutputAudioCodec,
			Bitrate:          s.startOpts.EncoderBitrateKbps,
			OutputHeight:     s.startOpts.OutputHeight,
			Deinterlace:      s.startOpts.Deinterlace,
			MaxBitDepth:      maxBitDepth,
			VideoCodecParams: ds.Demuxer().VideoCodecParameters(), AudioCodecParams: ds.Demuxer().AudioCodecParameters(),
			Log:              m.log.With().Str("session_id", s.ID).Logger(),
		})
		if err != nil {
			m.log.Error().Err(err).Str("session_id", s.ID).Msg("Go HLS transcode pipeline failed")
			s.setError(fmt.Errorf("HLS pipeline failed: %w", err))
			return
		}
		sink = gp
		stopFn = func() { gp.Stop() }
	}
	s.SetStopPipeline(stopFn)

	w, wErr := NewWatcher(s.OutputDir, m.log)
	if wErr != nil {
		m.log.Error().Err(wErr).Str("session_id", s.ID).Msg("failed to create watcher")
		s.setError(fmt.Errorf("watcher creation failed: %w", wErr))
		stopFn()
		return
	}
	s.SessionWatcher = w

	if s.startOpts.SeekOffset > 0 && !isLive {
		seekMs := int64(s.startOpts.SeekOffset * 1000)
		if seekErr := ds.Demuxer().SeekTo(seekMs); seekErr != nil {
			m.log.Warn().Err(seekErr).Str("session_id", s.ID).Msg("initial seek failed")
		}
	}

	type seekResetter interface {
		ResetForSeek()
	}
	ds.Demuxer().SetOnSeek(func() {
		if w != nil {
			w.Reset()
		}
		if sr, ok := sink.(seekResetter); ok {
			sr.ResetForSeek()
		}
		m.log.Info().Str("session_id", s.ID).Msg("seek: watcher+muxer reset (HLS)")
	})

	s.SetSeekFunc(func(position float64) {
		seekMs := int64(position * 1000)
		if err := ds.Demuxer().RequestSeek(seekMs); err != nil {
			m.log.Warn().Err(err).Str("session_id", s.ID).Float64("position", position).Msg("seek request failed")
		} else {
			m.log.Info().Str("session_id", s.ID).Float64("position", position).Msg("seek requested")
		}
	})

	go m.pollFileProgress(ctx, s)

	if m.probeCache != nil && s.StreamID != "" {
		go m.cachePassiveMetadata(ctx, s)
	}

	m.log.Info().Str("session_id", s.ID).Bool("transcode", forceDecode).Msg("Go HLS pipeline started")

	demuxErr := ds.RunWithSink(ctx, sink)

	stopFn()

	if ctx.Err() != nil {
		m.log.Info().Str("session_id", s.ID).Msg("HLS session ended (context cancelled)")
		return
	}

	if demuxErr == nil {
		m.log.Info().Str("session_id", s.ID).Msg("HLS session completed")
		return
	}

	if !isLive {
		s.setError(fmt.Errorf("HLS error: %w", demuxErr))
		return
	}

	m.log.Warn().Err(demuxErr).Str("session_id", s.ID).Msg("live HLS failed")
	s.setError(fmt.Errorf("live HLS failed: %w", demuxErr))
}

func (m *Manager) runGoFullTranscode(ctx context.Context, s *Session, ds *DemuxSession, info *probe.StreamInfo, audioIdx int, isLive bool, outCodec string, maxBitDepth int) {
	format := "mpegts"
	if s.startOpts.OutputContainer == "mp4" {
		format = "mp4"
	}

	gp, err := NewFullTranscodePipeline(FullTranscodeOpts{
		Info:             info,
		AudioIndex:       audioIdx,
		FilePath:         s.FilePath,
		OutputDir:        s.OutputDir,
		Format:           format,
		IsLive:           isLive,
		HWAccel:          s.startOpts.OutputHWAccel,
		DecodeHWAccel:    s.startOpts.DecodeHWAccel,
		OutputCodec:      outCodec,
		OutputAudioCodec: s.startOpts.OutputAudioCodec,
		Bitrate:          s.startOpts.EncoderBitrateKbps,
		OutputHeight:     s.startOpts.OutputHeight,
		Deinterlace:      s.startOpts.Deinterlace,
		MaxBitDepth:      maxBitDepth,
		VideoCodecParams: ds.Demuxer().VideoCodecParameters(), AudioCodecParams: ds.Demuxer().AudioCodecParameters(),
		Log:              m.log.With().Str("session_id", s.ID).Logger(),
	})
	if err != nil {
		m.log.Error().Err(err).Str("session_id", s.ID).Msg("Go full transcode pipeline creation failed")
		s.setError(fmt.Errorf("full transcode pipeline failed: %w", err))
		return
	}
	s.SetStopPipeline(func() { gp.Stop() })

	if s.startOpts.SeekOffset > 0 && !isLive {
		seekMs := int64(s.startOpts.SeekOffset * 1000)
		if seekErr := ds.Demuxer().SeekTo(seekMs); seekErr != nil {
			m.log.Warn().Err(seekErr).Str("session_id", s.ID).Msg("initial seek failed")
		}
	}

	type seekResetter interface {
		ResetForSeek()
	}
	ds.Demuxer().SetOnSeek(func() {
		if sr, ok := interface{}(gp).(seekResetter); ok {
			sr.ResetForSeek()
		}
		m.log.Info().Str("session_id", s.ID).Msg("seek: full transcode reset")
	})

	s.SetSeekFunc(func(position float64) {
		seekMs := int64(position * 1000)
		if err := ds.Demuxer().RequestSeek(seekMs); err != nil {
			m.log.Warn().Err(err).Str("session_id", s.ID).Float64("position", position).Msg("seek request failed")
		} else {
			m.log.Info().Str("session_id", s.ID).Float64("position", position).Msg("seek requested")
		}
	})

	go m.pollFileProgress(ctx, s)

	if m.probeCache != nil && s.StreamID != "" {
		go m.cachePassiveMetadata(ctx, s)
	}

	m.log.Info().Str("session_id", s.ID).Str("format", format).Str("codec", outCodec).Msg("Go full transcode pipeline started")

	demuxErr := ds.RunWithSink(ctx, gp)

	gp.Stop()

	if ctx.Err() != nil {
		m.log.Info().Str("session_id", s.ID).Msg("full transcode ended (context cancelled)")
		return
	}

	if demuxErr != nil {
		if !isLive {
			s.setError(fmt.Errorf("full transcode error: %w", demuxErr))
			return
		}
		m.log.Warn().Err(demuxErr).Str("session_id", s.ID).Msg("live full transcode failed")
		s.setError(fmt.Errorf("live full transcode failed: %w", demuxErr))
	} else {
		m.log.Info().Str("session_id", s.ID).Msg("full transcode completed")
	}
}

func (m *Manager) runGoAudioTranscode(ctx context.Context, s *Session, ds *DemuxSession, info *probe.StreamInfo, audioIdx int, isLive bool) {
	format := "mpegts"
	if s.startOpts.OutputContainer == "mp4" {
		format = "mp4"
	}

	gp, err := NewAudioTranscodePipeline(AudioTranscodeOpts{
		Info:             info,
		AudioIndex:       audioIdx,
		FilePath:         s.FilePath,
		OutputDir:        s.OutputDir,
		Format:           format,
		OutputAudioCodec: s.startOpts.OutputAudioCodec,
		Log:              m.log.With().Str("session_id", s.ID).Logger(),
	})
	if err != nil {
		m.log.Error().Err(err).Str("session_id", s.ID).Msg("Go audio transcode pipeline creation failed")
		s.setError(fmt.Errorf("audio transcode pipeline failed: %w", err))
		return
	}
	s.SetStopPipeline(func() { gp.Stop() })

	if s.startOpts.SeekOffset > 0 && !isLive {
		seekMs := int64(s.startOpts.SeekOffset * 1000)
		if seekErr := ds.Demuxer().SeekTo(seekMs); seekErr != nil {
			m.log.Warn().Err(seekErr).Str("session_id", s.ID).Msg("initial seek failed")
		}
	}

	type seekResetter interface {
		ResetForSeek()
	}
	ds.Demuxer().SetOnSeek(func() {
		if sr, ok := interface{}(gp).(seekResetter); ok {
			sr.ResetForSeek()
		}
		m.log.Info().Str("session_id", s.ID).Msg("seek: audio transcode reset")
	})

	s.SetSeekFunc(func(position float64) {
		seekMs := int64(position * 1000)
		if err := ds.Demuxer().RequestSeek(seekMs); err != nil {
			m.log.Warn().Err(err).Str("session_id", s.ID).Float64("position", position).Msg("seek request failed")
		} else {
			m.log.Info().Str("session_id", s.ID).Float64("position", position).Msg("seek requested")
		}
	})

	go m.pollFileProgress(ctx, s)

	if m.probeCache != nil && s.StreamID != "" {
		go m.cachePassiveMetadata(ctx, s)
	}

	m.log.Info().Str("session_id", s.ID).Str("format", format).Msg("Go audio transcode pipeline started")

	demuxErr := ds.RunWithSink(ctx, gp)

	gp.Stop()

	if ctx.Err() != nil {
		m.log.Info().Str("session_id", s.ID).Msg("audio transcode ended (context cancelled)")
		return
	}

	if demuxErr != nil {
		if !isLive {
			s.setError(fmt.Errorf("audio transcode error: %w", demuxErr))
			return
		}
		m.log.Warn().Err(demuxErr).Str("session_id", s.ID).Msg("live audio transcode failed")
		s.setError(fmt.Errorf("live audio transcode failed: %w", demuxErr))
	} else {
		m.log.Info().Str("session_id", s.ID).Msg("audio transcode completed")
	}
}

func probeAudioTracks(info *probe.StreamInfo) []media.AudioTrack {
	tracks := make([]media.AudioTrack, len(info.AudioTracks))
	for i, t := range info.AudioTracks {
		tracks[i] = media.AudioTrack{
			Index:      t.Index,
			Codec:      t.Codec,
			Channels:   t.Channels,
			SampleRate: fmt.Sprintf("%d", t.SampleRate),
			Language:   t.Language,
			BitRate:    fmt.Sprintf("%d", t.BitrateKbps*1000),
		}
	}
	return tracks
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

		if m.streamStore != nil && s.StreamID != "" {
			vcodec := ""
			acodec := ""
			if video != nil {
				vcodec = video.Codec
			}
			if len(audioTracks) > 0 {
				acodec = audioTracks[0].Codec
			}
			if err := m.streamStore.UpdateStreamProbeData(ctx, s.StreamID, duration, vcodec, acodec); err != nil {
				m.log.Debug().Err(err).Str("stream_id", s.StreamID).Msg("failed to update stream probe data")
			}
		}
	}
}


func resolveNeedsTranscode(outCodec, srcCodec string) bool {
	if outCodec == "" || outCodec == "copy" || outCodec == "default" {
		return false
	}
	if srcCodec == "" {
		return true
	}
	return media.BaseCodec(outCodec) != media.BaseCodec(srcCodec)
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

