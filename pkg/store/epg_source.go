package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/gavinmcnair/tvproxy/pkg/models"
)

type EPGSourceStore interface {
	Create(ctx context.Context, source *models.EPGSource) error
	GetByID(ctx context.Context, id string) (*models.EPGSource, error)
	List(ctx context.Context) ([]models.EPGSource, error)
	Update(ctx context.Context, source *models.EPGSource) error
	Delete(ctx context.Context, id string) error
	UpdateLastRefreshed(ctx context.Context, id string, lastRefreshed time.Time) error
	UpdateLastError(ctx context.Context, id, lastError string) error
	UpdateCounts(ctx context.Context, id string, channelCount, programCount int) error
}

type EPGSourceStoreImpl struct {
	filePath string
	sources  []models.EPGSource
	mu       sync.RWMutex
}

func NewEPGSourceStore(filePath string) *EPGSourceStoreImpl {
	return &EPGSourceStoreImpl{filePath: filePath}
}

func (s *EPGSourceStoreImpl) Load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return json.Unmarshal(data, &s.sources)
}

func (s *EPGSourceStoreImpl) save() error {
	data, err := json.MarshalIndent(s.sources, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, data, 0644)
}

func (s *EPGSourceStoreImpl) Create(_ context.Context, source *models.EPGSource) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if source.ID == "" {
		source.ID = uuid.New().String()
	}
	now := time.Now()
	source.CreatedAt = now
	source.UpdatedAt = now
	s.sources = append(s.sources, *source)
	return s.save()
}

func (s *EPGSourceStoreImpl) GetByID(_ context.Context, id string) (*models.EPGSource, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.sources {
		if s.sources[i].ID == id {
			src := s.sources[i]
			return &src, nil
		}
	}
	return nil, fmt.Errorf("epg source not found")
}

func (s *EPGSourceStoreImpl) List(_ context.Context) ([]models.EPGSource, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]models.EPGSource, len(s.sources))
	copy(result, s.sources)
	return result, nil
}

func (s *EPGSourceStoreImpl) Update(_ context.Context, source *models.EPGSource) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.sources {
		if s.sources[i].ID == source.ID {
			source.UpdatedAt = time.Now()
			source.CreatedAt = s.sources[i].CreatedAt
			s.sources[i] = *source
			return s.save()
		}
	}
	return fmt.Errorf("epg source not found")
}

func (s *EPGSourceStoreImpl) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.sources {
		if s.sources[i].ID == id {
			s.sources = append(s.sources[:i], s.sources[i+1:]...)
			return s.save()
		}
	}
	return nil
}

func (s *EPGSourceStoreImpl) UpdateLastRefreshed(_ context.Context, id string, lastRefreshed time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.sources {
		if s.sources[i].ID == id {
			s.sources[i].LastRefreshed = &lastRefreshed
			s.sources[i].UpdatedAt = time.Now()
			return s.save()
		}
	}
	return fmt.Errorf("epg source not found")
}

func (s *EPGSourceStoreImpl) UpdateLastError(_ context.Context, id, lastError string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.sources {
		if s.sources[i].ID == id {
			s.sources[i].LastError = lastError
			s.sources[i].UpdatedAt = time.Now()
			return s.save()
		}
	}
	return fmt.Errorf("epg source not found")
}

func (s *EPGSourceStoreImpl) UpdateCounts(_ context.Context, id string, channelCount, programCount int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.sources {
		if s.sources[i].ID == id {
			s.sources[i].ChannelCount = channelCount
			s.sources[i].ProgramCount = programCount
			s.sources[i].UpdatedAt = time.Now()
			return s.save()
		}
	}
	return fmt.Errorf("epg source not found")
}
