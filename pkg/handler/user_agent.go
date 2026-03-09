package handler

import (
	"net/http"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
)

// UserAgentHandler handles user agent HTTP requests.
type UserAgentHandler struct {
	repo *repository.UserAgentRepository
}

// NewUserAgentHandler creates a new UserAgentHandler.
func NewUserAgentHandler(repo *repository.UserAgentRepository) *UserAgentHandler {
	return &UserAgentHandler{repo: repo}
}

// List returns all user agents.
func (h *UserAgentHandler) List(w http.ResponseWriter, r *http.Request) {
	agents, err := h.repo.List(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list user agents")
		return
	}

	respondJSON(w, http.StatusOK, agents)
}

// Create creates a new user agent.
func (h *UserAgentHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string `json:"name"`
		UserAgent string `json:"user_agent"`
		IsDefault bool   `json:"is_default"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" || req.UserAgent == "" {
		respondError(w, http.StatusBadRequest, "name and user_agent are required")
		return
	}

	agent := &models.UserAgent{
		Name:      req.Name,
		UserAgent: req.UserAgent,
		IsDefault: req.IsDefault,
	}

	if err := h.repo.Create(r.Context(), agent); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create user agent")
		return
	}

	respondJSON(w, http.StatusCreated, agent)
}

// Get returns a user agent by ID.
func (h *UserAgentHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid user agent id")
		return
	}

	agent, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "user agent not found")
		return
	}

	respondJSON(w, http.StatusOK, agent)
}

// Update updates a user agent by ID.
func (h *UserAgentHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid user agent id")
		return
	}

	agent, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "user agent not found")
		return
	}

	var req struct {
		Name      string `json:"name"`
		UserAgent string `json:"user_agent"`
		IsDefault bool   `json:"is_default"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name != "" {
		agent.Name = req.Name
	}
	if req.UserAgent != "" {
		agent.UserAgent = req.UserAgent
	}
	agent.IsDefault = req.IsDefault

	if err := h.repo.Update(r.Context(), agent); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update user agent")
		return
	}

	respondJSON(w, http.StatusOK, agent)
}

// Delete deletes a user agent by ID.
func (h *UserAgentHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid user agent id")
		return
	}

	if err := h.repo.Delete(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to delete user agent")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
