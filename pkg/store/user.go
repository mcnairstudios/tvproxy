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

type UserStore interface {
	Create(ctx context.Context, user *models.User) error
	GetByID(ctx context.Context, id string) (*models.User, error)
	GetByUsername(ctx context.Context, username string) (*models.User, error)
	GetByInviteToken(ctx context.Context, token string) (*models.User, error)
	List(ctx context.Context) ([]models.User, error)
	GetFirstAdmin(ctx context.Context) (*models.User, error)
	Update(ctx context.Context, user *models.User) error
	Delete(ctx context.Context, id string) error
	GetGroupIDsForUser(ctx context.Context, userID string) ([]string, error)
	SetGroupIDsForUser(ctx context.Context, userID string, groupIDs []string) error
}

type userEntry struct {
	User         models.User `json:"user"`
	PasswordHash string      `json:"password_hash"`
	GroupIDs     []string    `json:"group_ids,omitempty"`
}

type UserStoreImpl struct {
	filePath string
	users    []userEntry
	mu       sync.RWMutex
}

func NewUserStore(filePath string) *UserStoreImpl {
	return &UserStoreImpl{filePath: filePath}
}

func (s *UserStoreImpl) Load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return json.Unmarshal(data, &s.users)
}

func (s *UserStoreImpl) save() error {
	data, err := json.MarshalIndent(s.users, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, data, 0644)
}

func (s *UserStoreImpl) Create(_ context.Context, user *models.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.users {
		if u.User.Username == user.Username {
			return fmt.Errorf("username already exists")
		}
	}
	if user.ID == "" {
		user.ID = uuid.New().String()
	}
	now := time.Now()
	user.CreatedAt = now
	user.UpdatedAt = now
	s.users = append(s.users, userEntry{User: *user, PasswordHash: user.PasswordHash})
	return s.save()
}

func (s *UserStoreImpl) hydrateUser(e *userEntry) *models.User {
	u := e.User
	u.PasswordHash = e.PasswordHash
	return &u
}

func (s *UserStoreImpl) GetByID(_ context.Context, id string) (*models.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.users {
		if s.users[i].User.ID == id {
			return s.hydrateUser(&s.users[i]), nil
		}
	}
	return nil, fmt.Errorf("user not found")
}

func (s *UserStoreImpl) GetByUsername(_ context.Context, username string) (*models.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.users {
		if s.users[i].User.Username == username {
			return s.hydrateUser(&s.users[i]), nil
		}
	}
	return nil, fmt.Errorf("user not found")
}

func (s *UserStoreImpl) GetByInviteToken(_ context.Context, token string) (*models.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.users {
		if s.users[i].User.InviteToken != nil && *s.users[i].User.InviteToken == token {
			return s.hydrateUser(&s.users[i]), nil
		}
	}
	return nil, fmt.Errorf("user not found")
}

func (s *UserStoreImpl) List(_ context.Context) ([]models.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]models.User, len(s.users))
	for i := range s.users {
		result[i] = s.users[i].User
		result[i].PasswordHash = s.users[i].PasswordHash
	}
	return result, nil
}

func (s *UserStoreImpl) GetFirstAdmin(_ context.Context) (*models.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.users {
		if s.users[i].User.IsAdmin {
			return s.hydrateUser(&s.users[i]), nil
		}
	}
	return nil, fmt.Errorf("no admin user found")
}

func (s *UserStoreImpl) Update(_ context.Context, user *models.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.users {
		if s.users[i].User.ID == user.ID {
			user.UpdatedAt = time.Now()
			user.CreatedAt = s.users[i].User.CreatedAt
			s.users[i].User = *user
			s.users[i].PasswordHash = user.PasswordHash
			return s.save()
		}
	}
	return fmt.Errorf("user not found")
}

func (s *UserStoreImpl) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.users {
		if s.users[i].User.ID == id {
			s.users = append(s.users[:i], s.users[i+1:]...)
			return s.save()
		}
	}
	return nil
}

func (s *UserStoreImpl) GetGroupIDsForUser(_ context.Context, userID string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.users {
		if u.User.ID == userID {
			return u.GroupIDs, nil
		}
	}
	return nil, fmt.Errorf("user not found")
}

func (s *UserStoreImpl) SetGroupIDsForUser(_ context.Context, userID string, groupIDs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.users {
		if s.users[i].User.ID == userID {
			s.users[i].GroupIDs = groupIDs
			return s.save()
		}
	}
	return fmt.Errorf("user not found")
}

func (s *UserStoreImpl) ClearAndSave() error {
	s.mu.Lock()
	s.users = nil
	s.mu.Unlock()
	return s.save()
}
