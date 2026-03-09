package handler

import (
	"net/http"

	"github.com/gavinmcnair/tvproxy/pkg/service"
)

// SettingsHandler handles application settings HTTP requests.
type SettingsHandler struct {
	settingsService *service.SettingsService
}

// NewSettingsHandler creates a new SettingsHandler.
func NewSettingsHandler(settingsService *service.SettingsService) *SettingsHandler {
	return &SettingsHandler{settingsService: settingsService}
}

// List returns all settings.
func (h *SettingsHandler) List(w http.ResponseWriter, r *http.Request) {
	settings, err := h.settingsService.List(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list settings")
		return
	}

	respondJSON(w, http.StatusOK, settings)
}

// Update updates settings from a key/value map.
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
