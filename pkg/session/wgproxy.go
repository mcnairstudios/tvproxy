package session

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"

	"github.com/gavinmcnair/tvproxy/pkg/config"
	"github.com/rs/zerolog"
)

type WGProxy struct {
	listener net.Listener
	server   *http.Server
	port     int
	log      zerolog.Logger
}

func NewWGProxy(wgClient *http.Client, cfg *config.Config, log zerolog.Logger) (*WGProxy, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("binding wg proxy: %w", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port

	proxyClient := &http.Client{
		Transport: wgClient.Transport,
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetURL := r.URL.Query().Get("url")
		if targetURL == "" {
			http.Error(w, "missing url param", http.StatusBadRequest)
			return
		}

		outReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if cfg != nil {
			outReq.Header.Set("User-Agent", cfg.UserAgent)
			if cfg.BypassHeader != "" && cfg.BypassSecret != "" {
				outReq.Header.Set(cfg.BypassHeader, cfg.BypassSecret)
			}
		}
		if rng := r.Header.Get("Range"); rng != "" {
			outReq.Header.Set("Range", rng)
		}

		resp, err := proxyClient.Do(outReq)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		for k, vv := range resp.Header {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	})

	server := &http.Server{Handler: handler}

	wp := &WGProxy{
		listener: listener,
		server:   server,
		port:     port,
		log:      log.With().Str("component", "wg_proxy").Int("port", port).Logger(),
	}

	go func() {
		wp.log.Info().Msg("wireguard proxy started")
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			wp.log.Error().Err(err).Msg("wireguard proxy error")
		}
	}()

	return wp, nil
}

func (p *WGProxy) ProxyURL(streamURL string) string {
	return fmt.Sprintf("http://127.0.0.1:%d/?url=%s", p.port, streamURL)
}

func (p *WGProxy) Port() int {
	return p.port
}

func (p *WGProxy) client() *http.Client {
	return &http.Client{
		Transport: &proxyTransport{proxyURL: fmt.Sprintf("http://127.0.0.1:%d", p.port)},
	}
}

type proxyTransport struct {
	proxyURL string
}

func (t *proxyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	targetURL := req.URL.String()
	proxyReq, err := http.NewRequestWithContext(req.Context(), req.Method, t.proxyURL+"/?url="+url.QueryEscape(targetURL), req.Body)
	if err != nil {
		return nil, err
	}
	for k, v := range req.Header {
		proxyReq.Header[k] = v
	}
	return http.DefaultClient.Do(proxyReq)
}

func (p *WGProxy) Stop() {
	p.server.Shutdown(context.Background())
	p.log.Info().Msg("wireguard proxy stopped")
}

type WGProxyManager struct {
	proxies map[string]*WGProxy
	mu      sync.RWMutex
}

func NewWGProxyManager() *WGProxyManager {
	return &WGProxyManager{
		proxies: make(map[string]*WGProxy),
	}
}

func (m *WGProxyManager) GetOrCreate(profileID string, wgClient *http.Client, cfg *config.Config, log zerolog.Logger) (*WGProxy, error) {
	m.mu.RLock()
	if p, ok := m.proxies[profileID]; ok {
		m.mu.RUnlock()
		return p, nil
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	if p, ok := m.proxies[profileID]; ok {
		return p, nil
	}

	p, err := NewWGProxy(wgClient, cfg, log)
	if err != nil {
		return nil, err
	}
	m.proxies[profileID] = p
	return p, nil
}

func (m *WGProxyManager) GetAny() *WGProxy {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, p := range m.proxies {
		return p
	}
	return nil
}

func (m *WGProxyManager) Get(profileID string) *WGProxy {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.proxies[profileID]
}

func (m *WGProxyManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, p := range m.proxies {
		p.Stop()
		delete(m.proxies, id)
	}
}
