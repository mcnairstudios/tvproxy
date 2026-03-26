package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/ffmpeg"
	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
)

type ClientService struct {
	clientRepo        *repository.ClientRepository
	streamProfileRepo *repository.StreamProfileRepository
	settingsService   *SettingsService
	log               zerolog.Logger
}

func NewClientService(
	clientRepo *repository.ClientRepository,
	streamProfileRepo *repository.StreamProfileRepository,
	settingsService *SettingsService,
	log zerolog.Logger,
) *ClientService {
	return &ClientService{
		clientRepo:        clientRepo,
		streamProfileRepo: streamProfileRepo,
		settingsService:   settingsService,
		log:               log.With().Str("service", "client").Logger(),
	}
}

func (s *ClientService) MatchClient(ctx context.Context, r *http.Request) (*models.StreamProfile, string, error) {
	clients, err := s.clientRepo.ListEnabledWithRules(ctx)
	if err != nil {
		return nil, "", err
	}

	debug := s.settingsService.IsDebug()
	for _, client := range clients {
		if len(client.MatchRules) == 0 {
			continue
		}
		if s.matchesAllRules(r, client, debug) {
			profile, err := s.streamProfileRepo.GetByID(ctx, client.StreamProfileID)
			if err != nil {
				s.log.Warn().Err(err).Str("client_id", client.ID).Str("client", client.Name).Msg("matched but profile not found")
				continue
			}
			s.log.Info().Str("client", client.Name).Str("profile", profile.Name).Msg("client detected")
			return profile, client.Name, nil
		}
	}

	return nil, "", nil
}

func (s *ClientService) ListClients(ctx context.Context) ([]models.Client, error) {
	return s.clientRepo.List(ctx)
}

func (s *ClientService) GetClient(ctx context.Context, id string) (*models.Client, error) {
	return s.clientRepo.GetByID(ctx, id)
}

func (s *ClientService) CreateClient(ctx context.Context, client *models.Client, rules []models.ClientMatchRule) error {
	args := ffmpeg.ComposeStreamProfileArgs(ffmpeg.ComposeOptions{SourceType: "m3u", HWAccel: "none", VideoCodec: "copy", Container: "mpegts"})
	profile := &models.StreamProfile{
		Name:       client.Name,
		StreamMode: "ffmpeg",
		SourceType: "m3u",
		HWAccel:    "none",
		VideoCodec: "copy",
		Container:  "mpegts",
		FPSMode:    "auto",
		Command:    "ffmpeg",
		Args:       args,
		IsClient:   true,
	}
	if err := s.streamProfileRepo.Create(ctx, profile); err != nil {
		return fmt.Errorf("creating stream profile: %w", err)
	}

	client.StreamProfileID = profile.ID

	if err := s.clientRepo.Create(ctx, client); err != nil {
		if delErr := s.streamProfileRepo.Delete(ctx, profile.ID); delErr != nil {
			s.log.Warn().Err(delErr).Str("profile_id", profile.ID).Msg("failed to clean up orphan profile")
		}
		return fmt.Errorf("creating client: %w", err)
	}

	if err := s.clientRepo.SetMatchRules(ctx, client.ID, rules); err != nil {
		return fmt.Errorf("setting match rules: %w", err)
	}

	return nil
}

func (s *ClientService) UpdateClient(ctx context.Context, client *models.Client, rules []models.ClientMatchRule) error {
	if err := s.clientRepo.Update(ctx, client); err != nil {
		return fmt.Errorf("updating client: %w", err)
	}

	if rules != nil {
		if err := s.clientRepo.SetMatchRules(ctx, client.ID, rules); err != nil {
			return fmt.Errorf("updating match rules: %w", err)
		}
	}

	return nil
}

func (s *ClientService) DeleteClient(ctx context.Context, id string) error {
	client, err := s.clientRepo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("getting client: %w", err)
	}

	profileID := client.StreamProfileID

	if err := s.clientRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("deleting client: %w", err)
	}

	profile, profileErr := s.streamProfileRepo.GetByID(ctx, profileID)
	if profileErr == nil && !profile.IsSystem {
		referenced, refErr := s.clientRepo.IsStreamProfileReferenced(ctx, profileID)
		if refErr == nil && !referenced {
			if delErr := s.streamProfileRepo.Delete(ctx, profileID); delErr != nil {
				s.log.Warn().Err(delErr).Str("profile_id", profileID).Msg("failed to clean up orphan profile")
			}
		}
	}

	return nil
}

func (s *ClientService) matchesAllRules(r *http.Request, client models.Client, debug bool) bool {
	for _, rule := range client.MatchRules {
		matched := matchRule(r, rule)
		if debug {
			s.log.Debug().Str("client", client.Name).Str("header", rule.HeaderName).Str("pattern", rule.MatchValue).Str("type", rule.MatchType).Bool("matched", matched).Msg("rule check")
		}
		if !matched {
			return false
		}
	}
	return true
}

func matchRule(r *http.Request, rule models.ClientMatchRule) bool {
	headerValue := r.Header.Get(rule.HeaderName)
	switch rule.MatchType {
	case "exists":
		return headerValue != ""
	case "contains":
		return strings.Contains(headerValue, rule.MatchValue)
	case "equals":
		return headerValue == rule.MatchValue
	case "prefix":
		return strings.HasPrefix(headerValue, rule.MatchValue)
	default:
		return false
	}
}
