package store

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/models"
)

type StreamIndex struct {
	ID            string
	Name          string
	Group         string
	M3UAccountID  string
	SatIPSourceID string
	HDHRSourceID  string
	VODType       string
	VODSeries     string
	VODCollection string
	VODSeason     int
	VODSeasonName string
	VODEpisode    int
	VODYear       int
	TMDBID        int
	TMDBManual    bool
	CacheType     string
	CacheKey      int
	Language      string
	UseWireGuard  bool
	IsActive      bool
	Logo          string
	CreatedAt     time.Time
	UpdatedAt     time.Time

	offset int64
	length int
}

type IndexedStreamStore struct {
	mu    sync.RWMutex
	index map[string]StreamIndex
	rev   *Revision

	dataDir    string
	legacyPath string
	dataFile   []byte
	log        zerolog.Logger
}

func NewIndexedStreamStore(dataDir string, legacyGobPath string, log zerolog.Logger) *IndexedStreamStore {
	return &IndexedStreamStore{
		index:      make(map[string]StreamIndex),
		rev:        NewRevision(),
		dataDir:    dataDir,
		legacyPath: legacyGobPath,
		log:        log.With().Str("store", "stream_indexed").Logger(),
	}
}

func (s *IndexedStreamStore) ETag() string {
	return s.rev.ETag()
}

func (s *IndexedStreamStore) indexFromStream(st models.Stream) StreamIndex {
	return StreamIndex{
		ID:            st.ID,
		Name:          st.Name,
		Group:         st.Group,
		M3UAccountID:  st.M3UAccountID,
		SatIPSourceID: st.SatIPSourceID,
		HDHRSourceID:  st.HDHRSourceID,
		VODType:       st.VODType,
		VODSeries:     st.VODSeries,
		VODCollection: st.VODCollection,
		VODSeason:     st.VODSeason,
		VODSeasonName: st.VODSeasonName,
		VODEpisode:    st.VODEpisode,
		VODYear:       st.VODYear,
		TMDBID:        st.TMDBID,
		TMDBManual:    st.TMDBManual,
		CacheType:     st.CacheType,
		CacheKey:      st.CacheKey,
		Language:      st.Language,
		UseWireGuard:  st.UseWireGuard,
		IsActive:      st.IsActive,
		Logo:          st.Logo,
		CreatedAt:     st.CreatedAt,
		UpdatedAt:     st.UpdatedAt,
	}
}

func (s *IndexedStreamStore) summaryFromIndex(idx StreamIndex) models.StreamSummary {
	return models.StreamSummary{
		ID:            idx.ID,
		M3UAccountID:  idx.M3UAccountID,
		SatIPSourceID: idx.SatIPSourceID,
		HDHRSourceID:  idx.HDHRSourceID,
		Name:          idx.Name,
		Group:         idx.Group,
		Logo:          idx.Logo,
		VODType:       idx.VODType,
		VODSeries:     idx.VODSeries,
		VODSeason:     idx.VODSeason,
		VODSeasonName: idx.VODSeasonName,
		VODEpisode:    idx.VODEpisode,
		VODYear:       idx.VODYear,
		TMDBID:        idx.TMDBID,
	}
}

func (s *IndexedStreamStore) dataPath() string {
	return filepath.Join(s.dataDir, "streams.dat")
}

func (s *IndexedStreamStore) readStream(idx StreamIndex) (*models.Stream, error) {
	if idx.offset < 0 || idx.length == 0 || int64(len(s.dataFile)) < idx.offset+int64(idx.length) {
		return nil, fmt.Errorf("invalid offset for %s", idx.ID)
	}
	var st models.Stream
	if err := json.Unmarshal(s.dataFile[idx.offset:idx.offset+int64(idx.length)], &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func (s *IndexedStreamStore) rebuildDataFile() {
	var buf bytes.Buffer
	for id, idx := range s.index {
		st, err := s.readStream(idx)
		if err != nil {
			continue
		}
		data, _ := json.Marshal(st)
		offset := int64(buf.Len()) + 4
		binary.Write(&buf, binary.LittleEndian, int32(len(data)))
		buf.Write(data)
		idx.offset = offset
		idx.length = len(data)
		s.index[id] = idx
	}
	s.dataFile = buf.Bytes()
}

func (s *IndexedStreamStore) appendStream(st models.Stream) StreamIndex {
	data, _ := json.Marshal(st)

	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(data)))

	offset := int64(len(s.dataFile)) + 4
	s.dataFile = append(s.dataFile, lenBuf[:]...)
	s.dataFile = append(s.dataFile, data...)

	idx := s.indexFromStream(st)
	idx.offset = offset
	idx.length = len(data)
	return idx
}

func (s *IndexedStreamStore) List(_ context.Context) ([]models.Stream, error) {
	s.mu.RLock()
	indices := make([]StreamIndex, 0, len(s.index))
	for _, idx := range s.index {
		indices = append(indices, idx)
	}
	s.mu.RUnlock()

	sort.Slice(indices, func(i, j int) bool {
		return indices[i].CreatedAt.Before(indices[j].CreatedAt)
	})

	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]models.Stream, 0, len(indices))
	for _, idx := range indices {
		if st, err := s.readStream(idx); err == nil {
			items = append(items, *st)
		}
	}
	return items, nil
}

func (s *IndexedStreamStore) ListSummaries(_ context.Context) ([]models.StreamSummary, error) {
	s.mu.RLock()
	indices := make([]StreamIndex, 0, len(s.index))
	for _, idx := range s.index {
		indices = append(indices, idx)
	}
	s.mu.RUnlock()

	sort.Slice(indices, func(i, j int) bool {
		return indices[i].Name < indices[j].Name
	})

	summaries := make([]models.StreamSummary, len(indices))
	for i, idx := range indices {
		summaries[i] = s.summaryFromIndex(idx)
	}
	return summaries, nil
}

func (s *IndexedStreamStore) ListByAccountID(_ context.Context, accountID string) ([]models.Stream, error) {
	s.mu.RLock()
	var matching []StreamIndex
	for _, idx := range s.index {
		if idx.M3UAccountID == accountID {
			matching = append(matching, idx)
		}
	}

	sort.Slice(matching, func(i, j int) bool {
		return matching[i].CreatedAt.Before(matching[j].CreatedAt)
	})

	var items []models.Stream
	for _, idx := range matching {
		if st, err := s.readStream(idx); err == nil {
			items = append(items, *st)
		}
	}
	s.mu.RUnlock()
	return items, nil
}

func (s *IndexedStreamStore) ListBySatIPSourceID(_ context.Context, sourceID string) ([]models.Stream, error) {
	s.mu.RLock()
	var matching []StreamIndex
	for _, idx := range s.index {
		if idx.SatIPSourceID == sourceID {
			matching = append(matching, idx)
		}
	}

	sort.Slice(matching, func(i, j int) bool {
		return matching[i].CreatedAt.Before(matching[j].CreatedAt)
	})

	var items []models.Stream
	for _, idx := range matching {
		if st, err := s.readStream(idx); err == nil {
			items = append(items, *st)
		}
	}
	s.mu.RUnlock()
	return items, nil
}

func (s *IndexedStreamStore) ListByVODType(_ context.Context, vodType string) ([]models.Stream, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var matching []StreamIndex
	for _, idx := range s.index {
		if idx.VODType == vodType {
			matching = append(matching, idx)
		}
	}

	sort.Slice(matching, func(i, j int) bool {
		return matching[i].Name < matching[j].Name
	})

	var items []models.Stream
	for _, idx := range matching {
		if st, err := s.readStream(idx); err == nil {
			items = append(items, *st)
		}
	}
	return items, nil
}

func (s *IndexedStreamStore) GetByID(_ context.Context, id string) (*models.Stream, error) {
	s.mu.RLock()
	idx, exists := s.index[id]
	if !exists {
		s.mu.RUnlock()
		return nil, fmt.Errorf("stream not found: %s", id)
	}
	st, err := s.readStream(idx)
	s.mu.RUnlock()
	if err != nil {
		return nil, fmt.Errorf("stream not found: %s", id)
	}
	return st, nil
}

func (s *IndexedStreamStore) BulkUpsert(_ context.Context, streams []models.Stream) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for _, st := range streams {
		if existing, exists := s.index[st.ID]; exists {
			st.CreatedAt = existing.CreatedAt
			if existing.TMDBID > 0 {
				st.TMDBID = existing.TMDBID
				st.TMDBManual = existing.TMDBManual
			}
			if st.CacheType == "" && existing.CacheType != "" {
				st.CacheType = existing.CacheType
			}
			if st.Language == "" && existing.Language != "" {
				st.Language = existing.Language
			}
			if st.VODType == "" && existing.VODType != "" {
				st.VODType = existing.VODType
			}
		} else {
			st.CreatedAt = now
		}
		st.UpdatedAt = now
		s.index[st.ID] = s.appendStream(st)
	}
	s.rev.Bump()
	return nil
}

func (s *IndexedStreamStore) DeleteStaleByAccountID(_ context.Context, accountID string, keepIDs []string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	keep := make(map[string]struct{}, len(keepIDs))
	for _, id := range keepIDs {
		keep[id] = struct{}{}
	}

	var deleted []string
	for id, idx := range s.index {
		if idx.M3UAccountID != accountID {
			continue
		}
		if _, shouldKeep := keep[id]; !shouldKeep {
			delete(s.index, id)
			deleted = append(deleted, id)
		}
	}
	if len(deleted) > 0 {
		s.rev.Bump()
	}
	return deleted, nil
}

func (s *IndexedStreamStore) DeleteByAccountID(_ context.Context, accountID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, idx := range s.index {
		if idx.M3UAccountID == accountID {
			delete(s.index, id)
		}
	}
	s.rev.Bump()
	return nil
}

func (s *IndexedStreamStore) DeleteStaleBySatIPSourceID(_ context.Context, sourceID string, keepIDs []string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	keep := make(map[string]struct{}, len(keepIDs))
	for _, id := range keepIDs {
		keep[id] = struct{}{}
	}

	var deleted []string
	for id, idx := range s.index {
		if idx.SatIPSourceID != sourceID {
			continue
		}
		if _, shouldKeep := keep[id]; !shouldKeep {
			delete(s.index, id)
			deleted = append(deleted, id)
		}
	}
	if len(deleted) > 0 {
		s.rev.Bump()
	}
	return deleted, nil
}

func (s *IndexedStreamStore) DeleteBySatIPSourceID(_ context.Context, sourceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, idx := range s.index {
		if idx.SatIPSourceID == sourceID {
			delete(s.index, id)
		}
	}
	s.rev.Bump()
	return nil
}

func (s *IndexedStreamStore) DeleteOrphanedM3UStreams(_ context.Context, knownAccountIDs []string) ([]string, error) {
	known := make(map[string]struct{}, len(knownAccountIDs))
	for _, id := range knownAccountIDs {
		known[id] = struct{}{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var deleted []string
	for id, idx := range s.index {
		if idx.M3UAccountID == "" {
			continue
		}
		if _, ok := known[idx.M3UAccountID]; !ok {
			delete(s.index, id)
			deleted = append(deleted, id)
		}
	}
	if len(deleted) > 0 {
		s.rev.Bump()
	}
	return deleted, nil
}

func (s *IndexedStreamStore) DeleteOrphanedSatIPStreams(_ context.Context, knownSourceIDs []string) ([]string, error) {
	known := make(map[string]struct{}, len(knownSourceIDs))
	for _, id := range knownSourceIDs {
		known[id] = struct{}{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var deleted []string
	for id, idx := range s.index {
		if idx.SatIPSourceID == "" {
			continue
		}
		if _, ok := known[idx.SatIPSourceID]; !ok {
			delete(s.index, id)
			deleted = append(deleted, id)
		}
	}
	if len(deleted) > 0 {
		s.rev.Bump()
	}
	return deleted, nil
}

func (s *IndexedStreamStore) ListByHDHRSourceID(_ context.Context, sourceID string) ([]models.Stream, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var matching []StreamIndex
	for _, idx := range s.index {
		if idx.HDHRSourceID == sourceID {
			matching = append(matching, idx)
		}
	}

	sort.Slice(matching, func(i, j int) bool {
		return matching[i].CreatedAt.Before(matching[j].CreatedAt)
	})

	var items []models.Stream
	for _, idx := range matching {
		if st, err := s.readStream(idx); err == nil {
			items = append(items, *st)
		}
	}
	return items, nil
}

func (s *IndexedStreamStore) DeleteStaleByHDHRSourceID(_ context.Context, sourceID string, keepIDs []string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	keep := make(map[string]struct{}, len(keepIDs))
	for _, id := range keepIDs {
		keep[id] = struct{}{}
	}
	var deleted []string
	for id, idx := range s.index {
		if idx.HDHRSourceID != sourceID {
			continue
		}
		if _, shouldKeep := keep[id]; !shouldKeep {
			delete(s.index, id)
			deleted = append(deleted, id)
		}
	}
	if len(deleted) > 0 {
		s.rev.Bump()
	}
	return deleted, nil
}

func (s *IndexedStreamStore) DeleteByHDHRSourceID(_ context.Context, sourceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, idx := range s.index {
		if idx.HDHRSourceID == sourceID {
			delete(s.index, id)
		}
	}
	s.rev.Bump()
	return nil
}

func (s *IndexedStreamStore) DeleteOrphanedHDHRStreams(_ context.Context, knownSourceIDs []string) ([]string, error) {
	known := make(map[string]struct{}, len(knownSourceIDs))
	for _, id := range knownSourceIDs {
		known[id] = struct{}{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var deleted []string
	for id, idx := range s.index {
		if idx.HDHRSourceID == "" {
			continue
		}
		if _, ok := known[idx.HDHRSourceID]; !ok {
			delete(s.index, id)
			deleted = append(deleted, id)
		}
	}
	if len(deleted) > 0 {
		s.rev.Bump()
	}
	return deleted, nil
}

func (s *IndexedStreamStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	delete(s.index, id)
	s.rev.Bump()
	s.mu.Unlock()
	return nil
}

func (s *IndexedStreamStore) updateStreamField(id string, fn func(*models.Stream)) error {
	idx, ok := s.index[id]
	if !ok {
		return fmt.Errorf("stream not found: %s", id)
	}
	st, err := s.readStream(idx)
	if err != nil {
		return err
	}
	fn(st)
	st.UpdatedAt = time.Now()
	s.index[id] = s.appendStream(*st)
	return nil
}

func (s *IndexedStreamStore) UpdateTMDBID(_ context.Context, id string, tmdbID int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, ok := s.index[id]
	if !ok {
		return fmt.Errorf("stream not found: %s", id)
	}
	idx.TMDBID = tmdbID
	idx.UpdatedAt = time.Now()
	s.index[id] = idx
	s.updateStreamField(id, func(st *models.Stream) { st.TMDBID = tmdbID })
	return nil
}

func (s *IndexedStreamStore) SetTMDBManual(_ context.Context, id string, tmdbID int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, ok := s.index[id]
	if !ok {
		return fmt.Errorf("stream not found: %s", id)
	}
	idx.TMDBID = tmdbID
	idx.TMDBManual = true
	idx.UpdatedAt = time.Now()
	s.index[id] = idx
	s.updateStreamField(id, func(st *models.Stream) { st.TMDBID = tmdbID; st.TMDBManual = true })
	return nil
}

func (s *IndexedStreamStore) ClearAutoTMDBByAccountID(_ context.Context, accountID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, idx := range s.index {
		if idx.M3UAccountID == accountID && idx.TMDBID > 0 && !idx.TMDBManual {
			idx.TMDBID = 0
			s.index[id] = idx
			s.updateStreamField(id, func(st *models.Stream) { st.TMDBID = 0 })
		}
	}
	return nil
}

func (s *IndexedStreamStore) UpdateWireGuardByAccountID(_ context.Context, accountID string, useWireGuard bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, idx := range s.index {
		if idx.M3UAccountID == accountID {
			idx.UseWireGuard = useWireGuard
			s.index[id] = idx
			s.updateStreamField(id, func(st *models.Stream) { st.UseWireGuard = useWireGuard })
		}
	}
	return nil
}

func (s *IndexedStreamStore) UpdateStreamProbeData(_ context.Context, id string, duration float64, vcodec, acodec string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.index[id]; !ok {
		return fmt.Errorf("stream not found: %s", id)
	}
	s.updateStreamField(id, func(st *models.Stream) {
		if duration > 0 && st.VODDuration == 0 {
			st.VODDuration = duration
		}
		if vcodec != "" && st.VODVCodec == "" {
			st.VODVCodec = vcodec
		}
		if acodec != "" && st.VODACodec == "" {
			st.VODACodec = acodec
		}
	})
	return nil
}

func (s *IndexedStreamStore) Clear() error {
	s.mu.Lock()
	s.index = make(map[string]StreamIndex)
	s.dataFile = nil
	s.rev.Bump()
	s.mu.Unlock()
	return nil
}

func (s *IndexedStreamStore) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var buf bytes.Buffer
	for id, idx := range s.index {
		st, err := s.readStream(idx)
		if err != nil {
			continue
		}
		data, _ := json.Marshal(st)
		newOffset := int64(buf.Len()) + 4
		binary.Write(&buf, binary.LittleEndian, int32(len(data)))
		buf.Write(data)
		idx.offset = newOffset
		idx.length = len(data)
		s.index[id] = idx
	}
	s.dataFile = buf.Bytes()

	os.MkdirAll(s.dataDir, 0755)
	tmp := s.dataPath() + ".tmp"
	if err := os.WriteFile(tmp, s.dataFile, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, s.dataPath())
}

func (s *IndexedStreamStore) Load() error {
	data, err := os.ReadFile(s.dataPath())
	if err == nil {
		s.mu.Lock()
		s.dataFile = data
		s.index = make(map[string]StreamIndex)
		offset := int64(0)
		for offset+4 <= int64(len(data)) {
			length := int(binary.LittleEndian.Uint32(data[offset : offset+4]))
			recStart := offset + 4
			if recStart+int64(length) > int64(len(data)) {
				break
			}
			var st models.Stream
			if err := json.Unmarshal(data[recStart:recStart+int64(length)], &st); err == nil {
				idx := s.indexFromStream(st)
				idx.offset = recStart
				idx.length = length
				s.index[st.ID] = idx
			}
			offset = recStart + int64(length)
		}
		s.mu.Unlock()
		s.log.Info().Int("count", len(s.index)).Msg("loaded stream index")
		return nil
	}

	if s.legacyPath == "" {
		return nil
	}
	f, err := os.Open(s.legacyPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	var legacy map[string]models.Stream
	if err := gob.NewDecoder(f).Decode(&legacy); err != nil {
		s.log.Error().Err(err).Msg("failed to decode legacy gob")
		return nil
	}

	s.mu.Lock()
	s.index = make(map[string]StreamIndex, len(legacy))
	s.dataFile = nil
	for _, st := range legacy {
		s.index[st.ID] = s.appendStream(st)
	}
	s.mu.Unlock()

	s.Save()
	s.log.Info().Int("count", len(legacy)).Msg("migrated legacy gob to indexed store")
	return nil
}
