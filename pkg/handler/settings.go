package handler

import (
	"net/http"

	"github.com/gavinmcnair/tvproxy/pkg/database"
	"github.com/gavinmcnair/tvproxy/pkg/service"
)

type SettingsHandler struct {
	settingsService *service.SettingsService
	db              *database.DB
	authService     *service.AuthService
}

func NewSettingsHandler(settingsService *service.SettingsService, db *database.DB, authService *service.AuthService) *SettingsHandler {
	return &SettingsHandler{settingsService: settingsService, db: db, authService: authService}
}

func (h *SettingsHandler) List(w http.ResponseWriter, r *http.Request) {
	settings, err := h.settingsService.List(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list settings")
		return
	}

	respondJSON(w, http.StatusOK, settings)
}

func (h *SettingsHandler) Update(w http.ResponseWriter, r *http.Request) {
	var req map[string]string
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	for key, value := range req {
		if err := h.settingsService.Set(r.Context(), key, value); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to update setting: "+key)
			return
		}
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "settings updated"})
}

func (h *SettingsHandler) SoftReset(w http.ResponseWriter, r *http.Request) {
	if err := h.db.SoftReset(r.Context()); err != nil {
		respondError(w, http.StatusInternalServerError, "soft reset failed: "+err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "soft reset complete"})
}

func (h *SettingsHandler) HardReset(w http.ResponseWriter, r *http.Request) {
	if err := h.db.HardReset(r.Context()); err != nil {
		respondError(w, http.StatusInternalServerError, "hard reset failed: "+err.Error())
		return
	}
	if _, err := h.authService.CreateUser(r.Context(), "admin", "admin", true); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create default admin user: "+err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "hard reset complete"})
}
