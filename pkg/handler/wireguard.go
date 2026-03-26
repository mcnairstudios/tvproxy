package handler

import (
	"context"
	"encoding/base64"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/service"
)

type WireGuardStatusProvider interface {
	Status(ctx context.Context) map[string]any
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
	respondJSON(w, http.StatusOK, h.wgService.Status(r.Context()))
}

func (h *WireGuardHandler) Reconnect(w http.ResponseWriter, r *http.Request) {
	if err := h.wgService.Reconfigure(r.Context()); err != nil {
		h.log.Error().Err(err).Msg("wireguard reconnect failed")
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, h.wgService.Status(r.Context()))
}

func (h *WireGuardHandler) Connect(w http.ResponseWriter, r *http.Request) {
	var req service.ConnectRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if errs := validateWireGuardConfig(req); len(errs) > 0 {
		respondJSON(w, http.StatusUnprocessableEntity, map[string]any{"errors": errs})
		return
	}

	if err := h.wgService.Connect(r.Context(), req); err != nil {
		h.log.Error().Err(err).Msg("wireguard connect failed")
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, h.wgService.Status(r.Context()))
}

func (h *WireGuardHandler) Disconnect(w http.ResponseWriter, r *http.Request) {
	h.wgService.Disconnect(r.Context())
	respondJSON(w, http.StatusOK, h.wgService.Status(r.Context()))
}

func validateWireGuardConfig(req service.ConnectRequest) map[string]string {
	errs := make(map[string]string)

	if req.PrivateKey == "" {
		errs["private_key"] = "Private key is required"
	} else {
		raw, err := base64.StdEncoding.DecodeString(req.PrivateKey)
		if err != nil || len(raw) != 32 {
			errs["private_key"] = "Configuration invalid \u2014 example: YWJjZGVm...base64...NTY="
		}
	}

	if req.Address == "" {
		errs["address"] = "Address is required"
	} else if _, err := netip.ParsePrefix(req.Address); err != nil {
		errs["address"] = "Configuration invalid \u2014 example: 10.20.30.40/24"
	}

	if req.DNS == "" {
		errs["dns"] = "DNS is required"
	} else {
		for _, d := range strings.Split(req.DNS, ",") {
			d = strings.TrimSpace(d)
			if d == "" {
				continue
			}
			if _, err := netip.ParseAddr(d); err != nil {
				errs["dns"] = "Configuration invalid \u2014 example: 1.1.1.1, 8.8.8.8"
				break
			}
		}
	}

	if req.PeerPublicKey == "" {
		errs["peer_public_key"] = "Peer public key is required"
	} else {
		raw, err := base64.StdEncoding.DecodeString(req.PeerPublicKey)
		if err != nil || len(raw) != 32 {
			errs["peer_public_key"] = "Configuration invalid \u2014 example: YWJjZGVm...base64...NTY="
		}
	}

	if req.PeerEndpoint == "" {
		errs["peer_endpoint"] = "Peer endpoint is required"
	} else if _, _, err := net.SplitHostPort(req.PeerEndpoint); err != nil {
		errs["peer_endpoint"] = "Configuration invalid \u2014 example: vpn.example.com:51820"
	}

	return errs
}
