package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/gavinmcnair/tvproxy/pkg/models"
)

var channelNamespace = uuid.MustParse("c8a5e2b1-7f3d-4a96-b8e1-d2c4f6a89012")

func deterministicChannelID(name, userID string) string {
	return uuid.NewSHA1(channelNamespace, []byte(name+":"+userID)).String()
}

type channelEntry struct {
	Channel   models.Channel        `json:"channel"`
	StreamIDs []models.ChannelStream `json:"stream_ids,omitempty"`
}

type ChannelStore interface {
	Create(ctx context.Context, channel *models.Channel) error
	GetByID(ctx context.Context, id string) (*models.Channel, error)
	GetByIDForUser(ctx context.Context, id, userID string) (*models.Channel, error)
	GetByIDLite(ctx context.Context, id string) (*models.Channel, error)
	List(ctx context.Context) ([]models.Channel, error)
	ListByUserID(ctx context.Context, userID string) ([]models.Channel, error)
	Update(ctx context.Context, channel *models.Channel) error
	UpdateForUser(ctx context.Context, channel *models.Channel, userID string) error
	Delete(ctx context.Context, id string) error
	DeleteForUser(ctx context.Context, id, userID string) error
	AssignStreams(ctx context.Context, channelID string, streamIDs []string, priorities []int) error
	GetStreams(ctx context.Context, channelID string) ([]models.ChannelStream, error)
	IncrementFailCount(ctx context.Context, id string) error
	ResetFailCount(ctx context.Context, id string) error
	RemoveStreamMappings(ctx context.Context, streamIDs []string) error
}

type ChannelStoreImpl struct {
	filePath string
	channels []channelEntry
	mu       sync.RWMutex
}

func NewChannelStore(filePath string) *ChannelStoreImpl {
	return &ChannelStoreImpl{filePath: filePath}
}

func (s *ChannelStoreImpl) Load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return json.Unmarshal(data, &s.channels)
}

func (s *ChannelStoreImpl) save() error {
	data, err := json.MarshalIndent(s.channels, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, data, 0644)
}

func (s *ChannelStoreImpl) findIndex(id string) int {
	for i := range s.channels {
		if s.channels[i].Channel.ID == id {
			return i
		}
	}
	return -1
}

func (s *ChannelStoreImpl) Create(_ context.Context, channel *models.Channel) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	channel.ID = deterministicChannelID(channel.Name, channel.UserID)
	now := time.Now()
	channel.CreatedAt = now
	channel.UpdatedAt = now
	s.channels = append(s.channels, channelEntry{Channel: *channel})
	return s.save()
}

func (s *ChannelStoreImpl) GetByID(_ context.Context, id string) (*models.Channel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	idx := s.findIndex(id)
	if idx < 0 {
		return nil, fmt.Errorf("channel not found")
	}
	ch := s.channels[idx].Channel
	return &ch, nil
}

func (s *ChannelStoreImpl) GetByIDForUser(_ context.Context, id, userID string) (*models.Channel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	idx := s.findIndex(id)
	if idx < 0 || s.channels[idx].Channel.UserID != userID {
		return nil, fmt.Errorf("channel not found")
	}
	ch := s.channels[idx].Channel
	return &ch, nil
}

func (s *ChannelStoreImpl) GetByIDLite(_ context.Context, id string) (*models.Channel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	idx := s.findIndex(id)
	if idx < 0 {
		return nil, fmt.Errorf("channel not found")
	}
	ch := s.channels[idx].Channel
	return &models.Channel{ID: ch.ID, Name: ch.Name, IsEnabled: ch.IsEnabled, StreamProfileID: ch.StreamProfileID}, nil
}

func (s *ChannelStoreImpl) List(_ context.Context) ([]models.Channel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]models.Channel, len(s.channels))
	for i := range s.channels {
		result[i] = s.channels[i].Channel
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})
	return result, nil
}

func (s *ChannelStoreImpl) ListByUserID(_ context.Context, userID string) ([]models.Channel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []models.Channel
	for i := range s.channels {
		if s.channels[i].Channel.UserID == userID {
			result = append(result, s.channels[i].Channel)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})
	return result, nil
}

func (s *ChannelStoreImpl) Update(_ context.Context, channel *models.Channel) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.findIndex(channel.ID)
	if idx < 0 {
		return fmt.Errorf("channel not found")
	}
	channel.UpdatedAt = time.Now()
	channel.CreatedAt = s.channels[idx].Channel.CreatedAt
	s.channels[idx].Channel = *channel
	return s.save()
}

func (s *ChannelStoreImpl) UpdateForUser(_ context.Context, channel *models.Channel, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.findIndex(channel.ID)
	if idx < 0 || s.channels[idx].Channel.UserID != userID {
		return fmt.Errorf("channel not found")
	}
	channel.UpdatedAt = time.Now()
	channel.CreatedAt = s.channels[idx].Channel.CreatedAt
	s.channels[idx].Channel = *channel
	return s.save()
}

func (s *ChannelStoreImpl) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.findIndex(id)
	if idx >= 0 {
		s.channels = append(s.channels[:idx], s.channels[idx+1:]...)
		return s.save()
	}
	return nil
}

func (s *ChannelStoreImpl) DeleteForUser(_ context.Context, id, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.findIndex(id)
	if idx >= 0 && s.channels[idx].Channel.UserID == userID {
		s.channels = append(s.channels[:idx], s.channels[idx+1:]...)
		return s.save()
	}
	return nil
}

func (s *ChannelStoreImpl) AssignStreams(_ context.Context, channelID string, streamIDs []string, priorities []int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.findIndex(channelID)
	if idx < 0 {
		return fmt.Errorf("channel not found")
	}
	cs := make([]models.ChannelStream, len(streamIDs))
	for i := range streamIDs {
		cs[i] = models.ChannelStream{
			ID:        uuid.New().String(),
			ChannelID: channelID,
			StreamID:  streamIDs[i],
			Priority:  priorities[i],
		}
	}
	s.channels[idx].StreamIDs = cs
	return s.save()
}

func (s *ChannelStoreImpl) GetStreams(_ context.Context, channelID string) ([]models.ChannelStream, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	idx := s.findIndex(channelID)
	if idx < 0 {
		return nil, fmt.Errorf("channel not found")
	}
	result := make([]models.ChannelStream, len(s.channels[idx].StreamIDs))
	copy(result, s.channels[idx].StreamIDs)
	sort.Slice(result, func(i, j int) bool { return result[i].Priority < result[j].Priority })
	return result, nil
}

func (s *ChannelStoreImpl) IncrementFailCount(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.findIndex(id)
	if idx < 0 {
		return fmt.Errorf("channel not found")
	}
	s.channels[idx].Channel.FailCount++
	return s.save()
}

func (s *ChannelStoreImpl) ResetFailCount(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.findIndex(id)
	if idx < 0 {
		return fmt.Errorf("channel not found")
	}
	s.channels[idx].Channel.FailCount = 0
	return s.save()
}

func (s *ChannelStoreImpl) RemoveStreamMappings(_ context.Context, streamIDs []string) error {
	if len(streamIDs) == 0 {
		return nil
	}
	removeSet := make(map[string]bool, len(streamIDs))
	for _, id := range streamIDs {
		removeSet[id] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for i := range s.channels {
		var kept []models.ChannelStream
		for _, cs := range s.channels[i].StreamIDs {
			if !removeSet[cs.StreamID] {
				kept = append(kept, cs)
			} else {
				changed = true
			}
		}
		s.channels[i].StreamIDs = kept
	}
	if changed {
		return s.save()
	}
	return nil
}

func (s *ChannelStoreImpl) ClearAndSave() error {
	s.mu.Lock()
	s.channels = nil
	s.mu.Unlock()
	return s.save()
}
