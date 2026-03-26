package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/gavinmcnair/tvproxy/pkg/service"
	"github.com/gavinmcnair/tvproxy/pkg/store"
)

type StreamHandler struct {
	streamStore store.StreamReader
	versioned   store.Versioned
	logoService *service.LogoService
}

func NewStreamHandler(streamStore store.StreamReader, versioned store.Versioned, logoService *service.LogoService) *StreamHandler {
	return &StreamHandler{streamStore: streamStore, versioned: versioned, logoService: logoService}
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
