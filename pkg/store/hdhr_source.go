package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/gavinmcnair/tvproxy/pkg/models"
)

type HDHRSourceStore interface {
	Create(ctx context.Context, source *models.HDHRSource) error
	GetByID(ctx context.Context, id string) (*models.HDHRSource, error)
	List(ctx context.Context) ([]models.HDHRSource, error)
	Update(ctx context.Context, source *models.HDHRSource) error
	Delete(ctx context.Context, id string) error
	UpdateLastScanned(ctx context.Context, id string, lastScanned time.Time) error
	UpdateLastError(ctx context.Context, id, lastError string) error
	UpdateStreamCount(ctx context.Context, id string, count int) error
	Load() error
}

type HDHRSourceStoreImpl struct {
	filePath string
	sources  []models.HDHRSource
	mu       sync.RWMutex
}

func NewHDHRSourceStore(filePath string) *HDHRSourceStoreImpl {
	return &HDHRSourceStoreImpl{filePath: filePath}
}

func (s *HDHRSourceStoreImpl) Load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading hdhr sources file: %w", err)
	}

	var sources []models.HDHRSource
	if err := json.Unmarshal(data, &sources); err != nil {
		return fmt.Errorf("parsing hdhr sources file: %w", err)
	}

	s.mu.Lock()
	s.sources = sources
	s.mu.Unlock()
	return nil
}

func (s *HDHRSourceStoreImpl) save() error {
	s.mu.RLock()
	snap := make([]models.HDHRSource, len(s.sources))
	copy(snap, s.sources)
	s.mu.RUnlock()

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling hdhr sources: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(s.filePath), 0755); err != nil {
		return fmt.Errorf("creating data directory: %w", err)
	}

	tmp := s.filePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("writing hdhr sources temp file: %w", err)
	}

	if err := os.Rename(tmp, s.filePath); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("renaming hdhr sources temp file: %w", err)
	}
	return nil
}

func (s *HDHRSourceStoreImpl) Create(_ context.Context, source *models.HDHRSource) error {
	now := time.Now()
	source.ID = uuid.New().String()
	source.CreatedAt = now
	source.UpdatedAt = now

	s.mu.Lock()
	for _, src := range s.sources {
		if src.Name == source.Name {
			s.mu.Unlock()
			return fmt.Errorf("hdhr source with name %q already exists", source.Name)
		}
	}
	s.sources = append(s.sources, *source)
	s.mu.Unlock()

	return s.save()
}

func (s *HDHRSourceStoreImpl) GetByID(_ context.Context, id string) (*models.HDHRSource, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, src := range s.sources {
		if src.ID == id {
			c := src
			return &c, nil
		}
	}
	return nil, fmt.Errorf("hdhr source not found: %s", id)
}

func (s *HDHRSourceStoreImpl) List(_ context.Context) ([]models.HDHRSource, error) {
	s.mu.RLock()
	result := make([]models.HDHRSource, len(s.sources))
	copy(result, s.sources)
	s.mu.RUnlock()
	return result, nil
}

func (s *HDHRSourceStoreImpl) Update(_ context.Context, source *models.HDHRSource) error {
	s.mu.Lock()
	found := false
	for i, src := range s.sources {
		if src.ID == source.ID {
			source.UpdatedAt = time.Now()
			source.CreatedAt = src.CreatedAt
			s.sources[i] = *source
			found = true
			break
		}
	}
	s.mu.Unlock()

	if !found {
		return fmt.Errorf("hdhr source not found: %s", source.ID)
	}
	return s.save()
}

func (s *HDHRSourceStoreImpl) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	newSources := make([]models.HDHRSource, 0, len(s.sources))
	for _, src := range s.sources {
		if src.ID != id {
			newSources = append(newSources, src)
		}
	}
	s.sources = newSources
	s.mu.Unlock()

	return s.save()
}

func (s *HDHRSourceStoreImpl) UpdateLastScanned(_ context.Context, id string, lastScanned time.Time) error {
	s.mu.Lock()
	for i, src := range s.sources {
		if src.ID == id {
			s.sources[i].LastScanned = &lastScanned
			s.sources[i].UpdatedAt = time.Now()
			break
		}
	}
	s.mu.Unlock()
	return s.save()
}

func (s *HDHRSourceStoreImpl) UpdateLastError(_ context.Context, id, lastError string) error {
	s.mu.Lock()
	for i, src := range s.sources {
		if src.ID == id {
			s.sources[i].LastError = lastError
			s.sources[i].UpdatedAt = time.Now()
			break
		}
	}
	s.mu.Unlock()
	return s.save()
}

func (s *HDHRSourceStoreImpl) UpdateStreamCount(_ context.Context, id string, count int) error {
	s.mu.Lock()
	for i, src := range s.sources {
		if src.ID == id {
			s.sources[i].StreamCount = count
			s.sources[i].UpdatedAt = time.Now()
			break
		}
	}
	s.mu.Unlock()
	return s.save()
}

