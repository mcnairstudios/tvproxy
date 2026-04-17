package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	bolt "go.etcd.io/bbolt"

	"github.com/gavinmcnair/tvproxy/pkg/media"
)

var (
	bucketProbe    = []byte("probe")
	bucketTSHeader = []byte("ts_header")
)

type BoltProbeCache struct {
	db *bolt.DB
}

func NewBoltProbeCache(dataDir string) (*BoltProbeCache, error) {
	os.MkdirAll(dataDir, 0755)
	dbPath := filepath.Join(dataDir, "probe.db")
	db, err := bolt.Open(dbPath, 0644, &bolt.Options{NoSync: true})
	if err != nil {
		return nil, fmt.Errorf("opening probe bolt db: %w", err)
	}
	db.Update(func(tx *bolt.Tx) error {
		tx.CreateBucketIfNotExists(bucketProbe)
		tx.CreateBucketIfNotExists(bucketTSHeader)
		return nil
	})
	return &BoltProbeCache{db: db}, nil
}

func (c *BoltProbeCache) Close() error {
	if c.db != nil {
		return c.db.Close()
	}
	return nil
}

func (c *BoltProbeCache) GetProbe(streamID string) (*media.ProbeResult, error) {
	var result media.ProbeResult
	var found bool
	c.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketProbe).Get([]byte(streamID))
		if v == nil {
			return nil
		}
		if json.Unmarshal(v, &result) == nil {
			found = true
		}
		return nil
	})
	if !found {
		return nil, nil
	}
	return &result, nil
}

func (c *BoltProbeCache) DeleteProbe(streamID string) error {
	return c.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketProbe).Delete([]byte(streamID))
	})
}

func (c *BoltProbeCache) SaveProbe(streamID string, result *media.ProbeResult) error {
	if result == nil {
		return nil
	}
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return c.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketProbe).Put([]byte(streamID), data)
	})
}

func (c *BoltProbeCache) SaveTSHeader(streamID string, header []byte) error {
	if header == nil {
		return nil
	}
	return c.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketTSHeader).Put([]byte(streamID), header)
	})
}

func (c *BoltProbeCache) GetTSHeader(streamID string) ([]byte, error) {
	var header []byte
	c.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketTSHeader).Get([]byte(streamID))
		if v != nil {
			header = make([]byte, len(v))
			copy(header, v)
		}
		return nil
	})
	return header, nil
}
