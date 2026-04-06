package handler

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/gavinmcnair/tvproxy/pkg/middleware"
	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/service"
)

type ChannelHandler struct {
	channelService *service.ChannelService
	logoService    *service.LogoService
}

func NewChannelHandler(channelService *service.ChannelService, logoService *service.LogoService) *ChannelHandler {
	return &ChannelHandler{channelService: channelService, logoService: logoService}
}

func (h *ChannelHandler) resolveLogoID(ctx context.Context, logoID *string, logoURL string, channelName string) (*string, error) {
	if logoID != nil {
		return logoID, nil
	}
	if logoURL == "" || strings.HasPrefix(logoURL, "data:") || strings.HasPrefix(logoURL, "/logo?") {
		return nil, nil
	}
	if !strings.HasPrefix(logoURL, "http://") && !strings.HasPrefix(logoURL, "https://") {
		return nil, nil
	}
	if strings.Contains(logoURL, "placeholder") {
		return nil, nil
	}
	logo, err := h.logoService.GetByURL(ctx, logoURL)
	if err != nil {
		return nil, err
	}
	if logo != nil {
		return &logo.ID, nil
	}
	name := logoName(logoURL, channelName)
	logo = &models.Logo{Name: name, URL: logoURL}
	if err := h.logoService.Create(ctx, logo); err != nil {
		return nil, err
	}
	return &logo.ID, nil
}

func logoName(logoURL, channelName string) string {
	prefix := "tvproxy"
	u, err := url.Parse(logoURL)
	if err == nil && u.Host != "" {
		prefix = u.Host
	}
	name := channelName
	if name == "" {
		name = "Auto"
	}
	return prefix + "/" + name
}

func (h *ChannelHandler) List(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())

	channels, err := h.channelService.ListChannelsForUser(r.Context(), user.UserID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list channels")
		return
	}

	h.logoService.ResolveChannelLogos(channels)
	w.Header().Set("Cache-Control", "private, no-store")
	respondJSON(w, http.StatusOK, channels)
}

func (h *ChannelHandler) Create(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())

	var req struct {
		Name            string  `json:"name"`
		Logo            string  `json:"logo"`
		LogoID          *string `json:"logo_id"`
		TvgID           string  `json:"tvg_id"`
		ChannelGroupID  *string `json:"channel_group_id"`
		StreamProfileID *string `json:"stream_profile_id"`
		IsEnabled       bool    `json:"is_enabled"`
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
		UserID:          user.UserID,
		Name:            req.Name,
		LogoID:          logoID,
		TvgID:           req.TvgID,
		ChannelGroupID:  req.ChannelGroupID,
		StreamProfileID: req.StreamProfileID,
		IsEnabled:       req.IsEnabled,
	}

	if err := h.channelService.CreateChannel(r.Context(), channel); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create channel")
		return
	}

	channel, err = h.channelService.GetChannelForUser(r.Context(), channel.ID, user.UserID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to fetch created channel")
		return
	}

	channel.Logo = h.logoService.ResolveChannel(*channel)
	respondJSON(w, http.StatusCreated, channel)
}

func (h *ChannelHandler) Get(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	id := chi.URLParam(r, "id")

	channel, err := h.channelService.GetChannelForUser(r.Context(), id, user.UserID)
	if err != nil {
		respondError(w, http.StatusNotFound, "channel not found")
		return
	}

	channel.Logo = h.logoService.ResolveChannel(*channel)
	respondJSON(w, http.StatusOK, channel)
}

func (h *ChannelHandler) Update(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	id := chi.URLParam(r, "id")

	channel, err := h.channelService.GetChannelForUser(r.Context(), id, user.UserID)
	if err != nil {
		respondError(w, http.StatusNotFound, "channel not found")
		return
	}

	var req struct {
		Name            string  `json:"name"`
		Logo            string  `json:"logo"`
		LogoID          *string `json:"logo_id"`
		TvgID           string  `json:"tvg_id"`
		ChannelGroupID  *string `json:"channel_group_id"`
		StreamProfileID *string `json:"stream_profile_id"`
		IsEnabled       bool    `json:"is_enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

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
	channel.StreamProfileID = req.StreamProfileID
	channel.IsEnabled = req.IsEnabled

	if err := h.channelService.UpdateChannelForUser(r.Context(), channel, user.UserID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update channel")
		return
	}

	channel, err = h.channelService.GetChannelForUser(r.Context(), channel.ID, user.UserID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to fetch updated channel")
		return
	}

	channel.Logo = h.logoService.ResolveChannel(*channel)
	respondJSON(w, http.StatusOK, channel)
}

func (h *ChannelHandler) Delete(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	id := chi.URLParam(r, "id")

	if err := h.channelService.DeleteChannelForUser(r.Context(), id, user.UserID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to delete channel")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *ChannelHandler) GetStreams(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	id := chi.URLParam(r, "id")

	streams, err := h.channelService.GetChannelStreamsForUser(r.Context(), id, user.UserID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get channel streams")
		return
	}

	respondJSON(w, http.StatusOK, streams)
}

func (h *ChannelHandler) IncrementFailCount(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if err := h.channelService.IncrementChannelFailCount(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to increment fail count")
		return
	}
	channel, err := h.channelService.GetChannelForUser(r.Context(), id, user.UserID)
	if err != nil {
		respondError(w, http.StatusNotFound, "channel not found")
		return
	}
	respondJSON(w, http.StatusOK, channel)
}

func (h *ChannelHandler) ResetFailCount(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.channelService.ResetChannelFailCount(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to reset fail count")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ChannelHandler) AssignStreams(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	id := chi.URLParam(r, "id")

	var req struct {
		StreamIDs []string `json:"stream_ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.channelService.AssignStreamsForUser(r.Context(), id, req.StreamIDs, user.UserID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to assign streams")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "streams assigned"})
}
