package service

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/store"
)

type ClientService struct {
	clientStore       store.ClientStore
	streamProfileRepo store.ProfileStore
	settingsService   *SettingsService
	log               zerolog.Logger
}

func NewClientService(
	clientStore store.ClientStore,
	streamProfileRepo store.ProfileStore,
	settingsService *SettingsService,
	log zerolog.Logger,
) *ClientService {
	return &ClientService{
		clientStore:       clientStore,
		streamProfileRepo: streamProfileRepo,
		settingsService:   settingsService,
		log:               log.With().Str("service", "client").Logger(),
	}
}

func (s *ClientService) MatchClient(ctx context.Context, r *http.Request) (*models.StreamProfile, string, error) {
	clients, err := s.clientStore.ListEnabledWithRules(ctx)
	if err != nil {
		return nil, "", err
	}

	originPort := requestOriginPort(r)
	debug := s.settingsService.IsDebug()

	for _, client := range clients {
		if !s.matchesClient(r, client, originPort, debug) {
			continue
		}
		profile, err := s.streamProfileRepo.GetByID(ctx, client.StreamProfileID)
		if err != nil {
			s.log.Warn().Err(err).Str("client_id", client.ID).Str("client", client.Name).Msg("matched but profile not found")
			continue
		}
		s.log.Info().Str("client", client.Name).Str("profile", profile.Name).Int("port", originPort).Msg("client detected")
		return profile, client.Name, nil
	}

	return nil, "", nil
}

func requestOriginPort(r *http.Request) int {
	if p := r.URL.Query().Get("_port"); p != "" {
		if port, err := strconv.Atoi(p); err == nil {
			return port
		}
	}
	if p := r.Header.Get("X-TVProxy-Port"); p != "" {
		if port, err := strconv.Atoi(p); err == nil {
			return port
		}
	}
	return 8080
}

func (s *ClientService) matchesClient(r *http.Request, client models.Client, originPort int, debug bool) bool {
	if client.ListenPort > 0 && client.ListenPort != originPort {
		if debug {
			s.log.Debug().Str("client", client.Name).Int("want_port", client.ListenPort).Int("got_port", originPort).Msg("port mismatch, skipping")
		}
		return false
	}

	if len(client.MatchRules) == 0 {
		return client.ListenPort > 0 && client.ListenPort == originPort
	}

	return s.matchesAllRules(r, client, debug)
}

func (s *ClientService) ListClients(ctx context.Context) ([]models.Client, error) {
	return s.clientStore.List(ctx)
}

func (s *ClientService) GetClient(ctx context.Context, id string) (*models.Client, error) {
	return s.clientStore.GetByID(ctx, id)
}

func (s *ClientService) CreateClient(ctx context.Context, client *models.Client, rules []models.ClientMatchRule) error {
	profile := &models.StreamProfile{
		Name:       client.Name,
		StreamMode: "ffmpeg",
		HWAccel:    "none",
		Container:  "mpegts",
		Command:    "ffmpeg",
		AutoDetect: true,
		IsClient:   true,
	}
	if err := s.streamProfileRepo.Create(ctx, profile); err != nil {
		return fmt.Errorf("creating stream profile: %w", err)
	}

	client.StreamProfileID = profile.ID

	if err := s.clientStore.Create(ctx, client); err != nil {
		if delErr := s.streamProfileRepo.Delete(ctx, profile.ID); delErr != nil {
			s.log.Warn().Err(delErr).Str("profile_id", profile.ID).Msg("failed to clean up orphan profile")
		}
		return fmt.Errorf("creating client: %w", err)
	}

	if err := s.clientStore.SetMatchRules(ctx, client.ID, rules); err != nil {
		return fmt.Errorf("setting match rules: %w", err)
	}

	return nil
}

func (s *ClientService) UpdateClient(ctx context.Context, client *models.Client, rules []models.ClientMatchRule) error {
	if err := s.clientStore.Update(ctx, client); err != nil {
		return fmt.Errorf("updating client: %w", err)
	}

	if rules != nil {
		if err := s.clientStore.SetMatchRules(ctx, client.ID, rules); err != nil {
			return fmt.Errorf("updating match rules: %w", err)
		}
	}

	return nil
}

func (s *ClientService) DeleteClient(ctx context.Context, id string) error {
	client, err := s.clientStore.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("getting client: %w", err)
	}

	profileID := client.StreamProfileID

	if err := s.clientStore.Delete(ctx, id); err != nil {
		return fmt.Errorf("deleting client: %w", err)
	}

	profile, profileErr := s.streamProfileRepo.GetByID(ctx, profileID)
	if profileErr == nil && !profile.IsSystem {
		referenced, refErr := s.clientStore.IsStreamProfileReferenced(ctx, profileID)
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
