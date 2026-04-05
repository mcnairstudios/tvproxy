package store

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/google/uuid"
)

type WireGuardProfileStore struct {
	mu       sync.RWMutex
	profiles []models.WireGuardProfile
	filePath string
}

func NewWireGuardProfileStore(filePath string) *WireGuardProfileStore {
	return &WireGuardProfileStore{filePath: filePath}
}

func (s *WireGuardProfileStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			s.profiles = nil
			return nil
		}
		return err
	}
	return json.Unmarshal(data, &s.profiles)
}

func (s *WireGuardProfileStore) save() error {
	data, err := json.MarshalIndent(s.profiles, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, data, 0644)
}

func (s *WireGuardProfileStore) List(_ context.Context) ([]models.WireGuardProfile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]models.WireGuardProfile, len(s.profiles))
	copy(result, s.profiles)
	sort.Slice(result, func(i, j int) bool { return result[i].Priority < result[j].Priority })
	return result, nil
}

func (s *WireGuardProfileStore) GetByID(_ context.Context, id string) (*models.WireGuardProfile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.profiles {
		if p.ID == id {
			cp := p
			return &cp, nil
		}
	}
	return nil, nil
}

func (s *WireGuardProfileStore) Create(_ context.Context, p *models.WireGuardProfile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p.ID = uuid.New().String()
	now := time.Now()
	p.CreatedAt = now
	p.UpdatedAt = now
	if p.HealthcheckMethod == "" {
		p.HealthcheckMethod = "HEAD"
	}
	if p.HealthcheckInterval <= 0 {
		p.HealthcheckInterval = 60
	}
	s.profiles = append(s.profiles, *p)
	return s.save()
}

func (s *WireGuardProfileStore) Update(_ context.Context, p *models.WireGuardProfile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.profiles {
		if existing.ID == p.ID {
			p.CreatedAt = existing.CreatedAt
			p.UpdatedAt = time.Now()
			if p.PrivateKey == "" {
				p.PrivateKey = existing.PrivateKey
			}
			if p.HealthcheckMethod == "" {
				p.HealthcheckMethod = "HEAD"
			}
			if p.HealthcheckInterval <= 0 {
				p.HealthcheckInterval = 60
			}
			s.profiles[i] = *p
			return s.save()
		}
	}
	return nil
}

func (s *WireGuardProfileStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, p := range s.profiles {
		if p.ID == id {
			s.profiles = append(s.profiles[:i], s.profiles[i+1:]...)
			return s.save()
		}
	}
	return nil
}

func (s *WireGuardProfileStore) ClearAndSave() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.profiles = nil
	return s.save()
}
