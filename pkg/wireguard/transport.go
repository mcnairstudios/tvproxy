package wireguard

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"
)

type RoutingTransport struct {
	tunnel    *Tunnel
	tunnelRT  http.RoundTripper
	direct    http.RoundTripper
	routeAll  bool
	hosts     map[string]bool
}

func NewRoutingTransport(tunnel *Tunnel, routeHosts string) *RoutingTransport {
	hosts := make(map[string]bool)
	for _, h := range strings.Split(routeHosts, ",") {
		h = strings.TrimSpace(h)
		if h != "" {
			hosts[strings.ToLower(h)] = true
		}
	}

	return &RoutingTransport{
		tunnel:   tunnel,
		tunnelRT: &http.Transport{DialContext: tunnel.DialContext},
		direct:   http.DefaultTransport,
		routeAll: len(hosts) == 0,
		hosts:    hosts,
	}
}

func (t *RoutingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.shouldRoute(req.URL.Hostname()) {
		return t.tunnelRT.RoundTrip(req)
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

func NewRoutingClient(tunnel *Tunnel, routeHosts string, timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: NewRoutingTransport(tunnel, routeHosts),
		Timeout:   timeout,
	}
}
