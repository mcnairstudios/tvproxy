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
	Name       string
	MediaType  string
	Collection string
}

type Client struct {
	http     *http.Client
	log      zerolog.Logger
	cache    *Cache
	meta     *MetadataStore
	images   *ImageCache
	apiKeyFn func() string

	syncTotal atomic.Int64
	syncDone  atomic.Int64
	syncing   atomic.Bool
}

func NewClient(baseDir string, apiKeyFn func() string, log zerolog.Logger) *Client {
	httpClient := &http.Client{Timeout: 10 * time.Second}
	cache := NewCache(filepath.Join(baseDir, "search"))
	cache.MigrateFrom(baseDir)

	return &Client{
		http:     httpClient,
		log:      log.With().Str("component", "tmdb").Logger(),
		cache:    cache,
		meta:     NewMetadataStore(baseDir),
		images:   NewImageCache(filepath.Join(baseDir, "images"), httpClient),
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
	} else if mediaType == "collection" {
		endpoint = "search/collection"
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
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

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

	appendExtra := "images,credits"
	if mediaType == "movie" {
		appendExtra += ",release_dates"
	} else if mediaType == "tv" {
		appendExtra += ",content_ratings"
	}
	detailURL := fmt.Sprintf("https://api.themoviedb.org/3/%s/%s?api_key=%s&language=en-GB&append_to_response=%s",
		url.PathEscape(mediaType), url.PathEscape(id), url.QueryEscape(apiKey), appendExtra)

	resp, err := c.http.Get(detailURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	c.cache.Set(cacheKey, result)
	return result, nil
}

func (c *Client) Season(tvID, seasonNum string) (any, error) {
	apiKey := c.apiKeyFn()
	if apiKey == "" {
		return map[string]any{}, nil
	}

	cacheKey := "season_" + tvID + "_" + seasonNum
	if cached, ok := c.cache.Get(cacheKey); ok {
		return cached, nil
	}

	seasonURL := fmt.Sprintf("https://api.themoviedb.org/3/tv/%s/season/%s?api_key=%s&language=en-GB",
		url.PathEscape(tvID), url.PathEscape(seasonNum), url.QueryEscape(apiKey))

	resp, err := c.http.Get(seasonURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	c.cache.Set(cacheKey, result)
	return result, nil
}

func (c *Client) Invalidate(query string) {
	c.cache.Delete("search_" + query)
	c.cache.Delete("search_" + query + "_movie")
	c.cache.Delete("search_" + query + "_tv")
}

func (c *Client) LookupPoster(name, mediaType string) string {
	clean, _ := CleanVODName(name)
	if mediaType == "movie" {
		if m := c.meta.GetMovie(clean); m != nil && m.PosterPath != "" {
			return PosterURL(m.PosterPath)
		}
	} else {
		if s := c.meta.GetSeries(clean); s != nil && s.PosterPath != "" {
			return PosterURL(s.PosterPath)
		}
	}
	return ""
}

func (c *Client) LookupCollection(name string) *CollectionMeta {
	if col := c.meta.GetCollection(name); col != nil {
		return col
	}
	return c.meta.GetCollection(name + " Collection")
}

func (c *Client) LookupBackdrop(name, mediaType string) string {
	clean, _ := CleanVODName(name)
	if mediaType == "movie" {
		if m := c.meta.GetMovie(clean); m != nil && m.BackdropPath != "" {
			return m.BackdropPath
		}
	} else {
		if s := c.meta.GetSeries(clean); s != nil && s.BackdropPath != "" {
			return s.BackdropPath
		}
	}
	return ""
}

func metaKey(name string) string {
	clean, year := CleanVODName(name)
	if year != "" {
		return clean + " (" + year + ")"
	}
	return clean
}

func (c *Client) LookupMovie(name string) *MovieMeta {
	return c.meta.GetMovie(metaKey(name))
}

func (c *Client) LookupSeries(name string) *SeriesMeta {
	return c.meta.GetSeries(metaKey(name))
}

func (c *Client) LookupEpisode(seriesName string, season, episode int) *EpisodeMeta {
	return c.meta.GetEpisode(metaKey(seriesName), season, episode)
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

func (c *Client) PopulateMetadataFromCache(items []VODItem) {
	populated := 0
	for _, item := range items {
		clean, year := CleanVODName(item.Name)
		query := clean
		if year != "" {
			query = clean + " (" + year + ")"
		}

		if item.MediaType == "movie" {
			if c.meta.GetMovie(clean) != nil {
				continue
			}
			cacheKey := SearchCacheKey(query, "movie")
			if cached, ok := c.cache.Get(cacheKey); ok {
				if result, ok := cached.(map[string]any); ok {
					c.resolveMovieFromCache(clean, result)
					populated++
				}
			}
		} else {
			if c.meta.GetSeries(clean) != nil {
				continue
			}
			cacheKey := SearchCacheKey(query, "tv")
			if cached, ok := c.cache.Get(cacheKey); ok {
				if result, ok := cached.(map[string]any); ok {
					c.resolveSeriesFromCache(clean, result)
					populated++
				}
			}
		}
	}
	if populated > 0 {
		c.log.Info().Int("populated", populated).Msg("populated metadata from cache")
	}
}

func (c *Client) resolveSeriesFromCache(cleanName string, searchResult map[string]any) {
	first := firstResult(searchResult)
	if first == nil {
		return
	}

	s := &SeriesMeta{
		Seasons: make(map[int]*SeasonMeta),
	}
	s.TMDBID = intVal(first, "id")
	s.PosterPath, _ = first["poster_path"].(string)
	s.BackdropPath, _ = first["backdrop_path"].(string)
	s.Overview, _ = first["overview"].(string)
	s.Rating, _ = first["vote_average"].(float64)
	if date, _ := first["first_air_date"].(string); len(date) >= 4 {
		s.Year = date[:4]
	}
	s.Genres = extractGenres(first)

	c.meta.SetSeries(cleanName, s)

	if s.TMDBID == 0 {
		return
	}

	tvID := fmt.Sprintf("%d", s.TMDBID)

	detailKey := DetailCacheKey("tv", tvID)
	detailCached, ok := c.cache.Get(detailKey)
	if !ok {
		return
	}
	detailMap, ok := detailCached.(map[string]any)
	if !ok {
		return
	}
	seasons, ok := detailMap["seasons"].([]any)
	if !ok {
		return
	}

	for _, raw := range seasons {
		sn, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		num := intVal(sn, "season_number")
		if num <= 0 {
			continue
		}

		seasonKey := "season_" + tvID + "_" + fmt.Sprintf("%d", num)
		seasonCached, ok := c.cache.Get(seasonKey)
		if !ok {
			continue
		}
		sdMap, ok := seasonCached.(map[string]any)
		if !ok {
			continue
		}
		rawEps, ok := sdMap["episodes"].([]any)
		if !ok {
			continue
		}

		episodes := make(map[int]*EpisodeMeta)
		for _, re := range rawEps {
			ep, ok := re.(map[string]any)
			if !ok {
				continue
			}
			epNum := intVal(ep, "episode_number")
			if epNum <= 0 {
				continue
			}
			em := &EpisodeMeta{}
			em.Name, _ = ep["name"].(string)
			em.Overview, _ = ep["overview"].(string)
			em.StillPath, _ = ep["still_path"].(string)
			em.AirDate, _ = ep["air_date"].(string)
			episodes[epNum] = em
		}
		c.meta.SetSeasonEpisodes(cleanName, num, episodes)
	}
}

func (c *Client) Sync(items []VODItem) {
	if c.syncing.Load() {
		return
	}

	var toSync []VODItem
	seen := make(map[string]bool)
	for _, item := range items {
		clean, _ := CleanVODName(item.Name)
		if item.MediaType == "movie" {
			if c.meta.GetMovie(clean) != nil {
				continue
			}
		} else {
			if c.meta.GetSeries(clean) != nil {
				continue
			}
		}
		if seen[clean+"_"+item.MediaType] {
			continue
		}
		seen[clean+"_"+item.MediaType] = true
		toSync = append(toSync, item)
	}

	var seriesNeedEpisodes []string
	c.meta.mu.RLock()
	for name, s := range c.meta.Series {
		if s.TMDBID > 0 && len(s.Seasons) == 0 {
			seriesNeedEpisodes = append(seriesNeedEpisodes, name)
		}
	}
	c.meta.mu.RUnlock()

	totalWork := len(toSync) + len(seriesNeedEpisodes)
	if totalWork == 0 {
		return
	}

	c.syncTotal.Store(int64(totalWork))
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

			clean, year := CleanVODName(item.Name)
			query := clean
			if year != "" {
				query = clean + " (" + year + ")"
			}

			result, err := c.Search(query, item.MediaType)
			if err != nil {
				c.log.Debug().Err(err).Str("query", query).Msg("search failed")
				c.syncDone.Add(1)
				time.Sleep(250 * time.Millisecond)
				continue
			}

			if item.MediaType == "movie" {
				c.resolveMovie(clean, result)
			} else {
				c.resolveSeries(clean, result)
			}

			c.syncDone.Add(1)
			time.Sleep(250 * time.Millisecond)
		}

		for _, name := range seriesNeedEpisodes {
			if c.apiKeyFn() == "" {
				break
			}
			c.fetchSeriesEpisodes(name)
			c.syncDone.Add(1)
		}

		c.log.Info().
			Int64("completed", c.syncDone.Load()).
			Int64("total", c.syncTotal.Load()).
			Msg("TMDB sync finished")
	}()
}

func (c *Client) fetchSeriesEpisodes(cleanName string) {
	s := c.meta.GetSeries(cleanName)
	if s == nil || s.TMDBID == 0 {
		return
	}

	tvID := fmt.Sprintf("%d", s.TMDBID)
	details, err := c.Details("tv", tvID)
	if err != nil {
		return
	}
	time.Sleep(250 * time.Millisecond)

	detailMap, ok := details.(map[string]any)
	if !ok {
		return
	}
	seasons, ok := detailMap["seasons"].([]any)
	if !ok {
		return
	}

	for _, raw := range seasons {
		sn, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		num := intVal(sn, "season_number")
		if num <= 0 {
			continue
		}

		seasonData, err := c.Season(tvID, fmt.Sprintf("%d", num))
		if err != nil {
			continue
		}
		time.Sleep(250 * time.Millisecond)

		sdMap, ok := seasonData.(map[string]any)
		if !ok {
			continue
		}
		rawEps, ok := sdMap["episodes"].([]any)
		if !ok {
			continue
		}

		episodes := make(map[int]*EpisodeMeta)
		for _, re := range rawEps {
			ep, ok := re.(map[string]any)
			if !ok {
				continue
			}
			epNum := intVal(ep, "episode_number")
			if epNum <= 0 {
				continue
			}
			em := &EpisodeMeta{}
			em.Name, _ = ep["name"].(string)
			em.Overview, _ = ep["overview"].(string)
			em.StillPath, _ = ep["still_path"].(string)
			em.AirDate, _ = ep["air_date"].(string)
			episodes[epNum] = em
		}
		c.meta.SetSeasonEpisodes(cleanName, num, episodes)
	}
}

func (c *Client) resolveMovieFromCache(cleanName string, searchResult map[string]any) {
	first := firstResult(searchResult)
	if first == nil {
		return
	}

	m := &MovieMeta{}
	m.TMDBID = intVal(first, "id")
	m.PosterPath, _ = first["poster_path"].(string)
	m.BackdropPath, _ = first["backdrop_path"].(string)
	m.Overview, _ = first["overview"].(string)
	m.Rating, _ = first["vote_average"].(float64)
	if date, _ := first["release_date"].(string); len(date) >= 4 {
		m.Year = date[:4]
	}
	m.Genres = extractGenres(first)

	if m.TMDBID > 0 {
		detailKey := DetailCacheKey("movie", fmt.Sprintf("%d", m.TMDBID))
		if details, ok := c.cache.Get(detailKey); ok {
			m.Certification = extractCertification(details, "GB", "US")
			if dm, ok := details.(map[string]any); ok {
				if btc, ok := dm["belongs_to_collection"].(map[string]any); ok {
					colName, _ := btc["name"].(string)
					if colName != "" {
						poster, _ := btc["poster_path"].(string)
						backdrop, _ := btc["backdrop_path"].(string)
						c.meta.SetCollection(colName, &CollectionMeta{
							TMDBID:       intVal(btc, "id"),
							PosterPath:   poster,
							BackdropPath: backdrop,
						})
					}
				}
			}
		}
	}

	c.meta.SetMovie(cleanName, m)
}

func (c *Client) resolveMovie(cleanName string, searchResult map[string]any) {
	first := firstResult(searchResult)
	if first == nil {
		return
	}

	m := &MovieMeta{}
	m.TMDBID = intVal(first, "id")
	m.PosterPath, _ = first["poster_path"].(string)
	m.BackdropPath, _ = first["backdrop_path"].(string)
	m.Overview, _ = first["overview"].(string)
	m.Rating, _ = first["vote_average"].(float64)
	if date, _ := first["release_date"].(string); len(date) >= 4 {
		m.Year = date[:4]
	}
	m.Genres = extractGenres(first)

	if m.TMDBID > 0 {
		if details, err := c.Details("movie", fmt.Sprintf("%d", m.TMDBID)); err == nil {
			m.Certification = extractCertification(details, "GB", "US")
			if dm, ok := details.(map[string]any); ok {
				if btc, ok := dm["belongs_to_collection"].(map[string]any); ok {
					colName, _ := btc["name"].(string)
					if colName != "" {
						poster, _ := btc["poster_path"].(string)
						backdrop, _ := btc["backdrop_path"].(string)
						c.meta.SetCollection(colName, &CollectionMeta{
							TMDBID:       intVal(btc, "id"),
							PosterPath:   poster,
							BackdropPath: backdrop,
						})
					}
				}
			}
		}
	}

	c.meta.SetMovie(cleanName, m)
}

func (c *Client) resolveSeries(cleanName string, searchResult map[string]any) {
	first := firstResult(searchResult)
	if first == nil {
		return
	}

	s := &SeriesMeta{
		Seasons: make(map[int]*SeasonMeta),
	}
	s.TMDBID = intVal(first, "id")
	s.PosterPath, _ = first["poster_path"].(string)
	s.BackdropPath, _ = first["backdrop_path"].(string)
	s.Overview, _ = first["overview"].(string)
	s.Rating, _ = first["vote_average"].(float64)
	if date, _ := first["first_air_date"].(string); len(date) >= 4 {
		s.Year = date[:4]
	}
	s.Genres = extractGenres(first)

	if s.TMDBID == 0 {
		c.meta.SetSeries(cleanName, s)
		return
	}

	tvID := fmt.Sprintf("%d", s.TMDBID)
	details, err := c.Details("tv", tvID)
	if err != nil {
		c.meta.SetSeries(cleanName, s)
		return
	}
	time.Sleep(250 * time.Millisecond)

	s.Certification = extractCertification(details, "GB", "US")
	c.meta.SetSeries(cleanName, s)

	detailMap, ok := details.(map[string]any)
	if !ok {
		return
	}
	seasons, ok := detailMap["seasons"].([]any)
	if !ok {
		return
	}

	for _, raw := range seasons {
		sn, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		num := intVal(sn, "season_number")
		if num <= 0 {
			continue
		}

		seasonData, err := c.Season(tvID, fmt.Sprintf("%d", num))
		if err != nil {
			continue
		}
		time.Sleep(250 * time.Millisecond)

		sdMap, ok := seasonData.(map[string]any)
		if !ok {
			continue
		}
		rawEps, ok := sdMap["episodes"].([]any)
		if !ok {
			continue
		}

		episodes := make(map[int]*EpisodeMeta)
		for _, re := range rawEps {
			ep, ok := re.(map[string]any)
			if !ok {
				continue
			}
			epNum := intVal(ep, "episode_number")
			if epNum <= 0 {
				continue
			}
			em := &EpisodeMeta{}
			em.Name, _ = ep["name"].(string)
			em.Overview, _ = ep["overview"].(string)
			em.StillPath, _ = ep["still_path"].(string)
			em.AirDate, _ = ep["air_date"].(string)
			episodes[epNum] = em
		}
		c.meta.SetSeasonEpisodes(cleanName, num, episodes)
	}
}

func firstResult(searchResult map[string]any) map[string]any {
	results, ok := searchResult["results"].([]any)
	if !ok || len(results) == 0 {
		return nil
	}
	first, ok := results[0].(map[string]any)
	if !ok {
		return nil
	}
	return first
}

func intVal(m map[string]any, key string) int {
	v, _ := m[key].(float64)
	return int(v)
}

func extractGenres(m map[string]any) []string {
	ids, ok := m["genre_ids"].([]any)
	if !ok {
		return nil
	}
	var genres []string
	for _, id := range ids {
		if gid, ok := id.(float64); ok {
			if name, exists := genreMap[int(gid)]; exists {
				genres = append(genres, name)
			}
		}
	}
	return genres
}

var genreMap = map[int]string{
	28: "Action", 12: "Adventure", 16: "Animation", 35: "Comedy", 80: "Crime",
	99: "Documentary", 18: "Drama", 10751: "Family", 14: "Fantasy", 36: "History",
	27: "Horror", 10402: "Music", 9648: "Mystery", 10749: "Romance", 878: "Sci-Fi",
	53: "Thriller", 10752: "War", 37: "Western",
	10759: "Action & Adventure", 10762: "Kids", 10763: "News", 10764: "Reality",
	10765: "Sci-Fi & Fantasy", 10766: "Soap", 10767: "Talk", 10768: "War & Politics",
}

func extractCertification(details any, countries ...string) string {
	d, ok := details.(map[string]any)
	if !ok {
		return ""
	}

	if rd, ok := d["release_dates"].(map[string]any); ok {
		if results, ok := rd["results"].([]any); ok {
			for _, country := range countries {
				for _, r := range results {
					entry, ok := r.(map[string]any)
					if !ok {
						continue
					}
					iso, _ := entry["iso_3166_1"].(string)
					if iso != country {
						continue
					}
					dates, ok := entry["release_dates"].([]any)
					if !ok {
						continue
					}
					for _, rd := range dates {
						rdm, ok := rd.(map[string]any)
						if !ok {
							continue
						}
						cert, _ := rdm["certification"].(string)
						if cert != "" {
							return cert
						}
					}
				}
			}
		}
	}

	if cr, ok := d["content_ratings"].(map[string]any); ok {
		if results, ok := cr["results"].([]any); ok {
			for _, country := range countries {
				for _, r := range results {
					entry, ok := r.(map[string]any)
					if !ok {
						continue
					}
					iso, _ := entry["iso_3166_1"].(string)
					if iso == country {
						cert, _ := entry["rating"].(string)
						if cert != "" {
							return cert
						}
					}
				}
			}
		}
	}

	return ""
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
