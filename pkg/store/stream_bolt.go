package store

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	bolt "go.etcd.io/bbolt"

	"github.com/gavinmcnair/tvproxy/pkg/models"
)

var (
	bktStream     = []byte("stream")
	bktIdxAccount = []byte("idx:account")
	bktIdxSatIP   = []byte("idx:satip")
	bktIdxHDHR    = []byte("idx:hdhr")
	bktIdxVOD     = []byte("idx:vodtype")
	bktIdxGroup   = []byte("idx:group")
	bktIdxName    = []byte("idx:name")
	bktTree       = []byte("tree")
	bktMeta       = []byte("meta")
)

type BoltStreamStore struct {
	db      *bolt.DB
	rev     atomic.Uint64
	dataDir string
	log     zerolog.Logger
}

func NewBoltStreamStore(dataDir string, log zerolog.Logger) (*BoltStreamStore, error) {
	os.MkdirAll(dataDir, 0755)
	dbPath := filepath.Join(dataDir, "streams.bolt")
	db, err := bolt.Open(dbPath, 0644, &bolt.Options{NoSync: true, InitialMmapSize: 256 * 1024 * 1024})
	if err != nil {
		return nil, fmt.Errorf("opening streams bolt db: %w", err)
	}
	db.Update(func(tx *bolt.Tx) error {
		for _, b := range [][]byte{bktStream, bktIdxAccount, bktIdxSatIP, bktIdxHDHR, bktIdxVOD, bktIdxGroup, bktIdxName, bktTree, bktMeta} {
			tx.CreateBucketIfNotExists(b)
		}
		return nil
	})
	s := &BoltStreamStore{db: db, dataDir: dataDir, log: log.With().Str("store", "stream_bolt").Logger()}
	s.loadRevision()
	return s, nil
}

func (s *BoltStreamStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *BoltStreamStore) loadRevision() {
	s.db.View(func(tx *bolt.Tx) error {
		if v := tx.Bucket(bktMeta).Get([]byte("revision")); v != nil && len(v) == 8 {
			s.rev.Store(binary.BigEndian.Uint64(v))
		}
		return nil
	})
}

func (s *BoltStreamStore) bumpRevision(tx *bolt.Tx) {
	r := s.rev.Add(1)
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], r)
	tx.Bucket(bktMeta).Put([]byte("revision"), buf[:])
}

func (s *BoltStreamStore) ETag() string {
	return fmt.Sprintf(`"%d"`, s.rev.Load())
}

func sourceKey(st *models.Stream) string {
	if st.SatIPSourceID != "" {
		return "satip:" + st.SatIPSourceID
	}
	if st.HDHRSourceID != "" {
		return "hdhr:" + st.HDHRSourceID
	}
	return "m3u:" + st.M3UAccountID
}

func (s *BoltStreamStore) GetByID(_ context.Context, id string) (*models.Stream, error) {
	var st models.Stream
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bktStream).Get([]byte(id))
		if v == nil {
			return fmt.Errorf("stream not found: %s", id)
		}
		return json.Unmarshal(v, &st)
	})
	if err != nil {
		return nil, err
	}
	return &st, nil
}

func (s *BoltStreamStore) List(_ context.Context) ([]models.Stream, error) {
	var items []models.Stream
	s.db.View(func(tx *bolt.Tx) error {
		tx.Bucket(bktStream).ForEach(func(k, v []byte) error {
			var st models.Stream
			if json.Unmarshal(v, &st) == nil {
				items = append(items, st)
			}
			return nil
		})
		return nil
	})
	return items, nil
}

func (s *BoltStreamStore) ListSummaries(_ context.Context) ([]models.StreamSummary, error) {
	var items []models.StreamSummary
	s.db.View(func(tx *bolt.Tx) error {
		tx.Bucket(bktStream).ForEach(func(k, v []byte) error {
			var st models.Stream
			if json.Unmarshal(v, &st) == nil {
				items = append(items, streamToSummary(&st))
			}
			return nil
		})
		return nil
	})
	return items, nil
}

func streamToSummary(st *models.Stream) models.StreamSummary {
	return models.StreamSummary{
		ID:            st.ID,
		M3UAccountID:  st.M3UAccountID,
		SatIPSourceID: st.SatIPSourceID,
		HDHRSourceID:  st.HDHRSourceID,
		Name:          st.Name,
		Group:         st.Group,
		Logo:          st.Logo,
		VODType:       st.VODType,
		VODSeries:     st.VODSeries,
		VODSeason:     st.VODSeason,
		VODSeasonName: st.VODSeasonName,
		VODEpisode:    st.VODEpisode,
		VODYear:       st.VODYear,
		TMDBID:        st.TMDBID,
	}
}

func (s *BoltStreamStore) listByIndex(bucket []byte, prefix string) ([]models.Stream, error) {
	var items []models.Stream
	s.db.View(func(tx *bolt.Tx) error {
		idx := tx.Bucket(bucket)
		main := tx.Bucket(bktStream)
		c := idx.Cursor()
		pfx := []byte(prefix)
		for k, _ := c.Seek(pfx); k != nil && hasPrefix(k, pfx); k, _ = c.Next() {
			streamID := suffixAfterSlash(k)
			if v := main.Get([]byte(streamID)); v != nil {
				var st models.Stream
				if json.Unmarshal(v, &st) == nil {
					items = append(items, st)
				}
			}
		}
		return nil
	})
	return items, nil
}

func (s *BoltStreamStore) ListByAccountID(_ context.Context, accountID string) ([]models.Stream, error) {
	return s.listByIndex(bktIdxAccount, accountID+"/")
}

func (s *BoltStreamStore) ListBySatIPSourceID(_ context.Context, sourceID string) ([]models.Stream, error) {
	return s.listByIndex(bktIdxSatIP, sourceID+"/")
}

func (s *BoltStreamStore) ListByHDHRSourceID(_ context.Context, sourceID string) ([]models.Stream, error) {
	return s.listByIndex(bktIdxHDHR, sourceID+"/")
}

func (s *BoltStreamStore) ListByVODType(_ context.Context, vodType string) ([]models.Stream, error) {
	return s.listByIndex(bktIdxVOD, vodType+"/")
}

func (s *BoltStreamStore) BulkUpsert(_ context.Context, streams []models.Stream) error {
	const batchSize = 5000
	now := time.Now()
	for i := 0; i < len(streams); i += batchSize {
		end := i + batchSize
		if end > len(streams) {
			end = len(streams)
		}
		if err := s.bulkUpsertBatch(streams[i:end], now); err != nil {
			return err
		}
	}
	return nil
}

func (s *BoltStreamStore) bulkUpsertBatch(streams []models.Stream, now time.Time) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		main := tx.Bucket(bktStream)
		acctIdx := tx.Bucket(bktIdxAccount)
		satipIdx := tx.Bucket(bktIdxSatIP)
		hdhrIdx := tx.Bucket(bktIdxHDHR)
		vodIdx := tx.Bucket(bktIdxVOD)
		groupIdx := tx.Bucket(bktIdxGroup)
		nameIdx := tx.Bucket(bktIdxName)
		tree := tx.Bucket(bktTree)

		for i := range streams {
			st := &streams[i]
			id := []byte(st.ID)

			if existing := main.Get(id); existing != nil {
				var old models.Stream
				if json.Unmarshal(existing, &old) == nil {
					st.CreatedAt = old.CreatedAt
					if old.TMDBID > 0 {
						st.TMDBID = old.TMDBID
						st.TMDBManual = old.TMDBManual
					}
					if st.CacheType == "" && old.CacheType != "" {
						st.CacheType = old.CacheType
					}
					if st.Language == "" && old.Language != "" {
						st.Language = old.Language
					}
					if st.VODType == "" && old.VODType != "" {
						st.VODType = old.VODType
					}
					s.removeIndexEntries(tx, &old)
				}
			} else {
				st.CreatedAt = now
			}
			st.UpdatedAt = now

			data, err := json.Marshal(st)
			if err != nil {
				continue
			}
			main.Put(id, data)
			s.addIndexEntries(tx, st, acctIdx, satipIdx, hdhrIdx, vodIdx, groupIdx, nameIdx, tree)
		}
		s.bumpRevision(tx)
		return nil
	})
}

func (s *BoltStreamStore) addIndexEntries(tx *bolt.Tx, st *models.Stream, acctIdx, satipIdx, hdhrIdx, vodIdx, groupIdx, nameIdx, tree *bolt.Bucket) {
	empty := []byte{}
	if st.M3UAccountID != "" {
		acctIdx.Put([]byte(st.M3UAccountID+"/"+st.ID), empty)
	}
	if st.SatIPSourceID != "" {
		satipIdx.Put([]byte(st.SatIPSourceID+"/"+st.ID), empty)
	}
	if st.HDHRSourceID != "" {
		hdhrIdx.Put([]byte(st.HDHRSourceID+"/"+st.ID), empty)
	}
	if st.VODType != "" {
		vodIdx.Put([]byte(st.VODType+"/"+st.ID), empty)
	}
	sk := sourceKey(st)
	if st.Group != "" {
		groupIdx.Put([]byte(sk+"/"+st.Group+"/"+st.ID), empty)
		incrementTreeCount(tree, []byte(sk+"/"+st.Group))
	}
	nameIdx.Put([]byte(strings.ToLower(st.Name)+"\x00"+st.ID), empty)
}

func (s *BoltStreamStore) removeIndexEntries(tx *bolt.Tx, st *models.Stream) {
	acctIdx := tx.Bucket(bktIdxAccount)
	satipIdx := tx.Bucket(bktIdxSatIP)
	hdhrIdx := tx.Bucket(bktIdxHDHR)
	vodIdx := tx.Bucket(bktIdxVOD)
	groupIdx := tx.Bucket(bktIdxGroup)
	nameIdx := tx.Bucket(bktIdxName)
	tree := tx.Bucket(bktTree)

	if st.M3UAccountID != "" {
		acctIdx.Delete([]byte(st.M3UAccountID + "/" + st.ID))
	}
	if st.SatIPSourceID != "" {
		satipIdx.Delete([]byte(st.SatIPSourceID + "/" + st.ID))
	}
	if st.HDHRSourceID != "" {
		hdhrIdx.Delete([]byte(st.HDHRSourceID + "/" + st.ID))
	}
	if st.VODType != "" {
		vodIdx.Delete([]byte(st.VODType + "/" + st.ID))
	}
	sk := sourceKey(st)
	if st.Group != "" {
		groupIdx.Delete([]byte(sk + "/" + st.Group + "/" + st.ID))
		decrementTreeCount(tree, []byte(sk+"/"+st.Group))
	}
	nameIdx.Delete([]byte(strings.ToLower(st.Name) + "\x00" + st.ID))
}

func (s *BoltStreamStore) deleteByIndex(bucket []byte, prefix string) ([]string, error) {
	var deleted []string
	s.db.Update(func(tx *bolt.Tx) error {
		idx := tx.Bucket(bucket)
		main := tx.Bucket(bktStream)
		c := idx.Cursor()
		pfx := []byte(prefix)
		var toDelete [][]byte
		for k, _ := c.Seek(pfx); k != nil && hasPrefix(k, pfx); k, _ = c.Next() {
			toDelete = append(toDelete, append([]byte{}, k...))
		}
		for _, k := range toDelete {
			streamID := suffixAfterSlash(k)
			if v := main.Get([]byte(streamID)); v != nil {
				var st models.Stream
				if json.Unmarshal(v, &st) == nil {
					s.removeIndexEntries(tx, &st)
					main.Delete([]byte(streamID))
					deleted = append(deleted, streamID)
				}
			}
		}
		if len(deleted) > 0 {
			s.bumpRevision(tx)
		}
		return nil
	})
	return deleted, nil
}

func (s *BoltStreamStore) DeleteByAccountID(_ context.Context, accountID string) error {
	_, err := s.deleteByIndex(bktIdxAccount, accountID+"/")
	return err
}

func (s *BoltStreamStore) DeleteBySatIPSourceID(_ context.Context, sourceID string) error {
	_, err := s.deleteByIndex(bktIdxSatIP, sourceID+"/")
	return err
}

func (s *BoltStreamStore) DeleteByHDHRSourceID(_ context.Context, sourceID string) error {
	_, err := s.deleteByIndex(bktIdxHDHR, sourceID+"/")
	return err
}

func (s *BoltStreamStore) deleteStaleByIndex(bucket []byte, prefix string, keepIDs []string) ([]string, error) {
	keep := make(map[string]struct{}, len(keepIDs))
	for _, id := range keepIDs {
		keep[id] = struct{}{}
	}
	var deleted []string
	s.db.Update(func(tx *bolt.Tx) error {
		idx := tx.Bucket(bucket)
		main := tx.Bucket(bktStream)
		c := idx.Cursor()
		pfx := []byte(prefix)
		var toDelete [][]byte
		for k, _ := c.Seek(pfx); k != nil && hasPrefix(k, pfx); k, _ = c.Next() {
			streamID := suffixAfterSlash(k)
			if _, shouldKeep := keep[streamID]; !shouldKeep {
				toDelete = append(toDelete, append([]byte{}, k...))
			}
		}
		for _, k := range toDelete {
			streamID := suffixAfterSlash(k)
			if v := main.Get([]byte(streamID)); v != nil {
				var st models.Stream
				if json.Unmarshal(v, &st) == nil {
					s.removeIndexEntries(tx, &st)
					main.Delete([]byte(streamID))
					deleted = append(deleted, streamID)
				}
			}
		}
		if len(deleted) > 0 {
			s.bumpRevision(tx)
		}
		return nil
	})
	return deleted, nil
}

func (s *BoltStreamStore) DeleteStaleByAccountID(_ context.Context, accountID string, keepIDs []string) ([]string, error) {
	return s.deleteStaleByIndex(bktIdxAccount, accountID+"/", keepIDs)
}

func (s *BoltStreamStore) DeleteStaleBySatIPSourceID(_ context.Context, sourceID string, keepIDs []string) ([]string, error) {
	return s.deleteStaleByIndex(bktIdxSatIP, sourceID+"/", keepIDs)
}

func (s *BoltStreamStore) DeleteStaleByHDHRSourceID(_ context.Context, sourceID string, keepIDs []string) ([]string, error) {
	return s.deleteStaleByIndex(bktIdxHDHR, sourceID+"/", keepIDs)
}

func (s *BoltStreamStore) deleteOrphanedByIndex(bucket []byte, knownIDs []string, prefixExtractor func([]byte) string) ([]string, error) {
	known := make(map[string]struct{}, len(knownIDs))
	for _, id := range knownIDs {
		known[id] = struct{}{}
	}
	var deleted []string
	s.db.Update(func(tx *bolt.Tx) error {
		idx := tx.Bucket(bucket)
		main := tx.Bucket(bktStream)
		c := idx.Cursor()
		var toDelete [][]byte
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			srcID := prefixExtractor(k)
			if _, ok := known[srcID]; !ok {
				toDelete = append(toDelete, append([]byte{}, k...))
			}
		}
		for _, k := range toDelete {
			streamID := suffixAfterSlash(k)
			if v := main.Get([]byte(streamID)); v != nil {
				var st models.Stream
				if json.Unmarshal(v, &st) == nil {
					s.removeIndexEntries(tx, &st)
					main.Delete([]byte(streamID))
					deleted = append(deleted, streamID)
				}
			}
		}
		if len(deleted) > 0 {
			s.bumpRevision(tx)
		}
		return nil
	})
	return deleted, nil
}

func (s *BoltStreamStore) DeleteOrphanedM3UStreams(_ context.Context, knownAccountIDs []string) ([]string, error) {
	return s.deleteOrphanedByIndex(bktIdxAccount, knownAccountIDs, prefixBeforeSlash)
}

func (s *BoltStreamStore) DeleteOrphanedSatIPStreams(_ context.Context, knownSourceIDs []string) ([]string, error) {
	return s.deleteOrphanedByIndex(bktIdxSatIP, knownSourceIDs, prefixBeforeSlash)
}

func (s *BoltStreamStore) DeleteOrphanedHDHRStreams(_ context.Context, knownSourceIDs []string) ([]string, error) {
	return s.deleteOrphanedByIndex(bktIdxHDHR, knownSourceIDs, prefixBeforeSlash)
}

func (s *BoltStreamStore) Delete(_ context.Context, id string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		main := tx.Bucket(bktStream)
		v := main.Get([]byte(id))
		if v == nil {
			return nil
		}
		var st models.Stream
		if json.Unmarshal(v, &st) == nil {
			s.removeIndexEntries(tx, &st)
		}
		main.Delete([]byte(id))
		s.bumpRevision(tx)
		return nil
	})
}

func (s *BoltStreamStore) UpdateTMDBID(_ context.Context, id string, tmdbID int) error {
	return s.updateStreamField(id, func(st *models.Stream) {
		st.TMDBID = tmdbID
	})
}

func (s *BoltStreamStore) SetTMDBManual(_ context.Context, id string, tmdbID int) error {
	return s.updateStreamField(id, func(st *models.Stream) {
		st.TMDBID = tmdbID
		st.TMDBManual = true
	})
}

func (s *BoltStreamStore) ClearAutoTMDBByAccountID(_ context.Context, accountID string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		idx := tx.Bucket(bktIdxAccount)
		main := tx.Bucket(bktStream)
		c := idx.Cursor()
		pfx := []byte(accountID + "/")
		for k, _ := c.Seek(pfx); k != nil && hasPrefix(k, pfx); k, _ = c.Next() {
			streamID := suffixAfterSlash(k)
			v := main.Get([]byte(streamID))
			if v == nil {
				continue
			}
			var st models.Stream
			if json.Unmarshal(v, &st) != nil {
				continue
			}
			if st.TMDBID > 0 && !st.TMDBManual {
				st.TMDBID = 0
				st.UpdatedAt = time.Now()
				if data, err := json.Marshal(&st); err == nil {
					main.Put([]byte(streamID), data)
				}
			}
		}
		s.bumpRevision(tx)
		return nil
	})
}

func (s *BoltStreamStore) UpdateWireGuardByAccountID(_ context.Context, accountID string, useWireGuard bool) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		idx := tx.Bucket(bktIdxAccount)
		main := tx.Bucket(bktStream)
		c := idx.Cursor()
		pfx := []byte(accountID + "/")
		for k, _ := c.Seek(pfx); k != nil && hasPrefix(k, pfx); k, _ = c.Next() {
			streamID := suffixAfterSlash(k)
			v := main.Get([]byte(streamID))
			if v == nil {
				continue
			}
			var st models.Stream
			if json.Unmarshal(v, &st) != nil {
				continue
			}
			st.UseWireGuard = useWireGuard
			st.UpdatedAt = time.Now()
			if data, err := json.Marshal(&st); err == nil {
				main.Put([]byte(streamID), data)
			}
		}
		return nil
	})
}

func (s *BoltStreamStore) Clear() error {
	return s.db.Update(func(tx *bolt.Tx) error {
		for _, name := range [][]byte{bktStream, bktIdxAccount, bktIdxSatIP, bktIdxHDHR, bktIdxVOD, bktIdxGroup, bktIdxName, bktTree} {
			tx.DeleteBucket(name)
			tx.CreateBucket(name)
		}
		s.bumpRevision(tx)
		return nil
	})
}

func (s *BoltStreamStore) Save() error {
	return s.db.Sync()
}

func (s *BoltStreamStore) Load() error {
	s.loadRevision()

	var streamCount, treeCount int
	s.db.View(func(tx *bolt.Tx) error {
		streamCount = tx.Bucket(bktStream).Stats().KeyN
		treeCount = tx.Bucket(bktTree).Stats().KeyN
		return nil
	})

	if streamCount > 0 && treeCount == 0 {
		s.log.Info().Int("streams", streamCount).Msg("rebuilding stream indexes")
		s.rebuildIndexes()
	}

	if streamCount > 0 {
		s.log.Info().Int("count", streamCount).Msg("loaded stream bolt store")
	}
	return nil
}

func (s *BoltStreamStore) rebuildIndexes() {
	var streams []models.Stream
	s.db.View(func(tx *bolt.Tx) error {
		tx.Bucket(bktStream).ForEach(func(k, v []byte) error {
			var st models.Stream
			if json.Unmarshal(v, &st) == nil {
				streams = append(streams, st)
			}
			return nil
		})
		return nil
	})

	for _, name := range [][]byte{bktIdxAccount, bktIdxSatIP, bktIdxHDHR, bktIdxVOD, bktIdxGroup, bktIdxName, bktTree} {
		s.db.Update(func(tx *bolt.Tx) error {
			tx.DeleteBucket(name)
			tx.CreateBucket(name)
			return nil
		})
	}

	const batchSize = 5000
	for i := 0; i < len(streams); i += batchSize {
		end := i + batchSize
		if end > len(streams) {
			end = len(streams)
		}
		s.db.Update(func(tx *bolt.Tx) error {
			acctIdx := tx.Bucket(bktIdxAccount)
			satipIdx := tx.Bucket(bktIdxSatIP)
			hdhrIdx := tx.Bucket(bktIdxHDHR)
			vodIdx := tx.Bucket(bktIdxVOD)
			groupIdx := tx.Bucket(bktIdxGroup)
			nameIdx := tx.Bucket(bktIdxName)
			tree := tx.Bucket(bktTree)
			for j := i; j < end; j++ {
				st := &streams[j]
				s.addIndexEntries(tx, st, acctIdx, satipIdx, hdhrIdx, vodIdx, groupIdx, nameIdx, tree)
			}
			return nil
		})
	}
	s.log.Info().Int("indexed", len(streams)).Msg("index rebuild complete")
}

type GroupInfo struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func (s *BoltStreamStore) ListGroups(sourceKey string) ([]GroupInfo, error) {
	var groups []GroupInfo
	s.db.View(func(tx *bolt.Tx) error {
		tree := tx.Bucket(bktTree)
		c := tree.Cursor()
		pfx := []byte(sourceKey + "/")
		for k, v := c.Seek(pfx); k != nil && hasPrefix(k, pfx); k, v = c.Next() {
			name := string(k[len(pfx):])
			var count int
			if len(v) == 4 {
				count = int(binary.BigEndian.Uint32(v))
			}
			groups = append(groups, GroupInfo{Name: name, Count: count})
		}
		return nil
	})
	return groups, nil
}

func (s *BoltStreamStore) ListByGroup(sourceKey, group string, offset, limit int) ([]models.StreamSummary, int, error) {
	var summaries []models.StreamSummary
	var total int
	s.db.View(func(tx *bolt.Tx) error {
		idx := tx.Bucket(bktIdxGroup)
		main := tx.Bucket(bktStream)
		c := idx.Cursor()
		pfx := []byte(sourceKey + "/" + group + "/")
		i := 0
		for k, _ := c.Seek(pfx); k != nil && hasPrefix(k, pfx); k, _ = c.Next() {
			total++
			if i < offset {
				i++
				continue
			}
			if limit > 0 && len(summaries) >= limit {
				i++
				continue
			}
			streamID := suffixAfterSlash(k)
			if v := main.Get([]byte(streamID)); v != nil {
				var st models.Stream
				if json.Unmarshal(v, &st) == nil {
					summaries = append(summaries, streamToSummary(&st))
				}
			}
			i++
		}
		return nil
	})
	return summaries, total, nil
}

func (s *BoltStreamStore) SearchByName(query string, limit int) ([]models.StreamSummary, error) {
	var summaries []models.StreamSummary
	q := strings.ToLower(query)
	s.db.View(func(tx *bolt.Tx) error {
		idx := tx.Bucket(bktIdxName)
		main := tx.Bucket(bktStream)
		c := idx.Cursor()
		pfx := []byte(q)
		for k, _ := c.Seek(pfx); k != nil && hasPrefix(k, pfx); k, _ = c.Next() {
			parts := strings.SplitN(string(k), "\x00", 2)
			if len(parts) != 2 {
				continue
			}
			streamID := parts[1]
			if v := main.Get([]byte(streamID)); v != nil {
				var st models.Stream
				if json.Unmarshal(v, &st) == nil {
					summaries = append(summaries, streamToSummary(&st))
				}
			}
			if len(summaries) >= limit {
				break
			}
		}
		return nil
	})
	return summaries, nil
}

func (s *BoltStreamStore) StreamCount() int {
	var count int
	s.db.View(func(tx *bolt.Tx) error {
		count = tx.Bucket(bktStream).Stats().KeyN
		return nil
	})
	return count
}

type StreamStats struct {
	Total       int            `json:"total"`
	BySource    map[string]int `json:"by_source"`
	ByVODType   map[string]int `json:"by_vod_type"`
}

func (s *BoltStreamStore) Stats() StreamStats {
	stats := StreamStats{
		BySource:  make(map[string]int),
		ByVODType: make(map[string]int),
	}
	s.db.View(func(tx *bolt.Tx) error {
		stats.Total = tx.Bucket(bktStream).Stats().KeyN

		c := tx.Bucket(bktIdxAccount).Cursor()
		var lastPrefix string
		var count int
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			p := prefixBeforeSlash(k)
			if p != lastPrefix {
				if lastPrefix != "" {
					stats.BySource[lastPrefix] = count
				}
				lastPrefix = p
				count = 0
			}
			count++
		}
		if lastPrefix != "" {
			stats.BySource[lastPrefix] = count
		}

		satipC := tx.Bucket(bktIdxSatIP).Cursor()
		lastPrefix = ""
		count = 0
		for k, _ := satipC.First(); k != nil; k, _ = satipC.Next() {
			p := prefixBeforeSlash(k)
			if p != lastPrefix {
				if lastPrefix != "" {
					stats.BySource["satip:"+lastPrefix] = count
				}
				lastPrefix = p
				count = 0
			}
			count++
		}
		if lastPrefix != "" {
			stats.BySource["satip:"+lastPrefix] = count
		}

		hdhrC := tx.Bucket(bktIdxHDHR).Cursor()
		lastPrefix = ""
		count = 0
		for k, _ := hdhrC.First(); k != nil; k, _ = hdhrC.Next() {
			p := prefixBeforeSlash(k)
			if p != lastPrefix {
				if lastPrefix != "" {
					stats.BySource["hdhr:"+lastPrefix] = count
				}
				lastPrefix = p
				count = 0
			}
			count++
		}
		if lastPrefix != "" {
			stats.BySource["hdhr:"+lastPrefix] = count
		}

		vodC := tx.Bucket(bktIdxVOD).Cursor()
		lastPrefix = ""
		count = 0
		for k, _ := vodC.First(); k != nil; k, _ = vodC.Next() {
			p := prefixBeforeSlash(k)
			if p != lastPrefix {
				if lastPrefix != "" {
					stats.ByVODType[lastPrefix] = count
				}
				lastPrefix = p
				count = 0
			}
			count++
		}
		if lastPrefix != "" {
			stats.ByVODType[lastPrefix] = count
		}

		return nil
	})
	return stats
}

func (s *BoltStreamStore) updateStreamField(id string, fn func(*models.Stream)) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		main := tx.Bucket(bktStream)
		v := main.Get([]byte(id))
		if v == nil {
			return fmt.Errorf("stream not found: %s", id)
		}
		var st models.Stream
		if err := json.Unmarshal(v, &st); err != nil {
			return err
		}
		fn(&st)
		st.UpdatedAt = time.Now()
		data, err := json.Marshal(&st)
		if err != nil {
			return err
		}
		main.Put([]byte(id), data)
		s.bumpRevision(tx)
		return nil
	})
}

func hasPrefix(key, prefix []byte) bool {
	return len(key) >= len(prefix) && string(key[:len(prefix)]) == string(prefix)
}

func suffixAfterSlash(key []byte) string {
	s := string(key)
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

func prefixBeforeSlash(key []byte) string {
	s := string(key)
	if i := strings.Index(s, "/"); i >= 0 {
		return s[:i]
	}
	return s
}

func incrementTreeCount(tree *bolt.Bucket, key []byte) {
	var count uint32
	if v := tree.Get(key); v != nil && len(v) == 4 {
		count = binary.BigEndian.Uint32(v)
	}
	count++
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], count)
	tree.Put(key, buf[:])
}

func decrementTreeCount(tree *bolt.Bucket, key []byte) {
	if v := tree.Get(key); v != nil && len(v) == 4 {
		count := binary.BigEndian.Uint32(v)
		if count <= 1 {
			tree.Delete(key)
		} else {
			count--
			var buf [4]byte
			binary.BigEndian.PutUint32(buf[:], count)
			tree.Put(key, buf[:])
		}
	}
}
