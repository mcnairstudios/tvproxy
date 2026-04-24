package tmdb

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
)

type SyncStatus struct {
	Syncing    bool                    `json:"syncing"`
	Total      int                     `json:"total"`
	Completed  int                     `json:"completed"`
	Categories map[string]*SyncCategory `json:"categories,omitempty"`
}

type SyncCategory struct {
	Total     int `json:"total"`
	Completed int `json:"completed"`
}

type VODItem struct {
	StreamID   string
	Name       string
	MediaType  string
	Collection string
	TMDBID     int
	IsLocal    bool
}

type ResolvedFunc func(streamID string, tmdbID int)

func syncCategoryKey(item VODItem) string {
	source := "iptv"
	if item.IsLocal {
		source = "local"
	}
	media := "movie"
	if item.MediaType != "movie" {
		media = "series"
	}
	return source + "_" + media
}

type syncCategoryCounts struct {
	total atomic.Int64
	done  atomic.Int64
}

type TMDBCache interface {
	Get(key string) (any, bool)
	Set(key string, value any)
	Delete(key string)
	Has(key string) bool
	Prune(keepKeys map[string]bool) int
	MigrateFrom(dir string)
}

type MetaStore interface {
	GetMovie(tmdbID int) *MovieMeta
	SetMovie(tmdbID int, m *MovieMeta)
	GetSeries(tmdbID int) *SeriesMeta
	SetSeries(tmdbID int, s *SeriesMeta)
	GetEpisode(tmdbID int, season, episode int) *EpisodeMeta
	SetSeasonEpisodes(tmdbID int, seasonNum int, episodes map[int]*EpisodeMeta)
	GetCollection(tmdbID int) *CollectionMeta
	SetCollection(tmdbID int, c *CollectionMeta)
	SeriesNeedingEpisodes() []int
	Save()
}

type Client struct {
	http     *http.Client
	log      zerolog.Logger
	cache    TMDBCache
	meta     MetaStore
	images   *ImageCache
	apiKeyFn func() string

	syncTotal    atomic.Int64
	syncDone     atomic.Int64
	syncing      atomic.Bool
	syncCategories sync.Map
}

func NewClient(baseDir string, apiKeyFn func() string, log zerolog.Logger) *Client {
	httpClient := &http.Client{Timeout: 10 * time.Second}

	var cache TMDBCache
	boltCache, err := NewBoltCache(baseDir)
	if err != nil {
		log.Error().Err(err).Msg("failed to open bolt tmdb cache, falling back to JSON")
		c := NewCache(filepath.Join(baseDir, "search"))
		c.MigrateFrom(baseDir)
		cache = c
	} else {
		cache = boltCache
	}

	var meta MetaStore
	boltMeta, err := NewBoltMetadataStore(baseDir)
	if err != nil {
		log.Error().Err(err).Msg("failed to open bolt metadata store, falling back to JSON")
		meta = NewMetadataStore(baseDir)
	} else {
		meta = boltMeta
	}

	return &Client{
		http:     httpClient,
		log:      log.With().Str("component", "tmdb").Logger(),
		cache:    cache,
		meta:     meta,
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

func (c *Client) resolveID(name, mediaType string) int {
	clean, year := CleanVODName(name)
	query := clean
	if year != "" {
		query = clean + " (" + year + ")"
	}
	searchType := mediaType
	if mediaType == "series" {
		searchType = "tv"
	}
	cacheKey := SearchCacheKey(query, searchType)
	if cached, ok := c.cache.Get(cacheKey); ok {
		if result, ok := cached.(map[string]any); ok {
			if first := firstResult(result); first != nil {
				return intVal(first, "id")
			}
		}
	}
	return 0
}

func (c *Client) LookupMovie(name string) *MovieMeta {
	tid := c.resolveID(name, "movie")
	if tid == 0 {
		return nil
	}
	return c.meta.GetMovie(tid)
}

func (c *Client) LookupSeries(name string) *SeriesMeta {
	tid := c.resolveID(name, "series")
	if tid == 0 {
		return nil
	}
	return c.meta.GetSeries(tid)
}

func (c *Client) LookupEpisode(name string, season, episode int) *EpisodeMeta {
	tid := c.resolveID(name, "series")
	if tid == 0 {
		return nil
	}
	return c.meta.GetEpisode(tid, season, episode)
}

func (c *Client) LookupCollectionByID(tmdbID int) *CollectionMeta {
	return c.meta.GetCollection(tmdbID)
}

func (c *Client) LookupPoster(name, mediaType string) string {
	if mediaType == "movie" {
		if m := c.LookupMovie(name); m != nil && m.PosterPath != "" {
			return PosterURL(m.PosterPath)
		}
	} else {
		if s := c.LookupSeries(name); s != nil && s.PosterPath != "" {
			return PosterURL(s.PosterPath)
		}
	}
	return ""
}

func (c *Client) LookupBackdrop(name, mediaType string) string {
	if mediaType == "movie" {
		if m := c.LookupMovie(name); m != nil && m.BackdropPath != "" {
			return m.BackdropPath
		}
	} else {
		if s := c.LookupSeries(name); s != nil && s.BackdropPath != "" {
			return s.BackdropPath
		}
	}
	return ""
}

func (c *Client) ServeImage(w http.ResponseWriter, r *http.Request) {
	c.images.Serve(w, r, r.URL.Query().Get("path"), r.URL.Query().Get("size"))
}

func (c *Client) Status() SyncStatus {
	s := SyncStatus{
		Syncing:    c.syncing.Load(),
		Total:      int(c.syncTotal.Load()),
		Completed:  int(c.syncDone.Load()),
		Categories: map[string]*SyncCategory{},
	}
	c.syncCategories.Range(func(k, v any) bool {
		counts := v.(*syncCategoryCounts)
		s.Categories[k.(string)] = &SyncCategory{
			Total:     int(counts.total.Load()),
			Completed: int(counts.done.Load()),
		}
		return true
	})
	return s
}

func (c *Client) UpdateSearchCacheForName(name, mediaType string, tmdbID int) {
	clean, year := CleanVODName(name)
	query := clean
	if year != "" {
		query = clean + " (" + year + ")"
	}
	searchType := mediaType
	if mediaType == "series" {
		searchType = "tv"
	}
	cacheKey := SearchCacheKey(query, searchType)
	c.cache.Set(cacheKey, map[string]any{
		"results": []any{
			map[string]any{"id": float64(tmdbID)},
		},
	})
}

func (c *Client) GetMovieByID(tmdbID int) *MovieMeta {
	return c.meta.GetMovie(tmdbID)
}

func (c *Client) GetSeriesByID(tmdbID int) *SeriesMeta {
	return c.meta.GetSeries(tmdbID)
}

func (c *Client) GetEpisodeByID(tmdbID int, season, episode int) *EpisodeMeta {
	return c.meta.GetEpisode(tmdbID, season, episode)
}

func (c *Client) GetCollectionByID(tmdbID int) *CollectionMeta {
	return c.meta.GetCollection(tmdbID)
}

func (c *Client) Rematch(tmdbID int, mediaType string) error {
	if tmdbID == 0 {
		return fmt.Errorf("tmdb_id required")
	}

	idStr := fmt.Sprintf("%d", tmdbID)
	apiMediaType := mediaType
	if mediaType == "series" {
		apiMediaType = "tv"
	}

	details, err := c.Details(apiMediaType, idStr)
	if err != nil {
		return fmt.Errorf("fetching details: %w", err)
	}

	dm, ok := details.(map[string]any)
	if !ok {
		return fmt.Errorf("invalid details response")
	}

	if mediaType == "movie" {
		m := &MovieMeta{TMDBID: tmdbID}
		m.PosterPath, _ = dm["poster_path"].(string)
		m.BackdropPath, _ = dm["backdrop_path"].(string)
		m.Overview, _ = dm["overview"].(string)
		m.Rating, _ = dm["vote_average"].(float64)
		if date, _ := dm["release_date"].(string); len(date) >= 4 {
			m.Year = date[:4]
		}
		m.Genres = extractDetailGenres(dm)
		m.Certification = extractCertification(details, "GB", "US")

		if btc, ok := dm["belongs_to_collection"].(map[string]any); ok {
			colID := intVal(btc, "id")
			if colID > 0 {
				m.CollectionID = colID
				poster, _ := btc["poster_path"].(string)
				backdrop, _ := btc["backdrop_path"].(string)
				c.meta.SetCollection(colID, &CollectionMeta{
					TMDBID:       colID,
					PosterPath:   poster,
					BackdropPath: backdrop,
				})
			}
		}

		c.meta.SetMovie(tmdbID, m)
	} else {
		s := &SeriesMeta{
			TMDBID:  tmdbID,
			Seasons: make(map[int]*SeasonMeta),
		}
		s.PosterPath, _ = dm["poster_path"].(string)
		s.BackdropPath, _ = dm["backdrop_path"].(string)
		s.Overview, _ = dm["overview"].(string)
		s.Rating, _ = dm["vote_average"].(float64)
		if date, _ := dm["first_air_date"].(string); len(date) >= 4 {
			s.Year = date[:4]
		}
		s.Genres = extractDetailGenres(dm)
		s.Certification = extractCertification(details, "GB", "US")
		c.meta.SetSeries(tmdbID, s)

		c.fetchSeriesEpisodes(tmdbID)
	}

	c.meta.Save()
	return nil
}

func (c *Client) PopulateMetadataFromCache(items []VODItem) {
	populated := 0
	for _, item := range items {
		if item.TMDBID > 0 {
			if item.MediaType == "movie" && c.meta.GetMovie(item.TMDBID) != nil {
				continue
			}
			if item.MediaType != "movie" && c.meta.GetSeries(item.TMDBID) != nil {
				continue
			}
		}

		clean, year := CleanVODName(item.Name)
		query := clean
		if year != "" {
			query = clean + " (" + year + ")"
		}

		if item.MediaType == "movie" {
			cacheKey := SearchCacheKey(query, "movie")
			if cached, ok := c.cache.Get(cacheKey); ok {
				if result, ok := cached.(map[string]any); ok {
					c.resolveMovieFromCache(result)
					populated++
				}
			}
		} else {
			cacheKey := SearchCacheKey(query, "tv")
			if cached, ok := c.cache.Get(cacheKey); ok {
				if result, ok := cached.(map[string]any); ok {
					c.resolveSeriesFromCache(result)
					populated++
				}
			}
		}
	}
	if populated > 0 {
		c.meta.Save()
		c.log.Info().Int("populated", populated).Msg("populated metadata from cache")
	}
}

func (c *Client) resolveSeriesFromCache(searchResult map[string]any) int {
	first := firstResult(searchResult)
	if first == nil {
		return 0
	}

	tmdbID := intVal(first, "id")
	if tmdbID == 0 {
		return 0
	}

	if c.meta.GetSeries(tmdbID) != nil {
		return tmdbID
	}

	s := &SeriesMeta{
		Seasons: make(map[int]*SeasonMeta),
	}
	s.TMDBID = tmdbID
	s.PosterPath, _ = first["poster_path"].(string)
	s.BackdropPath, _ = first["backdrop_path"].(string)
	s.Overview, _ = first["overview"].(string)
	s.Rating, _ = first["vote_average"].(float64)
	if date, _ := first["first_air_date"].(string); len(date) >= 4 {
		s.Year = date[:4]
	}
	s.Genres = extractGenres(first)

	c.meta.SetSeries(tmdbID, s)

	tvID := fmt.Sprintf("%d", tmdbID)
	detailKey := DetailCacheKey("tv", tvID)
	detailCached, ok := c.cache.Get(detailKey)
	if !ok {
		return tmdbID
	}
	s.Certification = extractCertification(detailCached, "GB", "US")
	if s.Certification != "" {
		c.meta.SetSeries(tmdbID, s)
	}
	detailMap, ok := detailCached.(map[string]any)
	if !ok {
		return tmdbID
	}
	seasons, ok := detailMap["seasons"].([]any)
	if !ok {
		return tmdbID
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
		c.meta.SetSeasonEpisodes(tmdbID, num, episodes)
	}

	return tmdbID
}

func (c *Client) Sync(items []VODItem, onResolved ResolvedFunc) {
	if c.syncing.Load() {
		return
	}

	var toSync []VODItem
	seen := make(map[string]bool)
	for _, item := range items {
		if item.TMDBID > 0 {
			if item.MediaType == "movie" && c.meta.GetMovie(item.TMDBID) != nil {
				continue
			}
			if item.MediaType != "movie" && c.meta.GetSeries(item.TMDBID) != nil {
				continue
			}
		}

		key := item.Name + "_" + item.MediaType
		if seen[key] {
			continue
		}
		seen[key] = true

		clean, year := CleanVODName(item.Name)
		query := clean
		if year != "" {
			query = clean + " (" + year + ")"
		}
		mediaType := item.MediaType
		if mediaType != "movie" {
			mediaType = "tv"
		}
		if _, cached := c.cache.Get(SearchCacheKey(query, mediaType)); cached {
			continue
		}

		toSync = append(toSync, item)
	}

	sort.SliceStable(toSync, func(i, j int) bool {
		return toSync[i].IsLocal && !toSync[j].IsLocal
	})

	seriesNeedEpisodes := c.meta.SeriesNeedingEpisodes()

	totalWork := len(toSync) + len(seriesNeedEpisodes)
	if totalWork == 0 {
		return
	}

	c.syncTotal.Store(int64(totalWork))
	c.syncDone.Store(0)
	c.syncCategories = sync.Map{}
	for _, item := range toSync {
		key := syncCategoryKey(item)
		v, _ := c.syncCategories.LoadOrStore(key, &syncCategoryCounts{})
		v.(*syncCategoryCounts).total.Add(1)
	}
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
				if v, ok := c.syncCategories.Load(syncCategoryKey(item)); ok {
					v.(*syncCategoryCounts).done.Add(1)
				}
				c.syncDone.Add(1)
				time.Sleep(250 * time.Millisecond)
				continue
			}

			var resolvedID int
			if item.MediaType == "movie" {
				resolvedID = c.resolveMovie(result)
			} else {
				resolvedID = c.resolveSeries(result)
			}

			if resolvedID > 0 && onResolved != nil && item.StreamID != "" {
				onResolved(item.StreamID, resolvedID)
			}

			if v, ok := c.syncCategories.Load(syncCategoryKey(item)); ok {
				v.(*syncCategoryCounts).done.Add(1)
			}
			done := c.syncDone.Add(1)
			if done%50 == 0 {
				c.meta.Save()
			}
			time.Sleep(250 * time.Millisecond)
		}

		for _, tmdbID := range seriesNeedEpisodes {
			if c.apiKeyFn() == "" {
				break
			}
			c.fetchSeriesEpisodes(tmdbID)
			c.syncDone.Add(1)
		}

		c.meta.Save()
		c.log.Info().
			Int64("completed", c.syncDone.Load()).
			Int64("total", c.syncTotal.Load()).
			Msg("TMDB sync finished")
	}()
}

func (c *Client) fetchSeriesEpisodes(tmdbID int) {
	s := c.meta.GetSeries(tmdbID)
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
		c.meta.SetSeasonEpisodes(tmdbID, num, episodes)
	}
}

func (c *Client) resolveMovieFromCache(searchResult map[string]any) int {
	first := firstResult(searchResult)
	if first == nil {
		return 0
	}

	tmdbID := intVal(first, "id")
	if tmdbID == 0 {
		return 0
	}

	if c.meta.GetMovie(tmdbID) != nil {
		return tmdbID
	}

	m := &MovieMeta{TMDBID: tmdbID}
	m.PosterPath, _ = first["poster_path"].(string)
	m.BackdropPath, _ = first["backdrop_path"].(string)
	m.Overview, _ = first["overview"].(string)
	m.Rating, _ = first["vote_average"].(float64)
	if date, _ := first["release_date"].(string); len(date) >= 4 {
		m.Year = date[:4]
	}
	m.Genres = extractGenres(first)

	detailKey := DetailCacheKey("movie", fmt.Sprintf("%d", tmdbID))
	if details, ok := c.cache.Get(detailKey); ok {
		m.Certification = extractCertification(details, "GB", "US")
		if dm, ok := details.(map[string]any); ok {
			if btc, ok := dm["belongs_to_collection"].(map[string]any); ok {
				colID := intVal(btc, "id")
				if colID > 0 {
					m.CollectionID = colID
					poster, _ := btc["poster_path"].(string)
					backdrop, _ := btc["backdrop_path"].(string)
					c.meta.SetCollection(colID, &CollectionMeta{
						TMDBID:       colID,
						PosterPath:   poster,
						BackdropPath: backdrop,
					})
				}
			}
		}
	}

	c.meta.SetMovie(tmdbID, m)
	return tmdbID
}

func (c *Client) ResolveMovieFromSearch(searchResult map[string]any) int {
	return c.resolveMovie(searchResult)
}

func (c *Client) ResolveSeriesFromSearch(searchResult map[string]any) int {
	return c.resolveSeries(searchResult)
}

func (c *Client) resolveMovie(searchResult map[string]any) int {
	first := firstResult(searchResult)
	if first == nil {
		return 0
	}

	tmdbID := intVal(first, "id")
	if tmdbID == 0 {
		return 0
	}

	m := &MovieMeta{TMDBID: tmdbID}
	m.PosterPath, _ = first["poster_path"].(string)
	m.BackdropPath, _ = first["backdrop_path"].(string)
	m.Overview, _ = first["overview"].(string)
	m.Rating, _ = first["vote_average"].(float64)
	if date, _ := first["release_date"].(string); len(date) >= 4 {
		m.Year = date[:4]
	}
	m.Genres = extractGenres(first)

	if details, err := c.Details("movie", fmt.Sprintf("%d", tmdbID)); err == nil {
		m.Certification = extractCertification(details, "GB", "US")
		if dm, ok := details.(map[string]any); ok {
			if btc, ok := dm["belongs_to_collection"].(map[string]any); ok {
				colID := intVal(btc, "id")
				if colID > 0 {
					m.CollectionID = colID
					poster, _ := btc["poster_path"].(string)
					backdrop, _ := btc["backdrop_path"].(string)
					c.meta.SetCollection(colID, &CollectionMeta{
						TMDBID:       colID,
						PosterPath:   poster,
						BackdropPath: backdrop,
					})
				}
			}
		}
	}

	c.meta.SetMovie(tmdbID, m)
	return tmdbID
}

func (c *Client) resolveSeries(searchResult map[string]any) int {
	first := firstResult(searchResult)
	if first == nil {
		return 0
	}

	tmdbID := intVal(first, "id")
	if tmdbID == 0 {
		return 0
	}

	s := &SeriesMeta{
		TMDBID:  tmdbID,
		Seasons: make(map[int]*SeasonMeta),
	}
	s.PosterPath, _ = first["poster_path"].(string)
	s.BackdropPath, _ = first["backdrop_path"].(string)
	s.Overview, _ = first["overview"].(string)
	s.Rating, _ = first["vote_average"].(float64)
	if date, _ := first["first_air_date"].(string); len(date) >= 4 {
		s.Year = date[:4]
	}
	s.Genres = extractGenres(first)

	tvID := fmt.Sprintf("%d", tmdbID)
	details, err := c.Details("tv", tvID)
	if err != nil {
		c.meta.SetSeries(tmdbID, s)
		return tmdbID
	}
	time.Sleep(250 * time.Millisecond)

	s.Certification = extractCertification(details, "GB", "US")
	c.meta.SetSeries(tmdbID, s)

	detailMap, ok := details.(map[string]any)
	if !ok {
		return tmdbID
	}
	seasons, ok := detailMap["seasons"].([]any)
	if !ok {
		return tmdbID
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
		c.meta.SetSeasonEpisodes(tmdbID, num, episodes)
	}

	return tmdbID
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

func extractDetailGenres(m map[string]any) []string {
	raw, ok := m["genres"].([]any)
	if !ok {
		return nil
	}
	var genres []string
	for _, g := range raw {
		gm, ok := g.(map[string]any)
		if !ok {
			continue
		}
		if name, ok := gm["name"].(string); ok && name != "" {
			genres = append(genres, name)
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
