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
	StreamIcon   any    `json:"stream_icon"`
	EPGChannelID string `json:"epg_channel_id"`
	CategoryID   string `json:"category_id"`
	CategoryName string `json:"category_name"`
}

func (s Stream) Icon() string {
	if str, ok := s.StreamIcon.(string); ok {
		return str
	}
	return ""
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
	c := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 0 {
				for k, v := range via[0].Header {
					if _, ok := req.Header[k]; !ok {
						req.Header[k] = v
					}
				}
			}
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
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

type VODStream struct {
	Num          int    `json:"num"`
	Name         string `json:"name"`
	StreamType   string `json:"stream_type"`
	StreamID     int    `json:"stream_id"`
	StreamIcon   any    `json:"stream_icon"`
	Rating       any    `json:"rating"`
	IsAdult      string `json:"is_adult"`
	CategoryID   string `json:"category_id"`
	CategoryName string `json:"category_name"`
	ContainerExt string `json:"container_extension"`
}

func (v VODStream) Icon() string {
	if s, ok := v.StreamIcon.(string); ok {
		return s
	}
	return ""
}

func (v VODStream) RatingStr() string {
	switch r := v.Rating.(type) {
	case string:
		return r
	case float64:
		if r > 0 {
			return fmt.Sprintf("%.1f", r)
		}
	}
	return ""
}

type VODInfo struct {
	Info struct {
		Name         string `json:"name"`
		Plot         string `json:"plot"`
		Cast         string `json:"cast"`
		Director     string `json:"director"`
		Genre        string `json:"genre"`
		ReleaseDate  string `json:"releasedate"`
		Rating       string `json:"rating"`
		Duration     string `json:"duration"`
		DurationSecs int    `json:"duration_secs"`
		Trailer      string `json:"youtube_trailer"`
		BackdropPath []string `json:"backdrop_path"`
		Video        VideoInfo `json:"video"`
		Audio        AudioInfo `json:"audio"`
	} `json:"info"`
	MovieData struct {
		StreamID     int    `json:"stream_id"`
		ContainerExt string `json:"container_extension"`
	} `json:"movie_data"`
}

type VideoInfo struct {
	CodecName string `json:"codec_name"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Profile   string `json:"profile"`
}

type AudioInfo struct {
	CodecName string `json:"codec_name"`
	Channels  int    `json:"channels"`
}

type Series struct {
	Num           int      `json:"num"`
	Name          string   `json:"name"`
	SeriesID      int      `json:"series_id"`
	Cover         string   `json:"cover"`
	Plot          string   `json:"plot"`
	Cast          string   `json:"cast"`
	Director      string   `json:"director"`
	Genre         string   `json:"genre"`
	ReleaseDate   string   `json:"releaseDate"`
	Rating        string   `json:"rating"`
	BackdropPath  []string `json:"backdrop_path"`
	YouTubeTrailer string  `json:"youtube_trailer"`
	CategoryID    string   `json:"category_id"`
	CategoryName  string   `json:"category_name"`
}

type SeasonInfo struct {
	SeasonNumber int    `json:"season_number"`
	Name         string `json:"name"`
	AirDate      string `json:"air_date"`
	EpisodeCount int    `json:"episode_count"`
	Cover        string `json:"cover"`
}

type SeriesInfo struct {
	Seasons    map[string][]SeriesEpisode `json:"episodes"`
	RawSeasons []SeasonInfo               `json:"seasons"`
	Info       SeriesDetail               `json:"info"`
}

type SeriesDetail struct {
	Name  string `json:"name"`
	Cover string `json:"cover"`
}

type SeriesEpisode struct {
	ID           string            `json:"id"`
	EpisodeNum   int               `json:"episode_num"`
	Title        string            `json:"title"`
	ContainerExt string            `json:"container_extension"`
	Info         SeriesEpisodeInfo `json:"info"`
}

type SeriesEpisodeInfo struct {
	Duration     string    `json:"duration"`
	DurationSecs int       `json:"duration_secs"`
	MovieImage   string    `json:"movie_image"`
	Season       int       `json:"season"`
	Plot         string    `json:"plot"`
	Video        VideoInfo `json:"video"`
	Audio        AudioInfo `json:"audio"`
}

func (c *Client) GetVODStreams(ctx context.Context) ([]VODStream, error) {
	url := fmt.Sprintf("%s/player_api.php?username=%s&password=%s&action=get_vod_streams", c.baseURL, c.username, c.password)
	var streams []VODStream
	if err := c.get(ctx, url, &streams); err != nil {
		return nil, fmt.Errorf("getting vod streams: %w", err)
	}
	return streams, nil
}

func (c *Client) GetSeries(ctx context.Context) ([]Series, error) {
	url := fmt.Sprintf("%s/player_api.php?username=%s&password=%s&action=get_series", c.baseURL, c.username, c.password)
	var series []Series
	if err := c.get(ctx, url, &series); err != nil {
		return nil, fmt.Errorf("getting series: %w", err)
	}
	return series, nil
}

func (c *Client) GetSeriesInfo(ctx context.Context, seriesID int) (*SeriesInfo, error) {
	url := fmt.Sprintf("%s/player_api.php?username=%s&password=%s&action=get_series_info&series_id=%d", c.baseURL, c.username, c.password, seriesID)
	var info SeriesInfo
	if err := c.get(ctx, url, &info); err != nil {
		return nil, fmt.Errorf("getting series info: %w", err)
	}
	return &info, nil
}

func (c *Client) GetVODInfo(ctx context.Context, streamID int) (*VODInfo, error) {
	url := fmt.Sprintf("%s/player_api.php?username=%s&password=%s&action=get_vod_info&vod_id=%d", c.baseURL, c.username, c.password, streamID)
	var info VODInfo
	if err := c.get(ctx, url, &info); err != nil {
		return nil, fmt.Errorf("getting vod info: %w", err)
	}
	return &info, nil
}

func (c *Client) GetVODStreamURL(streamID int, extension string) string {
	if extension == "" {
		extension = "mp4"
	}
	return fmt.Sprintf("%s/movie/%s/%s/%d.%s", c.baseURL, c.username, c.password, streamID, extension)
}

func (c *Client) GetSeriesStreamURL(streamID int, extension string) string {
	// return fmt.Sprintf("%s/series/%s/%s/%d.%s", c.baseURL, c.username, c.password, streamID, extension)
	return fmt.Sprintf("%s/series/%s/%s/%d", c.baseURL, c.username, c.password, streamID)
}

func (c *Client) GetStreamURL(streamID int, extension string) string {
	// return fmt.Sprintf("%s/%s/%s/%d.%s", c.baseURL, c.username, c.password, streamID, extension)
	return fmt.Sprintf("%s/%s/%s/%d", c.baseURL, c.username, c.password, streamID)
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
