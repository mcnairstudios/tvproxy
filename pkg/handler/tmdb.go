package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/service"
)

var (
	editionTag = regexp.MustCompile(`\{[^}]+\}`)
	yearParen  = regexp.MustCompile(`\((\d{4})\)`)
)

type TMDBHandler struct {
	settings   *service.SettingsService
	client     *http.Client
	log        zerolog.Logger
	cache      sync.Map
	baseDir    string
	searchDir  string
	imageDir   string
	enrichMu   sync.Mutex
	enriching  map[string]bool
}

func NewTMDBHandler(settings *service.SettingsService, baseDir string, log zerolog.Logger) *TMDBHandler {
	searchDir := filepath.Join(baseDir, "search")
	imageDir := filepath.Join(baseDir, "images")
	os.MkdirAll(searchDir, 0755)
	os.MkdirAll(imageDir, 0755)

	h := &TMDBHandler{
		settings:  settings,
		client:    &http.Client{Timeout: 10 * time.Second},
		log:       log,
		baseDir:   baseDir,
		searchDir: searchDir,
		imageDir:  imageDir,
		enriching: make(map[string]bool),
	}
	h.migrateOldCache()
	h.loadDiskCache()
	return h
}

func (h *TMDBHandler) migrateOldCache() {
	entries, err := os.ReadDir(h.baseDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		old := filepath.Join(h.baseDir, e.Name())
		os.Rename(old, filepath.Join(h.searchDir, e.Name()))
	}
}

func (h *TMDBHandler) loadDiskCache() {
	entries, err := os.ReadDir(h.searchDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(h.searchDir, e.Name()))
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
	safe := sanitizeKey(key)
	raw, _ := json.Marshal(data)
	os.WriteFile(filepath.Join(h.searchDir, safe+".json"), raw, 0644)
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

	mediaHint := r.URL.Query().Get("type")

	cacheKey := "search_" + query
	if mediaHint != "" {
		cacheKey += "_" + mediaHint
	}
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

	searchEndpoint := "search/multi"
	if mediaHint == "movie" {
		searchEndpoint = "search/movie"
	} else if mediaHint == "tv" || mediaHint == "series" {
		searchEndpoint = "search/tv"
	}

	searchURL := fmt.Sprintf("https://api.themoviedb.org/3/%s?api_key=%s&query=%s&language=en-GB%s",
		searchEndpoint, url.QueryEscape(apiKey), url.QueryEscape(searchQuery), yearParam)

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

func (h *TMDBHandler) InvalidateCache(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("query")
	if query == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	h.cache.Delete("search_" + query)
	safe := sanitizeKey("search_" + query)
	os.Remove(filepath.Join(h.searchDir, safe+".json"))
	w.WriteHeader(http.StatusNoContent)
}

func (h *TMDBHandler) ServeImage(w http.ResponseWriter, r *http.Request) {
	tmdbPath := r.URL.Query().Get("path")
	if tmdbPath == "" {
		http.Error(w, "missing path", http.StatusBadRequest)
		return
	}

	size := r.URL.Query().Get("size")
	if size == "" {
		size = "w342"
	}

	filename := size + "_" + sanitizeKey(tmdbPath)
	matches, _ := filepath.Glob(filepath.Join(h.imageDir, filename+".*"))
	if len(matches) > 0 {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.ServeFile(w, r, matches[0])
		return
	}

	imageURL := "https://image.tmdb.org/t/p/" + size + tmdbPath
	resp, err := h.client.Get(imageURL)
	if err != nil {
		http.Error(w, "fetch failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, "upstream error", resp.StatusCode)
		return
	}

	ct := resp.Header.Get("Content-Type")
	ext := detectImageExtension(ct, tmdbPath)
	cached := filepath.Join(h.imageDir, filename+ext)

	f, err := os.Create(cached)
	if err != nil {
		http.Error(w, "cache write failed", http.StatusInternalServerError)
		return
	}

	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(cached)
		http.Error(w, "cache write failed", http.StatusInternalServerError)
		return
	}
	f.Close()

	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeFile(w, r, cached)
}

func CleanVODName(name string) (string, string) {
	cleaned := editionTag.ReplaceAllString(name, "")
	year := ""
	if m := yearParen.FindStringSubmatch(cleaned); len(m) > 1 {
		year = m[1]
		cleaned = yearParen.ReplaceAllString(cleaned, "")
	}
	cleaned = strings.TrimSpace(cleaned)
	return cleaned, year
}

func (h *TMDBHandler) LookupPosterURL(name string, mediaType string) string {
	cleanName, year := CleanVODName(name)
	query := cleanName
	if year != "" {
		query = cleanName + " (" + year + ")"
	}

	cacheKey := "search_" + query
	if mediaType != "" {
		cacheKey += "_" + mediaType
	}

	cached, ok := h.cache.Load(cacheKey)
	if !ok {
		return ""
	}

	result, ok := cached.(map[string]any)
	if !ok {
		return ""
	}

	results, ok := result["results"].([]any)
	if !ok || len(results) == 0 {
		return ""
	}

	first, ok := results[0].(map[string]any)
	if !ok {
		return ""
	}

	posterPath, _ := first["poster_path"].(string)
	if posterPath == "" {
		return ""
	}

	return "/api/tmdb/image?path=" + url.QueryEscape(posterPath)
}

func (h *TMDBHandler) EnrichInBackground(items []struct{ Name, MediaType string }) {
	h.enrichMu.Lock()
	var toEnrich []struct{ Name, MediaType string }
	for _, item := range items {
		cleanName, year := CleanVODName(item.Name)
		query := cleanName
		if year != "" {
			query = cleanName + " (" + year + ")"
		}
		cacheKey := "search_" + query
		if item.MediaType != "" {
			cacheKey += "_" + item.MediaType
		}
		if _, ok := h.cache.Load(cacheKey); ok {
			continue
		}
		if h.enriching[cacheKey] {
			continue
		}
		h.enriching[cacheKey] = true
		toEnrich = append(toEnrich, item)
	}
	h.enrichMu.Unlock()

	if len(toEnrich) == 0 {
		return
	}

	go func() {
		for _, item := range toEnrich {
			cleanName, year := CleanVODName(item.Name)
			query := cleanName
			if year != "" {
				query = cleanName + " (" + year + ")"
			}
			cacheKey := "search_" + query
			if item.MediaType != "" {
				cacheKey += "_" + item.MediaType
			}

			apiKey, _ := h.settings.Get(context.Background(), "tmdb_api_key")
			if apiKey == "" {
				break
			}

			searchQuery := cleanName
			yearParam := ""
			if year != "" {
				yearParam = "&year=" + year
			}

			searchEndpoint := "search/multi"
			if item.MediaType == "movie" {
				searchEndpoint = "search/movie"
			} else if item.MediaType == "tv" || item.MediaType == "series" {
				searchEndpoint = "search/tv"
			}

			searchURL := fmt.Sprintf("https://api.themoviedb.org/3/%s?api_key=%s&query=%s&language=en-GB%s",
				searchEndpoint, url.QueryEscape(apiKey), url.QueryEscape(searchQuery), yearParam)

			resp, err := h.client.Get(searchURL)
			if err != nil {
				h.enrichMu.Lock()
				delete(h.enriching, cacheKey)
				h.enrichMu.Unlock()
				time.Sleep(250 * time.Millisecond)
				continue
			}

			var result map[string]any
			json.NewDecoder(resp.Body).Decode(&result)
			resp.Body.Close()

			if results, ok := result["results"].([]any); ok && len(results) > 0 {
				h.cache.Store(cacheKey, result)
				h.saveToDisk(cacheKey, result)
			}

			h.enrichMu.Lock()
			delete(h.enriching, cacheKey)
			h.enrichMu.Unlock()

			time.Sleep(250 * time.Millisecond)
		}
	}()
}

func sanitizeKey(key string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, key)
}

func detectImageExtension(contentType, path string) string {
	if contentType != "" {
		ct := strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0])
		exts, _ := mime.ExtensionsByType(ct)
		if len(exts) > 0 {
			for _, e := range exts {
				if e == ".png" || e == ".jpg" || e == ".jpeg" || e == ".webp" {
					return e
				}
			}
			return exts[0]
		}
	}
	ext := filepath.Ext(strings.SplitN(path, "?", 2)[0])
	if ext != "" && len(ext) <= 5 {
		return ext
	}
	return ".jpg"
}
