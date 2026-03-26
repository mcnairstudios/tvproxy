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

type M3UAccountStore interface {
	Create(ctx context.Context, account *models.M3UAccount) error
	GetByID(ctx context.Context, id string) (*models.M3UAccount, error)
	List(ctx context.Context) ([]models.M3UAccount, error)
	Update(ctx context.Context, account *models.M3UAccount) error
	Delete(ctx context.Context, id string) error
	UpdateLastRefreshed(ctx context.Context, id string, lastRefreshed time.Time) error
	UpdateLastError(ctx context.Context, id, lastError string) error
	UpdateStreamCount(ctx context.Context, id string, count int) error
}

type M3UAccountStoreImpl struct {
	filePath string
	accounts []models.M3UAccount
	mu       sync.RWMutex
}

func NewM3UAccountStore(filePath string) *M3UAccountStoreImpl {
	return &M3UAccountStoreImpl{filePath: filePath}
}

func (s *M3UAccountStoreImpl) Load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return json.Unmarshal(data, &s.accounts)
}

func (s *M3UAccountStoreImpl) save() error {
	data, err := json.MarshalIndent(s.accounts, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, data, 0644)
}

func (s *M3UAccountStoreImpl) Create(_ context.Context, account *models.M3UAccount) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if account.ID == "" {
		account.ID = uuid.New().String()
	}
	now := time.Now()
	account.CreatedAt = now
	account.UpdatedAt = now
	s.accounts = append(s.accounts, *account)
	return s.save()
}

func (s *M3UAccountStoreImpl) GetByID(_ context.Context, id string) (*models.M3UAccount, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.accounts {
		if s.accounts[i].ID == id {
			a := s.accounts[i]
			return &a, nil
		}
	}
	return nil, fmt.Errorf("m3u account not found")
}

func (s *M3UAccountStoreImpl) List(_ context.Context) ([]models.M3UAccount, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]models.M3UAccount, len(s.accounts))
	copy(result, s.accounts)
	return result, nil
}

func (s *M3UAccountStoreImpl) Update(_ context.Context, account *models.M3UAccount) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.accounts {
		if s.accounts[i].ID == account.ID {
			account.UpdatedAt = time.Now()
			account.CreatedAt = s.accounts[i].CreatedAt
			s.accounts[i] = *account
			return s.save()
		}
	}
	return fmt.Errorf("m3u account not found")
}

func (s *M3UAccountStoreImpl) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.accounts {
		if s.accounts[i].ID == id {
			s.accounts = append(s.accounts[:i], s.accounts[i+1:]...)
			return s.save()
		}
	}
	return nil
}

func (s *M3UAccountStoreImpl) UpdateLastRefreshed(_ context.Context, id string, lastRefreshed time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.accounts {
		if s.accounts[i].ID == id {
			s.accounts[i].LastRefreshed = &lastRefreshed
			s.accounts[i].UpdatedAt = time.Now()
			return s.save()
		}
	}
	return fmt.Errorf("m3u account not found")
}

func (s *M3UAccountStoreImpl) UpdateLastError(_ context.Context, id, lastError string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.accounts {
		if s.accounts[i].ID == id {
			s.accounts[i].LastError = lastError
			s.accounts[i].UpdatedAt = time.Now()
			return s.save()
		}
	}
	return fmt.Errorf("m3u account not found")
}

func (s *M3UAccountStoreImpl) UpdateStreamCount(_ context.Context, id string, count int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.accounts {
		if s.accounts[i].ID == id {
			s.accounts[i].StreamCount = count
			s.accounts[i].UpdatedAt = time.Now()
			return s.save()
		}
	}
	return fmt.Errorf("m3u account not found")
}

func (s *M3UAccountStoreImpl) ClearAndSave() error {
	s.mu.Lock()
	s.accounts = nil
	s.mu.Unlock()
	return s.save()
}
