package handler

import (
	"net/http"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/service"
)

// ProxyHandler handles stream proxying HTTP requests.
type ProxyHandler struct {
	proxyService *service.ProxyService
	log          zerolog.Logger
}

// NewProxyHandler creates a new ProxyHandler.
func NewProxyHandler(proxyService *service.ProxyService, log zerolog.Logger) *ProxyHandler {
	return &ProxyHandler{
		proxyService: proxyService,
		log:          log.With().Str("handler", "proxy").Logger(),
	}
}

// Stream proxies a stream for the given channel ID.
// Supports ?profile=NAME to override the channel's configured profile (e.g. ?profile=Browser).
func (h *ProxyHandler) Stream(w http.ResponseWriter, r *http.Request) {
	channelID, err := urlParamInt64(r, "channelID")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid channel id")
		return
	}

	profileOverride := r.URL.Query().Get("profile")

	if err := h.proxyService.ProxyStream(r.Context(), w, r, channelID, profileOverride); err != nil {
		h.log.Error().Err(err).Int64("channel_id", channelID).Msg("proxy stream failed")
		respondError(w, http.StatusInternalServerError, "failed to proxy stream")
		return
	}
}

// RawStream proxies a raw stream by stream ID (for preview/debug).
// Supports ?profile=NAME to transcode via ffmpeg (e.g. ?profile=Browser).
func (h *ProxyHandler) RawStream(w http.ResponseWriter, r *http.Request) {
	streamID, err := urlParamInt64(r, "streamID")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid stream id")
		return
	}

	profileOverride := r.URL.Query().Get("profile")

	if err := h.proxyService.ProxyRawStream(r.Context(), w, r, streamID, profileOverride); err != nil {
		h.log.Error().Err(err).Int64("stream_id", streamID).Msg("raw stream proxy failed")
		respondError(w, http.StatusInternalServerError, "failed to proxy stream")
		return
	}
}
