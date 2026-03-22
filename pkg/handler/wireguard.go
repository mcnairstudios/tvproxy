package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/service"
)

type WireGuardStatusProvider interface {
	Status(ctx context.Context) map[string]interface{}
	Reconfigure(ctx context.Context) error
	IsConnected() bool
	Connect(ctx context.Context, req service.ConnectRequest) error
	Disconnect(ctx context.Context)
}

type WireGuardHandler struct {
	wgService WireGuardStatusProvider
	log       zerolog.Logger
}

func NewWireGuardHandler(wgService WireGuardStatusProvider, log zerolog.Logger) *WireGuardHandler {
	return &WireGuardHandler{
		wgService: wgService,
		log:       log.With().Str("handler", "wireguard").Logger(),
	}
}

func (h *WireGuardHandler) Status(w http.ResponseWriter, r *http.Request) {
	status := h.wgService.Status(r.Context())
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (h *WireGuardHandler) Reconnect(w http.ResponseWriter, r *http.Request) {
	if err := h.wgService.Reconfigure(r.Context()); err != nil {
		h.log.Error().Err(err).Msg("wireguard reconnect failed")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	status := h.wgService.Status(r.Context())
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (h *WireGuardHandler) Connect(w http.ResponseWriter, r *http.Request) {
	var req service.ConnectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if errs := service.ValidateConfig(req); len(errs) > 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]interface{}{"errors": errs})
		return
	}

	if err := h.wgService.Connect(r.Context(), req); err != nil {
		h.log.Error().Err(err).Msg("wireguard connect failed")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	status := h.wgService.Status(r.Context())
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (h *WireGuardHandler) Disconnect(w http.ResponseWriter, r *http.Request) {
	h.wgService.Disconnect(r.Context())
	status := h.wgService.Status(r.Context())
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}
