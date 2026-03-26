package xtream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gavinmcnair/tvproxy/pkg/httputil"
	"time"
)

type Client struct {
	baseURL      string
	username     string
	password     string
	userAgent    string
	bypassHeader string
	bypassSecret string
	httpClient   *http.Client
}

type Category struct {
	ID   string `json:"category_id"`
	Name string `json:"category_name"`
}

type Stream struct {
	Num          int    `json:"num"`
	Name         string `json:"name"`
	StreamType   string `json:"stream_type"`
	StreamID     int    `json:"stream_id"`
	StreamIcon   string `json:"stream_icon"`
	EPGChannelID string `json:"epg_channel_id"`
	CategoryID   string `json:"category_id"`
	CategoryName string `json:"category_name"`
}

type ServerInfo struct {
	URL            string `json:"url"`
	Port           string `json:"port"`
	HTTPSPort      string `json:"https_port"`
	ServerProtocol string `json:"server_protocol"`
}

type UserInfo struct {
	Username          string `json:"username"`
	Password          string `json:"password"`
	Status            string `json:"status"`
	MaxConnections    string `json:"max_connections"`
	ActiveConnections string `json:"active_cons"`
}

type AuthResponse struct {
	UserInfo   UserInfo   `json:"user_info"`
	ServerInfo ServerInfo `json:"server_info"`
}

func NewClient(baseURL, username, password, userAgent, bypassHeader, bypassSecret string, timeout time.Duration, transport http.RoundTripper) *Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	c := &http.Client{Timeout: timeout}
	if transport != nil {
		c.Transport = transport
	}
	return &Client{
		baseURL:      baseURL,
		username:     username,
		password:     password,
		userAgent:    userAgent,
		bypassHeader: bypassHeader,
		bypassSecret: bypassSecret,
		httpClient:   c,
	}
}

func (c *Client) Authenticate(ctx context.Context) (*AuthResponse, error) {
	url := fmt.Sprintf("%s/player_api.php?username=%s&password=%s", c.baseURL, c.username, c.password)
	var resp AuthResponse
	if err := c.get(ctx, url, &resp); err != nil {
		return nil, fmt.Errorf("authenticating: %w", err)
	}
	return &resp, nil
}

func (c *Client) GetLiveCategories(ctx context.Context) ([]Category, error) {
	url := fmt.Sprintf("%s/player_api.php?username=%s&password=%s&action=get_live_categories", c.baseURL, c.username, c.password)
	var categories []Category
	if err := c.get(ctx, url, &categories); err != nil {
		return nil, fmt.Errorf("getting categories: %w", err)
	}
	return categories, nil
}

func (c *Client) GetLiveStreams(ctx context.Context) ([]Stream, error) {
	url := fmt.Sprintf("%s/player_api.php?username=%s&password=%s&action=get_live_streams", c.baseURL, c.username, c.password)
	var streams []Stream
	if err := c.get(ctx, url, &streams); err != nil {
		return nil, fmt.Errorf("getting streams: %w", err)
	}
	return streams, nil
}

func (c *Client) GetStreamURL(streamID int, extension string) string {
	if extension == "" {
		extension = "ts"
	}
	return fmt.Sprintf("%s/%s/%s/%d.%s", c.baseURL, c.username, c.password, streamID, extension)
}

func (c *Client) get(ctx context.Context, url string, result any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Connection", "keep-alive")
	if c.bypassHeader != "" && c.bypassSecret != "" {
		req.Header.Set(c.bypassHeader, c.bypassSecret)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	reader, err := httputil.DecompressReader(resp.Body, url)
	if err != nil {
		resp.Body.Close()
		return err
	}
	defer reader.Close()
	return json.NewDecoder(reader).Decode(result)
}
