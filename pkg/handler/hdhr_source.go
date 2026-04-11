package handler

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/service"
)

type HDHRSourceHandler struct {
	svc *service.HDHRSourceService
}

func NewHDHRSourceHandler(svc *service.HDHRSourceService) *HDHRSourceHandler {
	return &HDHRSourceHandler{svc: svc}
}

func (h *HDHRSourceHandler) List(w http.ResponseWriter, r *http.Request) {
	sources, err := h.svc.ListSources(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, sources)
}

func (h *HDHRSourceHandler) Create(w http.ResponseWriter, r *http.Request) {
	var source models.HDHRSource
	if err := decodeJSON(r, &source); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if source.Name == "" {
		respondError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := h.svc.CreateSource(r.Context(), &source); err != nil {
		respondError(w, http.StatusConflict, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, source)
}

func (h *HDHRSourceHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	source, err := h.svc.GetSource(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "source not found")
		return
	}
	respondJSON(w, http.StatusOK, source)
}

func (h *HDHRSourceHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var source models.HDHRSource
	if err := decodeJSON(r, &source); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	source.ID = id
	if err := h.svc.UpdateSource(r.Context(), &source); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	updated, _ := h.svc.GetSource(r.Context(), id)
	respondJSON(w, http.StatusOK, updated)
}

func (h *HDHRSourceHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.svc.DeleteSource(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *HDHRSourceHandler) Scan(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	go func() {
		if err := h.svc.ScanSource(context.Background(), id); err != nil {
			h.svc.Log().Error().Err(err).Str("source_id", id).Msg("hdhr scan failed")
		}
	}()
	respondJSON(w, http.StatusAccepted, map[string]string{"message": "scan started"})
}

func (h *HDHRSourceHandler) ScanStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	status := h.svc.Get(id)
	respondJSON(w, http.StatusOK, status)
}

func (h *HDHRSourceHandler) AddDevice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Host string `json:"host"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Host == "" {
		respondError(w, http.StatusBadRequest, "host required")
		return
	}
	if err := h.svc.AddDevice(r.Context(), req.Host); err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "device added"})
}

func (h *HDHRSourceHandler) Discover(w http.ResponseWriter, r *http.Request) {
	devices, err := h.svc.DiscoverDevices(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if devices == nil {
		devices = []service.DiscoveredHDHR{}
	}
	respondJSON(w, http.StatusOK, devices)
}

func (h *HDHRSourceHandler) Retune(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var deviceIdx int
	fmt.Sscanf(r.URL.Query().Get("device"), "%d", &deviceIdx)
	go func() {
		if err := h.svc.RetuneDevice(context.Background(), id, deviceIdx); err != nil {
			h.svc.Log().Error().Err(err).Str("source_id", id).Msg("hdhr retune failed")
		}
	}()
	respondJSON(w, http.StatusAccepted, map[string]string{"message": "retune started"})
}

func (h *HDHRSourceHandler) Clear(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.svc.ClearSource(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
