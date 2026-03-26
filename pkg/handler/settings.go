package handler

import (
	"net/http"

	"github.com/gavinmcnair/tvproxy/pkg/service"
)

type storeClearer interface {
	Clear() error
}

type Resetter interface {
	HardReset() error
	SoftReset() error
}

type SettingsHandler struct {
	settingsService *service.SettingsService
	exportService   *service.ExportService
	resetter        Resetter
	authService     *service.AuthService
	streamClearer   storeClearer
	epgClearer      storeClearer
}

func NewSettingsHandler(
	settingsService *service.SettingsService,
	exportService *service.ExportService,
	resetter Resetter,
	authService *service.AuthService,
	streamClearer storeClearer,
	epgClearer storeClearer,
) *SettingsHandler {
	return &SettingsHandler{
		settingsService: settingsService,
		exportService:   exportService,
		resetter:        resetter,
		authService:     authService,
		streamClearer:   streamClearer,
		epgClearer:      epgClearer,
	}
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
	var updates map[string]string
	if err := decodeJSON(r, &updates); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	for key, value := range updates {
		if !service.IsAPISettable(key) {
			respondError(w, http.StatusBadRequest, "unknown setting: "+key)
			return
		}
		if err := h.settingsService.Set(r.Context(), key, value); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to update setting: "+key)
			return
		}
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "settings updated"})
}

func (h *SettingsHandler) SoftReset(w http.ResponseWriter, r *http.Request) {
	if err := h.resetter.SoftReset(); err != nil {
		respondError(w, http.StatusInternalServerError, "soft reset failed: "+err.Error())
		return
	}
	h.streamClearer.Clear()
	h.epgClearer.Clear()
	respondJSON(w, http.StatusOK, map[string]string{"message": "soft reset complete"})
}

func (h *SettingsHandler) HardReset(w http.ResponseWriter, r *http.Request) {
	if err := h.resetter.HardReset(); err != nil {
		respondError(w, http.StatusInternalServerError, "hard reset failed: "+err.Error())
		return
	}
	h.streamClearer.Clear()
	h.epgClearer.Clear()
	if _, err := h.authService.CreateUser(r.Context(), "admin", "admin", true); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create default admin user: "+err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "hard reset complete"})
}

func (h *SettingsHandler) Export(w http.ResponseWriter, r *http.Request) {
	scope := r.URL.Query().Get("scope")
	if scope == "" {
		scope = "channels"
	}

	data, err := h.exportService.Export(r.Context(), scope)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "export failed: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, data)
}

func (h *SettingsHandler) Import(w http.ResponseWriter, r *http.Request) {
	var data service.ExportData
	if err := decodeJSON(r, &data); err != nil {
		respondError(w, http.StatusBadRequest, "invalid import data")
		return
	}

	imported, err := h.exportService.Import(r.Context(), &data)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "import failed: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"message":  "import complete",
		"imported": imported,
	})
}
