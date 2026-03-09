package handler

import (
	"net/http"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
)

// ChannelGroupHandler handles channel group HTTP requests.
type ChannelGroupHandler struct {
	repo *repository.ChannelGroupRepository
}

// NewChannelGroupHandler creates a new ChannelGroupHandler.
func NewChannelGroupHandler(repo *repository.ChannelGroupRepository) *ChannelGroupHandler {
	return &ChannelGroupHandler{repo: repo}
}

// List returns all channel groups.
func (h *ChannelGroupHandler) List(w http.ResponseWriter, r *http.Request) {
	groups, err := h.repo.List(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list channel groups")
		return
	}

	respondJSON(w, http.StatusOK, groups)
}

// Create creates a new channel group.
func (h *ChannelGroupHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string `json:"name"`
		IsEnabled bool   `json:"is_enabled"`
		SortOrder int    `json:"sort_order"`
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
		Name:      req.Name,
		IsEnabled: req.IsEnabled,
		SortOrder: req.SortOrder,
	}

	if err := h.repo.Create(r.Context(), group); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create channel group")
		return
	}

	respondJSON(w, http.StatusCreated, group)
}

// Get returns a channel group by ID.
func (h *ChannelGroupHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid channel group id")
		return
	}

	group, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "channel group not found")
		return
	}

	respondJSON(w, http.StatusOK, group)
}

// Update updates a channel group by ID.
func (h *ChannelGroupHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid channel group id")
		return
	}

	group, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "channel group not found")
		return
	}

	var req struct {
		Name      string `json:"name"`
		IsEnabled bool   `json:"is_enabled"`
		SortOrder int    `json:"sort_order"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name != "" {
		group.Name = req.Name
	}
	group.IsEnabled = req.IsEnabled
	group.SortOrder = req.SortOrder

	if err := h.repo.Update(r.Context(), group); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update channel group")
		return
	}

	respondJSON(w, http.StatusOK, group)
}

// Delete deletes a channel group by ID.
func (h *ChannelGroupHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid channel group id")
		return
	}

	if err := h.repo.Delete(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to delete channel group")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
