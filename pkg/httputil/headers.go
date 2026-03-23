package httputil

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/config"
)

func SetBrowserHeaders(req *http.Request, cfg *config.Config) {
	req.Header.Set("User-Agent", cfg.UserAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Connection", "keep-alive")
	if cfg.BypassHeader != "" && cfg.BypassSecret != "" {
		req.Header.Set(cfg.BypassHeader, cfg.BypassSecret)
	}
}

func LogUpstreamFailure(log zerolog.Logger, resp *http.Response, url string) {
	event := log.Debug().Int("status", resp.StatusCode).Str("url", url)
	for _, name := range []string{"Server", "Cf-Ray", "Cf-Mitigated", "Content-Type", "Retry-After", "X-Cache"} {
		if v := resp.Header.Get(name); v != "" {
			event = event.Str(strings.ToLower(strings.ReplaceAll(name, "-", "_")), v)
		}
	}
	body := make([]byte, 512)
	n, _ := io.ReadFull(resp.Body, body)
	if n > 0 {
		event = event.Str("body_snippet", string(body[:n]))
	}
	event.Msg("upstream response detail")
}

func Fetch(ctx context.Context, client *http.Client, cfg *config.Config, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	SetBrowserHeaders(req, cfg)
	return client.Do(req)
}

func FetchAndDecompress(ctx context.Context, client *http.Client, cfg *config.Config, url string, log zerolog.Logger) (io.ReadCloser, error) {
	resp, err := Fetch(ctx, client, cfg, url)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		LogUpstreamFailure(log, resp, url)
		resp.Body.Close()
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	return DecompressReader(resp.Body, url)
}
