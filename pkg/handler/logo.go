package handler

import (
	"net/http"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
)

// LogoHandler handles logo-related HTTP requests.
type LogoHandler struct {
	repo *repository.LogoRepository
}

// NewLogoHandler creates a new LogoHandler.
func NewLogoHandler(repo *repository.LogoRepository) *LogoHandler {
	return &LogoHandler{repo: repo}
}

// List returns all logos.
func (h *LogoHandler) List(w http.ResponseWriter, r *http.Request) {
	logos, err := h.repo.List(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list logos")
		return
	}

	respondJSON(w, http.StatusOK, logos)
}

// Create creates a new logo.
func (h *LogoHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" || req.URL == "" {
		respondError(w, http.StatusBadRequest, "name and url are required")
		return
	}

	logo := &models.Logo{
		Name: req.Name,
		URL:  req.URL,
	}

	if err := h.repo.Create(r.Context(), logo); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create logo")
		return
	}

	respondJSON(w, http.StatusCreated, logo)
}

// Get returns a logo by ID.
func (h *LogoHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid logo id")
		return
	}

	logo, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "logo not found")
		return
	}

	respondJSON(w, http.StatusOK, logo)
}

// Delete deletes a logo by ID.
func (h *LogoHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid logo id")
		return
	}

	if err := h.repo.Delete(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to delete logo")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
