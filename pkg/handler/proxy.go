package handler

import (
	"net/http"

	"github.com/gavinmcnair/tvproxy/pkg/service"
)

// ProxyHandler handles stream proxying HTTP requests.
type ProxyHandler struct {
	proxyService *service.ProxyService
}

// NewProxyHandler creates a new ProxyHandler.
func NewProxyHandler(proxyService *service.ProxyService) *ProxyHandler {
	return &ProxyHandler{proxyService: proxyService}
}

// Stream proxies a stream for the given channel ID.
func (h *ProxyHandler) Stream(w http.ResponseWriter, r *http.Request) {
	channelID, err := urlParamInt64(r, "channelID")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid channel id")
		return
	}

	if err := h.proxyService.ProxyStream(r.Context(), w, r, channelID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to proxy stream")
		return
	}
}
