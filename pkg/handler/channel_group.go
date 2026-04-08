package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/gavinmcnair/tvproxy/pkg/middleware"
	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/service"
)

type ChannelGroupHandler struct {
	channelService *service.ChannelService
}

func NewChannelGroupHandler(channelService *service.ChannelService) *ChannelGroupHandler {
	return &ChannelGroupHandler{channelService: channelService}
}

func (h *ChannelGroupHandler) List(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	groups, err := h.channelService.ListChannelGroupsForUser(r.Context(), user.UserID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list channel groups")
		return
	}

	w.Header().Set("Cache-Control", "private, no-store")
	respondJSON(w, http.StatusOK, groups)
}

func (h *ChannelGroupHandler) Create(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())

	var req struct {
		Name            string `json:"name"`
		ImageURL        string `json:"image_url"`
		IsEnabled       bool   `json:"is_enabled"`
		JellyfinEnabled bool   `json:"jellyfin_enabled"`
		JellyfinType    string `json:"jellyfin_type"`
		SortOrder       int    `json:"sort_order"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		respondError(w, http.StatusBadRequest, "name is required")
		return
	}

	group := &models.ChannelGroup{
		UserID:          user.UserID,
		Name:            req.Name,
		ImageURL:        req.ImageURL,
		IsEnabled:       req.IsEnabled,
		JellyfinEnabled: req.JellyfinEnabled,
		JellyfinType:    req.JellyfinType,
		SortOrder:       req.SortOrder,
	}

	if err := h.channelService.CreateChannelGroup(r.Context(), group); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create channel group")
		return
	}

	respondJSON(w, http.StatusCreated, group)
}

func (h *ChannelGroupHandler) Get(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	id := chi.URLParam(r, "id")

	group, err := h.channelService.GetChannelGroupForUser(r.Context(), id, user.UserID)
	if err != nil {
		respondError(w, http.StatusNotFound, "channel group not found")
		return
	}

	respondJSON(w, http.StatusOK, group)
}

func (h *ChannelGroupHandler) Update(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	id := chi.URLParam(r, "id")

	group, err := h.channelService.GetChannelGroupForUser(r.Context(), id, user.UserID)
	if err != nil {
		respondError(w, http.StatusNotFound, "channel group not found")
		return
	}

	var req struct {
		Name            string `json:"name"`
		ImageURL        string `json:"image_url"`
		IsEnabled       bool   `json:"is_enabled"`
		JellyfinEnabled bool   `json:"jellyfin_enabled"`
		JellyfinType    string `json:"jellyfin_type"`
		SortOrder       int    `json:"sort_order"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name != "" {
		group.Name = req.Name
	}
	group.ImageURL = req.ImageURL
	group.IsEnabled = req.IsEnabled
	group.JellyfinEnabled = req.JellyfinEnabled
	group.JellyfinType = req.JellyfinType
	group.SortOrder = req.SortOrder

	if err := h.channelService.UpdateChannelGroupForUser(r.Context(), group, user.UserID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update channel group")
		return
	}

	respondJSON(w, http.StatusOK, group)
}

func (h *ChannelGroupHandler) Delete(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	id := chi.URLParam(r, "id")

	if err := h.channelService.DeleteChannelGroupForUser(r.Context(), id, user.UserID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to delete channel group")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
