package handler

import (
	"net/http"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/service"
)

// ChannelHandler handles channel-related HTTP requests.
type ChannelHandler struct {
	channelService *service.ChannelService
}

// NewChannelHandler creates a new ChannelHandler.
func NewChannelHandler(channelService *service.ChannelService) *ChannelHandler {
	return &ChannelHandler{channelService: channelService}
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

	channel := &models.Channel{
		ChannelNumber:    req.ChannelNumber,
		Name:             req.Name,
		Logo:             req.Logo,
		TvgID:            req.TvgID,
		ChannelGroupID:   req.ChannelGroupID,
		ChannelProfileID: req.ChannelProfileID,
		IsEnabled:        req.IsEnabled,
	}

	if err := h.channelService.CreateChannel(r.Context(), channel); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create channel")
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
	channel.Logo = req.Logo
	channel.TvgID = req.TvgID
	channel.ChannelGroupID = req.ChannelGroupID
	channel.ChannelProfileID = req.ChannelProfileID
	channel.IsEnabled = req.IsEnabled

	if err := h.channelService.UpdateChannel(r.Context(), channel); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update channel")
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
