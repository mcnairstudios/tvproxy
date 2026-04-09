package handler

import (
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
	versioned   store.Versioned
	logoService *service.LogoService
	tmdb        *tmdb.Client
	xtreamCache *xtream.Cache
}

func NewStreamHandler(streamStore store.StreamReader, versioned store.Versioned, logoService *service.LogoService, tmdbClient *tmdb.Client, xtreamCache *xtream.Cache) *StreamHandler {
	return &StreamHandler{streamStore: streamStore, versioned: versioned, logoService: logoService, tmdb: tmdbClient, xtreamCache: xtreamCache}
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

	if source != "" || lang != "" {
		var filtered []models.Stream
		for _, s := range streams {
			if source == "xtream" && s.CacheType != "xtream" {
				continue
			}
			if source == "local" && s.CacheType == "xtream" {
				continue
			}
			if lang != "" && s.Language != lang {
				continue
			}
			filtered = append(filtered, s)
		}
		streams = filtered
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
	}

	var items []vodItem
	var uncached []tmdb.VODItem
	seen := make(map[string]bool)

	for _, s := range streams {
		if s.VODType == "" {
			continue
		}
		if vodType != "" && s.VODType != vodType {
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

		posterURL := ""
		if h.tmdb != nil {
			posterURL = h.tmdb.LookupPoster(lookupName, mediaType)
		}
		if h.tmdb != nil && posterURL == "" && !seen[lookupName+"_"+mediaType] {
			seen[lookupName+"_"+mediaType] = true
			uncached = append(uncached, tmdb.VODItem{StreamID: s.ID, Name: lookupName, MediaType: mediaType, TMDBID: s.TMDBID})
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
			VCodec:     s.VODVCodec,
			ACodec:     s.VODACodec,
			Res:        s.VODRes,
			Audio:      s.VODAudio,
			Duration:   s.VODDuration,
		}

		if s.CacheType == "xtream" && h.xtreamCache != nil {
			if s.VODType == "movie" {
				if m := h.xtreamCache.GetMovie(s.CacheKey); m != nil {
					item.PosterURL = m.PosterURL
					item.Overview = m.Plot
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
						item.CollectionBackdrop = m.BackdropURL
					}
				}
			} else if s.VODType == "series" {
				if sr := h.xtreamCache.GetSeries(s.CacheKey); sr != nil {
					item.PosterURL = sr.PosterURL
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
		} else if h.tmdb != nil {
			if s.VODType == "movie" {
				if m := h.tmdb.LookupMovie(lookupName); m != nil {
					item.Overview = m.Overview
					item.Rating = m.Rating
					item.Year = m.Year
					item.Genres = m.Genres
					item.Certification = m.Certification
					if m.CollectionID > 0 {
						if col := h.tmdb.LookupCollectionByID(m.CollectionID); col != nil {
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
				if sr := h.tmdb.LookupSeries(lookupName); sr != nil {
					item.Overview = sr.Overview
					item.Rating = sr.Rating
					item.Year = sr.Year
					item.Genres = sr.Genres
					item.Certification = sr.Certification
				}
				if s.VODSeason > 0 && s.VODEpisode > 0 {
					if ep := h.tmdb.LookupEpisode(lookupName, s.VODSeason, s.VODEpisode); ep != nil {
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

	if items == nil {
		items = []vodItem{}
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
