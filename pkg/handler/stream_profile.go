package handler

import (
	"net/http"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
)

// StreamProfileHandler handles stream profile HTTP requests.
type StreamProfileHandler struct {
	repo *repository.StreamProfileRepository
}

// NewStreamProfileHandler creates a new StreamProfileHandler.
func NewStreamProfileHandler(repo *repository.StreamProfileRepository) *StreamProfileHandler {
	return &StreamProfileHandler{repo: repo}
}

// List returns all stream profiles.
func (h *StreamProfileHandler) List(w http.ResponseWriter, r *http.Request) {
	profiles, err := h.repo.List(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list stream profiles")
		return
	}

	respondJSON(w, http.StatusOK, profiles)
}

// Create creates a new stream profile.
func (h *StreamProfileHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string `json:"name"`
		Command   string `json:"command"`
		Args      string `json:"args"`
		IsDefault bool   `json:"is_default"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		respondError(w, http.StatusBadRequest, "name is required")
		return
	}

	profile := &models.StreamProfile{
		Name:      req.Name,
		Command:   req.Command,
		Args:      req.Args,
		IsDefault: req.IsDefault,
	}

	if err := h.repo.Create(r.Context(), profile); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create stream profile")
		return
	}

	respondJSON(w, http.StatusCreated, profile)
}

// Get returns a stream profile by ID.
func (h *StreamProfileHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid stream profile id")
		return
	}

	profile, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "stream profile not found")
		return
	}

	respondJSON(w, http.StatusOK, profile)
}

// Update updates a stream profile by ID.
func (h *StreamProfileHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid stream profile id")
		return
	}

	profile, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "stream profile not found")
		return
	}

	var req struct {
		Name      string `json:"name"`
		Command   string `json:"command"`
		Args      string `json:"args"`
		IsDefault bool   `json:"is_default"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name != "" {
		profile.Name = req.Name
	}
	profile.Command = req.Command
	profile.Args = req.Args
	profile.IsDefault = req.IsDefault

	if err := h.repo.Update(r.Context(), profile); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update stream profile")
		return
	}

	respondJSON(w, http.StatusOK, profile)
}

// Delete deletes a stream profile by ID.
func (h *StreamProfileHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid stream profile id")
		return
	}

	if err := h.repo.Delete(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to delete stream profile")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
