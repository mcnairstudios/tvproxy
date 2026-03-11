package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
	"github.com/gavinmcnair/tvproxy/pkg/service"
)

// ChannelHandler handles channel-related HTTP requests.
type ChannelHandler struct {
	channelService *service.ChannelService
	logoRepo       *repository.LogoRepository
}

// NewChannelHandler creates a new ChannelHandler.
func NewChannelHandler(channelService *service.ChannelService, logoRepo *repository.LogoRepository) *ChannelHandler {
	return &ChannelHandler{channelService: channelService, logoRepo: logoRepo}
}

// resolveLogoID resolves a logo_id from a logo URL or direct logo_id.
// If logo_id is provided, it is used directly.
// If a logo URL is provided, the logo is found or created.
// Returns nil if no logo is specified.
func (h *ChannelHandler) resolveLogoID(ctx context.Context, logoID *int64, logoURL string, channelName string) (*int64, error) {
	if logoID != nil {
		return logoID, nil
	}
	if logoURL == "" {
		return nil, nil
	}
	// Find existing logo by URL
	logo, err := h.logoRepo.GetByURL(ctx, logoURL)
	if err != nil {
		return nil, err
	}
	if logo != nil {
		return &logo.ID, nil
	}
	// Create new logo
	name := channelName
	if name == "" {
		name = "Auto"
	}
	logo = &models.Logo{Name: name, URL: logoURL}
	if err := h.logoRepo.Create(ctx, logo); err != nil {
		return nil, err
	}
	return &logo.ID, nil
}

// List returns all channels.
func (h *ChannelHandler) List(w http.ResponseWriter, r *http.Request) {
	channels, err := h.channelService.ListChannels(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list channels")
		return
	}

	respondJSON(w, http.StatusOK, channels)
}

// Create creates a new channel.
func (h *ChannelHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChannelNumber    int    `json:"channel_number"`
		Name             string `json:"name"`
		Logo             string `json:"logo"`
		LogoID           *int64 `json:"logo_id"`
		TvgID            string `json:"tvg_id"`
		ChannelGroupID   *int64 `json:"channel_group_id"`
		ChannelProfileID *int64 `json:"channel_profile_id"`
		IsEnabled        bool   `json:"is_enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		respondError(w, http.StatusBadRequest, "name is required")
		return
	}

	logoID, err := h.resolveLogoID(r.Context(), req.LogoID, req.Logo, req.Name)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to resolve logo")
		return
	}

	channel := &models.Channel{
		ChannelNumber:    req.ChannelNumber,
		Name:             req.Name,
		LogoID:           logoID,
		TvgID:            req.TvgID,
		ChannelGroupID:   req.ChannelGroupID,
		ChannelProfileID: req.ChannelProfileID,
		IsEnabled:        req.IsEnabled,
	}

	if err := h.channelService.CreateChannel(r.Context(), channel); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "unique") {
			respondError(w, http.StatusConflict, "channel number already exists")
		} else {
			respondError(w, http.StatusInternalServerError, "failed to create channel")
		}
		return
	}

	// Re-fetch to hydrate logo URL via JOIN
	channel, err = h.channelService.GetChannel(r.Context(), channel.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to fetch created channel")
		return
	}

	respondJSON(w, http.StatusCreated, channel)
}

// Get returns a channel by ID.
func (h *ChannelHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid channel id")
		return
	}

	channel, err := h.channelService.GetChannel(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "channel not found")
		return
	}

	respondJSON(w, http.StatusOK, channel)
}

// Update updates a channel by ID.
func (h *ChannelHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid channel id")
		return
	}

	channel, err := h.channelService.GetChannel(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "channel not found")
		return
	}

	var req struct {
		ChannelNumber    int    `json:"channel_number"`
		Name             string `json:"name"`
		Logo             string `json:"logo"`
		LogoID           *int64 `json:"logo_id"`
		TvgID            string `json:"tvg_id"`
		ChannelGroupID   *int64 `json:"channel_group_id"`
		ChannelProfileID *int64 `json:"channel_profile_id"`
		IsEnabled        bool   `json:"is_enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	channel.ChannelNumber = req.ChannelNumber
	if req.Name != "" {
		channel.Name = req.Name
	}

	logoID, err := h.resolveLogoID(r.Context(), req.LogoID, req.Logo, channel.Name)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to resolve logo")
		return
	}
	channel.LogoID = logoID

	channel.TvgID = req.TvgID
	channel.ChannelGroupID = req.ChannelGroupID
	channel.ChannelProfileID = req.ChannelProfileID
	channel.IsEnabled = req.IsEnabled

	if err := h.channelService.UpdateChannel(r.Context(), channel); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update channel")
		return
	}

	// Re-fetch to hydrate logo URL via JOIN
	channel, err = h.channelService.GetChannel(r.Context(), channel.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to fetch updated channel")
		return
	}

	respondJSON(w, http.StatusOK, channel)
}

// Delete deletes a channel by ID.
func (h *ChannelHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid channel id")
		return
	}

	if err := h.channelService.DeleteChannel(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to delete channel")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetStreams returns the streams assigned to a channel.
func (h *ChannelHandler) GetStreams(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid channel id")
		return
	}

	streams, err := h.channelService.GetChannelStreams(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get channel streams")
		return
	}

	respondJSON(w, http.StatusOK, streams)
}

// AssignStreams assigns streams to a channel.
func (h *ChannelHandler) AssignStreams(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid channel id")
		return
	}

	var req struct {
		StreamIDs []int64 `json:"stream_ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.channelService.AssignStreams(r.Context(), id, req.StreamIDs); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to assign streams")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "streams assigned"})
}
