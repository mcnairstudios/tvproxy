package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/gavinmcnair/tvproxy/pkg/models"
)

type ChannelGroupStore interface {
	Create(ctx context.Context, group *models.ChannelGroup) error
	GetByID(ctx context.Context, id string) (*models.ChannelGroup, error)
	GetByName(ctx context.Context, name string) (*models.ChannelGroup, error)
	GetByIDForUser(ctx context.Context, id, userID string) (*models.ChannelGroup, error)
	List(ctx context.Context) ([]models.ChannelGroup, error)
	ListByUserID(ctx context.Context, userID string) ([]models.ChannelGroup, error)
	Update(ctx context.Context, group *models.ChannelGroup) error
	UpdateForUser(ctx context.Context, group *models.ChannelGroup, userID string) error
	Delete(ctx context.Context, id string) error
	DeleteForUser(ctx context.Context, id, userID string) error
}

type ChannelGroupStoreImpl struct {
	filePath string
	groups   []models.ChannelGroup
	mu       sync.RWMutex
}

func NewChannelGroupStore(filePath string) *ChannelGroupStoreImpl {
	return &ChannelGroupStoreImpl{filePath: filePath}
}

func (s *ChannelGroupStoreImpl) Load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return json.Unmarshal(data, &s.groups)
}

func (s *ChannelGroupStoreImpl) save() error {
	data, err := json.MarshalIndent(s.groups, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, data, 0644)
}

func (s *ChannelGroupStoreImpl) Create(_ context.Context, group *models.ChannelGroup) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if group.ID == "" {
		group.ID = uuid.New().String()
	}
	now := time.Now()
	group.CreatedAt = now
	group.UpdatedAt = now
	s.groups = append(s.groups, *group)
	return s.save()
}

func (s *ChannelGroupStoreImpl) GetByID(_ context.Context, id string) (*models.ChannelGroup, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.groups {
		if s.groups[i].ID == id {
			g := s.groups[i]
			return &g, nil
		}
	}
	return nil, fmt.Errorf("channel group not found")
}

func (s *ChannelGroupStoreImpl) GetByName(_ context.Context, name string) (*models.ChannelGroup, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.groups {
		if s.groups[i].Name == name {
			g := s.groups[i]
			return &g, nil
		}
	}
	return nil, fmt.Errorf("channel group not found")
}

func (s *ChannelGroupStoreImpl) GetByIDForUser(_ context.Context, id, userID string) (*models.ChannelGroup, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.groups {
		if s.groups[i].ID == id && s.groups[i].UserID == userID {
			g := s.groups[i]
			return &g, nil
		}
	}
	return nil, fmt.Errorf("channel group not found")
}

func (s *ChannelGroupStoreImpl) sorted(groups []models.ChannelGroup) []models.ChannelGroup {
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].SortOrder != groups[j].SortOrder {
			return groups[i].SortOrder < groups[j].SortOrder
		}
		return groups[i].Name < groups[j].Name
	})
	return groups
}

func (s *ChannelGroupStoreImpl) List(_ context.Context) ([]models.ChannelGroup, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]models.ChannelGroup, len(s.groups))
	copy(result, s.groups)
	return s.sorted(result), nil
}

func (s *ChannelGroupStoreImpl) ListByUserID(_ context.Context, userID string) ([]models.ChannelGroup, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []models.ChannelGroup
	for _, g := range s.groups {
		if g.UserID == userID {
			result = append(result, g)
		}
	}
	return s.sorted(result), nil
}

func (s *ChannelGroupStoreImpl) Update(_ context.Context, group *models.ChannelGroup) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.groups {
		if s.groups[i].ID == group.ID {
			group.UpdatedAt = time.Now()
			group.CreatedAt = s.groups[i].CreatedAt
			s.groups[i] = *group
			return s.save()
		}
	}
	return fmt.Errorf("channel group not found")
}

func (s *ChannelGroupStoreImpl) UpdateForUser(_ context.Context, group *models.ChannelGroup, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.groups {
		if s.groups[i].ID == group.ID && s.groups[i].UserID == userID {
			group.UpdatedAt = time.Now()
			group.CreatedAt = s.groups[i].CreatedAt
			s.groups[i] = *group
			return s.save()
		}
	}
	return fmt.Errorf("channel group not found")
}

func (s *ChannelGroupStoreImpl) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.groups {
		if s.groups[i].ID == id {
			s.groups = append(s.groups[:i], s.groups[i+1:]...)
			return s.save()
		}
	}
	return nil
}

func (s *ChannelGroupStoreImpl) DeleteForUser(_ context.Context, id, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.groups {
		if s.groups[i].ID == id && s.groups[i].UserID == userID {
			s.groups = append(s.groups[:i], s.groups[i+1:]...)
			return s.save()
		}
	}
	return nil
}

func (s *ChannelGroupStoreImpl) ClearAndSave() error {
	s.mu.Lock()
	s.groups = nil
	s.mu.Unlock()
	return s.save()
}
