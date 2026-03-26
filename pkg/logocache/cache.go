package logocache

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gavinmcnair/tvproxy/pkg/config"
	"github.com/gavinmcnair/tvproxy/pkg/httputil"
)

const Placeholder = `data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='200' height='200' viewBox='0 0 200 200'%3E%3Crect width='200' height='200' rx='20' fill='%23374151'/%3E%3Ctext x='100' y='115' font-family='sans-serif' font-size='80' fill='%239CA3AF' text-anchor='middle'%3ETV%3C/text%3E%3C/svg%3E`

type Cache struct {
	dir        string
	config     *config.Config
	httpClient *http.Client
	index      map[string]string
	mu         sync.RWMutex
}

func New(dir string, cfg *config.Config, timeout time.Duration) *Cache {
	c := &Cache{
		dir:        dir,
		config:     cfg,
		httpClient: &http.Client{Timeout: timeout},
		index:      make(map[string]string),
	}
	os.MkdirAll(dir, 0755)
	c.buildIndex()
	return c
}

func (c *Cache) buildIndex() {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		hash := strings.TrimSuffix(name, filepath.Ext(name))
		c.index[hash] = name
	}
}

func (c *Cache) Fetch(url string) string {
	if url == "" {
		return Placeholder
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		if strings.HasPrefix(url, "/") || strings.HasPrefix(url, "data:") {
			return url
		}
		return Placeholder
	}

	hash := hashURL(url)

	c.mu.RLock()
	filename, ok := c.index[hash]
	c.mu.RUnlock()
	if ok {
		return "/static/logocache/" + filename
	}

	filename = c.download(url, hash)
	if filename != "" {
		return "/static/logocache/" + filename
	}
	return url
}

func (c *Cache) download(url, hash string) string {
	resp, err := httputil.Fetch(context.Background(), c.httpClient, c.config, url)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	ext := detectExtension(resp.Header.Get("Content-Type"), url)
	filename := hash + ext

	f, err := os.Create(filepath.Join(c.dir, filename))
	if err != nil {
		return ""
	}

	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(filepath.Join(c.dir, filename))
		return ""
	}
	f.Close()

	c.mu.Lock()
	c.index[hash] = filename
	c.mu.Unlock()

	return filename
}

func hashURL(url string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(url)))[:16]
}

func detectExtension(contentType, url string) string {
	if contentType != "" {
		ct := strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0])
		exts, _ := mime.ExtensionsByType(ct)
		if len(exts) > 0 {
			for _, e := range exts {
				if e == ".png" || e == ".jpg" || e == ".jpeg" || e == ".svg" || e == ".webp" || e == ".gif" {
					return e
				}
			}
			return exts[0]
		}
	}
	ext := filepath.Ext(strings.SplitN(url, "?", 2)[0])
	if ext != "" && len(ext) <= 5 {
		return ext
	}
	return ".png"
}
