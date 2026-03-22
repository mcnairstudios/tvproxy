package wireguard

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/netstack"
)

type PeerStats struct {
	TxBytes           int64
	RxBytes           int64
	LastHandshakeSec  int64
	LastHandshakeNsec int64
	Endpoint          string
}

type Config struct {
	PrivateKey    string // base64
	Address       string // e.g. "10.20.30.40/24"
	DNS           string // e.g. "1.1.1.1"
	PeerPublicKey string // base64
	PeerEndpoint  string // e.g. "vpn.example.com:51820"
}

type Tunnel struct {
	device *device.Device
	tnet   *netstack.Net
	log    zerolog.Logger
}

func NewTunnel(cfg Config, log zerolog.Logger) (*Tunnel, error) {
	log = log.With().Str("component", "wireguard").Logger()

	addr, err := parseAddress(cfg.Address)
	if err != nil {
		return nil, fmt.Errorf("parsing address: %w", err)
	}

	var dnsAddrs []netip.Addr
	if cfg.DNS != "" {
		for _, d := range strings.Split(cfg.DNS, ",") {
			d = strings.TrimSpace(d)
			dnsAddr, err := netip.ParseAddr(d)
			if err != nil {
				return nil, fmt.Errorf("parsing dns %q: %w", d, err)
			}
			dnsAddrs = append(dnsAddrs, dnsAddr)
		}
	}

	tun, tnet, err := netstack.CreateNetTUN(
		[]netip.Addr{addr},
		dnsAddrs,
		1420,
	)
	if err != nil {
		return nil, fmt.Errorf("creating netstack tun: %w", err)
	}

	privKeyHex, err := base64ToHex(cfg.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("decoding private key: %w", err)
	}

	pubKeyHex, err := base64ToHex(cfg.PeerPublicKey)
	if err != nil {
		return nil, fmt.Errorf("decoding peer public key: %w", err)
	}

	resolvedEndpoint, err := resolveEndpoint(cfg.PeerEndpoint)
	if err != nil {
		return nil, fmt.Errorf("resolving endpoint: %w", err)
	}

	dev := device.NewDevice(tun, conn.NewDefaultBind(), device.NewLogger(device.LogLevelError, "wg: "))

	ipcConfig := fmt.Sprintf(
		"private_key=%s\npublic_key=%s\nendpoint=%s\nallowed_ip=0.0.0.0/0\nallowed_ip=::/0\npersistent_keepalive_interval=25\n",
		privKeyHex,
		pubKeyHex,
		resolvedEndpoint,
	)

	if err := dev.IpcSet(ipcConfig); err != nil {
		dev.Close()
		return nil, fmt.Errorf("setting ipc config: %w", err)
	}

	if err := dev.Up(); err != nil {
		dev.Close()
		return nil, fmt.Errorf("bringing device up: %w", err)
	}

	log.Info().Str("address", cfg.Address).Str("endpoint", cfg.PeerEndpoint).Msg("wireguard tunnel established")

	return &Tunnel{
		device: dev,
		tnet:   tnet,
		log:    log,
	}, nil
}

func (t *Tunnel) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return t.tnet.DialContext(ctx, network, address)
}

func (t *Tunnel) HTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: t.DialContext,
		},
		Timeout: timeout,
	}
}

func (t *Tunnel) Stats() (*PeerStats, error) {
	if t.device == nil {
		return nil, fmt.Errorf("device not initialized")
	}
	resp, err := t.device.IpcGet()
	if err != nil {
		return nil, fmt.Errorf("ipc get: %w", err)
	}

	var stats PeerStats
	for _, line := range strings.Split(resp, "\n") {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key, val := parts[0], parts[1]
		switch key {
		case "tx_bytes":
			stats.TxBytes, _ = strconv.ParseInt(val, 10, 64)
		case "rx_bytes":
			stats.RxBytes, _ = strconv.ParseInt(val, 10, 64)
		case "last_handshake_time_sec":
			stats.LastHandshakeSec, _ = strconv.ParseInt(val, 10, 64)
		case "last_handshake_time_nsec":
			stats.LastHandshakeNsec, _ = strconv.ParseInt(val, 10, 64)
		case "endpoint":
			stats.Endpoint = val
		}
	}
	return &stats, nil
}

func (t *Tunnel) Close() {
	if t.device != nil {
		t.device.Close()
		t.log.Info().Msg("wireguard tunnel closed")
	}
}

func base64ToHex(b64 string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func resolveEndpoint(endpoint string) (string, error) {
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		return "", fmt.Errorf("splitting host:port: %w", err)
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return endpoint, nil
	}
	ips, err := net.LookupHost(host)
	if err != nil {
		return "", fmt.Errorf("resolving %q: %w", host, err)
	}
	if len(ips) == 0 {
		return "", fmt.Errorf("no addresses for %q", host)
	}
	return net.JoinHostPort(ips[0], port), nil
}

func parseAddress(addr string) (netip.Addr, error) {
	if strings.Contains(addr, "/") {
		prefix, err := netip.ParsePrefix(addr)
		if err != nil {
			return netip.Addr{}, err
		}
		return prefix.Addr(), nil
	}
	return netip.ParseAddr(addr)
}
