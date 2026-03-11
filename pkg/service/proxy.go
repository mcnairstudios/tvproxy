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

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
)

const (
	// tsPacketSize is the size of an MPEG-TS packet.
	tsPacketSize = 188
	// tsReadChunks is the number of TS packets to read at a time.
	tsReadChunks = 7
	// tsBufferSize is the total buffer size for reading TS data.
	tsBufferSize = tsPacketSize * tsReadChunks
)

// client represents a connected downstream viewer.
type client struct {
	w       http.ResponseWriter
	flusher http.Flusher
	done    chan struct{}
}

// channelConnection tracks an active upstream connection and its downstream clients.
type channelConnection struct {
	streamURL string
	clients   []*client
	cancel    context.CancelFunc
	mu        sync.Mutex
}

// ProxyService handles stream proxying with connection sharing and failover.
type ProxyService struct {
	channelRepo        *repository.ChannelRepository
	streamRepo         *repository.StreamRepository
	m3uAccountRepo     *repository.M3UAccountRepository
	userAgentRepo      *repository.UserAgentRepository
	channelProfileRepo *repository.ChannelProfileRepository
	streamProfileRepo  *repository.StreamProfileRepository
	clientService      *ClientService
	log                zerolog.Logger

	mu          sync.RWMutex
	connections map[int64]*channelConnection
}

// NewProxyService creates a new ProxyService.
func NewProxyService(
	channelRepo *repository.ChannelRepository,
	streamRepo *repository.StreamRepository,
	m3uAccountRepo *repository.M3UAccountRepository,
	userAgentRepo *repository.UserAgentRepository,
	channelProfileRepo *repository.ChannelProfileRepository,
	streamProfileRepo *repository.StreamProfileRepository,
	clientService *ClientService,
	log zerolog.Logger,
) *ProxyService {
	return &ProxyService{
		channelRepo:        channelRepo,
		streamRepo:         streamRepo,
		m3uAccountRepo:     m3uAccountRepo,
		userAgentRepo:      userAgentRepo,
		channelProfileRepo: channelProfileRepo,
		streamProfileRepo:  streamProfileRepo,
		clientService:      clientService,
		log:                log.With().Str("service", "proxy").Logger(),
		connections:        make(map[int64]*channelConnection),
	}
}

// ProxyStream proxies a live stream for the given channel to the HTTP response writer.
// If another client is already watching the same channel, the new client shares
// the existing upstream connection. When the last client disconnects, the upstream
// connection is closed.
//
// profileOverride optionally overrides the channel's configured profile by name
// (e.g. "Browser"). When set, connection sharing is skipped to avoid mixing formats.
func (s *ProxyService) ProxyStream(ctx context.Context, w http.ResponseWriter, r *http.Request, channelID int64, profileOverride string) error {
	// Verify channel exists
	channel, err := s.channelRepo.GetByID(ctx, channelID)
	if err != nil {
		return fmt.Errorf("channel not found: %w", err)
	}

	if !channel.IsEnabled {
		return fmt.Errorf("channel %d is disabled", channelID)
	}

	// Resolve stream mode and profile
	var mode string
	var streamProfile *models.StreamProfile

	skipConnectionSharing := false
	if profileOverride != "" {
		// Priority 1: explicit query param
		sp, err := s.streamProfileRepo.GetByName(ctx, profileOverride)
		if err != nil {
			return fmt.Errorf("profile %q not found: %w", profileOverride, err)
		}
		mode = sp.StreamMode
		streamProfile = sp
		skipConnectionSharing = true
		s.log.Info().Int64("channel_id", channelID).Str("profile_override", profileOverride).Str("mode", mode).Msg("using profile override")
	} else if s.clientService != nil {
		// Priority 2: client header detection
		matched, err := s.clientService.MatchClient(ctx, r)
		if err != nil {
			s.log.Warn().Err(err).Msg("client detection error")
		}
		if matched != nil {
			mode = matched.StreamMode
			streamProfile = matched
			skipConnectionSharing = true
		}
	}
	if mode == "" {
		// Priority 3+4: channel profile chain + fallback
		mode, streamProfile = ResolveStreamMode(ctx, channel, s.channelProfileRepo, s.streamProfileRepo, s.log)
	}

	contentType := "video/mp2t"
	if streamProfile != nil && mode == "ffmpeg" {
		switch streamProfile.Container {
		case "mp4":
			contentType = "video/mp4"
		case "matroska":
			contentType = "video/x-matroska"
		case "webm":
			contentType = "video/webm"
		}
	}

	// Create client
	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming not supported")
	}

	c := &client{
		w:       w,
		flusher: flusher,
		done:    make(chan struct{}),
	}

	// Set response headers and flush immediately so the client receives
	// the HTTP 200 response before the upstream connection is established.
	// Without this, clients like Plex timeout waiting for a response while
	// ffmpeg starts up.
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Connection", "close")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Profile overrides and client-detected profiles skip connection sharing (different format, can't mix)
	if !skipConnectionSharing {
		// Check if there is already an active connection for this channel
		s.mu.RLock()
		conn, exists := s.connections[channelID]
		s.mu.RUnlock()

		if exists {
			// Join existing connection
			conn.mu.Lock()
			conn.clients = append(conn.clients, c)
			conn.mu.Unlock()

			s.log.Info().Int64("channel_id", channelID).Msg("client joined existing stream")

			// Wait for client to disconnect
			select {
			case <-c.done:
			case <-r.Context().Done():
			}

			s.removeClient(channelID, c)
			return nil
		}
	}

	// No existing connection (or profile override) - start a new upstream connection
	// Headers have already been sent (200 OK) so we cannot return an error
	// to the handler — it would try to write JSON on top of the stream response.
	if err := s.startUpstream(ctx, r, channelID, c, mode, streamProfile); err != nil {
		s.log.Error().Err(err).Int64("channel_id", channelID).Msg("all streams failed for channel")
	}
	return nil
}

// startUpstream initiates an upstream connection for the channel and begins proxying data.
func (s *ProxyService) startUpstream(ctx context.Context, r *http.Request, channelID int64, c *client, mode string, profile *models.StreamProfile) error {
	// Get channel streams in priority order
	channelStreams, err := s.channelRepo.GetStreams(ctx, channelID)
	if err != nil {
		return fmt.Errorf("getting channel streams: %w", err)
	}

	if len(channelStreams) == 0 {
		return fmt.Errorf("no streams assigned to channel %d", channelID)
	}

	// Get user agent for upstream requests
	var userAgent string
	ua, err := s.userAgentRepo.GetDefault(ctx)
	if err == nil && ua != nil {
		userAgent = ua.UserAgent
	}

	// Try each stream in priority order (failover)
	for _, cs := range channelStreams {
		stream, err := s.streamRepo.GetByID(ctx, cs.StreamID)
		if err != nil {
			s.log.Warn().Err(err).Int64("stream_id", cs.StreamID).Msg("stream not found, trying next")
			continue
		}

		if !stream.IsActive {
			s.log.Warn().Int64("stream_id", stream.ID).Msg("stream inactive, trying next")
			continue
		}

		upstreamCtx, cancel := context.WithCancel(context.Background())

		conn := &channelConnection{
			streamURL: stream.URL,
			clients:   []*client{c},
			cancel:    cancel,
		}

		// Register the connection
		s.mu.Lock()
		s.connections[channelID] = conn
		s.mu.Unlock()

		var reader io.ReadCloser
		var startErr error

		switch mode {
		case "ffmpeg":
			// Use profile args if available, otherwise use fallback copy args
			ffmpegProfile := profile
			if ffmpegProfile == nil || ffmpegProfile.Args == "" {
				ffmpegProfile = &models.StreamProfile{
					Name:    "fallback-copy",
					Command: "ffmpeg",
					Args:    "-hide_banner -loglevel warning -i {input} -c copy -f mpegts pipe:1",
				}
			}
			reader, startErr = s.startFFmpeg(upstreamCtx, channelID, stream, ffmpegProfile, userAgent)
		default:
			// "proxy" and "direct" both use HTTP passthrough at the proxy endpoint
			reader, startErr = s.startHTTPPassthrough(upstreamCtx, channelID, stream, userAgent)
		}

		if startErr != nil {
			cancel()
			s.cleanupConnection(channelID)
			s.log.Error().Err(startErr).Str("url", stream.URL).Msg("upstream start failed, trying next")
			continue
		}

		// Successfully connected - start proxying in a goroutine
		go s.proxyLoop(channelID, reader, cancel)

		// Wait for this client to disconnect
		select {
		case <-c.done:
		case <-r.Context().Done():
		}

		s.removeClient(channelID, c)
		return nil
	}

	return fmt.Errorf("all streams failed for channel %d", channelID)
}

// startHTTPPassthrough opens a direct HTTP connection to the upstream (no transcoding).
func (s *ProxyService) startHTTPPassthrough(ctx context.Context, channelID int64, stream *models.Stream, userAgent string) (io.ReadCloser, error) {
	s.log.Info().
		Int64("channel_id", channelID).
		Int64("stream_id", stream.ID).
		Str("url", stream.URL).
		Str("mode", "direct passthrough").
		Msg("starting upstream connection")

	upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodGet, stream.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating upstream request: %w", err)
	}

	if userAgent != "" {
		upstreamReq.Header.Set("User-Agent", userAgent)
	}

	resp, err := http.DefaultClient.Do(upstreamReq)
	if err != nil {
		return nil, fmt.Errorf("upstream connection failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("upstream returned %d", resp.StatusCode)
	}

	return resp.Body, nil
}

// shellSplit splits a command string into arguments, respecting double and single quotes.
func shellSplit(s string) []string {
	var args []string
	var current strings.Builder
	inDouble := false
	inSingle := false

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

// startFFmpeg spawns an ffmpeg process to transcode the upstream stream.
func (s *ProxyService) startFFmpeg(ctx context.Context, channelID int64, stream *models.Stream, profile *models.StreamProfile, userAgent string) (io.ReadCloser, error) {
	// Build the ffmpeg argument list from the stored args string.
	// The args contain {input} as a placeholder for the stream URL.
	argsStr := strings.Replace(profile.Args, "{input}", stream.URL, 1)
	args := shellSplit(argsStr)

	// Inject user agent before -i if one is configured and not already present
	if userAgent != "" {
		hasUserAgent := false
		for _, arg := range args {
			if arg == "-user_agent" {
				hasUserAgent = true
				break
			}
		}
		if !hasUserAgent {
			for i, arg := range args {
				if arg == "-i" {
					newArgs := make([]string, 0, len(args)+2)
					newArgs = append(newArgs, args[:i]...)
					newArgs = append(newArgs, "-user_agent", userAgent)
					newArgs = append(newArgs, args[i:]...)
					args = newArgs
					break
				}
			}
		}
	}

	s.log.Info().
		Int64("channel_id", channelID).
		Int64("stream_id", stream.ID).
		Str("url", stream.URL).
		Str("profile", profile.Name).
		Str("command", profile.Command).
		Strs("args", args).
		Msg("starting transcoding")

	cmd := exec.CommandContext(ctx, profile.Command, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("creating ffmpeg stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("creating ffmpeg stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting ffmpeg: %w", err)
	}

	// Log ffmpeg stderr in the background
	go s.logFFmpegStderr(channelID, stderr)

	// Wait for process exit in background to avoid zombie processes.
	// We capture the error BEFORE checking ctx so hardware/codec errors aren't masked.
	go func() {
		waitErr := cmd.Wait()
		if waitErr != nil {
			// Check if this was a genuine ffmpeg error vs a normal shutdown
			if ctx.Err() != nil && cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == -1 {
				// Killed by signal (context cancelled) — expected shutdown
				s.log.Info().Int64("channel_id", channelID).Msg("ffmpeg process stopped")
			} else {
				exitCode := -1
				if cmd.ProcessState != nil {
					exitCode = cmd.ProcessState.ExitCode()
				}
				s.log.Error().Err(waitErr).Int("exit_code", exitCode).Int64("channel_id", channelID).Msg("ffmpeg process exited with error")
			}
		} else {
			s.log.Info().Int64("channel_id", channelID).Msg("ffmpeg process finished")
		}
	}()

	return stdout, nil
}

// logFFmpegStderr reads ffmpeg's stderr and logs each line at WARN level
// so hardware/codec errors are always visible.
func (s *ProxyService) logFFmpegStderr(channelID int64, stderr io.ReadCloser) {
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		line := scanner.Text()
		s.log.Warn().Int64("channel_id", channelID).Str("ffmpeg", line).Msg("ffmpeg output")
	}
}

// proxyLoop reads from the upstream and distributes data to all connected clients.
func (s *ProxyService) proxyLoop(channelID int64, upstream io.ReadCloser, cancel context.CancelFunc) {
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
			// Write to all connected clients, removing any that fail
			alive := make([]*client, 0, len(conn.clients))
			for _, c := range conn.clients {
				if _, writeErr := c.w.Write(buf[:n]); writeErr != nil {
					close(c.done)
					continue
				}
				c.flusher.Flush()
				alive = append(alive, c)
			}
			conn.clients = alive

			// If no clients remain, stop the upstream
			if len(conn.clients) == 0 {
				conn.mu.Unlock()
				s.log.Info().Int64("channel_id", channelID).Msg("no clients remaining, closing upstream")
				return
			}
			conn.mu.Unlock()
		}

		if err != nil {
			if err != io.EOF {
				s.log.Error().Err(err).Int64("channel_id", channelID).Msg("upstream read error")
			}
			// Notify all remaining clients
			s.mu.RLock()
			conn, exists := s.connections[channelID]
			s.mu.RUnlock()
			if exists {
				conn.mu.Lock()
				for _, c := range conn.clients {
					close(c.done)
				}
				conn.clients = nil
				conn.mu.Unlock()
			}
			return
		}
	}
}

// removeClient removes a client from the channel connection. If this is the last
// client, it cancels the upstream connection.
func (s *ProxyService) removeClient(channelID int64, c *client) {
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

	s.log.Info().Int64("channel_id", channelID).Int("remaining", remaining).Msg("client disconnected")

	if remaining == 0 {
		conn.cancel()
		s.cleanupConnection(channelID)
	}
}

// cleanupConnection removes the connection entry for a channel.
func (s *ProxyService) cleanupConnection(channelID int64) {
	s.mu.Lock()
	delete(s.connections, channelID)
	s.mu.Unlock()
}

// ProxyRawStream proxies a raw stream by stream ID (for preview/debug).
// This bypasses the channel → profile resolution chain and always does HTTP passthrough.
func (s *ProxyService) ProxyRawStream(ctx context.Context, w http.ResponseWriter, r *http.Request, streamID int64) error {
	stream, err := s.streamRepo.GetByID(ctx, streamID)
	if err != nil {
		return fmt.Errorf("stream not found: %w", err)
	}

	if !stream.IsActive {
		return fmt.Errorf("stream %d is inactive", streamID)
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming not supported")
	}

	// Get user agent for upstream requests
	var userAgent string
	ua, err := s.userAgentRepo.GetDefault(ctx)
	if err == nil && ua != nil {
		userAgent = ua.UserAgent
	}

	// Connect upstream BEFORE writing response headers so the handler
	// can return a proper HTTP error if the upstream is unreachable.
	upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, stream.URL, nil)
	if err != nil {
		return fmt.Errorf("creating upstream request: %w", err)
	}

	if userAgent != "" {
		upstreamReq.Header.Set("User-Agent", userAgent)
	}

	resp, err := http.DefaultClient.Do(upstreamReq)
	if err != nil {
		return fmt.Errorf("upstream connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("upstream returned %d", resp.StatusCode)
	}

	s.log.Info().Int64("stream_id", streamID).Str("url", stream.URL).Msg("raw stream proxy started")

	// Upstream confirmed — now write response headers
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Connection", "close")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	buf := make([]byte, tsBufferSize)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return nil // client disconnected
			}
			flusher.Flush()
		}
		if readErr != nil {
			if readErr != io.EOF {
				s.log.Error().Err(readErr).Int64("stream_id", streamID).Msg("raw stream read error")
			}
			return nil
		}
	}
}

// ActiveConnections returns the number of channels currently being proxied.
func (s *ProxyService) ActiveConnections() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.connections)
}

// ActiveClients returns the total number of connected clients across all channels.
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
