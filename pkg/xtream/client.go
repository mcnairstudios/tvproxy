package xtream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Client struct {
	baseURL    string
	username   string
	password   string
	httpClient *http.Client
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

func NewClient(baseURL, username, password string) *Client {
	return &Client{
		baseURL:    baseURL,
		username:   username,
		password:   password,
		httpClient: &http.Client{Timeout: 30 * time.Second},
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

func (c *Client) get(ctx context.Context, url string, result interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(result)
}
