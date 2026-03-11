package service

import (
	"context"
	"net/http"
	"strings"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
)

// ClientService handles client detection and management.
type ClientService struct {
	clientRepo        *repository.ClientRepository
	streamProfileRepo *repository.StreamProfileRepository
	log               zerolog.Logger
}

// NewClientService creates a new ClientService.
func NewClientService(
	clientRepo *repository.ClientRepository,
	streamProfileRepo *repository.StreamProfileRepository,
	log zerolog.Logger,
) *ClientService {
	return &ClientService{
		clientRepo:        clientRepo,
		streamProfileRepo: streamProfileRepo,
		log:               log.With().Str("service", "client").Logger(),
	}
}

// MatchClient checks request headers against enabled clients and returns the
// matching stream profile, or nil if no client matches.
func (s *ClientService) MatchClient(ctx context.Context, r *http.Request) (*models.StreamProfile, error) {
	clients, err := s.clientRepo.ListEnabledWithRules(ctx)
	if err != nil {
		return nil, err
	}

	for _, client := range clients {
		if len(client.MatchRules) == 0 {
			continue
		}
		if matchesAllRules(r, client.MatchRules) {
			profile, err := s.streamProfileRepo.GetByID(ctx, client.StreamProfileID)
			if err != nil {
				s.log.Warn().Err(err).Int64("client_id", client.ID).Str("client", client.Name).Msg("client matched but stream profile not found")
				continue
			}
			s.log.Info().Str("client", client.Name).Int64("profile_id", profile.ID).Str("profile", profile.Name).Msg("client detected")
			return profile, nil
		}
	}

	return nil, nil
}

// matchesAllRules checks if all match rules are satisfied by the request headers (AND logic).
func matchesAllRules(r *http.Request, rules []models.ClientMatchRule) bool {
	for _, rule := range rules {
		if !matchRule(r, rule) {
			return false
		}
	}
	return true
}

// matchRule checks a single rule against request headers.
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
