package handler

import (
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/gavinmcnair/tvproxy/pkg/config"
	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/service"
)

type HDHRHandler struct {
	hdhrService  *service.HDHRService
	proxyService *service.ProxyService
	cfg          *config.Config
}

func NewHDHRHandler(hdhrService *service.HDHRService, proxyService *service.ProxyService, cfg *config.Config) *HDHRHandler {
	return &HDHRHandler{
		hdhrService:  hdhrService,
		proxyService: proxyService,
		cfg:          cfg,
	}
}

type lineupStatusResponse struct {
	ScanInProgress int      `json:"ScanInProgress"`
	ScanPossible   int      `json:"ScanPossible"`
	Source         string   `json:"Source"`
	SourceList     []string `json:"SourceList"`
}

type hdhrDeviceRequest struct {
	Name            string   `json:"name"`
	DeviceID        string   `json:"device_id"`
	DeviceAuth      string   `json:"device_auth"`
	FirmwareVersion string   `json:"firmware_version"`
	TunerCount      int      `json:"tuner_count"`
	Port            int      `json:"port"`
	ChannelGroupIDs []string `json:"channel_group_ids"`
	IsEnabled       bool     `json:"is_enabled"`
}

func (h *HDHRHandler) baseURL() string {
	return fmt.Sprintf("%s:%d", h.cfg.BaseURL, h.cfg.Port)
}

func (h *HDHRHandler) Discover(w http.ResponseWriter, r *http.Request) {
	baseURL := h.baseURL()

	data, err := h.hdhrService.GetDiscoverData(r.Context(), baseURL)
	if err != nil {
		if errors.Is(err, service.ErrNoHDHRDevice) {
			respondError(w, http.StatusNotFound, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to get discover info")
		return
	}

	respondJSON(w, http.StatusOK, data)
}

func (h *HDHRHandler) LineupStatus(w http.ResponseWriter, r *http.Request) {
	resp := lineupStatusResponse{
		ScanInProgress: 0,
		ScanPossible:   1,
		Source:         "Cable",
		SourceList:     []string{"Cable"},
	}

	respondJSON(w, http.StatusOK, resp)
}

func (h *HDHRHandler) Lineup(w http.ResponseWriter, r *http.Request) {
	baseURL := h.baseURL()

	lineup, err := h.hdhrService.GetLineup(r.Context(), baseURL)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get lineup")
		return
	}

	respondJSON(w, http.StatusOK, lineup)
}

func (h *HDHRHandler) DeviceXML(w http.ResponseWriter, r *http.Request) {
	baseURL := h.baseURL()

	deviceXML, err := h.hdhrService.GetDeviceXML(r.Context(), baseURL)
	if err != nil {
		if errors.Is(err, service.ErrNoHDHRDevice) {
			respondError(w, http.StatusNotFound, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to get device info")
		return
	}

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	xml.NewEncoder(w).Encode(deviceXML)
}

func (h *HDHRHandler) ListDevices(w http.ResponseWriter, r *http.Request) {
	devices, err := h.hdhrService.ListDevices(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list hdhr devices")
		return
	}

	respondJSON(w, http.StatusOK, devices)
}

func (h *HDHRHandler) CreateDevice(w http.ResponseWriter, r *http.Request) {
	var req hdhrDeviceRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" || req.DeviceID == "" {
		respondError(w, http.StatusBadRequest, "name and device_id are required")
		return
	}

	port := req.Port
	if port == 0 {
		nextPort, err := h.hdhrService.NextAvailablePort(r.Context())
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to assign port")
			return
		}
		port = nextPort
	}

	device := &models.HDHRDevice{
		Name:            req.Name,
		DeviceID:        req.DeviceID,
		DeviceAuth:      req.DeviceAuth,
		FirmwareVersion: req.FirmwareVersion,
		TunerCount:      req.TunerCount,
		Port:            port,
		IsEnabled:       req.IsEnabled,
	}

	if err := h.hdhrService.CreateDevice(r.Context(), device); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create hdhr device")
		return
	}

	if len(req.ChannelGroupIDs) > 0 {
		if err := h.hdhrService.SetChannelGroups(r.Context(), device.ID, req.ChannelGroupIDs); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to set channel groups")
			return
		}
		device.ChannelGroupIDs = req.ChannelGroupIDs
	}

	respondJSON(w, http.StatusCreated, device)
}

func (h *HDHRHandler) GetDevice(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	device, err := h.hdhrService.GetDevice(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "hdhr device not found")
		return
	}

	respondJSON(w, http.StatusOK, device)
}

func (h *HDHRHandler) UpdateDevice(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	device, err := h.hdhrService.GetDevice(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "hdhr device not found")
		return
	}

	var req hdhrDeviceRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name != "" {
		device.Name = req.Name
	}
	if req.DeviceID != "" {
		device.DeviceID = req.DeviceID
	}
	device.DeviceAuth = req.DeviceAuth
	device.FirmwareVersion = req.FirmwareVersion
	device.TunerCount = req.TunerCount
	if req.Port != 0 {
		device.Port = req.Port
	}
	device.IsEnabled = req.IsEnabled

	if err := h.hdhrService.UpdateDevice(r.Context(), device); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update hdhr device")
		return
	}

	if err := h.hdhrService.SetChannelGroups(r.Context(), device.ID, req.ChannelGroupIDs); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to set channel groups")
		return
	}
	device.ChannelGroupIDs = req.ChannelGroupIDs

	respondJSON(w, http.StatusOK, device)
}

func (h *HDHRHandler) DeleteDevice(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.hdhrService.DeleteDevice(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to delete hdhr device")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
