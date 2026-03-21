package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/service"
)

var knownExtensions = []string{".mp4", ".ts", ".mkv", ".avi", ".m3u8", ".mpg", ".mpeg", ".webm", ".flv", ".mov"}

type ProxyHandler struct {
	proxyService *service.ProxyService
	log          zerolog.Logger
}

func NewProxyHandler(proxyService *service.ProxyService, log zerolog.Logger) *ProxyHandler {
	return &ProxyHandler{
		proxyService: proxyService,
		log:          log.With().Str("handler", "proxy").Logger(),
	}
}

func stripExtension(id string) string {
	lower := strings.ToLower(id)
	for _, ext := range knownExtensions {
		if strings.HasSuffix(lower, ext) {
			return id[:len(id)-len(ext)]
		}
	}
	return id
}

func (h *ProxyHandler) Stream(w http.ResponseWriter, r *http.Request) {
	channelID := stripExtension(chi.URLParam(r, "channelID"))
	h.log.Info().Str("channel_id", channelID).Str("user_agent", r.UserAgent()).Str("remote_addr", r.RemoteAddr).Msg("client connected")

	if err := h.proxyService.ProxyStream(r.Context(), w, r, channelID, r.URL.Query().Get("profile")); err != nil {
		switch {
		case errors.Is(err, service.ErrChannelNotFound):
			respondError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, service.ErrChannelDisabled):
			respondError(w, http.StatusForbidden, err.Error())
		default:
			h.log.Error().Err(err).Str("channel_id", channelID).Msg("proxy stream failed")
			respondError(w, http.StatusInternalServerError, "failed to proxy stream")
		}
	}
}

func (h *ProxyHandler) StreamHead(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Accept-Ranges", "none")
	w.Header().Set("transferMode.dlna.org", "Streaming")
	w.Header().Set("contentFeatures.dlna.org", "DLNA.ORG_PN=MPEG_TS_SD_EU;DLNA.ORG_OP=00;DLNA.ORG_CI=0;DLNA.ORG_FLAGS=89000000000000000000000000000000")
	w.WriteHeader(http.StatusOK)
}

func (h *ProxyHandler) RawStream(w http.ResponseWriter, r *http.Request) {
	streamID := stripExtension(chi.URLParam(r, "streamID"))
	h.log.Info().Str("stream_id", streamID).Str("user_agent", r.UserAgent()).Str("remote_addr", r.RemoteAddr).Msg("client connected")

	if err := h.proxyService.ProxyRawStream(r.Context(), w, r, streamID, r.URL.Query().Get("profile")); err != nil {
		switch {
		case errors.Is(err, service.ErrChannelNotFound):
			respondError(w, http.StatusNotFound, err.Error())
		default:
			h.log.Error().Err(err).Str("stream_id", streamID).Msg("raw stream proxy failed")
			respondError(w, http.StatusInternalServerError, "failed to proxy stream")
		}
	}
}
