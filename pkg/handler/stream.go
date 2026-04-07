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
		ID        string  `json:"id"`
		Name      string  `json:"name"`
		URL       string  `json:"url"`
		Logo      string  `json:"logo,omitempty"`
		PosterURL string  `json:"poster_url,omitempty"`
		Type      string  `json:"type"`
		Series    string  `json:"series,omitempty"`
		Season    int     `json:"season,omitempty"`
		Episode   int     `json:"episode,omitempty"`
		VCodec    string  `json:"vcodec,omitempty"`
		ACodec    string  `json:"acodec,omitempty"`
		Res       string  `json:"resolution,omitempty"`
		Audio     string  `json:"audio,omitempty"`
		Duration  float64 `json:"duration,omitempty"`
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

		items = append(items, vodItem{
			ID:        s.ID,
			Name:      s.Name,
			URL:       s.URL,
			Logo:      h.logoService.Resolve(s.Logo),
			PosterURL: posterURL,
			Type:      s.VODType,
			Series:    s.VODSeries,
			Season:    s.VODSeason,
			Episode:   s.VODEpisode,
			VCodec:    s.VODVCodec,
			ACodec:    s.VODACodec,
			Res:       s.VODRes,
			Audio:     s.VODAudio,
			Duration:  s.VODDuration,
		})
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
