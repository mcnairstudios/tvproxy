package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/store"
	"github.com/gavinmcnair/tvproxy/pkg/wireguard"
)

type tunnelState struct {
	profileID   string
	profileName string
	tunnel      *wireguard.Tunnel
	transport   *wireguard.RoutingTransport
	state       string
	connectedAt time.Time
	exitIP      string
	lastError   string
	healthy     bool
	lastCheck   time.Time
}

type MultiWireGuardService struct {
	profileStore    *store.WireGuardProfileStore
	settingsService *SettingsService
	mu              sync.RWMutex
	tunnels         map[string]*tunnelState
	activeID        string
	log             zerolog.Logger
}

func NewMultiWireGuardService(profileStore *store.WireGuardProfileStore, settingsService *SettingsService, log zerolog.Logger) *MultiWireGuardService {
	return &MultiWireGuardService{
		profileStore:    profileStore,
		settingsService: settingsService,
		tunnels:         make(map[string]*tunnelState),
		log:             log.With().Str("service", "wireguard_multi").Logger(),
	}
}

func (s *MultiWireGuardService) Start(ctx context.Context) error {
	s.migrateLegacyConfig(ctx)

	profiles, err := s.profileStore.List(ctx)
	if err != nil {
		return fmt.Errorf("listing profiles: %w", err)
	}

	for _, p := range profiles {
		if !p.IsEnabled {
			continue
		}
		if err := s.connectProfile(ctx, p); err != nil {
			s.log.Error().Err(err).Str("profile", p.Name).Msg("failed to connect profile")
		}
	}

	s.selectBestActive(ctx)
	return nil
}

func (s *MultiWireGuardService) migrateLegacyConfig(ctx context.Context) {
	profiles, _ := s.profileStore.List(ctx)
	if len(profiles) > 0 {
		return
	}

	get := func(key string) string {
		val, _ := s.settingsService.Get(ctx, key)
		return val
	}

	addr := get("wg_address")
	if addr == "" {
		return
	}

	enabled := get("wg_enabled") == "true"

	p := &models.WireGuardProfile{
		Name:          "Default",
		PrivateKey:    get("wg_private_key"),
		Address:       addr,
		DNS:           get("wg_dns"),
		PeerPublicKey: get("wg_peer_public_key"),
		PeerEndpoint:  get("wg_peer_endpoint"),
		RouteHosts:    get("wg_route_hosts"),
		IsEnabled:     enabled,
		Priority:      0,
	}

	if err := s.profileStore.Create(ctx, p); err != nil {
		s.log.Error().Err(err).Msg("failed to migrate legacy wireguard config")
		return
	}

	s.log.Info().Str("name", p.Name).Bool("enabled", enabled).Msg("migrated legacy wireguard config to profile")
}

func (s *MultiWireGuardService) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, ts := range s.tunnels {
		if ts.tunnel != nil {
			ts.tunnel.Close()
		}
		delete(s.tunnels, id)
	}
	s.activeID = ""
}

func (s *MultiWireGuardService) connectProfile(ctx context.Context, p models.WireGuardProfile) error {
	cfg := wireguard.Config{
		PrivateKey:    p.PrivateKey,
		Address:       p.Address,
		DNS:           p.DNS,
		PeerPublicKey: p.PeerPublicKey,
		PeerEndpoint:  p.PeerEndpoint,
	}

	tunnel, err := wireguard.NewTunnel(cfg, s.log)
	if err != nil {
		s.mu.Lock()
		s.tunnels[p.ID] = &tunnelState{
			profileID:   p.ID,
			profileName: p.Name,
			state:       "error",
			lastError:   err.Error(),
		}
		s.mu.Unlock()
		return err
	}

	transport := wireguard.NewRoutingTransport(tunnel, p.RouteHosts, s.log)

	ts := &tunnelState{
		profileID:   p.ID,
		profileName: p.Name,
		tunnel:      tunnel,
		transport:   transport,
		state:       "connected",
		connectedAt: time.Now(),
		healthy:     true,
	}

	s.mu.Lock()
	s.tunnels[p.ID] = ts
	s.mu.Unlock()

	s.log.Info().Str("profile", p.Name).Str("endpoint", p.PeerEndpoint).Msg("profile connected")

	go s.fetchExitIP(ts)

	return nil
}

func (s *MultiWireGuardService) disconnectProfile(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ts, ok := s.tunnels[id]
	if !ok {
		return
	}
	if ts.tunnel != nil {
		ts.tunnel.Close()
	}
	delete(s.tunnels, id)
	if s.activeID == id {
		s.activeID = ""
	}
}

func (s *MultiWireGuardService) selectBestActive(ctx context.Context) {
	profiles, _ := s.profileStore.List(ctx)

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, p := range profiles {
		if !p.IsEnabled {
			continue
		}
		ts, ok := s.tunnels[p.ID]
		if !ok || ts.tunnel == nil {
			continue
		}
		if ts.healthy {
			if s.activeID != p.ID {
				s.activeID = p.ID
				s.log.Info().Str("profile", p.Name).Msg("switched active profile")
			}
			return
		}
	}

	if s.activeID != "" {
		s.log.Warn().Msg("no healthy profiles, clearing active")
		s.activeID = ""
	}
}

func (s *MultiWireGuardService) ActiveProfileID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeID
}

func (s *MultiWireGuardService) ActiveProfileName() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if ts, ok := s.tunnels[s.activeID]; ok {
		return ts.profileName
	}
	return ""
}

func (s *MultiWireGuardService) HTTPClient() *http.Client {
	return &http.Client{
		Transport: &multiTransport{svc: s},
	}
}

type multiTransport struct {
	svc *MultiWireGuardService
}

func (t *multiTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.svc.mu.RLock()
	var transport *wireguard.RoutingTransport
	if ts, ok := t.svc.tunnels[t.svc.activeID]; ok && ts.transport != nil {
		transport = ts.transport
	}
	t.svc.mu.RUnlock()

	if transport != nil {
		return transport.RoundTrip(req)
	}
	return http.DefaultTransport.RoundTrip(req)
}

func (s *MultiWireGuardService) RunHealthChecks(ctx context.Context) {
	profiles, err := s.profileStore.List(ctx)
	if err != nil {
		return
	}

	for _, p := range profiles {
		if !p.IsEnabled || p.HealthcheckURL == "" {
			continue
		}

		s.mu.RLock()
		ts, ok := s.tunnels[p.ID]
		s.mu.RUnlock()
		if !ok || ts.tunnel == nil {
			continue
		}

		healthy := s.checkHealth(ts, p)

		s.mu.Lock()
		if ts2, ok := s.tunnels[p.ID]; ok {
			ts2.healthy = healthy
			ts2.lastCheck = time.Now()
		}
		s.mu.Unlock()
	}

	s.selectBestActive(ctx)
}

func (s *MultiWireGuardService) checkHealth(ts *tunnelState, p models.WireGuardProfile) bool {
	client := ts.tunnel.HTTPClient(10 * time.Second)

	cacheBust := "?_t=" + strconv.FormatInt(time.Now().UnixNano(), 10)
	url := p.HealthcheckURL
	if strings.Contains(url, "?") {
		url += "&_t=" + strconv.FormatInt(time.Now().UnixNano(), 10)
	} else {
		url += cacheBust
	}

	method := p.HealthcheckMethod
	if method == "" {
		method = "HEAD"
	}

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		s.log.Warn().Err(err).Str("profile", p.Name).Msg("healthcheck request creation failed")
		return false
	}
	req.Header.Set("User-Agent", "TVProxy-HealthCheck/1.0")

	resp, err := client.Do(req)
	if err != nil {
		s.log.Warn().Err(err).Str("profile", p.Name).Str("url", p.HealthcheckURL).Msg("healthcheck failed")
		return false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))

	healthy := resp.StatusCode >= 200 && resp.StatusCode < 400
	if !healthy {
		s.log.Warn().Str("profile", p.Name).Int("status", resp.StatusCode).Msg("healthcheck unhealthy")
	}
	return healthy
}

func (s *MultiWireGuardService) ProfileStatus(ctx context.Context, profileID string) map[string]any {
	s.mu.RLock()
	ts, ok := s.tunnels[profileID]
	isActive := s.activeID == profileID
	s.mu.RUnlock()

	result := map[string]any{
		"active": isActive,
	}

	if !ok {
		result["state"] = "disconnected"
		return result
	}

	result["state"] = ts.state
	result["error"] = ts.lastError
	result["healthy"] = ts.healthy
	result["exit_ip"] = ts.exitIP

	if !ts.connectedAt.IsZero() {
		result["connected_since"] = ts.connectedAt.UTC().Format(time.RFC3339)
	}
	if !ts.lastCheck.IsZero() {
		result["last_healthcheck"] = ts.lastCheck.UTC().Format(time.RFC3339)
	}

	if ts.tunnel != nil {
		if stats, err := ts.tunnel.Stats(); err == nil {
			result["tx_bytes"] = stats.TxBytes
			result["rx_bytes"] = stats.RxBytes
			result["peer_endpoint"] = stats.Endpoint
			if stats.LastHandshakeSec > 0 {
				hs := time.Unix(stats.LastHandshakeSec, stats.LastHandshakeNsec)
				result["last_handshake"] = hs.UTC().Format(time.RFC3339)
			}
		}
	}

	return result
}

func (s *MultiWireGuardService) AllStatus(ctx context.Context) map[string]any {
	profiles, _ := s.profileStore.List(ctx)

	s.mu.RLock()
	activeID := s.activeID
	s.mu.RUnlock()

	profileStatuses := make([]map[string]any, 0, len(profiles))
	for _, p := range profiles {
		ps := s.ProfileStatus(ctx, p.ID)
		ps["id"] = p.ID
		ps["name"] = p.Name
		ps["priority"] = p.Priority
		ps["healthcheck_url"] = p.HealthcheckURL
		profileStatuses = append(profileStatuses, ps)
	}

	return map[string]any{
		"active_profile_id":   activeID,
		"active_profile_name": s.ActiveProfileName(),
		"profiles":            profileStatuses,
	}
}

func (s *MultiWireGuardService) ConnectedTransports() map[string]*http.Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]*http.Client)
	for id, ts := range s.tunnels {
		if ts.transport != nil && ts.state == "connected" {
			result[id] = &http.Client{Transport: ts.transport}
		}
	}
	return result
}

func (s *MultiWireGuardService) ProfileName(profileID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if ts, ok := s.tunnels[profileID]; ok {
		return ts.profileName
	}
	return profileID
}

func (s *MultiWireGuardService) SetActiveProfile(ctx context.Context, profileID string) error {
	s.mu.Lock()
	ts, ok := s.tunnels[profileID]
	if !ok || ts.tunnel == nil {
		s.mu.Unlock()
		return fmt.Errorf("profile not connected")
	}
	s.activeID = profileID
	s.mu.Unlock()
	s.log.Info().Str("profile", ts.profileName).Msg("manually switched active profile")
	return nil
}

func (s *MultiWireGuardService) ReconnectProfile(ctx context.Context, profileID string) error {
	profile, err := s.profileStore.GetByID(ctx, profileID)
	if err != nil || profile == nil {
		return fmt.Errorf("profile not found")
	}
	s.disconnectProfile(profileID)
	if err := s.connectProfile(ctx, *profile); err != nil {
		return err
	}
	s.selectBestActive(ctx)
	return nil
}

func (s *MultiWireGuardService) SyncProfiles(ctx context.Context) {
	profiles, err := s.profileStore.List(ctx)
	if err != nil {
		return
	}

	enabledIDs := make(map[string]bool)
	for _, p := range profiles {
		if !p.IsEnabled {
			continue
		}
		enabledIDs[p.ID] = true

		s.mu.RLock()
		_, connected := s.tunnels[p.ID]
		s.mu.RUnlock()

		if !connected {
			if err := s.connectProfile(ctx, p); err != nil {
				s.log.Error().Err(err).Str("profile", p.Name).Msg("sync: failed to connect profile")
			}
		}
	}

	s.mu.RLock()
	var toDisconnect []string
	for id := range s.tunnels {
		if !enabledIDs[id] {
			toDisconnect = append(toDisconnect, id)
		}
	}
	s.mu.RUnlock()

	for _, id := range toDisconnect {
		s.disconnectProfile(id)
	}

	s.selectBestActive(ctx)
}

func (s *MultiWireGuardService) fetchExitIP(ts *tunnelState) {
	if ts.tunnel == nil {
		return
	}
	client := ts.tunnel.HTTPClient(10 * time.Second)
	resp, err := client.Get("https://api.ipify.org")
	if err != nil {
		s.log.Warn().Err(err).Str("profile", ts.profileName).Msg("failed to fetch exit IP")
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return
	}
	ip := strings.TrimSpace(string(body))
	s.mu.Lock()
	ts.exitIP = ip
	s.mu.Unlock()
	s.log.Info().Str("profile", ts.profileName).Str("exit_ip", ip).Msg("exit IP resolved")
}

func (s *MultiWireGuardService) IsConnected() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeID != "" && s.tunnels[s.activeID] != nil
}
