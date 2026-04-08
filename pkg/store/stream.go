package store

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/models"
)

type StreamStoreImpl struct {
	mu    sync.RWMutex
	items map[string]models.Stream
	rev   *Revision

	path string
	log  zerolog.Logger
}

func NewStreamStore(path string, log zerolog.Logger) *StreamStoreImpl {
	return &StreamStoreImpl{
		items: make(map[string]models.Stream),
		rev:   NewRevision(),
		path:  path,
		log:   log.With().Str("store", "stream").Logger(),
	}
}

func (s *StreamStoreImpl) ETag() string {
	return s.rev.ETag()
}

func (s *StreamStoreImpl) List(_ context.Context) ([]models.Stream, error) {
	s.mu.RLock()
	items := make([]models.Stream, 0, len(s.items))
	for _, v := range s.items {
		items = append(items, v)
	}
	s.mu.RUnlock()

	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items, nil
}

func (s *StreamStoreImpl) ListSummaries(_ context.Context) ([]models.StreamSummary, error) {
	s.mu.RLock()
	items := make([]models.Stream, 0, len(s.items))
	for _, v := range s.items {
		items = append(items, v)
	}
	s.mu.RUnlock()

	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	summaries := make([]models.StreamSummary, len(items))
	for i, st := range items {
		summaries[i] = models.StreamSummary{
			ID:            st.ID,
			M3UAccountID:  st.M3UAccountID,
			SatIPSourceID: st.SatIPSourceID,
			Name:          st.Name,
			Group:         st.Group,
			Logo:          st.Logo,
			VODType:       st.VODType,
			VODSeries:     st.VODSeries,
			VODSeason:     st.VODSeason,
			VODSeasonName: st.VODSeasonName,
			VODEpisode:    st.VODEpisode,
			VODYear:       st.VODYear,
		}
	}
	return summaries, nil
}

func (s *StreamStoreImpl) ListByAccountID(_ context.Context, accountID string) ([]models.Stream, error) {
	s.mu.RLock()
	var items []models.Stream
	for _, v := range s.items {
		if v.M3UAccountID == accountID {
			items = append(items, v)
		}
	}
	s.mu.RUnlock()

	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items, nil
}

func (s *StreamStoreImpl) GetByID(_ context.Context, id string) (*models.Stream, error) {
	s.mu.RLock()
	st, ok := s.items[id]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("stream not found: %s", id)
	}
	return &st, nil
}

func (s *StreamStoreImpl) BulkUpsert(_ context.Context, streams []models.Stream) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for _, st := range streams {
		if existing, exists := s.items[st.ID]; exists {
			st.CreatedAt = existing.CreatedAt
		} else {
			st.CreatedAt = now
		}
		st.UpdatedAt = now
		s.items[st.ID] = st
	}
	s.rev.Bump()
	return nil
}

func (s *StreamStoreImpl) DeleteStaleByAccountID(_ context.Context, accountID string, keepIDs []string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	keep := make(map[string]struct{}, len(keepIDs))
	for _, id := range keepIDs {
		keep[id] = struct{}{}
	}

	var deleted []string
	for id, st := range s.items {
		if st.M3UAccountID != accountID {
			continue
		}
		if _, shouldKeep := keep[id]; !shouldKeep {
			delete(s.items, id)
			deleted = append(deleted, id)
		}
	}
	if len(deleted) > 0 {
		s.rev.Bump()
	}
	return deleted, nil
}

func (s *StreamStoreImpl) DeleteByAccountID(_ context.Context, accountID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, st := range s.items {
		if st.M3UAccountID == accountID {
			delete(s.items, id)
		}
	}
	s.rev.Bump()
	return nil
}

func (s *StreamStoreImpl) ListBySatIPSourceID(_ context.Context, sourceID string) ([]models.Stream, error) {
	s.mu.RLock()
	var items []models.Stream
	for _, v := range s.items {
		if v.SatIPSourceID == sourceID {
			items = append(items, v)
		}
	}
	s.mu.RUnlock()
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items, nil
}

func (s *StreamStoreImpl) DeleteBySatIPSourceID(_ context.Context, sourceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, st := range s.items {
		if st.SatIPSourceID == sourceID {
			delete(s.items, id)
		}
	}
	s.rev.Bump()
	return nil
}

func (s *StreamStoreImpl) DeleteStaleBySatIPSourceID(_ context.Context, sourceID string, keepIDs []string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	keep := make(map[string]struct{}, len(keepIDs))
	for _, id := range keepIDs {
		keep[id] = struct{}{}
	}
	var deleted []string
	for id, st := range s.items {
		if st.SatIPSourceID != sourceID {
			continue
		}
		if _, shouldKeep := keep[id]; !shouldKeep {
			delete(s.items, id)
			deleted = append(deleted, id)
		}
	}
	if len(deleted) > 0 {
		s.rev.Bump()
	}
	return deleted, nil
}

func (s *StreamStoreImpl) DeleteOrphanedM3UStreams(_ context.Context, knownAccountIDs []string) ([]string, error) {
	known := make(map[string]struct{}, len(knownAccountIDs))
	for _, id := range knownAccountIDs {
		known[id] = struct{}{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var deleted []string
	for id, st := range s.items {
		if st.M3UAccountID == "" {
			continue
		}
		if _, ok := known[st.M3UAccountID]; !ok {
			delete(s.items, id)
			deleted = append(deleted, id)
		}
	}
	if len(deleted) > 0 {
		s.rev.Bump()
	}
	return deleted, nil
}

func (s *StreamStoreImpl) DeleteOrphanedSatIPStreams(_ context.Context, knownSourceIDs []string) ([]string, error) {
	known := make(map[string]struct{}, len(knownSourceIDs))
	for _, id := range knownSourceIDs {
		known[id] = struct{}{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var deleted []string
	for id, st := range s.items {
		if st.SatIPSourceID == "" {
			continue
		}
		if _, ok := known[st.SatIPSourceID]; !ok {
			delete(s.items, id)
			deleted = append(deleted, id)
		}
	}
	if len(deleted) > 0 {
		s.rev.Bump()
	}
	return deleted, nil
}

func (s *StreamStoreImpl) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	delete(s.items, id)
	s.rev.Bump()
	s.mu.Unlock()
	return nil
}

func (s *StreamStoreImpl) UpdateWireGuardByAccountID(_ context.Context, accountID string, useWireGuard bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, st := range s.items {
		if st.M3UAccountID == accountID {
			st.UseWireGuard = useWireGuard
			s.items[id] = st
		}
	}
	return nil
}

func (s *StreamStoreImpl) UpdateTMDBID(_ context.Context, id string, tmdbID int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.items[id]
	if !ok {
		return fmt.Errorf("stream not found: %s", id)
	}
	st.TMDBID = tmdbID
	st.UpdatedAt = time.Now()
	s.items[id] = st
	return nil
}

func (s *StreamStoreImpl) Clear() error {
	s.mu.Lock()
	s.items = make(map[string]models.Stream)
	s.rev.Bump()
	s.mu.Unlock()
	return nil
}

func (s *StreamStoreImpl) Save() error {
	s.mu.RLock()
	snap := make(map[string]models.Stream, len(s.items))
	for k, v := range s.items {
		snap[k] = v
	}
	s.mu.RUnlock()

	if err := saveGob(s.path, snap); err != nil {
		return fmt.Errorf("saving stream store: %w", err)
	}
	s.log.Info().Int("count", len(snap)).Msg("saved stream store")
	return nil
}

func (s *StreamStoreImpl) Load() error {
	var items map[string]models.Stream
	if err := loadGob(s.path, &items); err != nil {
		if os.IsNotExist(err) {
			s.log.Info().Msg("no stream store file found, starting empty")
			return nil
		}
		return fmt.Errorf("loading stream store: %w", err)
	}

	s.mu.Lock()
	s.items = items
	s.rev.Bump()
	s.mu.Unlock()

	s.log.Info().Int("count", len(items)).Msg("loaded stream store")
	return nil
}
