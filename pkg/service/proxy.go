package service

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/config"
	"github.com/gavinmcnair/tvproxy/pkg/ffmpeg"
	"github.com/gavinmcnair/tvproxy/pkg/httputil"
	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
	"github.com/gavinmcnair/tvproxy/pkg/store"
)

var (
	ErrChannelNotFound = errors.New("channel not found")
	ErrChannelDisabled = errors.New("channel is disabled")
)

const (
	tsPacketSize = 188
	tsReadChunks = 7
	tsBufferSize = tsPacketSize * tsReadChunks
)

type streamClient struct {
	w        http.ResponseWriter
	flusher  http.Flusher
	done     chan struct{}
	viewerID string
}

type channelConnection struct {
	streamURL string
	clients   []*streamClient
	cancel    context.CancelFunc
	mu        sync.Mutex
}

type ProxyService struct {
	channelRepo       *repository.ChannelRepository
	streamStore       store.StreamReader
	streamProfileRepo *repository.StreamProfileRepository
	clientService     *ClientService
	activity          *ActivityService
	config            *config.Config
	httpClient        *http.Client
	log               zerolog.Logger

	mu          sync.RWMutex
	connections map[string]*channelConnection
}

func NewProxyService(
	channelRepo *repository.ChannelRepository,
	streamStore store.StreamReader,
	streamProfileRepo *repository.StreamProfileRepository,
	clientService *ClientService,
	activity *ActivityService,
	cfg *config.Config,
	httpClient *http.Client,
	log zerolog.Logger,
) *ProxyService {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &ProxyService{
		channelRepo:       channelRepo,
		streamStore:       streamStore,
		streamProfileRepo: streamProfileRepo,
		clientService:     clientService,
		activity:          activity,
		config:            cfg,
		httpClient:        httpClient,
		log:               log.With().Str("service", "proxy").Logger(),
		connections:       make(map[string]*channelConnection),
	}
}

func ContentTypeForProfile(mode string, profile *models.StreamProfile) string {
	if profile != nil && mode == "ffmpeg" {
		switch profile.Container {
		case "mp4":
			return "video/mp4"
		case "matroska":
			return "video/x-matroska"
		case "webm":
			return "video/webm"
		}
	}
	return "video/mp2t"
}

func profileDisplayName(profile *models.StreamProfile) string {
	if profile != nil {
		return profile.Name
	}
	return "Direct"
}

func writeStreamHeaders(w http.ResponseWriter, contentType string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Connection", "close")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.WriteHeader(http.StatusOK)
}

func (s *ProxyService) ProxyStream(ctx context.Context, w http.ResponseWriter, r *http.Request, channelID string, profileOverride string) error {
	channel, err := s.channelRepo.GetByIDLite(ctx, channelID)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrChannelNotFound, channelID)
	}
	if !channel.IsEnabled {
		return fmt.Errorf("%w: %s", ErrChannelDisabled, channelID)
	}

	mode, streamProfile, dedicated, clientName := s.resolveProfile(ctx, r, channel, profileOverride)
	contentType := ContentTypeForProfile(mode, streamProfile)

	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming not supported")
	}

	var viewerID string
	if s.activity != nil {
		viewerID = s.activity.Add(ViewerOpts{
			ChannelID:   channelID,
			ChannelName: channel.Name,
			ProfileName: profileDisplayName(streamProfile),
			ClientName:  clientName,
			UserAgent:   r.UserAgent(),
			RemoteAddr:  r.RemoteAddr,
			Type:        "channel",
		})
	}

	c := &streamClient{
		w:        w,
		flusher:  flusher,
		done:     make(chan struct{}),
		viewerID: viewerID,
	}

	writeStreamHeaders(w, contentType)
	flusher.Flush()

	if !dedicated {
		if joined := s.tryJoinExisting(channelID, c, r); joined {
			return nil
		}
	}

	if err := s.startUpstream(ctx, r, channelID, c, mode, streamProfile); err != nil {
		if s.activity != nil && viewerID != "" {
			s.activity.Remove(viewerID)
		}
		s.log.Error().Err(err).Str("channel_id", channelID).Msg("all streams failed for channel")
	}
	return nil
}

func (s *ProxyService) resolveProfile(ctx context.Context, r *http.Request, channel *models.Channel, profileOverride string) (string, *models.StreamProfile, bool, string) {
	if profileOverride != "" {
		sp, err := s.streamProfileRepo.GetByName(ctx, profileOverride)
		if err == nil {
			s.log.Info().Str("channel_id", channel.ID).Str("profile", profileOverride).Str("mode", sp.StreamMode).Msg("using profile override")
			return sp.StreamMode, sp, true, ""
		}
	}

	if channel.StreamProfileID != nil {
		sp, err := s.streamProfileRepo.GetByID(ctx, *channel.StreamProfileID)
		if err == nil {
			s.log.Info().Str("channel_id", channel.ID).Str("profile", sp.Name).Str("mode", sp.StreamMode).Msg("using channel stream profile")
			return sp.StreamMode, sp, true, ""
		}
		s.log.Warn().Err(err).Str("channel_id", channel.ID).Str("profile_id", *channel.StreamProfileID).Msg("channel stream profile not found, falling through")
	}

	if s.clientService != nil {
		matched, clientName, err := s.clientService.MatchClient(ctx, r)
		if err != nil {
			s.log.Warn().Err(err).Msg("client detection error")
		}
		if matched != nil {
			return matched.StreamMode, matched, true, clientName
		}
	}

	return "proxy", nil, false, ""
}

func (s *ProxyService) ResolveContentType(ctx context.Context, r *http.Request, channelID string, profileOverride string) string {
	channel, err := s.channelRepo.GetByIDLite(ctx, channelID)
	if err != nil {
		return "video/mp2t"
	}
	mode, profile, _, _ := s.resolveProfile(ctx, r, channel, profileOverride)
	return ContentTypeForProfile(mode, profile)
}

func (s *ProxyService) tryJoinExisting(channelID string, c *streamClient, r *http.Request) bool {
	s.mu.RLock()
	conn, exists := s.connections[channelID]
	s.mu.RUnlock()

	if !exists {
		return false
	}

	conn.mu.Lock()
	conn.clients = append(conn.clients, c)
	conn.mu.Unlock()

	s.log.Info().Str("channel_id", channelID).Msg("client joined existing stream")

	select {
	case <-c.done:
	case <-r.Context().Done():
	}

	s.removeClient(channelID, c)
	return true
}

func (s *ProxyService) startUpstream(ctx context.Context, r *http.Request, channelID string, c *streamClient, mode string, profile *models.StreamProfile) error {
	channelStreams, err := s.channelRepo.GetStreams(ctx, channelID)
	if err != nil {
		return fmt.Errorf("getting channel streams: %w", err)
	}
	if len(channelStreams) == 0 {
		return fmt.Errorf("no streams assigned to channel %s", channelID)
	}

	for _, cs := range channelStreams {
		stream, err := s.streamStore.GetByID(ctx, cs.StreamID)
		if err != nil {
			s.log.Warn().Err(err).Str("stream_id", cs.StreamID).Msg("stream not found, trying next")
			continue
		}
		if !stream.IsActive {
			s.log.Warn().Str("stream_id", stream.ID).Msg("stream inactive, trying next")
			continue
		}

		if s.activity != nil && c.viewerID != "" {
			s.activity.SetM3UAccountID(c.viewerID, stream.M3UAccountID)
		}

		upstreamCtx, cancel := context.WithCancel(context.Background())
		conn := &channelConnection{
			streamURL: stream.URL,
			clients:   []*streamClient{c},
			cancel:    cancel,
		}

		s.mu.Lock()
		s.connections[channelID] = conn
		s.mu.Unlock()

		reader, startErr := s.openUpstream(upstreamCtx, channelID, stream, mode, profile)
		if startErr != nil {
			cancel()
			s.cleanupConnection(channelID)
			s.log.Error().Err(startErr).Str("url", stream.URL).Msg("upstream start failed, trying next")
			continue
		}

		if err := s.channelRepo.ResetFailCount(ctx, channelID); err != nil {
			s.log.Warn().Err(err).Str("channel_id", channelID).Msg("failed to reset fail count on stream start")
		}

		go s.proxyLoop(channelID, reader, cancel)

		select {
		case <-c.done:
		case <-r.Context().Done():
		}

		s.removeClient(channelID, c)
		return nil
	}

	return fmt.Errorf("all streams failed for channel %s", channelID)
}

func (s *ProxyService) openUpstream(ctx context.Context, channelID string, stream *models.Stream, mode string, profile *models.StreamProfile) (io.ReadCloser, error) {
	switch mode {
	case "ffmpeg":
		p := profile
		if p == nil || p.Args == "" {
			p = &models.StreamProfile{
				Name:    "fallback-copy",
				Command: "ffmpeg",
				Args:    "-hide_banner -loglevel warning -i {input} -c copy -f mpegts pipe:1",
			}
		}
		return s.startFFmpeg(ctx, channelID, stream, p)
	default:
		return s.startHTTPPassthrough(ctx, channelID, stream)
	}
}

func (s *ProxyService) startHTTPPassthrough(ctx context.Context, channelID string, stream *models.Stream) (io.ReadCloser, error) {
	s.log.Info().Str("channel_id", channelID).Str("stream_id", stream.ID).Str("url", stream.URL).Msg("starting HTTP passthrough")

	resp, err := httputil.Fetch(ctx, s.httpClient, s.config, stream.URL)
	if err != nil {
		s.log.Error().Err(err).Str("channel_id", channelID).Str("url", stream.URL).Msg("passthrough upstream connection failed")
		return nil, fmt.Errorf("upstream connection failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		httputil.LogUpstreamFailure(s.log, resp, stream.URL)
		resp.Body.Close()
		return nil, fmt.Errorf("upstream returned %d", resp.StatusCode)
	}

	return resp.Body, nil
}

func (s *ProxyService) startFFmpeg(ctx context.Context, channelID string, stream *models.Stream, profile *models.StreamProfile) (io.ReadCloser, error) {
	argsStr := strings.Replace(profile.Args, "{input}", "pipe:0", 1)
	args := ffmpeg.ShellSplit(argsStr)

	s.log.Info().
		Str("channel_id", channelID).
		Str("stream_id", stream.ID).
		Str("url", stream.URL).
		Str("profile", profile.Name).
		Strs("args", args).
		Msg("starting transcoding")

	resp, err := httputil.Fetch(ctx, s.httpClient, s.config, stream.URL)
	if err != nil {
		s.log.Error().Err(err).Str("channel_id", channelID).Str("url", stream.URL).Msg("ffmpeg upstream connection failed")
		return nil, fmt.Errorf("upstream connection failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		httputil.LogUpstreamFailure(s.log, resp, stream.URL)
		resp.Body.Close()
		return nil, fmt.Errorf("upstream returned %d", resp.StatusCode)
	}

	cmd := exec.CommandContext(ctx, profile.Command, args...)
	cmd.Stdin = resp.Body

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		resp.Body.Close()
		return nil, fmt.Errorf("creating stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		resp.Body.Close()
		return nil, fmt.Errorf("creating stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		resp.Body.Close()
		return nil, fmt.Errorf("starting ffmpeg: %w", err)
	}

	go s.logFFmpegStderr(channelID, stderr)
	go s.waitFFmpeg(ctx, channelID, cmd, resp.Body)

	return stdout, nil
}

func (s *ProxyService) waitFFmpeg(ctx context.Context, channelID string, cmd *exec.Cmd, stdinBody io.Closer) {
	waitErr := cmd.Wait()
	if stdinBody != nil {
		stdinBody.Close()
	}
	if waitErr == nil {
		s.log.Info().Str("channel_id", channelID).Msg("proxy ffmpeg finished (stream ended)")
		return
	}
	if ctx.Err() != nil && cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == -1 {
		s.log.Info().Str("channel_id", channelID).Msg("proxy ffmpeg stopped (client disconnected)")
		return
	}
	exitCode := -1
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	s.log.Error().Err(waitErr).Int("exit_code", exitCode).Str("channel_id", channelID).Msg("ffmpeg exited with error")
}

func (s *ProxyService) logFFmpegStderr(channelID string, stderr io.ReadCloser) {
	defer stderr.Close()
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		line := scanner.Text()
		if ffmpeg.IsFFmpegNoise(line) {
			continue
		}
		s.log.Warn().Str("channel_id", channelID).Str("ffmpeg", line).Msg("ffmpeg output")
	}
}

func (s *ProxyService) proxyLoop(channelID string, upstream io.ReadCloser, cancel context.CancelFunc) {
	defer upstream.Close()
	defer cancel()
	defer s.cleanupConnection(channelID)

	buf := make([]byte, tsBufferSize)
	var lastTouch time.Time
	for {
		n, err := upstream.Read(buf)
		if n > 0 {
			s.mu.RLock()
			conn, exists := s.connections[channelID]
			s.mu.RUnlock()
			if !exists {
				return
			}

			conn.mu.Lock()
			alive := make([]*streamClient, 0, len(conn.clients))
			for _, c := range conn.clients {
				if _, writeErr := c.w.Write(buf[:n]); writeErr != nil {
					close(c.done)
					continue
				}
				c.flusher.Flush()
				alive = append(alive, c)
			}
			conn.clients = alive
			if len(conn.clients) == 0 {
				conn.mu.Unlock()
				s.log.Info().Str("channel_id", channelID).Msg("no clients remaining, closing upstream")
				return
			}
			if s.activity != nil && time.Since(lastTouch) > 2*time.Second {
				for _, c := range alive {
					if c.viewerID != "" {
						s.activity.Touch(c.viewerID)
					}
				}
				lastTouch = time.Now()
			}
			conn.mu.Unlock()
		}

		if err != nil {
			if err != io.EOF {
				s.log.Error().Err(err).Str("channel_id", channelID).Msg("upstream read error")
			}
			s.notifyAllClients(channelID)
			return
		}
	}
}

func (s *ProxyService) notifyAllClients(channelID string) {
	s.mu.RLock()
	conn, exists := s.connections[channelID]
	s.mu.RUnlock()
	if !exists {
		return
	}
	conn.mu.Lock()
	for _, c := range conn.clients {
		close(c.done)
	}
	conn.clients = nil
	conn.mu.Unlock()
}

func (s *ProxyService) removeClient(channelID string, c *streamClient) {
	if s.activity != nil && c.viewerID != "" {
		s.activity.Remove(c.viewerID)
	}

	s.mu.RLock()
	conn, exists := s.connections[channelID]
	s.mu.RUnlock()
	if !exists {
		return
	}

	conn.mu.Lock()
	for i, existing := range conn.clients {
		if existing == c {
			conn.clients = append(conn.clients[:i], conn.clients[i+1:]...)
			break
		}
	}
	remaining := len(conn.clients)
	conn.mu.Unlock()

	s.log.Info().Str("channel_id", channelID).Int("remaining", remaining).Msg("client disconnected")

	if remaining == 0 {
		conn.cancel()
		s.cleanupConnection(channelID)
	}
}

func (s *ProxyService) cleanupConnection(channelID string) {
	s.mu.Lock()
	delete(s.connections, channelID)
	s.mu.Unlock()
}

func (s *ProxyService) ProxyRawStream(ctx context.Context, w http.ResponseWriter, r *http.Request, streamID string, profileOverride string) error {
	stream, err := s.streamStore.GetByID(ctx, streamID)
	if err != nil {
		return fmt.Errorf("stream not found: %w", err)
	}
	if !stream.IsActive {
		return fmt.Errorf("stream %s is inactive", streamID)
	}

	var profile *models.StreamProfile
	if profileOverride != "" {
		sp, err := s.streamProfileRepo.GetByName(ctx, profileOverride)
		if err != nil {
			return fmt.Errorf("profile %q not found: %w", profileOverride, err)
		}
		profile = sp
	}

	var viewerID string
	if s.activity != nil {
		viewerID = s.activity.Add(ViewerOpts{
			StreamID:     streamID,
			StreamName:   stream.Name,
			M3UAccountID: stream.M3UAccountID,
			ProfileName:  profileDisplayName(profile),
			UserAgent:    r.UserAgent(),
			RemoteAddr:   r.RemoteAddr,
			Type:         "stream",
		})
		defer s.activity.Remove(viewerID)
	}

	if profile != nil && profile.Args != "" {
		return s.proxyRawStreamFFmpeg(ctx, w, r, stream, profile, viewerID)
	}
	return s.proxyRawStreamPassthrough(ctx, w, r, stream, viewerID)
}

func (s *ProxyService) proxyRawStreamFFmpeg(ctx context.Context, w http.ResponseWriter, r *http.Request, stream *models.Stream, profile *models.StreamProfile, viewerID string) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming not supported")
	}

	ffmpegCtx, cancel := context.WithCancel(context.Background())
	reader, err := s.startFFmpeg(ffmpegCtx, "", stream, profile)
	if err != nil {
		cancel()
		return fmt.Errorf("starting ffmpeg: %w", err)
	}
	defer cancel()
	defer reader.Close()

	s.log.Info().Str("stream_id", stream.ID).Str("profile", profile.Name).Msg("raw stream ffmpeg started")

	writeStreamHeaders(w, ContentTypeForProfile("ffmpeg", profile))
	flusher.Flush()

	s.copyToClient(reader, w, flusher, stream.ID, viewerID)
	return nil
}

func (s *ProxyService) proxyRawStreamPassthrough(ctx context.Context, w http.ResponseWriter, r *http.Request, stream *models.Stream, viewerID string) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming not supported")
	}

	resp, err := httputil.Fetch(r.Context(), s.httpClient, s.config, stream.URL)
	if err != nil {
		return fmt.Errorf("upstream connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("upstream returned %d", resp.StatusCode)
	}

	s.log.Info().Str("stream_id", stream.ID).Str("url", stream.URL).Msg("raw stream passthrough started")

	writeStreamHeaders(w, "video/mp2t")
	flusher.Flush()

	s.copyToClient(resp.Body, w, flusher, stream.ID, viewerID)
	return nil
}

func (s *ProxyService) copyToClient(reader io.Reader, w http.ResponseWriter, flusher http.Flusher, streamID string, viewerID string) {
	buf := make([]byte, tsBufferSize)
	var lastTouch time.Time
	for {
		n, readErr := reader.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return
			}
			flusher.Flush()
			if s.activity != nil && viewerID != "" && time.Since(lastTouch) > 2*time.Second {
				s.activity.Touch(viewerID)
				lastTouch = time.Now()
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				s.log.Error().Err(readErr).Str("stream_id", streamID).Msg("raw stream read error")
			}
			return
		}
	}
}

func (s *ProxyService) ActiveConnections() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.connections)
}

func (s *ProxyService) ActiveClients() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := 0
	for _, conn := range s.connections {
		conn.mu.Lock()
		total += len(conn.clients)
		conn.mu.Unlock()
	}
	return total
}
