package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/service"
	"github.com/gavinmcnair/tvproxy/pkg/store"
	"github.com/gavinmcnair/tvproxy/pkg/tmdb"
	"github.com/gavinmcnair/tvproxy/pkg/xtream"
)

type StreamHandler struct {
	streamStore store.StreamReader
	browser     store.StreamBrowser
	versioned   store.Versioned
	logoService *service.LogoService
	tmdb        *tmdb.Client
	xtreamCache *xtream.Cache
}

func NewStreamHandler(streamStore store.StreamReader, versioned store.Versioned, logoService *service.LogoService, tmdbClient *tmdb.Client, xtreamCache *xtream.Cache) *StreamHandler {
	h := &StreamHandler{streamStore: streamStore, versioned: versioned, logoService: logoService, tmdb: tmdbClient, xtreamCache: xtreamCache}
	if b, ok := streamStore.(store.StreamBrowser); ok {
		h.browser = b
	}
	return h
}

func (h *StreamHandler) List(w http.ResponseWriter, r *http.Request) {
	accountIDStr := r.URL.Query().Get("account_id")
	if accountIDStr != "" {
		streams, err := h.streamStore.ListByAccountID(r.Context(), accountIDStr)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to list streams")
			return
		}
		for i := range streams {
			streams[i].Logo = h.logoService.Resolve(streams[i].Logo)
		}
		respondJSON(w, http.StatusOK, streams)
		return
	}

	etag := h.versioned.ETag()
	if r.Header.Get("If-None-Match") == etag {
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
		return
	}

	if r.URL.Query().Get("full") == "true" {
		streams, err := h.streamStore.List(r.Context())
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to list streams")
			return
		}
		for i := range streams {
			streams[i].Logo = h.logoService.Resolve(streams[i].Logo)
		}
		respondCacheable(w, r, etag, http.StatusOK, streams)
		return
	}

	summaries, err := h.streamStore.ListSummaries(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list streams")
		return
	}
	for i := range summaries {
		summaries[i].Logo = h.logoService.Resolve(summaries[i].Logo)
	}
	respondCacheable(w, r, etag, http.StatusOK, summaries)
}

func (h *StreamHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	stream, err := h.streamStore.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "stream not found")
		return
	}

	stream.Logo = h.logoService.Resolve(stream.Logo)
	respondJSON(w, http.StatusOK, stream)
}

func (h *StreamHandler) Delete(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (h *StreamHandler) Tree(w http.ResponseWriter, r *http.Request) {
	if h.browser == nil {
		respondError(w, http.StatusNotImplemented, "tree browsing not available")
		return
	}
	sourceType := r.URL.Query().Get("source_type")
	sourceID := r.URL.Query().Get("source_id")
	if sourceID == "" {
		respondError(w, http.StatusBadRequest, "source_id required")
		return
	}
	sk := sourceType + ":" + sourceID
	groups, err := h.browser.ListGroups(sk)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=30")
	respondJSON(w, http.StatusOK, map[string]any{
		"groups": groups,
		"total":  h.browser.StreamCount(),
	})
}

func (h *StreamHandler) Group(w http.ResponseWriter, r *http.Request) {
	if h.browser == nil {
		respondError(w, http.StatusNotImplemented, "group browsing not available")
		return
	}
	sourceType := r.URL.Query().Get("source_type")
	sourceID := r.URL.Query().Get("source_id")
	group := r.URL.Query().Get("group")
	if sourceID == "" {
		respondError(w, http.StatusBadRequest, "source_id required")
		return
	}
	var offset, limit int
	fmt.Sscanf(r.URL.Query().Get("offset"), "%d", &offset)
	fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &limit)
	if limit <= 0 {
		limit = 200
	}
	sk := sourceType + ":" + sourceID
	summaries, total, err := h.browser.ListByGroup(sk, group, offset, limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=30")
	respondJSON(w, http.StatusOK, map[string]any{
		"streams": summaries,
		"total":   total,
	})
}

func (h *StreamHandler) Stats(w http.ResponseWriter, r *http.Request) {
	if h.browser == nil {
		respondError(w, http.StatusNotImplemented, "stats not available")
		return
	}
	type statsProvider interface {
		Stats() store.StreamStats
	}
	if sp, ok := h.streamStore.(statsProvider); ok {
		w.Header().Set("Cache-Control", "public, max-age=10")
		respondJSON(w, http.StatusOK, sp.Stats())
	} else {
		respondError(w, http.StatusNotImplemented, "stats not available")
	}
}

func (h *StreamHandler) Search(w http.ResponseWriter, r *http.Request) {
	if h.browser == nil {
		respondError(w, http.StatusNotImplemented, "search not available")
		return
	}
	q := r.URL.Query().Get("q")
	if q == "" {
		respondJSON(w, http.StatusOK, []any{})
		return
	}
	var limit int
	fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &limit)
	if limit <= 0 {
		limit = 50
	}
	summaries, err := h.browser.SearchByName(q, limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, summaries)
}

func (h *StreamHandler) VODLibrary(w http.ResponseWriter, r *http.Request) {
	vodType := r.URL.Query().Get("type")
	series := r.URL.Query().Get("series")

	source := r.URL.Query().Get("source")
	lang := r.URL.Query().Get("lang")

	var streams []models.Stream
	var err error
	if vodType != "" {
		streams, err = h.streamStore.ListByVODType(r.Context(), vodType)
	} else {
		streams, err = h.streamStore.List(r.Context())
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list streams")
		return
	}

	if source != "" {
		var sourceFiltered []models.Stream
		for _, s := range streams {
			if source == "xtream" && s.CacheType == "local" {
				continue
			}
			if source == "local" && s.CacheType != "local" {
				continue
			}
			sourceFiltered = append(sourceFiltered, s)
		}
		streams = sourceFiltered
	}

	langCounts := make(map[string]int)
	for _, s := range streams {
		if s.Language == "" {
			continue
		}
		if s.VODType == "" {
			continue
		}
		if vodType != "" && s.VODType != vodType {
			continue
		}
		if s.VODType == "series" && s.VODSeason == 0 && s.VODEpisode == 0 && s.CacheType == "local" {
			continue
		}
		langCounts[s.Language]++
	}

	if lang != "" {
		var filtered []models.Stream
		for _, s := range streams {
			if s.Language != lang {
				continue
			}
			filtered = append(filtered, s)
		}
		streams = filtered
	}

	type vodAlt struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		URL   string `json:"url"`
		Group string `json:"group,omitempty"`
	}

	type vodItem struct {
		ID                 string   `json:"id"`
		Name               string   `json:"name"`
		URL                string   `json:"url"`
		Logo               string   `json:"logo,omitempty"`
		PosterURL          string   `json:"poster_url,omitempty"`
		TMDBID             int      `json:"tmdb_id,omitempty"`
		CacheType          string   `json:"cache_type,omitempty"`
		CacheKey           int      `json:"cache_key,omitempty"`
		Language           string   `json:"language,omitempty"`
		Type               string   `json:"type"`
		Series             string   `json:"series,omitempty"`
		Collection         string   `json:"collection,omitempty"`
		CollectionPoster   string   `json:"collection_poster,omitempty"`
		CollectionBackdrop string   `json:"collection_backdrop,omitempty"`
		Season             int      `json:"season,omitempty"`
		SeasonName         string   `json:"vod_season_name,omitempty"`
		Episode            int      `json:"episode,omitempty"`
		EpisodeName        string   `json:"episode_name,omitempty"`
		EpisodeOverview    string   `json:"episode_overview,omitempty"`
		EpisodeStill       string   `json:"episode_still,omitempty"`
		Overview           string   `json:"overview,omitempty"`
		Rating             float64  `json:"rating,omitempty"`
		Year               string   `json:"year,omitempty"`
		Genres             []string `json:"genres,omitempty"`
		Certification      string   `json:"certification,omitempty"`
		VCodec             string   `json:"vcodec,omitempty"`
		ACodec             string   `json:"acodec,omitempty"`
		Res                string   `json:"resolution,omitempty"`
		Audio              string   `json:"audio,omitempty"`
		Duration           float64  `json:"duration,omitempty"`
		Alternates         []vodAlt `json:"alternates,omitempty"`
		Group              string   `json:"group,omitempty"`
	}

	var items []vodItem
	var uncached []tmdb.VODItem
	seen := make(map[string]bool)

	type tmdbCached struct {
		posterURL string
		movie     *tmdb.MovieMeta
		series    *tmdb.SeriesMeta
	}
	tmdbLookupCache := make(map[string]*tmdbCached)

	for _, s := range streams {
		if s.VODType == "" {
			continue
		}
		if vodType != "" && s.VODType != vodType {
			continue
		}
		if s.VODType == "series" && s.VODSeason == 0 && s.VODEpisode == 0 && s.CacheType == "local" {
			continue
		}
		if series != "" && s.VODSeries != series {
			continue
		}

		lookupName := s.Name
		mediaType := "movie"
		if s.VODType == "series" {
			if s.VODSeries != "" {
				lookupName = s.VODSeries
			}
			mediaType = "tv"
		}

		cacheKey := lookupName + "_" + mediaType
		cached, hasCached := tmdbLookupCache[cacheKey]
		if !hasCached && h.tmdb != nil {
			cached = &tmdbCached{}
			tmdbID := s.TMDBID
			if mediaType == "movie" {
				var m *tmdb.MovieMeta
				if tmdbID > 0 {
					m = h.tmdb.GetMovieByID(tmdbID)
				}
				if m == nil {
					m = h.tmdb.LookupMovie(lookupName)
				}
				cached.movie = m
				if m != nil && m.PosterPath != "" {
					cached.posterURL = tmdb.PosterURL(m.PosterPath)
				}
			} else {
				var sr *tmdb.SeriesMeta
				if tmdbID > 0 {
					sr = h.tmdb.GetSeriesByID(tmdbID)
				}
				if sr == nil {
					sr = h.tmdb.LookupSeries(lookupName)
				}
				cached.series = sr
				if sr != nil && sr.PosterPath != "" {
					cached.posterURL = tmdb.PosterURL(sr.PosterPath)
				}
			}
			tmdbLookupCache[cacheKey] = cached
		}

		posterURL := ""
		if cached != nil {
			posterURL = cached.posterURL
		}
		if h.tmdb != nil && posterURL == "" && !seen[cacheKey] {
			seen[lookupName+"_"+mediaType] = true
			uncached = append(uncached, tmdb.VODItem{StreamID: s.ID, Name: lookupName, MediaType: mediaType, TMDBID: s.TMDBID, IsLocal: s.CacheType == "local"})
		}

		item := vodItem{
			ID:         s.ID,
			Name:       s.Name,
			URL:        s.URL,
			Logo:       h.logoService.Resolve(s.Logo),
			PosterURL:  posterURL,
			TMDBID:     s.TMDBID,
			CacheType:  s.CacheType,
			CacheKey:   s.CacheKey,
			Language:   s.Language,
			Type:       s.VODType,
			Series:     s.VODSeries,
			Collection: s.VODCollection,
			Season:     s.VODSeason,
			SeasonName: s.VODSeasonName,
			Episode:    s.VODEpisode,
			Group:      s.Group,
			VCodec:     s.VODVCodec,
			ACodec:     s.VODACodec,
			Res:        s.VODRes,
			Audio:      s.VODAudio,
			Duration:   s.VODDuration,
		}

			xtreamEnriched := false
		if s.CacheType == "xtream" && h.xtreamCache != nil {
			if s.VODType == "movie" {
				if m := h.xtreamCache.GetMovie(s.CacheKey); m != nil {
					if item.PosterURL == "" {
						item.PosterURL = h.logoService.Resolve(m.PosterURL)
					}
					if m.Plot != "" {
						item.Overview = m.Plot
						xtreamEnriched = true
					}
					item.Rating = 0
					if m.Rating != "" && m.Rating != "0" {
						var r float64
						fmt.Sscanf(m.Rating, "%f", &r)
						item.Rating = r
					}
					if m.Genre != "" {
						item.Genres = splitGenres(m.Genre)
					}
					if m.ReleaseDate != "" && len(m.ReleaseDate) >= 4 {
						item.Year = m.ReleaseDate[:4]
					}
					if m.BackdropURL != "" {
						item.CollectionBackdrop = h.logoService.Resolve(m.BackdropURL)
					}
				}
			} else if s.VODType == "series" {
				sr := h.xtreamCache.GetSeries(s.CacheKey)
				if sr == nil && s.VODSeries != "" {
					sr = h.xtreamCache.FindSeriesByName(s.VODSeries)
				}
				if sr != nil {
					if item.PosterURL == "" {
						item.PosterURL = h.logoService.Resolve(sr.PosterURL)
					}
					item.Overview = sr.Plot
					if sr.Genre != "" {
						item.Genres = splitGenres(sr.Genre)
					}
					if sr.ReleaseDate != "" && len(sr.ReleaseDate) >= 4 {
						item.Year = sr.ReleaseDate[:4]
					}
				}
				if ep := h.xtreamCache.GetEpisode(s.CacheKey); ep != nil {
					item.EpisodeName = ep.Title
					if ep.Duration > 0 {
						item.Duration = float64(ep.Duration)
					}
				}
			}
		}
		if (!xtreamEnriched || s.CacheType != "xtream") && cached != nil {
			if s.VODType == "movie" {
				if m := cached.movie; m != nil {
					item.Overview = m.Overview
					item.Rating = m.Rating
					item.Year = m.Year
					item.Genres = m.Genres
					item.Certification = m.Certification
					if m.CollectionID > 0 {
						if col := h.tmdb.GetCollectionByID(m.CollectionID); col != nil {
							if col.PosterPath != "" {
								item.CollectionPoster = tmdb.PosterURL(col.PosterPath)
							}
							if col.BackdropPath != "" {
								item.CollectionBackdrop = tmdb.PosterURL(col.BackdropPath) + "&size=w1280"
							}
						}
					}
				}
			} else if s.VODType == "series" {
				if sr := cached.series; sr != nil {
					item.Overview = sr.Overview
					item.Rating = sr.Rating
					item.Year = sr.Year
					item.Genres = sr.Genres
					item.Certification = sr.Certification
				}
				if s.VODSeason > 0 && s.VODEpisode > 0 && h.tmdb != nil {
					ep := h.tmdb.GetEpisodeByID(s.TMDBID, s.VODSeason, s.VODEpisode)
					if ep == nil {
						ep = h.tmdb.LookupEpisode(lookupName, s.VODSeason, s.VODEpisode)
					}
					if ep != nil {
						item.EpisodeName = ep.Name
						item.EpisodeOverview = ep.Overview
						if ep.StillPath != "" {
							item.EpisodeStill = tmdb.PosterURL(ep.StillPath) + "&size=w300"
						}
					}
				}
			}
		}

		items = append(items, item)
	}

	if h.tmdb != nil && len(uncached) > 0 {
		h.tmdb.Sync(uncached, nil)
	}

	scoreItem := func(item vodItem) int {
		score := 0
		if item.TMDBID > 0 {
			score += 10
		}
		if item.PosterURL != "" {
			score += 5
		}
		if item.Overview != "" {
			score += 3
		}
		if item.Rating > 0 {
			score += 2
		}
		if item.Year != "" {
			score += 1
		}
		return score
	}

	if vodType == "movie" || (vodType == "series" && series == "") {
		deduped := make(map[string]int)
		var merged []vodItem
		for _, item := range items {
			key := strings.ToLower(item.Name)
			if idx, exists := deduped[key]; exists {
				existing := &merged[idx]
				if scoreItem(item) > scoreItem(*existing) {
					existing.Alternates = append(existing.Alternates, vodAlt{ID: existing.ID, Name: existing.Name, URL: existing.URL, Group: existing.Group})
					item.Alternates = existing.Alternates
					merged[idx] = item
				} else {
					existing.Alternates = append(existing.Alternates, vodAlt{ID: item.ID, Name: item.Name, URL: item.URL, Group: item.Group})
				}
			} else {
				deduped[key] = len(merged)
				merged = append(merged, item)
			}
		}
		items = merged
	}

	if items == nil {
		items = []vodItem{}
	}
	if len(langCounts) > 0 {
		if lj, err := json.Marshal(langCounts); err == nil {
			w.Header().Set("X-Language-Counts", string(lj))
		}
	}
	respondJSON(w, http.StatusOK, items)
}

func splitGenres(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var genres []string
	for _, p := range parts {
		g := strings.TrimSpace(p)
		if g != "" {
			genres = append(genres, g)
		}
	}
	return genres
}
