package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/service"
)

type TMDBHandler struct {
	settings *service.SettingsService
	client   *http.Client
	log      zerolog.Logger
	cache    sync.Map
}

type tmdbCacheEntry struct {
	data      any
	expiresAt time.Time
}

func NewTMDBHandler(settings *service.SettingsService, log zerolog.Logger) *TMDBHandler {
	return &TMDBHandler{
		settings: settings,
		client:   &http.Client{Timeout: 10 * time.Second},
		log:      log,
	}
}

func (h *TMDBHandler) Search(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("query")
	if query == "" {
		respondError(w, http.StatusBadRequest, "query required")
		return
	}

	apiKey, _ := h.settings.Get(r.Context(), "tmdb_api_key")
	if apiKey == "" {
		respondJSON(w, http.StatusOK, map[string]any{"results": []any{}})
		return
	}

	cacheKey := "search:" + query
	if cached, ok := h.cache.Load(cacheKey); ok {
		entry := cached.(*tmdbCacheEntry)
		if time.Now().Before(entry.expiresAt) {
			respondJSON(w, http.StatusOK, entry.data)
			return
		}
	}

	searchURL := fmt.Sprintf("https://api.themoviedb.org/3/search/multi?api_key=%s&query=%s&language=en-GB",
		url.QueryEscape(apiKey), url.QueryEscape(query))

	resp, err := h.client.Get(searchURL)
	if err != nil {
		respondError(w, http.StatusBadGateway, "tmdb request failed")
		return
	}
	defer resp.Body.Close()

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)

	if results, ok := result["results"].([]any); ok && len(results) > 0 {
		h.cache.Store(cacheKey, &tmdbCacheEntry{data: result, expiresAt: time.Now().Add(24 * time.Hour)})
	}

	respondJSON(w, http.StatusOK, result)
}

func (h *TMDBHandler) Details(w http.ResponseWriter, r *http.Request) {
	mediaType := r.URL.Query().Get("type")
	id := r.URL.Query().Get("id")
	if mediaType == "" || id == "" {
		respondError(w, http.StatusBadRequest, "type and id required")
		return
	}

	apiKey, _ := h.settings.Get(r.Context(), "tmdb_api_key")
	if apiKey == "" {
		respondJSON(w, http.StatusOK, map[string]any{})
		return
	}

	cacheKey := "detail:" + mediaType + ":" + id
	if cached, ok := h.cache.Load(cacheKey); ok {
		entry := cached.(*tmdbCacheEntry)
		if time.Now().Before(entry.expiresAt) {
			respondJSON(w, http.StatusOK, entry.data)
			return
		}
	}

	detailURL := fmt.Sprintf("https://api.themoviedb.org/3/%s/%s?api_key=%s&language=en-GB&append_to_response=images,credits",
		url.PathEscape(mediaType), url.PathEscape(id), url.QueryEscape(apiKey))

	resp, err := h.client.Get(detailURL)
	if err != nil {
		respondError(w, http.StatusBadGateway, "tmdb request failed")
		return
	}
	defer resp.Body.Close()

	var result any
	json.NewDecoder(resp.Body).Decode(&result)

	h.cache.Store(cacheKey, &tmdbCacheEntry{data: result, expiresAt: time.Now().Add(24 * time.Hour)})

	respondJSON(w, http.StatusOK, result)
}
