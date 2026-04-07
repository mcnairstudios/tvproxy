package handler

import (
	"net/http"

	"github.com/gavinmcnair/tvproxy/pkg/tmdb"
)

type TMDBHandler struct {
	client *tmdb.Client
}

func NewTMDBHandler(client *tmdb.Client) *TMDBHandler {
	return &TMDBHandler{client: client}
}

func (h *TMDBHandler) Client() *tmdb.Client {
	return h.client
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
	if query != "" {
		h.client.Invalidate(query)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *TMDBHandler) ServeImage(w http.ResponseWriter, r *http.Request) {
	h.client.ServeImage(w, r)
}

func (h *TMDBHandler) SyncStatus(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, h.client.Status())
}
