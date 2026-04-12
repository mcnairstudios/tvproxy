package tmdb

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	bolt "go.etcd.io/bbolt"
)

var bucketTMDB = []byte("tmdb")

type BoltCache struct {
	db *bolt.DB
}

func NewBoltCache(baseDir string) (*BoltCache, error) {
	os.MkdirAll(baseDir, 0755)
	dbPath := filepath.Join(baseDir, "tmdb.db")
	db, err := bolt.Open(dbPath, 0644, nil)
	if err != nil {
		return nil, fmt.Errorf("opening tmdb bolt db: %w", err)
	}
	db.Update(func(tx *bolt.Tx) error {
		tx.CreateBucketIfNotExists(bucketTMDB)
		return nil
	})
	return &BoltCache{db: db}, nil
}

func (c *BoltCache) Close() error {
	if c.db != nil {
		return c.db.Close()
	}
	return nil
}

func (c *BoltCache) Get(key string) (any, bool) {
	safe := sanitizeKey(key)
	var result any
	var found bool
	c.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketTMDB).Get([]byte(safe))
		if v == nil {
			return nil
		}
		if json.Unmarshal(v, &result) == nil {
			found = true
		}
		return nil
	})
	return result, found
}

func (c *BoltCache) Set(key string, value any) {
	safe := sanitizeKey(key)
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	c.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketTMDB).Put([]byte(safe), data)
	})
}

func (c *BoltCache) Delete(key string) {
	safe := sanitizeKey(key)
	c.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketTMDB).Delete([]byte(safe))
	})
}

func (c *BoltCache) Has(key string) bool {
	safe := sanitizeKey(key)
	var found bool
	c.db.View(func(tx *bolt.Tx) error {
		found = tx.Bucket(bucketTMDB).Get([]byte(safe)) != nil
		return nil
	})
	return found
}

func (c *BoltCache) Prune(keepKeys map[string]bool) int {
	var removed int
	c.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketTMDB)
		var toDelete [][]byte
		b.ForEach(func(k, v []byte) error {
			if !keepKeys[string(k)] {
				toDelete = append(toDelete, k)
			}
			return nil
		})
		for _, k := range toDelete {
			b.Delete(k)
			removed++
		}
		return nil
	})
	return removed
}

func (c *BoltCache) MigrateFrom(dir string) {}
