package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	bolt "go.etcd.io/bbolt"

	"github.com/gavinmcnair/tvproxy/pkg/ffmpeg"
)

var (
	bucketProbeHash = []byte("probe_hash")
	bucketProbeID   = []byte("probe_id")
	bucketTSHeader  = []byte("ts_header")
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
		tx.CreateBucketIfNotExists(bucketProbeHash)
		tx.CreateBucketIfNotExists(bucketProbeID)
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

func (c *BoltProbeCache) GetProbe(streamHash string) (*ffmpeg.ProbeResult, error) {
	var result ffmpeg.ProbeResult
	var found bool
	c.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketProbeHash).Get([]byte(streamHash))
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

func (c *BoltProbeCache) SaveProbe(streamHash string, result *ffmpeg.ProbeResult) error {
	if result == nil {
		return nil
	}
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return c.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketProbeHash).Put([]byte(streamHash), data)
	})
}

func (c *BoltProbeCache) InvalidateProbe(streamHash string) error {
	return c.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketProbeHash).Delete([]byte(streamHash))
	})
}

func (c *BoltProbeCache) GetProbeByStreamID(streamID string) (*ffmpeg.ProbeResult, error) {
	var result ffmpeg.ProbeResult
	var found bool
	c.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketProbeID).Get([]byte(streamID))
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

func (c *BoltProbeCache) SaveProbeByStreamID(streamID string, result *ffmpeg.ProbeResult) error {
	if result == nil {
		return nil
	}
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return c.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketProbeID).Put([]byte(streamID), data)
	})
}

func (c *BoltProbeCache) SaveTSHeader(streamHash string, header []byte) error {
	if header == nil {
		return nil
	}
	return c.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketTSHeader).Put([]byte(streamHash), header)
	})
}

func (c *BoltProbeCache) GetTSHeader(streamHash string) ([]byte, error) {
	var header []byte
	c.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketTSHeader).Get([]byte(streamHash))
		if v != nil {
			header = make([]byte, len(v))
			copy(header, v)
		}
		return nil
	})
	return header, nil
}
