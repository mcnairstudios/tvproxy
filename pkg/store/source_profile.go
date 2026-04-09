package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/models"
)

type SourceProfileStore interface {
	List(ctx context.Context) ([]models.SourceProfile, error)
	GetByID(ctx context.Context, id string) (*models.SourceProfile, error)
	Create(ctx context.Context, profile *models.SourceProfile) error
	Update(ctx context.Context, profile *models.SourceProfile) error
	Delete(ctx context.Context, id string) error
	Save() error
}

type SourceProfileStoreImpl struct {
	filePath string
	profiles []models.SourceProfile
	mu       sync.RWMutex
	log      zerolog.Logger
}

func NewSourceProfileStore(filePath string, log zerolog.Logger) *SourceProfileStoreImpl {
	return &SourceProfileStoreImpl{
		filePath: filePath,
		log:      log.With().Str("store", "source_profile").Logger(),
	}
}

func (s *SourceProfileStoreImpl) Load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			s.profiles = nil
			return nil
		}
		return fmt.Errorf("reading source profile store: %w", err)
	}
	var profiles []models.SourceProfile
	if err := json.Unmarshal(data, &profiles); err != nil {
		return fmt.Errorf("parsing source profile store: %w", err)
	}
	s.mu.Lock()
	s.profiles = profiles
	s.mu.Unlock()
	s.log.Info().Int("count", len(profiles)).Msg("loaded source profiles from json")
	return nil
}

func (s *SourceProfileStoreImpl) Save() error {
	s.mu.RLock()
	data, err := json.MarshalIndent(s.profiles, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return fmt.Errorf("marshaling source profiles: %w", err)
	}
	if err := os.WriteFile(s.filePath, data, 0644); err != nil {
		return fmt.Errorf("writing source profile store: %w", err)
	}
	return nil
}

func (s *SourceProfileStoreImpl) List(_ context.Context) ([]models.SourceProfile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]models.SourceProfile, len(s.profiles))
	copy(result, s.profiles)
	return result, nil
}

func (s *SourceProfileStoreImpl) GetByID(_ context.Context, id string) (*models.SourceProfile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.profiles {
		if s.profiles[i].ID == id {
			p := s.profiles[i]
			return &p, nil
		}
	}
	return nil, fmt.Errorf("source profile not found")
}

func (s *SourceProfileStoreImpl) Create(_ context.Context, profile *models.SourceProfile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.profiles {
		if p.Name == profile.Name {
			return fmt.Errorf("source profile name already exists")
		}
	}
	if profile.ID == "" {
		profile.ID = uuid.New().String()
	}
	s.profiles = append(s.profiles, *profile)
	return s.saveUnlocked()
}

func (s *SourceProfileStoreImpl) Update(_ context.Context, profile *models.SourceProfile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.profiles {
		if s.profiles[i].ID == profile.ID {
			s.profiles[i] = *profile
			return s.saveUnlocked()
		}
	}
	return fmt.Errorf("source profile not found")
}

func (s *SourceProfileStoreImpl) Delete(_ context.Context, id string) error {
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

func (s *SourceProfileStoreImpl) SeedDefaults() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.profiles) > 0 {
		return
	}
	s.profiles = []models.SourceProfile{
		{
			ID: uuid.New().String(), Name: "IPTV", ProbeMode: "none", Transport: "http",
			VideoCodec: "h264", AudioCodec: "aac", Container: "mpegts",
			AnalyzeDuration: 500000, ProbeSize: 500000,
			ErrDetect: "ignore_err", FFlags: "+genpts+discardcorrupt",
			InputFormat: "mpegts",
		},
		{
			ID: uuid.New().String(), Name: "SAT>IP", ProbeMode: "declared", Transport: "rtsp",
			VideoCodec: "mpeg2video", AudioCodec: "mp2", Container: "mpegts",
			Deinterlace: true, RTSPTransport: "tcp",
			AnalyzeDuration: 3000000, ProbeSize: 10000000, MaxDelay: 500000,
			ErrDetect: "ignore_err", FFlags: "+genpts+discardcorrupt",
			AudioResync: true, FPSMode: "cfr",
		},
		{
			ID: uuid.New().String(), Name: "TVProxy-streams", ProbeMode: "auto", Transport: "http",
			AnalyzeDuration: 1000000, ProbeSize: 1000000,
			ErrDetect: "ignore_err", FFlags: "+genpts+discardcorrupt",
		},
	}
	s.saveUnlocked()
	s.log.Info().Int("count", len(s.profiles)).Msg("seeded default source profiles")
}

func (s *SourceProfileStoreImpl) saveUnlocked() error {
	data, err := json.MarshalIndent(s.profiles, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling source profiles: %w", err)
	}
	if err := os.WriteFile(s.filePath, data, 0644); err != nil {
		return fmt.Errorf("writing source profile store: %w", err)
	}
	return nil
}
