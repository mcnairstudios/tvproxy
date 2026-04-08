package handler

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/service"
)

type M3UAccountHandler struct {
	m3uService *service.M3UService
}

func NewM3UAccountHandler(m3uService *service.M3UService) *M3UAccountHandler {
	return &M3UAccountHandler{m3uService: m3uService}
}

func (h *M3UAccountHandler) List(w http.ResponseWriter, r *http.Request) {
	accounts, err := h.m3uService.ListAccounts(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list m3u accounts")
		return
	}

	respondJSON(w, http.StatusOK, accounts)
}

func (h *M3UAccountHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name            string `json:"name"`
		URL             string `json:"url"`
		Type            string `json:"type"`
		Username        string `json:"username"`
		Password        string `json:"password"`
		MaxStreams      int    `json:"max_streams"`
		IsEnabled       bool   `json:"is_enabled"`
		UseWireGuard    bool   `json:"use_wireguard"`
		RefreshInterval int    `json:"refresh_interval"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" || req.URL == "" {
		respondError(w, http.StatusBadRequest, "name and url are required")
		return
	}

	account := &models.M3UAccount{
		Name:            req.Name,
		URL:             req.URL,
		Type:            req.Type,
		Username:        req.Username,
		Password:        req.Password,
		MaxStreams:       req.MaxStreams,
		IsEnabled:       req.IsEnabled,
		UseWireGuard:    req.UseWireGuard,
		RefreshInterval: req.RefreshInterval,
	}

	if err := h.m3uService.CreateAccount(r.Context(), account); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create m3u account")
		return
	}

	respondJSON(w, http.StatusCreated, account)
}

func (h *M3UAccountHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	account, err := h.m3uService.GetAccount(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "m3u account not found")
		return
	}

	respondJSON(w, http.StatusOK, account)
}

func (h *M3UAccountHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	account, err := h.m3uService.GetAccount(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "m3u account not found")
		return
	}

	var req struct {
		Name            string `json:"name"`
		URL             string `json:"url"`
		Type            string `json:"type"`
		Username        string `json:"username"`
		Password        string `json:"password"`
		MaxStreams      int    `json:"max_streams"`
		IsEnabled       bool   `json:"is_enabled"`
		UseWireGuard    bool   `json:"use_wireguard"`
		RefreshInterval int    `json:"refresh_interval"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name != "" {
		account.Name = req.Name
	}
	if req.URL != "" {
		account.URL = req.URL
	}
	if req.Type != "" {
		account.Type = req.Type
	}
	account.Username = req.Username
	account.Password = req.Password
	account.MaxStreams = req.MaxStreams
	account.IsEnabled = req.IsEnabled
	account.UseWireGuard = req.UseWireGuard
	account.RefreshInterval = req.RefreshInterval

	if err := h.m3uService.UpdateAccount(r.Context(), account); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update m3u account")
		return
	}

	respondJSON(w, http.StatusOK, account)
}

func (h *M3UAccountHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.m3uService.DeleteAccount(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to delete m3u account")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *M3UAccountHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	go func() {
		if err := h.m3uService.RefreshAccount(context.Background(), id); err != nil {
			h.m3uService.Log().Error().Err(err).Str("account_id", id).Msg("background m3u refresh failed")
		}
	}()

	respondJSON(w, http.StatusAccepted, map[string]string{"message": "refresh started"})
}

func (h *M3UAccountHandler) RefreshStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	respondJSON(w, http.StatusOK, h.m3uService.Get(id))
}
