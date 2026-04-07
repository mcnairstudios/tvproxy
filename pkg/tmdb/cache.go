package tmdb

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Cache struct {
	mem sync.Map
	dir string
}

func NewCache(dir string) *Cache {
	os.MkdirAll(dir, 0755)
	c := &Cache{dir: dir}
	c.loadFromDisk()
	return c
}

func (c *Cache) Get(key string) (any, bool) {
	return c.mem.Load(sanitizeKey(key))
}

func (c *Cache) Set(key string, value any) {
	safe := sanitizeKey(key)
	c.mem.Store(safe, value)
	raw, err := json.Marshal(value)
	if err == nil {
		os.WriteFile(filepath.Join(c.dir, safe+".json"), raw, 0644)
	}
}

func (c *Cache) Delete(key string) {
	safe := sanitizeKey(key)
	c.mem.Delete(safe)
	os.Remove(filepath.Join(c.dir, safe+".json"))
}

func (c *Cache) Has(key string) bool {
	_, ok := c.mem.Load(sanitizeKey(key))
	return ok
}

func (c *Cache) Prune(keepKeys map[string]bool) int {
	var removed int
	c.mem.Range(func(key, _ any) bool {
		k := key.(string)
		if !keepKeys[k] {
			c.mem.Delete(k)
			os.Remove(filepath.Join(c.dir, k+".json"))
			removed++
		}
		return true
	})
	return removed
}

func (c *Cache) loadFromDisk() {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(c.dir, e.Name()))
		if err != nil {
			continue
		}
		var result any
		if json.Unmarshal(data, &result) == nil {
			key := strings.TrimSuffix(e.Name(), ".json")
			c.mem.Store(key, result)
		}
	}
}

func (c *Cache) MigrateFrom(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		old := filepath.Join(dir, e.Name())
		os.Rename(old, filepath.Join(c.dir, e.Name()))
	}
}

func sanitizeKey(key string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, key)
}
