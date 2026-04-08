package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type FavoriteStore interface {
	ListByUser(ctx context.Context, userID string) ([]string, error)
	IsFavorite(ctx context.Context, userID, channelID string) bool
	Add(ctx context.Context, userID, channelID string) error
	Remove(ctx context.Context, userID, channelID string) error
}

type FavoriteStoreImpl struct {
	mu      sync.RWMutex
	cache   map[string][]string
	baseDir string
}

func NewFavoriteStore(baseDir string) *FavoriteStoreImpl {
	return &FavoriteStoreImpl{
		cache:   make(map[string][]string),
		baseDir: baseDir,
	}
}

func (s *FavoriteStoreImpl) filePath(userID string) string {
	return filepath.Join(s.baseDir, "users", userID, "favorites.json")
}

func (s *FavoriteStoreImpl) load(userID string) []string {
	if cached, ok := s.cache[userID]; ok {
		return cached
	}
	data, err := os.ReadFile(s.filePath(userID))
	if err != nil {
		s.cache[userID] = []string{}
		return s.cache[userID]
	}
	var ids []string
	json.Unmarshal(data, &ids)
	if ids == nil {
		ids = []string{}
	}
	s.cache[userID] = ids
	return ids
}

func (s *FavoriteStoreImpl) save(userID string) error {
	path := s.filePath(userID)
	os.MkdirAll(filepath.Dir(path), 0755)
	data, err := json.MarshalIndent(s.cache[userID], "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func (s *FavoriteStoreImpl) ListByUser(_ context.Context, userID string) ([]string, error) {
	s.mu.Lock()
	ids := s.load(userID)
	s.mu.Unlock()

	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]string, len(ids))
	copy(result, ids)
	return result, nil
}

func (s *FavoriteStoreImpl) IsFavorite(_ context.Context, userID, channelID string) bool {
	s.mu.Lock()
	ids := s.load(userID)
	s.mu.Unlock()

	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, id := range ids {
		if id == channelID {
			return true
		}
	}
	return false
}

func (s *FavoriteStoreImpl) Add(_ context.Context, userID, channelID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := s.load(userID)
	for _, id := range ids {
		if id == channelID {
			return nil
		}
	}
	s.cache[userID] = append(ids, channelID)
	return s.save(userID)
}

func (s *FavoriteStoreImpl) Remove(_ context.Context, userID, channelID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := s.load(userID)
	for i, id := range ids {
		if id == channelID {
			s.cache[userID] = append(ids[:i], ids[i+1:]...)
			return s.save(userID)
		}
	}
	return nil
}
