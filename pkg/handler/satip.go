package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/service"
)

func sanitizeHost(host string) string {
	for _, scheme := range []string{"http://", "https://", "rtsp://", "rtsps://"} {
		host = strings.TrimPrefix(host, scheme)
	}
	return strings.TrimRight(host, "/")
}

type SatIPHandler struct {
	satipService *service.SatIPService
}

func NewSatIPHandler(satipService *service.SatIPService) *SatIPHandler {
	return &SatIPHandler{satipService: satipService}
}

func (h *SatIPHandler) List(w http.ResponseWriter, r *http.Request) {
	sources, err := h.satipService.ListSources(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list satip sources")
		return
	}
	respondJSON(w, http.StatusOK, sources)
}

func (h *SatIPHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string `json:"name"`
		Host      string `json:"host"`
		HTTPPort  int    `json:"http_port"`
		IsEnabled bool   `json:"is_enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" || req.Host == "" {
		respondError(w, http.StatusBadRequest, "name and host are required")
		return
	}

	source := &models.SatIPSource{
		Name:      req.Name,
		Host:      sanitizeHost(req.Host),
		HTTPPort:  req.HTTPPort,
		IsEnabled: req.IsEnabled,
	}

	if err := h.satipService.CreateSource(r.Context(), source); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create satip source")
		return
	}

	respondJSON(w, http.StatusCreated, source)
}

func (h *SatIPHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	source, err := h.satipService.GetSource(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "satip source not found")
		return
	}

	respondJSON(w, http.StatusOK, source)
}

func (h *SatIPHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	source, err := h.satipService.GetSource(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "satip source not found")
		return
	}

	var req struct {
		Name      string `json:"name"`
		Host      string `json:"host"`
		HTTPPort  int    `json:"http_port"`
		IsEnabled bool   `json:"is_enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name != "" {
		source.Name = req.Name
	}
	if req.Host != "" {
		source.Host = sanitizeHost(req.Host)
	}
	source.HTTPPort = req.HTTPPort
	source.IsEnabled = req.IsEnabled

	if err := h.satipService.UpdateSource(r.Context(), source); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update satip source")
		return
	}

	respondJSON(w, http.StatusOK, source)
}

func (h *SatIPHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.satipService.DeleteSource(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to delete satip source")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *SatIPHandler) Scan(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	go func() {
		if err := h.satipService.ScanSource(context.Background(), id); err != nil {
			h.satipService.Log().Error().Err(err).Str("source_id", id).Msg("background satip scan failed")
		}
	}()

	respondJSON(w, http.StatusAccepted, map[string]string{"message": "scan started"})
}

func (h *SatIPHandler) ScanStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	respondJSON(w, http.StatusOK, h.satipService.Get(id))
}

func (h *SatIPHandler) Clear(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.satipService.ClearSource(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to clear satip source streams")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "streams cleared"})
}
