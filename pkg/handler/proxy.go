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
func (h *ProxyHandler) Stream(w http.ResponseWriter, r *http.Request) {
	channelID, err := urlParamInt64(r, "channelID")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid channel id")
		return
	}

	h.log.Info().
		Int64("channel_id", channelID).
		Str("user_agent", r.UserAgent()).
		Str("remote", r.RemoteAddr).
		Msg("stream request")

	if err := h.proxyService.ProxyStream(r.Context(), w, r, channelID); err != nil {
		h.log.Error().Err(err).Int64("channel_id", channelID).Msg("proxy stream failed")
		respondError(w, http.StatusInternalServerError, "failed to proxy stream")
		return
	}
}
