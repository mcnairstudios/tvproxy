package session

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gavinmcnair/tvproxy/pkg/config"
	"github.com/rs/zerolog"
)

type WGPool struct {
	proxies     []*poolEntry
	activeIDFn  func() string
	mu          sync.RWMutex
	log         zerolog.Logger
}

type poolEntry struct {
	profileID   string
	profileName string
	proxy       *WGProxy
	failCount   int
	lastFail    time.Time
}

func NewWGPool(log zerolog.Logger) *WGPool {
	return &WGPool{
		log: log.With().Str("component", "wg_pool").Logger(),
	}
}

func (p *WGPool) AddProxy(profileID, profileName string, client *http.Client, cfg *config.Config) error {
	proxy, err := NewWGProxy(client, cfg, p.log)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.proxies = append(p.proxies, &poolEntry{
		profileID:   profileID,
		profileName: profileName,
		proxy:       proxy,
	})
	p.mu.Unlock()
	p.log.Info().Str("profile", profileName).Int("port", proxy.Port()).Msgf("pool proxy: curl \"http://127.0.0.1:%d/?url=...\"", proxy.Port())
	return nil
}

func (p *WGPool) Do(req *http.Request) (*http.Response, error) {
	p.mu.RLock()
	entries := make([]*poolEntry, len(p.proxies))
	copy(entries, p.proxies)
	p.mu.RUnlock()

	if len(entries) == 0 {
		return http.DefaultTransport.RoundTrip(req)
	}

	sorted := make([]*poolEntry, len(entries))
	copy(sorted, entries)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j].failCount < sorted[j-1].failCount; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}

	var lastErr error
	for _, e := range sorted {
		cloned := req.Clone(req.Context())
		resp, err := e.proxy.client().Do(cloned)
		if err != nil {
			p.markFail(e)
			lastErr = err
			p.log.Warn().Str("profile", e.profileName).Err(err).Msg("proxy failed, trying next")
			continue
		}
		if resp.StatusCode == 403 {
			resp.Body.Close()
			p.markFail(e)
			lastErr = fmt.Errorf("403 via %s", e.profileName)
			p.log.Warn().Str("profile", e.profileName).Msg("got 403, trying next proxy")
			continue
		}
		p.markSuccess(e)
		return resp, nil
	}

	return nil, fmt.Errorf("all %d proxies failed: %w", len(sorted), lastErr)
}

func (p *WGPool) markFail(e *poolEntry) {
	p.mu.Lock()
	e.failCount++
	e.lastFail = time.Now()
	p.mu.Unlock()
}

func (p *WGPool) markSuccess(e *poolEntry) {
	p.mu.Lock()
	e.failCount = 0
	p.mu.Unlock()
}

func (p *WGPool) Transport() http.RoundTripper {
	return &poolTransport{pool: p}
}

func (p *WGPool) Client() *http.Client {
	return &http.Client{Transport: p.Transport()}
}

func (p *WGPool) AddDirect(profileID, profileName string, cfg *config.Config, log zerolog.Logger) {
	proxy, err := NewWGProxy(&http.Client{}, cfg, log)
	if err != nil {
		log.Error().Err(err).Msg("failed to start direct proxy")
		return
	}
	p.mu.Lock()
	p.proxies = append(p.proxies, &poolEntry{
		profileID:   profileID,
		profileName: profileName,
		proxy:       proxy,
		failCount:   1000,
	})
	p.mu.Unlock()
	p.log.Info().Str("profile", profileName).Int("port", proxy.Port()).Msgf("pool direct: curl \"http://127.0.0.1:%d/?url=...\" (lowest priority)", proxy.Port())
}

type PoolStatus struct {
	ProfileID   string `json:"profile_id"`
	ProfileName string `json:"profile_name"`
	Port        int    `json:"port"`
	FailCount   int    `json:"fail_count"`
	IsDirect    bool   `json:"is_direct"`
}

func (p *WGPool) Status() []PoolStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var result []PoolStatus
	for _, e := range p.proxies {
		result = append(result, PoolStatus{
			ProfileID:   e.profileID,
			ProfileName: e.profileName,
			Port:        e.proxy.Port(),
			FailCount:   e.failCount,
			IsDirect:    e.failCount >= 1000,
		})
	}
	return result
}

func (p *WGPool) SetActiveIDFunc(fn func() string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.activeIDFn = fn
}

func (p *WGPool) ProxyURL(streamURL string) string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.proxies) == 0 {
		return streamURL
	}
	if p.activeIDFn != nil {
		activeID := p.activeIDFn()
		if activeID != "" {
			for _, e := range p.proxies {
				if e.profileID == activeID {
					return e.proxy.ProxyURL(streamURL)
				}
			}
		}
	}
	best := p.proxies[0]
	for _, e := range p.proxies[1:] {
		if e.failCount < best.failCount {
			best = e
		}
	}
	return best.proxy.ProxyURL(streamURL)
}

func (p *WGPool) Count() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.proxies)
}

func (p *WGPool) StopAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, e := range p.proxies {
		e.proxy.Stop()
	}
	p.proxies = nil
}

type poolTransport struct {
	pool *WGPool
}

func (t *poolTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.pool.Do(req)
}
