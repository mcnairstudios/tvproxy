package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/rs/zerolog"

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
	w      http.ResponseWriter
	flusher http.Flusher
	done   chan struct{}
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
	channelRepo    *repository.ChannelRepository
	streamRepo     *repository.StreamRepository
	m3uAccountRepo *repository.M3UAccountRepository
	userAgentRepo  *repository.UserAgentRepository
	log            zerolog.Logger

	mu          sync.RWMutex
	connections map[int64]*channelConnection
}

// NewProxyService creates a new ProxyService.
func NewProxyService(
	channelRepo *repository.ChannelRepository,
	streamRepo *repository.StreamRepository,
	m3uAccountRepo *repository.M3UAccountRepository,
	userAgentRepo *repository.UserAgentRepository,
	log zerolog.Logger,
) *ProxyService {
	return &ProxyService{
		channelRepo:    channelRepo,
		streamRepo:     streamRepo,
		m3uAccountRepo: m3uAccountRepo,
		userAgentRepo:  userAgentRepo,
		log:            log.With().Str("service", "proxy").Logger(),
		connections:    make(map[int64]*channelConnection),
	}
}

// ProxyStream proxies a live stream for the given channel to the HTTP response writer.
// If another client is already watching the same channel, the new client shares
// the existing upstream connection. When the last client disconnects, the upstream
// connection is closed.
func (s *ProxyService) ProxyStream(ctx context.Context, w http.ResponseWriter, r *http.Request, channelID int64) error {
	// Verify channel exists
	channel, err := s.channelRepo.GetByID(ctx, channelID)
	if err != nil {
		return fmt.Errorf("channel not found: %w", err)
	}

	if !channel.IsEnabled {
		return fmt.Errorf("channel %d is disabled", channelID)
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

	// Set response headers for TS streaming
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("Connection", "keep-alive")

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

	// No existing connection - start a new upstream connection
	return s.startUpstream(ctx, r, channelID, c)
}

// startUpstream initiates an upstream connection for the channel and begins proxying data.
func (s *ProxyService) startUpstream(ctx context.Context, r *http.Request, channelID int64, c *client) error {
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

		s.log.Info().
			Int64("channel_id", channelID).
			Int64("stream_id", stream.ID).
			Str("url", stream.URL).
			Msg("starting upstream connection")

		// Create upstream request
		upstreamReq, err := http.NewRequestWithContext(upstreamCtx, http.MethodGet, stream.URL, nil)
		if err != nil {
			cancel()
			s.cleanupConnection(channelID)
			s.log.Error().Err(err).Msg("creating upstream request")
			continue
		}

		if userAgent != "" {
			upstreamReq.Header.Set("User-Agent", userAgent)
		}

		resp, err := http.DefaultClient.Do(upstreamReq)
		if err != nil {
			cancel()
			s.cleanupConnection(channelID)
			s.log.Error().Err(err).Str("url", stream.URL).Msg("upstream connection failed, trying next")
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			cancel()
			s.cleanupConnection(channelID)
			s.log.Warn().Int("status", resp.StatusCode).Str("url", stream.URL).Msg("upstream returned non-200, trying next")
			continue
		}

		// Successfully connected - start proxying in a goroutine
		go s.proxyLoop(channelID, resp.Body, cancel)

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
