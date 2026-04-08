package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/gavinmcnair/tvproxy/pkg/service"
	"github.com/gavinmcnair/tvproxy/pkg/store"
	"github.com/gavinmcnair/tvproxy/pkg/tmdb"
)

type StreamHandler struct {
	streamStore store.StreamReader
	versioned   store.Versioned
	logoService *service.LogoService
	tmdb        *tmdb.Client
}

func NewStreamHandler(streamStore store.StreamReader, versioned store.Versioned, logoService *service.LogoService, tmdbClient *tmdb.Client) *StreamHandler {
	return &StreamHandler{streamStore: streamStore, versioned: versioned, logoService: logoService, tmdb: tmdbClient}
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
	streams, err := h.streamStore.List(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list streams")
		return
	}

	vodType := r.URL.Query().Get("type")
	series := r.URL.Query().Get("series")

	type vodItem struct {
		ID              string   `json:"id"`
		Name            string   `json:"name"`
		URL             string   `json:"url"`
		Logo            string   `json:"logo,omitempty"`
		PosterURL       string   `json:"poster_url,omitempty"`
		Type            string   `json:"type"`
		Series          string   `json:"series,omitempty"`
		Collection         string `json:"collection,omitempty"`
		CollectionPoster   string `json:"collection_poster,omitempty"`
		CollectionBackdrop string `json:"collection_backdrop,omitempty"`
		Season          int      `json:"season,omitempty"`
		SeasonName      string   `json:"vod_season_name,omitempty"`
		Episode         int      `json:"episode,omitempty"`
		EpisodeName     string   `json:"episode_name,omitempty"`
		EpisodeOverview string   `json:"episode_overview,omitempty"`
		EpisodeStill    string   `json:"episode_still,omitempty"`
		Overview        string   `json:"overview,omitempty"`
		Rating          float64  `json:"rating,omitempty"`
		Year            string   `json:"year,omitempty"`
		Genres          []string `json:"genres,omitempty"`
		Certification   string   `json:"certification,omitempty"`
		VCodec          string   `json:"vcodec,omitempty"`
		ACodec          string   `json:"acodec,omitempty"`
		Res             string   `json:"resolution,omitempty"`
		Audio           string   `json:"audio,omitempty"`
		Duration        float64  `json:"duration,omitempty"`
	}

	var items []vodItem
	var uncached []struct{ Name, MediaType string }
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
			if posterURL == "" && !seen[lookupName+"_"+mediaType] {
				seen[lookupName+"_"+mediaType] = true
				uncached = append(uncached, struct{ Name, MediaType string }{lookupName, mediaType})
			}
		}

		item := vodItem{
			ID:         s.ID,
			Name:       s.Name,
			URL:        s.URL,
			Logo:       h.logoService.Resolve(s.Logo),
			PosterURL:  posterURL,
			Type:       s.VODType,
			Series:     s.VODSeries,
			Collection: s.VODCollection,
			Season:     s.VODSeason,
			SeasonName: s.VODSeasonName,
			Episode:    s.VODEpisode,
			VCodec:    s.VODVCodec,
			ACodec:    s.VODACodec,
			Res:       s.VODRes,
			Audio:     s.VODAudio,
			Duration:  s.VODDuration,
		}

		if h.tmdb != nil {
			if s.VODType == "movie" {
				if m := h.tmdb.LookupMovie(lookupName); m != nil {
					item.Overview = m.Overview
					item.Rating = m.Rating
					item.Year = m.Year
					item.Genres = m.Genres
					item.Certification = m.Certification
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

			if s.VODCollection != "" {
				if col := h.tmdb.LookupCollection(s.VODCollection); col != nil {
					if col.PosterPath != "" {
						item.CollectionPoster = tmdb.PosterURL(col.PosterPath)
					}
					if col.BackdropPath != "" {
						item.CollectionBackdrop = tmdb.PosterURL(col.BackdropPath) + "&size=w1280"
					}
				}
			}
		}

		items = append(items, item)
	}

	if h.tmdb != nil && len(uncached) > 0 {
		var items []tmdb.VODItem
		for _, u := range uncached {
			items = append(items, tmdb.VODItem{Name: u.Name, MediaType: u.MediaType})
		}
		h.tmdb.Sync(items)
	}

	if items == nil {
		items = []vodItem{}
	}
	respondJSON(w, http.StatusOK, items)
}
