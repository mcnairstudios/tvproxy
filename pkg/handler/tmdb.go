package handler

import (
	"context"
	"net/http"

	"github.com/gavinmcnair/tvproxy/pkg/store"
	"github.com/gavinmcnair/tvproxy/pkg/tmdb"
)

type TMDBHandler struct {
	client      *tmdb.Client
	streamStore store.StreamStore
}

func NewTMDBHandler(client *tmdb.Client, streamStore store.StreamStore) *TMDBHandler {
	return &TMDBHandler{client: client, streamStore: streamStore}
}

func (h *TMDBHandler) Search(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("query")
	if query == "" {
		respondError(w, http.StatusBadRequest, "query required")
		return
	}

	result, err := h.client.Search(query, r.URL.Query().Get("type"))
	if err != nil {
		respondError(w, http.StatusBadGateway, "tmdb request failed")
		return
	}

	respondJSON(w, http.StatusOK, result)
}

func (h *TMDBHandler) Details(w http.ResponseWriter, r *http.Request) {
	mediaType := r.URL.Query().Get("type")
	id := r.URL.Query().Get("id")
	if mediaType == "" || id == "" {
		respondError(w, http.StatusBadRequest, "type and id required")
		return
	}

	result, err := h.client.Details(mediaType, id)
	if err != nil {
		respondError(w, http.StatusBadGateway, "tmdb request failed")
		return
	}

	respondJSON(w, http.StatusOK, result)
}

func (h *TMDBHandler) Season(w http.ResponseWriter, r *http.Request) {
	tvID := r.URL.Query().Get("id")
	season := r.URL.Query().Get("season")
	if tvID == "" || season == "" {
		respondError(w, http.StatusBadRequest, "id and season required")
		return
	}

	result, err := h.client.Season(tvID, season)
	if err != nil {
		respondError(w, http.StatusBadGateway, "tmdb request failed")
		return
	}

	respondJSON(w, http.StatusOK, result)
}

func (h *TMDBHandler) InvalidateCache(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("query")
	mediaType := r.URL.Query().Get("type")
	if query != "" {
		h.client.Invalidate(query)
		if mediaType == "" {
			mediaType = "tv"
		}
		go func() {
			result, err := h.client.Search(query, mediaType)
			if err != nil {
				return
			}
			if mediaType == "movie" {
				h.client.ResolveMovieFromSearch(result)
			} else {
				h.client.ResolveSeriesFromSearch(result)
			}
		}()
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *TMDBHandler) ServeImage(w http.ResponseWriter, r *http.Request) {
	h.client.ServeImage(w, r)
}

func (h *TMDBHandler) SyncStatus(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, h.client.Status())
}

func (h *TMDBHandler) Rematch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		StreamID  string `json:"stream_id"`
		TMDBID    int    `json:"tmdb_id"`
		MediaType string `json:"media_type"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.StreamID == "" || req.TMDBID == 0 || req.MediaType == "" {
		respondError(w, http.StatusBadRequest, "stream_id, tmdb_id, and media_type required")
		return
	}

	if err := h.streamStore.SetTMDBManual(r.Context(), req.StreamID, req.TMDBID); err != nil {
		respondError(w, http.StatusNotFound, "stream not found")
		return
	}
	if err := h.streamStore.Save(); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save")
		return
	}

	if err := h.client.Rematch(req.TMDBID, req.MediaType); err != nil {
		respondError(w, http.StatusBadGateway, "tmdb fetch failed")
		return
	}

	stream, _ := h.streamStore.GetByID(r.Context(), req.StreamID)
	if stream != nil {
		lookupName := stream.Name
		if stream.VODSeries != "" {
			lookupName = stream.VODSeries
		}
		h.client.UpdateSearchCacheForName(lookupName, req.MediaType, req.TMDBID)
	}

	h.updateRelatedStreams(r.Context(), req.StreamID, req.TMDBID, req.MediaType)

	result := map[string]any{"tmdb_id": req.TMDBID}
	if req.MediaType == "movie" {
		if m := h.client.GetMovieByID(req.TMDBID); m != nil {
			result["poster_url"] = tmdb.PosterURL(m.PosterPath)
			result["overview"] = m.Overview
			result["rating"] = m.Rating
			result["year"] = m.Year
			result["genres"] = m.Genres
			result["certification"] = m.Certification
		}
	} else {
		if s := h.client.GetSeriesByID(req.TMDBID); s != nil {
			result["poster_url"] = tmdb.PosterURL(s.PosterPath)
			result["overview"] = s.Overview
			result["rating"] = s.Rating
			result["year"] = s.Year
			result["genres"] = s.Genres
			result["certification"] = s.Certification
		}
	}
	respondJSON(w, http.StatusOK, result)
}

func (h *TMDBHandler) updateRelatedStreams(ctx context.Context, sourceStreamID string, tmdbID int, mediaType string) {
	if mediaType != "series" {
		return
	}

	source, err := h.streamStore.GetByID(ctx, sourceStreamID)
	if err != nil || source == nil || source.VODSeries == "" {
		return
	}

	streams, err := h.streamStore.ListByVODType(ctx, "series")
	if err != nil {
		return
	}

	for _, st := range streams {
		if st.ID == sourceStreamID || st.VODType != "series" {
			continue
		}
		if st.VODSeries == source.VODSeries && st.TMDBID == 0 {
			h.streamStore.UpdateTMDBID(ctx, st.ID, tmdbID)
		}
	}
	h.streamStore.Save()
}
