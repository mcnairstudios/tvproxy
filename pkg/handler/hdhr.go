package handler

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"

	"github.com/gavinmcnair/tvproxy/pkg/config"
	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/service"
)

// HDHRHandler handles HDHomeRun device emulation and device management HTTP requests.
type HDHRHandler struct {
	hdhrService    *service.HDHRService
	hdhrDeviceRepo interface {
		NextAvailablePort(ctx context.Context) (int, error)
		SetChannelGroups(ctx context.Context, deviceID int64, groupIDs []int64) error
	}
	proxyService *service.ProxyService
	cfg          *config.Config
}

// NewHDHRHandler creates a new HDHRHandler.
func NewHDHRHandler(hdhrService *service.HDHRService, hdhrDeviceRepo interface {
	NextAvailablePort(ctx context.Context) (int, error)
	SetChannelGroups(ctx context.Context, deviceID int64, groupIDs []int64) error
}, proxyService *service.ProxyService, cfg *config.Config) *HDHRHandler {
	return &HDHRHandler{
		hdhrService:    hdhrService,
		hdhrDeviceRepo: hdhrDeviceRepo,
		proxyService:   proxyService,
		cfg:            cfg,
	}
}

// lineupStatusResponse represents the HDHomeRun lineup status response.
type lineupStatusResponse struct {
	ScanInProgress int      `json:"ScanInProgress"`
	ScanPossible   int      `json:"ScanPossible"`
	Source         string   `json:"Source"`
	SourceList     []string `json:"SourceList"`
}

// resolveBaseURL returns the externally reachable base URL for HDHR responses.
// BaseURL is portless (e.g. http://192.168.1.149), so we append the main server port.
func (h *HDHRHandler) resolveBaseURL(r *http.Request) string {
	return fmt.Sprintf("%s:%d", h.cfg.BaseURL, h.cfg.Port)
}

// Discover returns the HDHomeRun discover.json response.
func (h *HDHRHandler) Discover(w http.ResponseWriter, r *http.Request) {
	baseURL := h.resolveBaseURL(r)

	data, err := h.hdhrService.GetDiscoverData(r.Context(), baseURL)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get discover info")
		return
	}

	respondJSON(w, http.StatusOK, data)
}

// LineupStatus returns the HDHomeRun lineup scan status.
func (h *HDHRHandler) LineupStatus(w http.ResponseWriter, r *http.Request) {
	resp := lineupStatusResponse{
		ScanInProgress: 0,
		ScanPossible:   1,
		Source:         "Cable",
		SourceList:     []string{"Cable"},
	}

	respondJSON(w, http.StatusOK, resp)
}

// Lineup returns the HDHomeRun channel lineup.
func (h *HDHRHandler) Lineup(w http.ResponseWriter, r *http.Request) {
	baseURL := h.resolveBaseURL(r)

	lineup, err := h.hdhrService.GetLineup(r.Context(), baseURL)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get lineup")
		return
	}

	respondJSON(w, http.StatusOK, lineup)
}

// DeviceXML returns the HDHomeRun device XML description.
func (h *HDHRHandler) DeviceXML(w http.ResponseWriter, r *http.Request) {
	baseURL := h.resolveBaseURL(r)

	deviceXML, err := h.hdhrService.GetDeviceXML(r.Context(), baseURL)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get device info")
		return
	}

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	xml.NewEncoder(w).Encode(deviceXML)
}

// ListDevices returns all HDHR devices.
func (h *HDHRHandler) ListDevices(w http.ResponseWriter, r *http.Request) {
	devices, err := h.hdhrService.ListDevices(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list hdhr devices")
		return
	}

	respondJSON(w, http.StatusOK, devices)
}

// CreateDevice creates a new HDHR device.
func (h *HDHRHandler) CreateDevice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name             string  `json:"name"`
		DeviceID         string  `json:"device_id"`
		DeviceAuth       string  `json:"device_auth"`
		FirmwareVersion  string  `json:"firmware_version"`
		TunerCount       int     `json:"tuner_count"`
		Port             int     `json:"port"`
		ChannelProfileID *int64  `json:"channel_profile_id"`
		ChannelGroupIDs  []int64 `json:"channel_group_ids"`
		IsEnabled        bool    `json:"is_enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" || req.DeviceID == "" {
		respondError(w, http.StatusBadRequest, "name and device_id are required")
		return
	}

	// Auto-assign port if not provided
	port := req.Port
	if port == 0 {
		nextPort, err := h.hdhrDeviceRepo.NextAvailablePort(r.Context())
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to assign port")
			return
		}
		port = nextPort
	}

	device := &models.HDHRDevice{
		Name:             req.Name,
		DeviceID:         req.DeviceID,
		DeviceAuth:       req.DeviceAuth,
		FirmwareVersion:  req.FirmwareVersion,
		TunerCount:       req.TunerCount,
		Port:             port,
		ChannelProfileID: req.ChannelProfileID,
		IsEnabled:        req.IsEnabled,
	}

	if err := h.hdhrService.CreateDevice(r.Context(), device); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create hdhr device")
		return
	}

	if len(req.ChannelGroupIDs) > 0 {
		if err := h.hdhrDeviceRepo.SetChannelGroups(r.Context(), device.ID, req.ChannelGroupIDs); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to set channel groups")
			return
		}
		device.ChannelGroupIDs = req.ChannelGroupIDs
	}

	respondJSON(w, http.StatusCreated, device)
}

// GetDevice returns an HDHR device by ID.
func (h *HDHRHandler) GetDevice(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid device id")
		return
	}

	device, err := h.hdhrService.GetDevice(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "hdhr device not found")
		return
	}

	respondJSON(w, http.StatusOK, device)
}

// UpdateDevice updates an HDHR device by ID.
func (h *HDHRHandler) UpdateDevice(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid device id")
		return
	}

	device, err := h.hdhrService.GetDevice(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "hdhr device not found")
		return
	}

	var req struct {
		Name             string  `json:"name"`
		DeviceID         string  `json:"device_id"`
		DeviceAuth       string  `json:"device_auth"`
		FirmwareVersion  string  `json:"firmware_version"`
		TunerCount       int     `json:"tuner_count"`
		Port             int     `json:"port"`
		ChannelProfileID *int64  `json:"channel_profile_id"`
		ChannelGroupIDs  []int64 `json:"channel_group_ids"`
		IsEnabled        bool    `json:"is_enabled"`
	}
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
	device.ChannelProfileID = req.ChannelProfileID
	device.IsEnabled = req.IsEnabled

	if err := h.hdhrService.UpdateDevice(r.Context(), device); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update hdhr device")
		return
	}

	if err := h.hdhrDeviceRepo.SetChannelGroups(r.Context(), device.ID, req.ChannelGroupIDs); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to set channel groups")
		return
	}
	device.ChannelGroupIDs = req.ChannelGroupIDs

	respondJSON(w, http.StatusOK, device)
}

// DeleteDevice deletes an HDHR device by ID.
func (h *HDHRHandler) DeleteDevice(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid device id")
		return
	}

	if err := h.hdhrService.DeleteDevice(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to delete hdhr device")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
