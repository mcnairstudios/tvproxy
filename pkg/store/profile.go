package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/models"
)

type ProfileStore interface {
	List(ctx context.Context) ([]models.StreamProfile, error)
	GetByID(ctx context.Context, id string) (*models.StreamProfile, error)
	GetByName(ctx context.Context, name string) (*models.StreamProfile, error)
	GetDefault(ctx context.Context) (*models.StreamProfile, error)
	Create(ctx context.Context, profile *models.StreamProfile) error
	Update(ctx context.Context, profile *models.StreamProfile) error
	Delete(ctx context.Context, id string) error
	RemoveClientProfiles()
	CreateDirect(profile *models.StreamProfile)
	Save() error
}

type ProfileStoreImpl struct {
	filePath string
	profiles []models.StreamProfile
	mu       sync.RWMutex
	log      zerolog.Logger
}

func NewProfileStore(filePath string, log zerolog.Logger) *ProfileStoreImpl {
	return &ProfileStoreImpl{
		filePath: filePath,
		log:      log.With().Str("store", "profile").Logger(),
	}
}

func (s *ProfileStoreImpl) Load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			s.profiles = nil
			return nil
		}
		return fmt.Errorf("reading profile store: %w", err)
	}
	var profiles []models.StreamProfile
	if err := json.Unmarshal(data, &profiles); err != nil {
		return fmt.Errorf("parsing profile store: %w", err)
	}
	changed := false
	for i := range profiles {
		if profiles[i].Name == "Browser" && profiles[i].IsClient && profiles[i].Delivery == "" {
			profiles[i].Delivery = "mse"
			changed = true
		}
	}

	s.mu.Lock()
	s.profiles = profiles
	s.mu.Unlock()
	s.log.Info().Int("count", len(profiles)).Msg("loaded profiles from json")

	if changed {
		s.Save()
		s.log.Info().Msg("migrated empty browser delivery to mse")
	}
	return nil
}

func (s *ProfileStoreImpl) Save() error {
	s.mu.RLock()
	data, err := json.MarshalIndent(s.profiles, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return fmt.Errorf("marshaling profiles: %w", err)
	}
	if err := os.WriteFile(s.filePath, data, 0644); err != nil {
		return fmt.Errorf("writing profile store: %w", err)
	}
	return nil
}

func (s *ProfileStoreImpl) List(_ context.Context) ([]models.StreamProfile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]models.StreamProfile, len(s.profiles))
	copy(result, s.profiles)
	sort.Slice(result, func(i, j int) bool {
		ci, cj := sortCategory(result[i]), sortCategory(result[j])
		if ci != cj {
			return ci < cj
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}

func sortCategory(p models.StreamProfile) int {
	if p.IsSystem {
		return 0
	}
	if p.IsClient {
		return 1
	}
	return 2
}

func (s *ProfileStoreImpl) GetByID(_ context.Context, id string) (*models.StreamProfile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.profiles {
		if s.profiles[i].ID == id {
			p := s.profiles[i]
			return &p, nil
		}
	}
	return nil, fmt.Errorf("stream profile not found")
}

func (s *ProfileStoreImpl) GetByName(_ context.Context, name string) (*models.StreamProfile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.profiles {
		if s.profiles[i].Name == name {
			p := s.profiles[i]
			return &p, nil
		}
	}
	return nil, fmt.Errorf("stream profile not found")
}

func (s *ProfileStoreImpl) GetDefault(_ context.Context) (*models.StreamProfile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.profiles {
		if s.profiles[i].IsDefault {
			p := s.profiles[i]
			return &p, nil
		}
	}
	return nil, fmt.Errorf("default stream profile not found")
}

func (s *ProfileStoreImpl) Create(_ context.Context, profile *models.StreamProfile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.profiles {
		if p.Name == profile.Name {
			return fmt.Errorf("stream profile name already exists")
		}
	}
	if profile.ID == "" {
		profile.ID = uuid.New().String()
	}
	s.profiles = append(s.profiles, *profile)
	return s.saveUnlocked()
}

func (s *ProfileStoreImpl) Update(_ context.Context, profile *models.StreamProfile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.profiles {
		if s.profiles[i].ID == profile.ID {
			s.profiles[i] = *profile
			return s.saveUnlocked()
		}
	}
	return fmt.Errorf("stream profile not found")
}

func (s *ProfileStoreImpl) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.profiles {
		if s.profiles[i].ID == id {
			s.profiles = append(s.profiles[:i], s.profiles[i+1:]...)
			return s.saveUnlocked()
		}
	}
	return nil
}

func (s *ProfileStoreImpl) saveUnlocked() error {
	data, err := json.MarshalIndent(s.profiles, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling profiles: %w", err)
	}
	if err := os.WriteFile(s.filePath, data, 0644); err != nil {
		return fmt.Errorf("writing profile store: %w", err)
	}
	return nil
}

func (s *ProfileStoreImpl) SeedSystemProfiles() {
	s.mu.Lock()
	defer s.mu.Unlock()

	hasSystem := make(map[string]bool)
	for _, p := range s.profiles {
		if p.IsSystem {
			hasSystem[p.Name] = true
		}
	}

	systemProfiles := []models.StreamProfile{
		{Name: "Direct", StreamMode: "direct", HWAccel: "none", VideoCodec: "copy", Container: "mpegts", IsSystem: true},
		{Name: "Proxy", StreamMode: "proxy", HWAccel: "none", VideoCodec: "copy", Container: "mpegts", IsDefault: true, IsSystem: true},
	}

	for _, sp := range systemProfiles {
		if hasSystem[sp.Name] {
			continue
		}
		sp.ID = uuid.New().String()
		s.profiles = append(s.profiles, sp)
	}
}

func (s *ProfileStoreImpl) RemoveClientProfiles() {
	s.mu.Lock()
	defer s.mu.Unlock()
	filtered := s.profiles[:0]
	for _, p := range s.profiles {
		if !p.IsClient {
			filtered = append(filtered, p)
		}
	}
	s.profiles = filtered
}

func (s *ProfileStoreImpl) CreateDirect(profile *models.StreamProfile) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.profiles = append(s.profiles, *profile)
}

func (s *ProfileStoreImpl) ClearAndSave() error {
	s.mu.Lock()
	s.profiles = nil
	s.mu.Unlock()
	return s.Save()
}
