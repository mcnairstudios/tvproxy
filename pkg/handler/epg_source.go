package handler

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/service"
)

type EPGSourceHandler struct {
	epgService *service.EPGService
}

func NewEPGSourceHandler(epgService *service.EPGService) *EPGSourceHandler {
	return &EPGSourceHandler{epgService: epgService}
}

func (h *EPGSourceHandler) List(w http.ResponseWriter, r *http.Request) {
	sources, err := h.epgService.ListSources(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list epg sources")
		return
	}

	respondJSON(w, http.StatusOK, sources)
}

func (h *EPGSourceHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string `json:"name"`
		URL       string `json:"url"`
		IsEnabled bool   `json:"is_enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" || req.URL == "" {
		respondError(w, http.StatusBadRequest, "name and url are required")
		return
	}

	source := &models.EPGSource{
		Name:      req.Name,
		URL:       req.URL,
		IsEnabled: req.IsEnabled,
	}

	if err := h.epgService.CreateSource(r.Context(), source); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create epg source")
		return
	}

	respondJSON(w, http.StatusCreated, source)
}

func (h *EPGSourceHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	source, err := h.epgService.GetSource(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "epg source not found")
		return
	}

	respondJSON(w, http.StatusOK, source)
}

func (h *EPGSourceHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	source, err := h.epgService.GetSource(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "epg source not found")
		return
	}

	var req struct {
		Name      string `json:"name"`
		URL       string `json:"url"`
		IsEnabled bool   `json:"is_enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name != "" {
		source.Name = req.Name
	}
	if req.URL != "" {
		source.URL = req.URL
	}
	source.IsEnabled = req.IsEnabled

	if err := h.epgService.UpdateSource(r.Context(), source); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update epg source")
		return
	}

	respondJSON(w, http.StatusOK, source)
}

func (h *EPGSourceHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.epgService.DeleteSource(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to delete epg source")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *EPGSourceHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	go func() {
		if err := h.epgService.RefreshSource(context.Background(), id); err != nil {
			h.epgService.Log().Error().Err(err).Str("source_id", id).Msg("background epg refresh failed")
		}
	}()

	respondJSON(w, http.StatusAccepted, map[string]string{"message": "refresh started"})
}

func (h *EPGSourceHandler) RefreshStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	respondJSON(w, http.StatusOK, h.epgService.Get(id))
}
