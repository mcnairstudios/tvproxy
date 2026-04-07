package tmdb

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
)

type SyncStatus struct {
	Syncing   bool `json:"syncing"`
	Total     int  `json:"total"`
	Completed int  `json:"completed"`
}

type VODItem struct {
	Name      string
	MediaType string
}

type Client struct {
	http       *http.Client
	log        zerolog.Logger
	cache      *Cache
	images     *ImageCache
	apiKeyFn   func() string
	syncTotal  atomic.Int64
	syncDone   atomic.Int64
	syncing    atomic.Bool
}

func NewClient(baseDir string, apiKeyFn func() string, log zerolog.Logger) *Client {
	httpClient := &http.Client{Timeout: 10 * time.Second}
	cache := NewCache(filepath.Join(baseDir, "search"))
	cache.MigrateFrom(baseDir)
	images := NewImageCache(filepath.Join(baseDir, "images"), httpClient)

	return &Client{
		http:     httpClient,
		log:      log.With().Str("component", "tmdb").Logger(),
		cache:    cache,
		images:   images,
		apiKeyFn: apiKeyFn,
	}
}

func (c *Client) Search(query, mediaType string) (map[string]any, error) {
	apiKey := c.apiKeyFn()
	if apiKey == "" {
		return map[string]any{"results": []any{}}, nil
	}

	cacheKey := SearchCacheKey(query, mediaType)
	if cached, ok := c.cache.Get(cacheKey); ok {
		if result, ok := cached.(map[string]any); ok {
			return result, nil
		}
	}

	searchQuery, year := extractSearchParams(query)

	endpoint := "search/multi"
	if mediaType == "movie" {
		endpoint = "search/movie"
	} else if mediaType == "tv" || mediaType == "series" {
		endpoint = "search/tv"
	}

	yearParam := ""
	if year != "" {
		yearParam = "&year=" + year
	}

	searchURL := fmt.Sprintf("https://api.themoviedb.org/3/%s?api_key=%s&query=%s&language=en-GB%s",
		endpoint, url.QueryEscape(apiKey), url.QueryEscape(searchQuery), yearParam)

	resp, err := c.http.Get(searchURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)

	if results, ok := result["results"].([]any); ok && len(results) > 0 {
		c.cache.Set(cacheKey, result)
	}

	return result, nil
}

func (c *Client) Details(mediaType, id string) (any, error) {
	apiKey := c.apiKeyFn()
	if apiKey == "" {
		return map[string]any{}, nil
	}

	cacheKey := DetailCacheKey(mediaType, id)
	if cached, ok := c.cache.Get(cacheKey); ok {
		return cached, nil
	}

	detailURL := fmt.Sprintf("https://api.themoviedb.org/3/%s/%s?api_key=%s&language=en-GB&append_to_response=images,credits",
		url.PathEscape(mediaType), url.PathEscape(id), url.QueryEscape(apiKey))

	resp, err := c.http.Get(detailURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result any
	json.NewDecoder(resp.Body).Decode(&result)

	c.cache.Set(cacheKey, result)
	return result, nil
}

func (c *Client) Invalidate(query string) {
	c.cache.Delete("search_" + query)
	c.cache.Delete("search_" + query + "_movie")
	c.cache.Delete("search_" + query + "_tv")
}

func (c *Client) LookupPoster(name, mediaType string) string {
	query, year := BuildQuery(name)
	if year != "" {
		query = query + " (" + year + ")"
	}

	cacheKey := SearchCacheKey(query, mediaType)
	cached, ok := c.cache.Get(cacheKey)
	if !ok {
		return ""
	}

	result, ok := cached.(map[string]any)
	if !ok {
		return ""
	}

	results, ok := result["results"].([]any)
	if !ok || len(results) == 0 {
		return ""
	}

	first, ok := results[0].(map[string]any)
	if !ok {
		return ""
	}

	posterPath, _ := first["poster_path"].(string)
	return PosterURL(posterPath)
}

func (c *Client) ServeImage(w http.ResponseWriter, r *http.Request) {
	c.images.Serve(w, r, r.URL.Query().Get("path"), r.URL.Query().Get("size"))
}

func (c *Client) Status() SyncStatus {
	return SyncStatus{
		Syncing:   c.syncing.Load(),
		Total:     int(c.syncTotal.Load()),
		Completed: int(c.syncDone.Load()),
	}
}

func (c *Client) Sync(items []VODItem) {
	if c.syncing.Load() {
		return
	}

	var toSync []VODItem
	seen := make(map[string]bool)
	for _, item := range items {
		query, year := BuildQuery(item.Name)
		if year != "" {
			query = query + " (" + year + ")"
		}
		cacheKey := SearchCacheKey(query, item.MediaType)
		if c.cache.Has(cacheKey) {
			continue
		}
		if seen[cacheKey] {
			continue
		}
		seen[cacheKey] = true
		toSync = append(toSync, item)
	}

	if len(toSync) == 0 {
		return
	}

	c.syncTotal.Store(int64(len(toSync)))
	c.syncDone.Store(0)
	c.syncing.Store(true)

	go func() {
		defer c.syncing.Store(false)
		c.log.Info().Int("items", len(toSync)).Msg("starting TMDB sync")

		for _, item := range toSync {
			if c.apiKeyFn() == "" {
				c.log.Warn().Msg("no TMDB API key, stopping sync")
				break
			}

			query, year := BuildQuery(item.Name)
			if year != "" {
				query = query + " (" + year + ")"
			}

			if _, err := c.Search(query, item.MediaType); err != nil {
				c.log.Debug().Err(err).Str("query", query).Msg("search failed")
			}

			c.syncDone.Add(1)
			time.Sleep(250 * time.Millisecond)
		}

		c.log.Info().
			Int64("completed", c.syncDone.Load()).
			Int64("total", c.syncTotal.Load()).
			Msg("TMDB sync finished")
	}()
}

func extractSearchParams(query string) (string, string) {
	if idx := strings.LastIndex(query, "("); idx > 0 {
		end := strings.Index(query[idx:], ")")
		if end > 0 {
			year := strings.TrimSpace(query[idx+1 : idx+end])
			if len(year) == 4 && year[0] >= '1' && year[0] <= '2' {
				return strings.TrimSpace(query[:idx]), year
			}
		}
	}
	return query, ""
}
