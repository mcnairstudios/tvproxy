package handler

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/store"
)

type SourceProfileHandler struct {
	store store.SourceProfileStore
}

func NewSourceProfileHandler(store store.SourceProfileStore) *SourceProfileHandler {
	return &SourceProfileHandler{store: store}
}

func (h *SourceProfileHandler) List(w http.ResponseWriter, r *http.Request) {
	profiles, err := h.store.List(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list source profiles")
		return
	}
	respondJSON(w, http.StatusOK, profiles)
}

func (h *SourceProfileHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	profile, err := h.store.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "source profile not found")
		return
	}
	respondJSON(w, http.StatusOK, profile)
}

func (h *SourceProfileHandler) Create(w http.ResponseWriter, r *http.Request) {
	var profile models.SourceProfile
	if err := decodeJSON(r, &profile); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if profile.Name == "" {
		respondError(w, http.StatusBadRequest, "name is required")
		return
	}
	profile.CreatedAt = time.Now()
	profile.UpdatedAt = time.Now()
	if err := h.store.Create(r.Context(), &profile); err != nil {
		respondError(w, http.StatusConflict, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, profile)
}

func (h *SourceProfileHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var profile models.SourceProfile
	if err := decodeJSON(r, &profile); err != nil {
		respondError(w, http.StatusBadRequest, "invalid json")
		return
	}
	profile.ID = id
	profile.UpdatedAt = time.Now()
	if err := h.store.Update(r.Context(), &profile); err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, profile)
}

func (h *SourceProfileHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.store.Delete(r.Context(), id)
	w.WriteHeader(http.StatusNoContent)
}
