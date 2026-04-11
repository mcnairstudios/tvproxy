package session

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
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
	Command           string
	Args              string
	OutputDir         string
	HLSOutputDir      string
	SourceInputArgs   string
	SourceAudioCodec  string
	SourceDeinterlace bool
	SourceAudioResync bool
	SourceFPSMode     string
	SkipProbe         bool
	MetadataOnly      bool
	Transcoder        string // "auto", "gstreamer", "ffmpeg"
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


func (m *Manager) buildArgs(argsStr string, inputURL string, outputPath string, useWireGuard bool, hlsOutputDir string, opts StartOpts) []string {
	hasTSHeader := false
	if m.probeCache != nil {
		if h, _ := m.probeCache.GetTSHeader(media.StreamHash(inputURL)); h != nil {
			hasTSHeader = true
		}
	}
	pipeInput := media.IsHTTPURL(inputURL) && (useWireGuard || hasTSHeader)

	if hlsOutputDir != "" {
		if useWireGuard && m.wgClient != nil && media.IsHTTPURL(inputURL) {
			proxy, err := m.wgProxyMgr.GetOrCreate("default", m.wgClient, m.config, m.log)
			if err == nil {
				opts.StreamURL = proxy.ProxyURL(inputURL)
				pipeInput = false
				m.log.Info().Str("proxy_url", opts.StreamURL[:60]+"...").Msg("using wg proxy for ffmpeg")
			}
		}
		return m.buildDualOutputArgs(hlsOutputDir, outputPath, pipeInput, opts)
	}

	var args []string
	if argsStr == "" {
		if pipeInput {
			args = media.ShellSplit("-hide_banner -loglevel warning -nostdin -f mpegts -analyzeduration 1000000 -probesize 1000000 -err_detect ignore_err -fflags +genpts+discardcorrupt -i pipe:0 -map 0:v:0? -map 0:a:0? -c:v copy -c:a aac -ac 2 -b:a 192k -f mp4 -movflags frag_keyframe+empty_moov+default_base_moof {output}")
		} else {
			args = media.ShellSplit("-hide_banner -loglevel warning -i {input} -c copy -f mp4 -movflags frag_keyframe+empty_moov+default_base_moof {output}")
		}
	} else {
		args = media.ShellSplit(argsStr)
	}

	for i, arg := range args {
		switch arg {
		case "{input}":
			if pipeInput {
				args[i] = "pipe:0"
			} else {
				args[i] = inputURL
			}
		case "{output}", "pipe:1":
			args[i] = outputPath
		}
	}

	args = append([]string{"-y"}, args...)
	args = append(args, "-progress", "pipe:2")

	return args
}

func (m *Manager) buildDualOutputArgs(hlsDir, mp4Path string, pipeInput bool, opts StartOpts) []string {
	videoCodec := opts.OutputVideoCodec
	if videoCodec == "" || videoCodec == "default" {
		videoCodec = "copy"
	}
	audioCodec := opts.OutputAudioCodec
	if audioCodec == "" || audioCodec == "default" {
		audioCodec = "aac"
	}
	hwaccel := opts.OutputHWAccel

	var args []string
	args = append(args, "-y", "-hide_banner", "-loglevel", "warning", "-nostdin")

	if videoCodec != "copy" && hwaccel != "" && hwaccel != "none" && hwaccel != "default" {
		switch hwaccel {
		case "vaapi":
			args = append(args, "-hwaccel", "vaapi", "-hwaccel_device", "/dev/dri/renderD128", "-hwaccel_output_format", "vaapi")
		case "qsv":
			args = append(args, "-hwaccel", "qsv", "-hwaccel_device", "/dev/dri/renderD128", "-hwaccel_output_format", "qsv")
		case "nvenc", "cuda":
			args = append(args, "-hwaccel", "cuda", "-hwaccel_output_format", "cuda")
		case "videotoolbox":
			args = append(args, "-hwaccel", "videotoolbox")
		}
	}

	if opts.SourceInputArgs != "" {
		inputParts := media.ShellSplit(opts.SourceInputArgs)
		args = append(args, inputParts...)
	} else {
		args = append(args,
			"-f", "mpegts",
			"-analyzeduration", "1000000",
			"-probesize", "1000000",
			"-err_detect", "ignore_err",
			"-fflags", "+genpts+discardcorrupt",
		)
	}

	inputArg := opts.StreamURL
	if pipeInput {
		inputArg = "pipe:0"
	}
	args = append(args,
		"-i", inputArg,
		"-map", "0:v:0?",
		"-map", "0:a:0?",
	)

	venc := media.MapEncoderHW(videoCodec, hwaccel)
	if opts.SourceDeinterlace && venc == "copy" {
		venc = "libx264"
	}
	args = append(args, "-c:v", venc)

	var vfParts []string
	if hwaccel == "vaapi" && strings.Contains(venc, "h264") {
		vfParts = append(vfParts, "scale_vaapi=format=nv12")
	}
	if opts.SourceDeinterlace && venc != "copy" {
		switch hwaccel {
		case "vaapi":
			vfParts = append(vfParts, "deinterlace_vaapi")
		case "qsv":
			vfParts = append(vfParts, "vpp_qsv=deinterlace=2")
		default:
			vfParts = append(vfParts, "yadif")
		}
	}
	if len(vfParts) > 0 {
		args = append(args, "-vf", strings.Join(vfParts, ","))
	}
	if opts.SourceFPSMode != "" && venc != "copy" {
		args = append(args, "-fps_mode", opts.SourceFPSMode)
	}

	if audioCodec == "copy" {
		args = append(args, "-c:a", "aac", "-ac", "2", "-b:a", "192k")
	} else {
		args = append(args, "-c:a", audioCodec, "-ac", "2", "-b:a", "192k")
	}
	if opts.SourceAudioResync {
		args = append(args, "-af", "aresample=async=1000:first_pts=0")
	}

	args = append(args,
		"-max_muxing_queue_size", "2048",
		"-f", "hls",
		"-hls_time", "6",
		"-hls_segment_type", "fmp4",
		"-hls_fmp4_init_filename", "init.mp4",
		"-start_number", "0",
		"-hls_playlist_type", "event",
		"-hls_list_size", "0",
		"-hls_segment_filename", filepath.Join(hlsDir, "seg%d.mp4"),
		"-y", filepath.Join(hlsDir, "playlist.m3u8"),
	)

	mp4AudioCodec := "copy"
	if opts.SourceAudioCodec == "aac_latm" || opts.SourceAudioCodec == "mp2" || opts.SourceAudioCodec == "mp3" {
		mp4AudioCodec = "aac"
	}
	mp4Args := []string{"-c:v", "copy", "-c:a", mp4AudioCodec}
	if mp4AudioCodec == "copy" {
		mp4Args = append(mp4Args, "-bsf:a", "aac_adtstoasc")
	} else {
		mp4Args = append(mp4Args, "-ac", "2", "-b:a", "192k")
	}
	mp4Args = append(mp4Args,
		"-f", "mp4",
		"-movflags", "frag_keyframe+empty_moov+default_base_moof",
		mp4Path,
	)
	args = append(args, mp4Args...)

	args = append(args, "-progress", "pipe:2")
	return args
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
			s.mu.Lock()
			s.lingerTimer = time.AfterFunc(lingerDuration, func() {
				m.log.Info().Str("channel_id", channelID).Msg("linger expired, cleaning up session")
				go m.stopAndCleanup(channelID, s)
			})
			s.mu.Unlock()
			m.log.Info().Str("channel_id", channelID).Dur("linger", lingerDuration).Msg("no consumers, starting linger timer")
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

	seekStr := fmt.Sprintf("%.1f", position)
	origArgs := opts.Args
	if strings.Contains(origArgs, " -ss ") {
		origArgs = strings.Join(removeSSArgs(strings.Fields(origArgs)), " ")
	}
	if opts.StreamURL != "" {
		origArgs = strings.Replace(origArgs, "{input}", opts.StreamURL, 1)
		origArgs = strings.Replace(origArgs, "pipe:0", opts.StreamURL, 1)
	}
	if idx := strings.Index(origArgs, "-i "); idx >= 0 {
		opts.Args = origArgs[:idx] + "-ss " + seekStr + " " + origArgs[idx:]
	}

	opts.KnownDuration = s.Duration
	opts.SeekOffset = position

	m.log.Info().Str("channel_id", channelID).Float64("position", position).Msg("restarting session with seek")

	m.GetOrCreateWithConsumer(ctx, opts, ConsumerViewer)
}

func removeSSArgs(args []string) []string {
	var out []string
	skip := false
	for _, a := range args {
		if a == "-ss" {
			skip = true
			continue
		}
		if skip {
			skip = false
			continue
		}
		out = append(out, a)
	}
	return out
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

	fileName := media.SanitizeFilename(opts.StreamName, time.Now()) + ".mp4"
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
		command, args := m.resolveTranscoder(opts, filePath)

		go m.run(sessionCtx, s, command, args, opts.StreamURL)
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

func (m *Manager) resolveTranscoder(opts StartOpts, filePath string) (string, []string) {
	hwAccel := gstreamer.HWNone
	switch opts.OutputHWAccel {
	case "vaapi":
		hwAccel = gstreamer.HWVAAPI
	case "qsv":
		hwAccel = gstreamer.HWQSV
	case "videotoolbox":
		hwAccel = gstreamer.HWVideoToolbox
	}

	transcoder := opts.Transcoder
	if transcoder == "" {
		transcoder = "auto"
	}
	if transcoder == "ffmpeg" {
		m.log.Info().Str("channel_id", opts.ChannelID).Msg("using ffmpeg (forced by setting)")
		args := m.buildArgs(opts.Args, opts.StreamURL, filePath, opts.UseWireGuard, opts.HLSOutputDir, opts)
		command := opts.Command
		if command == "" {
			command = "ffmpeg"
		}
		return command, args
	}

	m.log.Info().Str("stream_url", opts.StreamURL).Str("stream_id", opts.StreamID).Str("channel_id", opts.ChannelID).Msg("resolveTranscoder lookup")
	choice := gstreamer.ShouldUseGStreamer(m.probeCache, opts.StreamURL, opts.StreamID, hwAccel)
	if !choice.UseGStreamer && gstreamer.Available() {
		choice.UseGStreamer = true
		choice.Reason = "gstreamer available (no probe data, using defaults)"
	}
	if choice.UseGStreamer {
		probe, _ := m.probeCache.GetProbe(media.StreamHash(opts.StreamURL))
		if probe == nil {
			probe, _ = m.probeCache.GetProbeByStreamID(opts.StreamID)
		}

		inputType := "http"
		if strings.HasPrefix(opts.StreamURL, "rtsp://") {
			inputType = "rtsp"
		}

		pipeOpts := gstreamer.PipelineOpts{
			InputURL:         opts.StreamURL,
			InputType:        inputType,
			IsLive:           true,
			OutputVideoCodec: opts.OutputVideoCodec,
			OutputAudioCodec: opts.OutputAudioCodec,
			OutputBitrate:    0,
			OutputFormat:     gstreamer.OutputMP4,
			HWAccel:          hwAccel,
			RecordingPath:    filePath,
		}

		if opts.HLSOutputDir != "" {
			pipeOpts.OutputFormat = gstreamer.OutputHLS
			pipeOpts.HLSDir = opts.HLSOutputDir
			pipeOpts.HLSSegmentTime = 6
			pipeOpts.RecordingPath = ""
		}

		pipeline := gstreamer.BuildFromProbe(probe, opts.StreamURL, pipeOpts)
		m.log.Info().
			Str("channel_id", opts.ChannelID).
			Str("reason", choice.Reason).
			Str("pipeline", pipeline.PipelineStr).
			Msg("using gstreamer")
		return "gstreamer-native", []string{pipeline.PipelineStr}
	}

	m.log.Info().
		Str("channel_id", opts.ChannelID).
		Str("reason", choice.Reason).
		Msg("using ffmpeg (gstreamer unavailable)")

	args := m.buildArgs(opts.Args, opts.StreamURL, filePath, opts.UseWireGuard, opts.HLSOutputDir, opts)
	command := opts.Command
	if command == "" {
		command = "ffmpeg"
	}
	return command, args
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

func (m *Manager) run(ctx context.Context, s *Session, command string, args []string, inputURL string) {
	defer s.markDone()

	if command == "gstreamer-native" && len(args) > 0 {
		m.runGStreamerNative(ctx, s, args[0], inputURL)
		return
	}

	isGStreamer := strings.Contains(command, "gst-launch")
	m.log.Info().Str("session_id", s.ID).Str("command", command).Msg("starting transcoder")

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Cancel = func() error {
		return cmd.Process.Signal(syscall.SIGTERM)
	}
	waitDelay := 5 * time.Second
	if m.config.Settings != nil {
		waitDelay = m.config.Settings.FFmpeg.WaitDelay
	}
	cmd.WaitDelay = waitDelay

	var tsHeader []byte
	if !isGStreamer && m.probeCache != nil {
		tsHeader, _ = m.probeCache.GetTSHeader(media.StreamHash(inputURL))
	}

	var httpResp *http.Response
	if !isGStreamer && tsHeader != nil && media.IsHTTPURL(inputURL) && s.HLSOutputDir == "" {
		m.log.Info().Str("session_id", s.ID).Int("header_size", len(tsHeader)).Msg("injecting cached TS header for fast startup")
		resp, err := httputil.Fetch(ctx, m.clientForSession(s), m.config, inputURL)
		if err != nil {
			m.log.Error().Err(err).Msg("upstream connection failed")
			s.setError(fmt.Errorf("upstream connection failed: %w", err))
			return
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			s.setError(fmt.Errorf("upstream returned %d", resp.StatusCode))
			return
		}
		httpResp = resp
		pr, pw := io.Pipe()
		cmd.Stdin = pr
		go func() {
			pw.Write(tsHeader)
			io.Copy(pw, resp.Body)
			pw.Close()
		}()
	} else if !isGStreamer && media.IsHTTPURL(inputURL) && s.UseWireGuard && s.HLSOutputDir == "" {
		m.log.Info().Str("session_id", s.ID).Str("url", inputURL).Msg("routing upstream via wireguard")
		resp, err := httputil.Fetch(ctx, m.clientForSession(s), m.config, inputURL)
		if err != nil {
			m.log.Error().Err(err).Str("session_id", s.ID).Str("url", inputURL).Msg("upstream connection failed")
			s.setError(fmt.Errorf("upstream connection failed: %w", err))
			return
		}
		if resp.StatusCode != http.StatusOK {
			httputil.LogUpstreamFailure(m.log, resp, inputURL)
			resp.Body.Close()
			m.log.Error().Int("status", resp.StatusCode).Str("session_id", s.ID).Str("url", inputURL).Msg("upstream returned non-200")
			s.setError(fmt.Errorf("upstream returned %d", resp.StatusCode))
			return
		}
		httpResp = resp
		cmd.Stdin = resp.Body
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		if httpResp != nil {
			httpResp.Body.Close()
		}
		s.setError(fmt.Errorf("creating stderr pipe: %w", err))
		return
	}

	if err := cmd.Start(); err != nil {
		if httpResp != nil {
			httpResp.Body.Close()
		}
		s.setError(fmt.Errorf("starting ffmpeg: %w", err))
		return
	}

	go m.parseProgress(s, stderr)

	if isGStreamer {
		go m.pollFileProgress(ctx, s)
	}

	startupDur := 30 * time.Second
	if m.config.Settings != nil {
		startupDur = m.config.Settings.FFmpeg.StartupTimeout
	}
	startupTimeout := time.AfterFunc(startupDur, func() {
		if s.getBuffered() == 0 {
			m.log.Warn().Str("session_id", s.ID).Dur("timeout", startupDur).Msg("transcoder startup timeout, no data received")
			s.cancel()
		}
	})

	waitErr := cmd.Wait()
	startupTimeout.Stop()

	if httpResp != nil {
		httpResp.Body.Close()
	}

	if waitErr != nil && ctx.Err() == nil {
		stderr := s.LastStderr()
		if stderr != "" {
			s.setError(fmt.Errorf("transcoder failed: %w\n%s", waitErr, stderr))
		} else {
			s.setError(fmt.Errorf("transcoder failed: %w", waitErr))
		}
		if m.probeCache != nil && inputURL != "" {
			_ = m.probeCache.InvalidateProbe(media.StreamHash(inputURL))
		}
	}
}

func (m *Manager) runGStreamerNative(ctx context.Context, s *Session, pipelineStr string, inputURL string) {
	m.log.Info().Str("session_id", s.ID).Str("pipeline", pipelineStr).Msg("starting native gstreamer pipeline")

	var pipeline *gst.Pipeline
	var err error
	if gstreamer.PluginsAvailable() {
		pipeline, err = gst.NewPipelineFromString(pipelineStr)
	} else {
		pipeline, err = gstreamer.BuildNativeFromOpts(s.startOpts.OutputVideoCodec, s.startOpts.OutputAudioCodec, s.startOpts.OutputHWAccel, inputURL, s.FilePath)
	}
	if err != nil {
		s.setError(fmt.Errorf("gstreamer pipeline creation failed: %w", err))
		return
	}

	s.SetStopPipeline(func() {
		pipeline.SetState(gst.StateNull)
	})

	pipeline.SetState(gst.StatePlaying)

	go m.pollFileProgress(ctx, s)

	bus := pipeline.GetBus()
loop:
	for {
		msg := bus.TimedPop(gst.ClockTime(500000000))
		if ctx.Err() != nil {
			break loop
		}
		if msg == nil {
			continue
		}
		switch msg.Type() {
		case gst.MessageEOS:
			m.log.Info().Str("session_id", s.ID).Msg("gstreamer EOS")
			break loop
		case gst.MessageError:
			gstErr := msg.ParseError()
			m.log.Error().Str("session_id", s.ID).Err(gstErr).Msg("gstreamer error")
			s.setError(fmt.Errorf("gstreamer: %w", gstErr))
			s.setLastStderr(gstErr.Error())
			break loop
		}
	}

	pipeline.SetState(gst.StateNull)
	s.SetStopPipeline(nil)
	m.log.Info().Str("session_id", s.ID).Msg("gstreamer pipeline stopped")
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

func (m *Manager) parseProgress(s *Session, r io.Reader) {
	scanner := bufio.NewScanner(r)
	var lastErrors []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "out_time_us=") {
			usStr := strings.TrimPrefix(line, "out_time_us=")
			us, err := strconv.ParseInt(usStr, 10, 64)
			if err == nil && us > 0 {
				secs := float64(us) / 1_000_000.0
				if secs > maxBufferedSecs {
					m.log.Warn().Str("session_id", s.ID).Float64("secs", secs).Msg("transcoder progress exceeds 48h cap")
					secs = maxBufferedSecs
				}
				s.setBuffered(secs)
			}
		} else if !isProgressNoise(line) && line != "" {
			m.log.Warn().Str("session_id", s.ID).Str("stderr", line).Msg("transcoder output")
			lastErrors = append(lastErrors, line)
			if len(lastErrors) > 10 {
				lastErrors = lastErrors[1:]
			}
		}
	}
	if len(lastErrors) > 0 {
		s.setLastStderr(strings.Join(lastErrors, "\n"))
	}
}

var progressPrefixes = []string{
	"progress=", "out_time_ms=", "out_time=", "frame=", "fps=",
	"stream_", "bitrate=", "total_size=", "speed=", "dup_frames=", "drop_frames=",
}

func isProgressNoise(line string) bool {
	for _, p := range progressPrefixes {
		if strings.HasPrefix(line, p) {
			return true
		}
	}
	return media.IsFFmpegNoise(line)
}

func (m *Manager) PreserveTempDir(channelID string) string {
	m.mu.RLock()
	s, ok := m.sessions[channelID]
	m.mu.RUnlock()
	if !ok {
		return ""
	}
	return s.FilePath
}
