package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"

	"github.com/gavinmcnair/tvproxy/pkg/models"
)

type SettingsStore interface {
	Get(ctx context.Context, key string) (*models.CoreSetting, error)
	Set(ctx context.Context, key, value string) error
	List(ctx context.Context) ([]models.CoreSetting, error)
	Delete(ctx context.Context, key string) error
}

type SettingsStoreImpl struct {
	filePath string
	data     map[string]string
	mu       sync.RWMutex
}

func NewSettingsStore(filePath string) *SettingsStoreImpl {
	return &SettingsStoreImpl{
		filePath: filePath,
		data:     make(map[string]string),
	}
}

func (s *SettingsStoreImpl) Load() error {
	raw, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading settings: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := json.Unmarshal(raw, &s.data); err != nil {
		return fmt.Errorf("parsing settings: %w", err)
	}
	return nil
}

func (s *SettingsStoreImpl) save() error {
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling settings: %w", err)
	}
	return os.WriteFile(s.filePath, raw, 0644)
}

func (s *SettingsStoreImpl) Get(_ context.Context, key string) (*models.CoreSetting, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	val, ok := s.data[key]
	if !ok {
		return nil, fmt.Errorf("setting %q not found", key)
	}
	return &models.CoreSetting{Key: key, Value: val}, nil
}

func (s *SettingsStoreImpl) Set(_ context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
	return s.save()
}

func (s *SettingsStoreImpl) List(_ context.Context) ([]models.CoreSetting, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]models.CoreSetting, 0, len(s.data))
	for k, v := range s.data {
		result = append(result, models.CoreSetting{Key: k, Value: v})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result, nil
}

func (s *SettingsStoreImpl) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return s.save()
}

func (s *SettingsStoreImpl) ClearAndSave() error {
	s.mu.Lock()
	s.data = make(map[string]string)
	s.mu.Unlock()
	return s.save()
}
