package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/wireguard"
)

type ConnectRequest struct {
	PrivateKey    string `json:"private_key"`
	Address       string `json:"address"`
	DNS           string `json:"dns"`
	PeerPublicKey string `json:"peer_public_key"`
	PeerEndpoint  string `json:"peer_endpoint"`
	RouteHosts    string `json:"route_hosts"`
}

type WireGuardService struct {
	settingsService *SettingsService
	mu              sync.RWMutex
	tunnel          *wireguard.Tunnel
	httpClient      *http.Client
	transport       *wireguard.RoutingTransport
	lastConfig      string
	state           string
	connectedAt     time.Time
	exitIP          string
	lastError       string
	log             zerolog.Logger
}

func NewWireGuardService(settingsService *SettingsService, log zerolog.Logger) *WireGuardService {
	return &WireGuardService{
		settingsService: settingsService,
		state:           "unconfigured",
		log:             log.With().Str("service", "wireguard").Logger(),
	}
}

func (s *WireGuardService) Start(ctx context.Context) error {
	fp := s.ConfigFingerprint(ctx)
	s.mu.Lock()
	s.lastConfig = fp
	s.mu.Unlock()

	if !s.hasConfig(ctx) {
		s.mu.Lock()
		s.state = "unconfigured"
		s.mu.Unlock()
		s.log.Info().Msg("wireguard unconfigured")
		return nil
	}

	if !s.IsEnabled(ctx) {
		s.mu.Lock()
		s.state = "disconnected"
		s.mu.Unlock()
		s.log.Info().Msg("wireguard disabled")
		return nil
	}
	return s.doConnect(ctx)
}

func (s *WireGuardService) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeLocked()
}

func (s *WireGuardService) Reconfigure(ctx context.Context) error {
	s.mu.Lock()
	s.closeLocked()
	s.mu.Unlock()

	fp := s.ConfigFingerprint(ctx)

	if !s.IsEnabled(ctx) {
		s.mu.Lock()
		s.lastConfig = fp
		if s.hasConfig(ctx) {
			s.state = "disconnected"
		} else {
			s.state = "unconfigured"
		}
		s.mu.Unlock()
		s.log.Info().Msg("wireguard disabled after reconfigure")
		return nil
	}
	return s.doConnect(ctx)
}

func (s *WireGuardService) Connect(ctx context.Context, req ConnectRequest) error {
	settings := map[string]string{
		"wg_private_key":    req.PrivateKey,
		"wg_address":        req.Address,
		"wg_dns":            req.DNS,
		"wg_peer_public_key": req.PeerPublicKey,
		"wg_peer_endpoint":  req.PeerEndpoint,
		"wg_route_hosts":    req.RouteHosts,
		"wg_enabled":        "true",
	}
	for k, v := range settings {
		if err := s.settingsService.Set(ctx, k, v); err != nil {
			return fmt.Errorf("saving %s: %w", k, err)
		}
	}
	return s.Reconfigure(ctx)
}

func (s *WireGuardService) Disconnect(ctx context.Context) {
	_ = s.settingsService.Set(ctx, "wg_enabled", "false")

	s.mu.Lock()
	s.closeLocked()
	s.state = "disconnected"
	s.exitIP = ""
	s.connectedAt = time.Time{}
	s.mu.Unlock()

	fp := s.ConfigFingerprint(ctx)
	s.mu.Lock()
	s.lastConfig = fp
	s.mu.Unlock()

	s.log.Info().Msg("wireguard disconnected")
}

func ValidateConfig(req ConnectRequest) map[string]string {
	errs := make(map[string]string)

	if req.PrivateKey == "" {
		errs["private_key"] = "Private key is required"
	} else {
		raw, err := base64.StdEncoding.DecodeString(req.PrivateKey)
		if err != nil || len(raw) != 32 {
			errs["private_key"] = "Configuration invalid \u2014 example: YWJjZGVm...base64...NTY="
		}
	}

	if req.Address == "" {
		errs["address"] = "Address is required"
	} else if _, err := netip.ParsePrefix(req.Address); err != nil {
		errs["address"] = "Configuration invalid \u2014 example: 10.20.30.40/24"
	}

	if req.DNS == "" {
		errs["dns"] = "DNS is required"
	} else {
		for _, d := range strings.Split(req.DNS, ",") {
			d = strings.TrimSpace(d)
			if d == "" {
				continue
			}
			if _, err := netip.ParseAddr(d); err != nil {
				errs["dns"] = "Configuration invalid \u2014 example: 1.1.1.1, 8.8.8.8"
				break
			}
		}
	}

	if req.PeerPublicKey == "" {
		errs["peer_public_key"] = "Peer public key is required"
	} else {
		raw, err := base64.StdEncoding.DecodeString(req.PeerPublicKey)
		if err != nil || len(raw) != 32 {
			errs["peer_public_key"] = "Configuration invalid \u2014 example: YWJjZGVm...base64...NTY="
		}
	}

	if req.PeerEndpoint == "" {
		errs["peer_endpoint"] = "Peer endpoint is required"
	} else if _, _, err := net.SplitHostPort(req.PeerEndpoint); err != nil {
		errs["peer_endpoint"] = "Configuration invalid \u2014 example: vpn.example.com:51820"
	}

	return errs
}

func (s *WireGuardService) HTTPClient() *http.Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.httpClient != nil {
		return s.httpClient
	}
	return http.DefaultClient
}

func (s *WireGuardService) Transport() http.RoundTripper {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.transport != nil {
		return s.transport
	}
	return nil
}

func (s *WireGuardService) IsEnabled(ctx context.Context) bool {
	val, err := s.settingsService.Get(ctx, "wg_enabled")
	if err != nil {
		return false
	}
	return val == "true"
}

func (s *WireGuardService) IsConnected() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tunnel != nil
}

func (s *WireGuardService) Status(ctx context.Context) map[string]interface{} {
	s.mu.RLock()
	currentState := s.state
	connAt := s.connectedAt
	eip := s.exitIP
	lastErr := s.lastError
	tunnel := s.tunnel
	s.mu.RUnlock()

	result := map[string]interface{}{
		"state": currentState,
		"error": lastErr,
	}

	if tunnel != nil {
		result["exit_ip"] = eip
		if !connAt.IsZero() {
			result["connected_since"] = connAt.UTC().Format(time.RFC3339)
		}
		if stats, err := tunnel.Stats(); err == nil {
			result["tx_bytes"] = stats.TxBytes
			result["rx_bytes"] = stats.RxBytes
			result["peer_endpoint"] = stats.Endpoint
			if stats.LastHandshakeSec > 0 {
				hs := time.Unix(stats.LastHandshakeSec, stats.LastHandshakeNsec)
				result["last_handshake"] = hs.UTC().Format(time.RFC3339)
			}
		}
	}

	config := map[string]interface{}{}
	if addr, err := s.settingsService.Get(ctx, "wg_address"); err == nil {
		config["address"] = addr
	}
	if dns, err := s.settingsService.Get(ctx, "wg_dns"); err == nil {
		config["dns"] = dns
	}
	if pk, err := s.settingsService.Get(ctx, "wg_peer_public_key"); err == nil {
		config["peer_public_key"] = pk
	}
	if ep, err := s.settingsService.Get(ctx, "wg_peer_endpoint"); err == nil {
		config["peer_endpoint"] = ep
	}
	config["private_key"] = "***"
	result["config"] = config

	return result
}

func (s *WireGuardService) ConfigFingerprint(ctx context.Context) string {
	keys := []string{"wg_enabled", "wg_private_key", "wg_address", "wg_dns", "wg_peer_public_key", "wg_peer_endpoint", "wg_route_hosts"}
	var fp string
	for _, k := range keys {
		val, _ := s.settingsService.Get(ctx, k)
		fp += k + "=" + val + ";"
	}
	return fp
}

func (s *WireGuardService) SyncIfChanged(ctx context.Context) {
	fp := s.ConfigFingerprint(ctx)
	s.mu.RLock()
	changed := fp != s.lastConfig
	s.mu.RUnlock()

	if !changed {
		return
	}

	s.log.Info().Msg("wireguard config changed, reconfiguring")
	if err := s.Reconfigure(ctx); err != nil {
		s.log.Error().Err(err).Msg("wireguard reconfigure failed")
	}
}

func (s *WireGuardService) doConnect(ctx context.Context) error {
	s.mu.Lock()
	s.state = "connecting"
	s.lastError = ""
	s.exitIP = ""
	s.mu.Unlock()

	cfg, err := s.loadConfig(ctx)
	if err != nil {
		s.mu.Lock()
		s.state = "error"
		s.lastError = err.Error()
		s.mu.Unlock()
		return err
	}

	tunnel, err := wireguard.NewTunnel(cfg, s.log)
	if err != nil {
		s.mu.Lock()
		s.state = "error"
		s.lastError = err.Error()
		s.mu.Unlock()
		return err
	}

	routeHosts, _ := s.settingsService.Get(ctx, "wg_route_hosts")
	transport := wireguard.NewRoutingTransport(tunnel, routeHosts)
	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	fp := s.ConfigFingerprint(ctx)
	now := time.Now()

	s.mu.Lock()
	s.tunnel = tunnel
	s.httpClient = client
	s.transport = transport
	s.lastConfig = fp
	s.state = "connected"
	s.connectedAt = now
	s.mu.Unlock()

	s.log.Info().Msg("wireguard connected")

	go s.fetchExitIP(tunnel)

	return nil
}

func (s *WireGuardService) fetchExitIP(tunnel *wireguard.Tunnel) {
	client := tunnel.HTTPClient(10 * time.Second)
	resp, err := client.Get("https://api.ipify.org")
	if err != nil {
		s.log.Warn().Err(err).Msg("failed to fetch exit IP")
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return
	}
	ip := strings.TrimSpace(string(body))
	s.mu.Lock()
	if s.tunnel == tunnel {
		s.exitIP = ip
	}
	s.mu.Unlock()
	s.log.Info().Str("exit_ip", ip).Msg("wireguard exit IP resolved")
}

func (s *WireGuardService) loadConfig(ctx context.Context) (wireguard.Config, error) {
	get := func(key string) string {
		val, _ := s.settingsService.Get(ctx, key)
		return val
	}

	return wireguard.Config{
		PrivateKey:    get("wg_private_key"),
		Address:       get("wg_address"),
		DNS:           get("wg_dns"),
		PeerPublicKey: get("wg_peer_public_key"),
		PeerEndpoint:  get("wg_peer_endpoint"),
	}, nil
}

func (s *WireGuardService) hasConfig(ctx context.Context) bool {
	addr, _ := s.settingsService.Get(ctx, "wg_address")
	return addr != ""
}

func (s *WireGuardService) closeLocked() {
	if s.tunnel != nil {
		s.tunnel.Close()
		s.tunnel = nil
		s.httpClient = nil
		s.transport = nil
		s.lastConfig = ""
	}
}
