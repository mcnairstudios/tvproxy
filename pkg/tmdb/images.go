package tmdb

import (
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type ImageCache struct {
	dir    string
	client *http.Client
}

func NewImageCache(dir string, client *http.Client) *ImageCache {
	os.MkdirAll(dir, 0755)
	return &ImageCache{dir: dir, client: client}
}

func (ic *ImageCache) Serve(w http.ResponseWriter, r *http.Request, tmdbPath, size string) {
	if tmdbPath == "" {
		http.Error(w, "missing path", http.StatusBadRequest)
		return
	}
	if size == "" {
		size = "w342"
	}

	filename := sanitizeKey(size + tmdbPath)
	if cached := ic.findCached(filename); cached != "" {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.ServeFile(w, r, cached)
		return
	}

	imageURL := "https://image.tmdb.org/t/p/" + size + tmdbPath
	resp, err := ic.client.Get(imageURL)
	if err != nil {
		http.Error(w, "fetch failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, "upstream error", resp.StatusCode)
		return
	}

	ext := detectExtension(resp.Header.Get("Content-Type"), tmdbPath)
	cached := filepath.Join(ic.dir, filename+ext)

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

func (ic *ImageCache) findCached(filename string) string {
	matches, _ := filepath.Glob(filepath.Join(ic.dir, filename+".*"))
	if len(matches) > 0 {
		return matches[0]
	}
	return ""
}

func detectExtension(contentType, path string) string {
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
