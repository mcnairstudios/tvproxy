package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"

	"github.com/google/uuid"

	"github.com/gavinmcnair/tvproxy/pkg/models"
)

type ClientStore interface {
	List(ctx context.Context) ([]models.Client, error)
	ListEnabledWithRules(ctx context.Context) ([]models.Client, error)
	GetByID(ctx context.Context, id string) (*models.Client, error)
	Create(ctx context.Context, client *models.Client) error
	Update(ctx context.Context, client *models.Client) error
	Delete(ctx context.Context, id string) error
	SetMatchRules(ctx context.Context, clientID string, rules []models.ClientMatchRule) error
	IsStreamProfileReferenced(ctx context.Context, profileID string) (bool, error)
	Clear()
	AddDirect(client *models.Client)
	Save() error
}

type ClientStoreImpl struct {
	filePath string
	clients  []models.Client
	mu       sync.RWMutex
}

func NewClientStore(filePath string) *ClientStoreImpl {
	return &ClientStoreImpl{
		filePath: filePath,
	}
}

func (s *ClientStoreImpl) Load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading client store: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return json.Unmarshal(data, &s.clients)
}

func (s *ClientStoreImpl) Save() error {
	s.mu.RLock()
	data, err := json.MarshalIndent(s.clients, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, data, 0644)
}

func (s *ClientStoreImpl) List(_ context.Context) ([]models.Client, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]models.Client, len(s.clients))
	copy(result, s.clients)
	sort.Slice(result, func(i, j int) bool {
		if result[i].Priority != result[j].Priority {
			return result[i].Priority < result[j].Priority
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}

func (s *ClientStoreImpl) ListEnabledWithRules(_ context.Context) ([]models.Client, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []models.Client
	for _, c := range s.clients {
		if c.IsEnabled {
			cp := c
			result = append(result, cp)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Priority != result[j].Priority {
			return result[i].Priority < result[j].Priority
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}

func (s *ClientStoreImpl) GetByID(_ context.Context, id string) (*models.Client, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.clients {
		if s.clients[i].ID == id {
			c := s.clients[i]
			return &c, nil
		}
	}
	return nil, fmt.Errorf("client not found")
}

func (s *ClientStoreImpl) Create(_ context.Context, client *models.Client) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if client.ID == "" {
		client.ID = uuid.New().String()
	}
	s.clients = append(s.clients, *client)
	return s.saveUnlocked()
}

func (s *ClientStoreImpl) Update(_ context.Context, client *models.Client) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.clients {
		if s.clients[i].ID == client.ID {
			s.clients[i].Name = client.Name
			s.clients[i].Priority = client.Priority
			s.clients[i].StreamProfileID = client.StreamProfileID
			s.clients[i].IsEnabled = client.IsEnabled
			return s.saveUnlocked()
		}
	}
	return fmt.Errorf("client not found")
}

func (s *ClientStoreImpl) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.clients {
		if s.clients[i].ID == id {
			s.clients = append(s.clients[:i], s.clients[i+1:]...)
			return s.saveUnlocked()
		}
	}
	return nil
}

func (s *ClientStoreImpl) SetMatchRules(_ context.Context, clientID string, rules []models.ClientMatchRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.clients {
		if s.clients[i].ID == clientID {
			for j := range rules {
				if rules[j].ID == "" {
					rules[j].ID = uuid.New().String()
				}
				rules[j].ClientID = clientID
			}
			s.clients[i].MatchRules = rules
			return s.saveUnlocked()
		}
	}
	return fmt.Errorf("client not found")
}

func (s *ClientStoreImpl) IsStreamProfileReferenced(_ context.Context, profileID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.clients {
		if c.StreamProfileID == profileID {
			return true, nil
		}
	}
	return false, nil
}

func (s *ClientStoreImpl) Clear() {
	s.mu.Lock()
	s.clients = nil
	s.mu.Unlock()
}

func (s *ClientStoreImpl) AddDirect(client *models.Client) {
	s.mu.Lock()
	s.clients = append(s.clients, *client)
	s.mu.Unlock()
}

func (s *ClientStoreImpl) saveUnlocked() error {
	data, err := json.MarshalIndent(s.clients, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, data, 0644)
}

func (s *ClientStoreImpl) ClearAndSave() error {
	s.mu.Lock()
	s.clients = nil
	s.mu.Unlock()
	return s.Save()
}
