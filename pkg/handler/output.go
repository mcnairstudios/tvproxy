package handler

import (
	"net/http"

	"github.com/gavinmcnair/tvproxy/pkg/service"
)

type OutputHandler struct {
	outputService *service.OutputService
}

func NewOutputHandler(outputService *service.OutputService) *OutputHandler {
	return &OutputHandler{outputService: outputService}
}

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

func (h *OutputHandler) M3U8(w http.ResponseWriter, r *http.Request) {
	content, err := h.outputService.GenerateM3UWithExtension(r.Context(), ".mp4")
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to generate m3u8")
		return
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Content-Disposition", "inline; filename=\"channels.m3u8\"")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(content))
}

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
