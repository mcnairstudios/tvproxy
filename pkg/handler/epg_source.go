package handler

import (
	"net/http"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/service"
)

// EPGSourceHandler handles EPG source HTTP requests.
type EPGSourceHandler struct {
	epgService *service.EPGService
}

// NewEPGSourceHandler creates a new EPGSourceHandler.
func NewEPGSourceHandler(epgService *service.EPGService) *EPGSourceHandler {
	return &EPGSourceHandler{epgService: epgService}
}

// List returns all EPG sources.
func (h *EPGSourceHandler) List(w http.ResponseWriter, r *http.Request) {
	sources, err := h.epgService.ListSources(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list epg sources")
		return
	}

	respondJSON(w, http.StatusOK, sources)
}

// Create creates a new EPG source.
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

// Get returns an EPG source by ID.
func (h *EPGSourceHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid epg source id")
		return
	}

	source, err := h.epgService.GetSource(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "epg source not found")
		return
	}

	respondJSON(w, http.StatusOK, source)
}

// Update updates an EPG source by ID.
func (h *EPGSourceHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid epg source id")
		return
	}

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

// Delete deletes an EPG source by ID.
func (h *EPGSourceHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid epg source id")
		return
	}

	if err := h.epgService.DeleteSource(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to delete epg source")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Refresh triggers a refresh of the EPG source data.
func (h *EPGSourceHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid epg source id")
		return
	}

	if err := h.epgService.RefreshSource(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to refresh epg source")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "refresh started"})
}
