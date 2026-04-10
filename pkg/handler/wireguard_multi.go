package handler

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/session"
	"github.com/gavinmcnair/tvproxy/pkg/store"
)

type MultiWireGuardProvider interface {
	Start(ctx context.Context) error
	Stop()
	AllStatus(ctx context.Context) map[string]any
	ProfileStatus(ctx context.Context, profileID string) map[string]any
	SetActiveProfile(ctx context.Context, profileID string) error
	ReconnectProfile(ctx context.Context, profileID string) error
	IsConnected() bool
}

type PoolStatusProvider interface {
	Status() []session.PoolStatus
}

type MultiWireGuardHandler struct {
	svc          MultiWireGuardProvider
	profileStore *store.WireGuardProfileStore
	pool         PoolStatusProvider
	log          zerolog.Logger
}

func NewMultiWireGuardHandler(svc MultiWireGuardProvider, profileStore *store.WireGuardProfileStore, log zerolog.Logger) *MultiWireGuardHandler {
	return &MultiWireGuardHandler{
		svc:          svc,
		profileStore: profileStore,
		log:          log.With().Str("handler", "wireguard_multi").Logger(),
	}
}

func (h *MultiWireGuardHandler) SetPool(pool PoolStatusProvider) {
	h.pool = pool
}

func (h *MultiWireGuardHandler) PoolStatus(w http.ResponseWriter, r *http.Request) {
	if h.pool == nil {
		respondJSON(w, http.StatusOK, []session.PoolStatus{})
		return
	}
	respondJSON(w, http.StatusOK, h.pool.Status())
}

func (h *MultiWireGuardHandler) Status(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, h.svc.AllStatus(r.Context()))
}

func (h *MultiWireGuardHandler) ProfileStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	respondJSON(w, http.StatusOK, h.svc.ProfileStatus(r.Context(), id))
}

func (h *MultiWireGuardHandler) ListProfiles(w http.ResponseWriter, r *http.Request) {
	profiles, err := h.profileStore.List(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	safe := make([]map[string]any, 0, len(profiles))
	for _, p := range profiles {
		safe = append(safe, profileToResponse(p))
	}
	respondJSON(w, http.StatusOK, safe)
}

func (h *MultiWireGuardHandler) GetProfile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := h.profileStore.GetByID(r.Context(), id)
	if err != nil || p == nil {
		respondError(w, http.StatusNotFound, "profile not found")
		return
	}
	respondJSON(w, http.StatusOK, profileToResponse(*p))
}

func (h *MultiWireGuardHandler) CreateProfile(w http.ResponseWriter, r *http.Request) {
	var p models.WireGuardProfile
	if err := decodeJSON(r, &p); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if p.Name == "" {
		respondError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := h.profileStore.Create(r.Context(), &p); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, profileToResponse(p))
}

func (h *MultiWireGuardHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := h.profileStore.GetByID(r.Context(), id)
	if err != nil || existing == nil {
		respondError(w, http.StatusNotFound, "profile not found")
		return
	}
	var p models.WireGuardProfile
	if err := decodeJSON(r, &p); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	p.ID = id
	if err := h.profileStore.Update(r.Context(), &p); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	updated, _ := h.profileStore.GetByID(r.Context(), id)
	if updated != nil {
		respondJSON(w, http.StatusOK, profileToResponse(*updated))
	} else {
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *MultiWireGuardHandler) DeleteProfile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.profileStore.Delete(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *MultiWireGuardHandler) SetActive(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.svc.SetActiveProfile(r.Context(), id); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, h.svc.AllStatus(r.Context()))
}

func (h *MultiWireGuardHandler) Reconnect(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.svc.ReconnectProfile(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, h.svc.ProfileStatus(r.Context(), id))
}

func profileToResponse(p models.WireGuardProfile) map[string]any {
	resp := map[string]any{
		"id":                   p.ID,
		"name":                 p.Name,
		"address":              p.Address,
		"dns":                  p.DNS,
		"peer_public_key":      p.PeerPublicKey,
		"peer_endpoint":        p.PeerEndpoint,
		"route_hosts":          p.RouteHosts,
		"healthcheck_url":      p.HealthcheckURL,
		"healthcheck_method":   p.HealthcheckMethod,
		"healthcheck_interval": p.HealthcheckInterval,
		"is_enabled":           p.IsEnabled,
		"priority":             p.Priority,
		"created_at":           p.CreatedAt,
		"updated_at":           p.UpdatedAt,
	}
	if p.PrivateKey != "" {
		resp["private_key"] = "***"
	}
	return resp
}
