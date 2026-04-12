package tmdb

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	bolt "go.etcd.io/bbolt"
)

var (
	bucketMovies      = []byte("movies")
	bucketSeries      = []byte("series")
	bucketCollections = []byte("collections")
)

type BoltMetadataStore struct {
	db   *bolt.DB
	path string
}

func NewBoltMetadataStore(baseDir string) (*BoltMetadataStore, error) {
	os.MkdirAll(baseDir, 0755)
	dbPath := filepath.Join(baseDir, "metadata.db")
	db, err := bolt.Open(dbPath, 0644, nil)
	if err != nil {
		return nil, fmt.Errorf("opening metadata bolt db: %w", err)
	}
	db.Update(func(tx *bolt.Tx) error {
		tx.CreateBucketIfNotExists(bucketMovies)
		tx.CreateBucketIfNotExists(bucketSeries)
		tx.CreateBucketIfNotExists(bucketCollections)
		return nil
	})

	return &BoltMetadataStore{db: db, path: dbPath}, nil
}

func (bs *BoltMetadataStore) Close() error {
	if bs.db != nil {
		return bs.db.Close()
	}
	return nil
}

func itob(v int) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(v))
	return b
}

func (bs *BoltMetadataStore) GetMovie(tmdbID int) *MovieMeta {
	var m MovieMeta
	bs.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketMovies)
		v := b.Get(itob(tmdbID))
		if v == nil {
			return fmt.Errorf("not found")
		}
		return json.Unmarshal(v, &m)
	})
	if m.TMDBID == 0 {
		return nil
	}
	return &m
}

func (bs *BoltMetadataStore) SetMovie(tmdbID int, m *MovieMeta) {
	if tmdbID == 0 {
		return
	}
	bs.db.Update(func(tx *bolt.Tx) error {
		data, err := json.Marshal(m)
		if err != nil {
			return err
		}
		return tx.Bucket(bucketMovies).Put(itob(tmdbID), data)
	})
}

func (bs *BoltMetadataStore) GetSeries(tmdbID int) *SeriesMeta {
	var s SeriesMeta
	bs.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketSeries)
		v := b.Get(itob(tmdbID))
		if v == nil {
			return fmt.Errorf("not found")
		}
		return json.Unmarshal(v, &s)
	})
	if s.TMDBID == 0 {
		return nil
	}
	return &s
}

func (bs *BoltMetadataStore) SetSeries(tmdbID int, s *SeriesMeta) {
	if tmdbID == 0 {
		return
	}
	bs.db.Update(func(tx *bolt.Tx) error {
		data, err := json.Marshal(s)
		if err != nil {
			return err
		}
		return tx.Bucket(bucketSeries).Put(itob(tmdbID), data)
	})
}

func (bs *BoltMetadataStore) GetEpisode(tmdbID int, season, episode int) *EpisodeMeta {
	s := bs.GetSeries(tmdbID)
	if s == nil || s.Seasons == nil {
		return nil
	}
	sm := s.Seasons[season]
	if sm == nil || sm.Episodes == nil {
		return nil
	}
	return sm.Episodes[episode]
}

func (bs *BoltMetadataStore) SetSeasonEpisodes(tmdbID int, seasonNum int, episodes map[int]*EpisodeMeta) {
	if tmdbID == 0 {
		return
	}
	s := bs.GetSeries(tmdbID)
	if s == nil {
		return
	}
	if s.Seasons == nil {
		s.Seasons = make(map[int]*SeasonMeta)
	}
	s.Seasons[seasonNum] = &SeasonMeta{Episodes: episodes}
	bs.SetSeries(tmdbID, s)
}

func (bs *BoltMetadataStore) SeriesNeedingEpisodes() []int {
	var ids []int
	bs.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketSeries)
		b.ForEach(func(k, v []byte) error {
			var s SeriesMeta
			if json.Unmarshal(v, &s) == nil && s.TMDBID > 0 && len(s.Seasons) == 0 {
				ids = append(ids, s.TMDBID)
			}
			return nil
		})
		return nil
	})
	return ids
}

func (bs *BoltMetadataStore) GetCollection(tmdbID int) *CollectionMeta {
	var c CollectionMeta
	bs.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketCollections)
		v := b.Get(itob(tmdbID))
		if v == nil {
			return fmt.Errorf("not found")
		}
		return json.Unmarshal(v, &c)
	})
	if c.TMDBID == 0 {
		return nil
	}
	return &c
}

func (bs *BoltMetadataStore) SetCollection(tmdbID int, c *CollectionMeta) {
	if tmdbID == 0 {
		return
	}
	bs.db.Update(func(tx *bolt.Tx) error {
		data, err := json.Marshal(c)
		if err != nil {
			return err
		}
		return tx.Bucket(bucketCollections).Put(itob(tmdbID), data)
	})
}

func (bs *BoltMetadataStore) Save() {}
