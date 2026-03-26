package wireguard

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog"

	utls "github.com/refraction-networking/utls"
)

type RoutingTransport struct {
	tunnel   *Tunnel
	tunnelRT http.RoundTripper
	direct   http.RoundTripper
	routeAll bool
	hosts    map[string]bool
	log      zerolog.Logger
}

func NewRoutingTransport(tunnel *Tunnel, routeHosts string, log zerolog.Logger) *RoutingTransport {
	hosts := make(map[string]bool)
	for _, h := range strings.Split(routeHosts, ",") {
		h = strings.TrimSpace(h)
		if h != "" {
			hosts[strings.ToLower(h)] = true
		}
	}

	dialCtx := tunnel.DialContext
	rtLog := log.With().Str("component", "wg_routing").Logger()

	hostList := make([]string, 0, len(hosts))
	for h := range hosts {
		hostList = append(hostList, h)
	}
	rtLog.Info().Bool("route_all", len(hosts) == 0).Strs("hosts", hostList).Msg("routing transport configured")

	return &RoutingTransport{
		tunnel: tunnel,
		tunnelRT: &http.Transport{
			DialContext:           dialCtx,
			DialTLSContext:        chromeTLSDialer(dialCtx),
			ResponseHeaderTimeout: 30 * time.Second,
		},
		direct:   http.DefaultTransport,
		routeAll: len(hosts) == 0,
		hosts:    hosts,
		log:      rtLog,
	}
}

func chromeTLSDialer(dialCtx func(ctx context.Context, network, address string) (net.Conn, error)) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := dialCtx(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		host, _, _ := net.SplitHostPort(addr)
		spec, err := utls.UTLSIdToSpec(utls.HelloChrome_Auto)
		if err != nil {
			conn.Close()
			return nil, err
		}
		for i, ext := range spec.Extensions {
			if _, ok := ext.(*utls.ALPNExtension); ok {
				spec.Extensions[i] = &utls.ALPNExtension{AlpnProtocols: []string{"http/1.1"}}
				break
			}
		}
		uconn := utls.UClient(conn, &utls.Config{ServerName: host}, utls.HelloCustom)
		if err := uconn.ApplyPreset(&spec); err != nil {
			conn.Close()
			return nil, err
		}
		if err := uconn.HandshakeContext(ctx); err != nil {
			conn.Close()
			return nil, err
		}
		return uconn, nil
	}
}

func (t *RoutingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Hostname()
	if t.shouldRoute(host) {
		if e := t.log.Debug(); e.Enabled() {
			e.Str("host", host).Str("path", "tunnel").Msg("routing request")
		}
		return t.tunnelRT.RoundTrip(req)
	}
	if e := t.log.Debug(); e.Enabled() {
		e.Str("host", host).Str("path", "direct").Msg("routing request")
	}
	return t.direct.RoundTrip(req)
}

func (t *RoutingTransport) shouldRoute(host string) bool {
	if t.routeAll {
		return true
	}
	return t.hosts[strings.ToLower(host)]
}

func (t *RoutingTransport) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, _, _ := net.SplitHostPort(address)
	if t.shouldRoute(host) {
		return t.tunnel.DialContext(ctx, network, address)
	}
	var d net.Dialer
	return d.DialContext(ctx, network, address)
}

