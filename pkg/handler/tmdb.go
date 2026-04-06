package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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
	cacheDir string
}

func NewTMDBHandler(settings *service.SettingsService, cacheDir string, log zerolog.Logger) *TMDBHandler {
	h := &TMDBHandler{
		settings: settings,
		client:   &http.Client{Timeout: 10 * time.Second},
		log:      log,
		cacheDir: cacheDir,
	}
	h.loadDiskCache()
	return h
}

func (h *TMDBHandler) loadDiskCache() {
	if h.cacheDir == "" {
		return
	}
	entries, err := os.ReadDir(h.cacheDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(h.cacheDir, e.Name()))
		if err != nil {
			continue
		}
		var result any
		if json.Unmarshal(data, &result) == nil {
			key := strings.TrimSuffix(e.Name(), ".json")
			h.cache.Store(key, result)
		}
	}
}

func (h *TMDBHandler) saveToDisk(key string, data any) {
	if h.cacheDir == "" {
		return
	}
	os.MkdirAll(h.cacheDir, 0755)
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, key)
	raw, _ := json.Marshal(data)
	os.WriteFile(filepath.Join(h.cacheDir, safe+".json"), raw, 0644)
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

	cacheKey := "search_" + query
	if cached, ok := h.cache.Load(cacheKey); ok {
		respondJSON(w, http.StatusOK, cached)
		return
	}

	searchQuery := query
	yearParam := ""
	if idx := strings.LastIndex(query, "("); idx > 0 {
		end := strings.Index(query[idx:], ")")
		if end > 0 {
			year := strings.TrimSpace(query[idx+1 : idx+end])
			if len(year) == 4 && year[0] >= '1' && year[0] <= '2' {
				yearParam = "&year=" + year
				searchQuery = strings.TrimSpace(query[:idx])
			}
		}
	}

	searchURL := fmt.Sprintf("https://api.themoviedb.org/3/search/multi?api_key=%s&query=%s&language=en-GB%s",
		url.QueryEscape(apiKey), url.QueryEscape(searchQuery), yearParam)

	resp, err := h.client.Get(searchURL)
	if err != nil {
		respondError(w, http.StatusBadGateway, "tmdb request failed")
		return
	}
	defer resp.Body.Close()

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)

	if results, ok := result["results"].([]any); ok && len(results) > 0 {
		h.cache.Store(cacheKey, result)
		h.saveToDisk(cacheKey, result)
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

	cacheKey := "detail_" + mediaType + "_" + id
	if cached, ok := h.cache.Load(cacheKey); ok {
		respondJSON(w, http.StatusOK, cached)
		return
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

	h.cache.Store(cacheKey, result)
	h.saveToDisk(cacheKey, result)

	respondJSON(w, http.StatusOK, result)
}
