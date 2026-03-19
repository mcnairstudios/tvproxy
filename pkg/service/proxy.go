package service

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/config"
	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
)

const (
	tsPacketSize = 188
	tsReadChunks = 7
	tsBufferSize = tsPacketSize * tsReadChunks
)

type streamClient struct {
	w       http.ResponseWriter
	flusher http.Flusher
	done    chan struct{}
}

type channelConnection struct {
	streamURL string
	clients   []*streamClient
	cancel    context.CancelFunc
	mu        sync.Mutex
}

type ProxyService struct {
	channelRepo        *repository.ChannelRepository
	streamRepo         *repository.StreamRepository
	m3uAccountRepo     *repository.M3UAccountRepository
	channelProfileRepo *repository.ChannelProfileRepository
	streamProfileRepo  *repository.StreamProfileRepository
	clientService      *ClientService
	config             *config.Config
	log                zerolog.Logger

	mu          sync.RWMutex
	connections map[string]*channelConnection
}

func NewProxyService(
	channelRepo *repository.ChannelRepository,
	streamRepo *repository.StreamRepository,
	m3uAccountRepo *repository.M3UAccountRepository,
	channelProfileRepo *repository.ChannelProfileRepository,
	streamProfileRepo *repository.StreamProfileRepository,
	clientService *ClientService,
	cfg *config.Config,
	log zerolog.Logger,
) *ProxyService {
	return &ProxyService{
		channelRepo:        channelRepo,
		streamRepo:         streamRepo,
		m3uAccountRepo:     m3uAccountRepo,
		channelProfileRepo: channelProfileRepo,
		streamProfileRepo:  streamProfileRepo,
		clientService:      clientService,
		config:             cfg,
		log:                log.With().Str("service", "proxy").Logger(),
		connections:        make(map[string]*channelConnection),
	}
}

func contentTypeForProfile(mode string, profile *models.StreamProfile) string {
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

func writeStreamHeaders(w http.ResponseWriter, contentType string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Connection", "close")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.WriteHeader(http.StatusOK)
}

// ProxyStream proxies a live stream for the given channel. When another client
// is already watching the same channel with the default profile, the new client
// shares the existing upstream connection. Profile overrides and client-detected
// profiles get dedicated connections to avoid mixing output formats.
func (s *ProxyService) ProxyStream(ctx context.Context, w http.ResponseWriter, r *http.Request, channelID string, profileOverride string) error {
	channel, err := s.channelRepo.GetByID(ctx, channelID)
	if err != nil {
		return fmt.Errorf("channel not found: %w", err)
	}
	if !channel.IsEnabled {
		return fmt.Errorf("channel %s is disabled", channelID)
	}

	mode, streamProfile, dedicated := s.resolveProfile(ctx, r, channel, profileOverride)
	contentType := contentTypeForProfile(mode, streamProfile)

	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming not supported")
	}

	c := &streamClient{
		w:       w,
		flusher: flusher,
		done:    make(chan struct{}),
	}

	// Flush 200 OK immediately so clients like Plex don't timeout during ffmpeg startup
	writeStreamHeaders(w, contentType)
	flusher.Flush()

	if !dedicated {
		if joined := s.tryJoinExisting(channelID, c, r); joined {
			return nil
		}
	}

	// Headers already sent — cannot return HTTP errors after this point
	if err := s.startUpstream(ctx, r, channelID, c, mode, streamProfile); err != nil {
		s.log.Error().Err(err).Str("channel_id", channelID).Msg("all streams failed for channel")
	}
	return nil
}

func (s *ProxyService) resolveProfile(ctx context.Context, r *http.Request, channel *models.Channel, profileOverride string) (string, *models.StreamProfile, bool) {
	if profileOverride != "" {
		sp, err := s.streamProfileRepo.GetByName(ctx, profileOverride)
		if err == nil {
			s.log.Info().Str("channel_id", channel.ID).Str("profile", profileOverride).Str("mode", sp.StreamMode).Msg("using profile override")
			return sp.StreamMode, sp, true
		}
	}

	if s.clientService != nil {
		matched, err := s.clientService.MatchClient(ctx, r)
		if err != nil {
			s.log.Warn().Err(err).Msg("client detection error")
		}
		if matched != nil {
			return matched.StreamMode, matched, true
		}
	}

	mode, sp := ResolveStreamMode(ctx, channel, s.channelProfileRepo, s.streamProfileRepo, s.log)
	return mode, sp, false
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
		stream, err := s.streamRepo.GetByID(ctx, cs.StreamID)
		if err != nil {
			s.log.Warn().Err(err).Str("stream_id", cs.StreamID).Msg("stream not found, trying next")
			continue
		}
		if !stream.IsActive {
			s.log.Warn().Str("stream_id", stream.ID).Msg("stream inactive, trying next")
			continue
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, stream.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating upstream request: %w", err)
	}
	req.Header.Set("User-Agent", s.config.UserAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upstream connection failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("upstream returned %d", resp.StatusCode)
	}

	return resp.Body, nil
}

func ShellSplit(s string) []string {
	var args []string
	var current strings.Builder
	inDouble, inSingle := false, false

	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == ' ' && !inDouble && !inSingle:
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteByte(c)
		}
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}

func InjectUserAgent(args []string, userAgent string) []string {
	for _, arg := range args {
		if arg == "-user_agent" {
			return args
		}
	}
	for i, arg := range args {
		if arg == "-i" {
			newArgs := make([]string, 0, len(args)+2)
			newArgs = append(newArgs, args[:i]...)
			newArgs = append(newArgs, "-user_agent", userAgent)
			newArgs = append(newArgs, args[i:]...)
			return newArgs
		}
	}
	return args
}

func (s *ProxyService) startFFmpeg(ctx context.Context, channelID string, stream *models.Stream, profile *models.StreamProfile) (io.ReadCloser, error) {
	argsStr := strings.Replace(profile.Args, "{input}", stream.URL, 1)
	args := InjectUserAgent(ShellSplit(argsStr), s.config.UserAgent)

	s.log.Info().
		Str("channel_id", channelID).
		Str("stream_id", stream.ID).
		Str("url", stream.URL).
		Str("profile", profile.Name).
		Strs("args", args).
		Msg("starting transcoding")

	cmd := exec.CommandContext(ctx, profile.Command, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting ffmpeg: %w", err)
	}

	go s.logFFmpegStderr(channelID, stderr)
	go s.waitFFmpeg(ctx, channelID, cmd)

	return stdout, nil
}

func (s *ProxyService) waitFFmpeg(ctx context.Context, channelID string, cmd *exec.Cmd) {
	waitErr := cmd.Wait()
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
		s.log.Warn().Str("channel_id", channelID).Str("ffmpeg", scanner.Text()).Msg("ffmpeg output")
	}
}

func (s *ProxyService) proxyLoop(channelID string, upstream io.ReadCloser, cancel context.CancelFunc) {
	defer upstream.Close()
	defer cancel()
	defer s.cleanupConnection(channelID)

	buf := make([]byte, tsBufferSize)
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

// ProxyRawStream proxies a single stream by ID, optionally transcoding via the named profile.
func (s *ProxyService) ProxyRawStream(ctx context.Context, w http.ResponseWriter, r *http.Request, streamID string, profileOverride string) error {
	stream, err := s.streamRepo.GetByID(ctx, streamID)
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

	if profile != nil && profile.Args != "" {
		return s.proxyRawStreamFFmpeg(ctx, w, r, stream, profile)
	}
	return s.proxyRawStreamPassthrough(ctx, w, r, stream)
}

func (s *ProxyService) proxyRawStreamFFmpeg(ctx context.Context, w http.ResponseWriter, r *http.Request, stream *models.Stream, profile *models.StreamProfile) error {
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

	s.log.Info().Str("stream_id", stream.ID).Str("profile", profile.Name).Msg("raw stream ffmpeg started")

	writeStreamHeaders(w, contentTypeForProfile("ffmpeg", profile))
	flusher.Flush()

	buf := make([]byte, tsBufferSize)
	for {
		n, readErr := reader.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				cancel()
				reader.Close()
				return nil
			}
			flusher.Flush()
		}
		if readErr != nil {
			if readErr != io.EOF {
				s.log.Error().Err(readErr).Str("stream_id", stream.ID).Msg("raw stream read error")
			}
			cancel()
			reader.Close()
			return nil
		}
	}
}

func (s *ProxyService) proxyRawStreamPassthrough(ctx context.Context, w http.ResponseWriter, r *http.Request, stream *models.Stream) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming not supported")
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, stream.URL, nil)
	if err != nil {
		return fmt.Errorf("creating upstream request: %w", err)
	}
	req.Header.Set("User-Agent", s.config.UserAgent)

	resp, err := http.DefaultClient.Do(req)
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

	buf := make([]byte, tsBufferSize)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return nil
			}
			flusher.Flush()
		}
		if readErr != nil {
			if readErr != io.EOF {
				s.log.Error().Err(readErr).Str("stream_id", stream.ID).Msg("raw stream read error")
			}
			return nil
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
