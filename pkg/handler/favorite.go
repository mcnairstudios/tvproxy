package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/gavinmcnair/tvproxy/pkg/middleware"
	"github.com/gavinmcnair/tvproxy/pkg/store"
)

type FavoriteHandler struct {
	favoriteStore store.FavoriteStore
}

func NewFavoriteHandler(favoriteStore store.FavoriteStore) *FavoriteHandler {
	return &FavoriteHandler{favoriteStore: favoriteStore}
}

func (h *FavoriteHandler) List(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	ids, err := h.favoriteStore.ListByUser(r.Context(), user.UserID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list favorites")
		return
	}
	respondJSON(w, http.StatusOK, ids)
}

func (h *FavoriteHandler) Add(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	itemID := chi.URLParam(r, "itemId")

	if err := h.favoriteStore.Add(r.Context(), user.UserID, itemID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to add favorite")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *FavoriteHandler) Remove(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	itemID := chi.URLParam(r, "itemId")

	if err := h.favoriteStore.Remove(r.Context(), user.UserID, itemID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to remove favorite")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *FavoriteHandler) IsFavorite(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	itemID := chi.URLParam(r, "itemId")

	respondJSON(w, http.StatusOK, map[string]bool{
		"is_favorite": h.favoriteStore.IsFavorite(r.Context(), user.UserID, itemID),
	})
}
