package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/google/uuid"

	"github.com/gavinmcnair/tvproxy/pkg/models"
)

type LogoStore interface {
	Create(ctx context.Context, logo *models.Logo) error
	GetByID(ctx context.Context, id string) (*models.Logo, error)
	GetByURL(ctx context.Context, url string) (*models.Logo, error)
	List(ctx context.Context) ([]models.Logo, error)
	Update(ctx context.Context, logo *models.Logo) error
	Delete(ctx context.Context, id string) error
}

type LogoStoreImpl struct {
	filePath string
	logos    []models.Logo
	mu       sync.RWMutex
}

func NewLogoStore(filePath string) *LogoStoreImpl {
	return &LogoStoreImpl{filePath: filePath}
}

func (s *LogoStoreImpl) Load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return json.Unmarshal(data, &s.logos)
}

func (s *LogoStoreImpl) save() error {
	data, err := json.MarshalIndent(s.logos, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, data, 0644)
}

func (s *LogoStoreImpl) Create(_ context.Context, logo *models.Logo) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if logo.ID == "" {
		logo.ID = uuid.New().String()
	}
	s.logos = append(s.logos, *logo)
	return s.save()
}

func (s *LogoStoreImpl) GetByID(_ context.Context, id string) (*models.Logo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.logos {
		if s.logos[i].ID == id {
			l := s.logos[i]
			return &l, nil
		}
	}
	return nil, fmt.Errorf("logo not found")
}

func (s *LogoStoreImpl) GetByURL(_ context.Context, url string) (*models.Logo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.logos {
		if s.logos[i].URL == url {
			l := s.logos[i]
			return &l, nil
		}
	}
	return nil, nil
}

func (s *LogoStoreImpl) List(_ context.Context) ([]models.Logo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]models.Logo, len(s.logos))
	copy(result, s.logos)
	return result, nil
}

func (s *LogoStoreImpl) Update(_ context.Context, logo *models.Logo) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.logos {
		if s.logos[i].ID == logo.ID {
			s.logos[i].Name = logo.Name
			s.logos[i].URL = logo.URL
			return s.save()
		}
	}
	return fmt.Errorf("logo not found")
}

func (s *LogoStoreImpl) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.logos {
		if s.logos[i].ID == id {
			s.logos = append(s.logos[:i], s.logos[i+1:]...)
			return s.save()
		}
	}
	return nil
}

func (s *LogoStoreImpl) ClearAndSave() error {
	s.mu.Lock()
	s.logos = nil
	s.mu.Unlock()
	return s.save()
}
