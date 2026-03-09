package handler

import (
	"net/http"

	"github.com/gavinmcnair/tvproxy/pkg/service"
)

// OutputHandler handles M3U and EPG output generation HTTP requests.
type OutputHandler struct {
	outputService *service.OutputService
}

// NewOutputHandler creates a new OutputHandler.
func NewOutputHandler(outputService *service.OutputService) *OutputHandler {
	return &OutputHandler{outputService: outputService}
}

// M3U generates and returns the M3U playlist output.
func (h *OutputHandler) M3U(w http.ResponseWriter, r *http.Request) {
	content, err := h.outputService.GenerateM3U(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to generate m3u")
		return
	}

	w.Header().Set("Content-Type", "audio/x-mpegurl")
	w.Header().Set("Content-Disposition", "attachment; filename=\"playlist.m3u\"")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(content))
}

// EPG generates and returns the EPG XML output.
func (h *OutputHandler) EPG(w http.ResponseWriter, r *http.Request) {
	content, err := h.outputService.GenerateEPG(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to generate epg")
		return
	}

	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("Content-Disposition", "attachment; filename=\"epg.xml\"")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(content))
}
