package service

import (
	"context"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/logocache"
	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/store"
)

type LogoService struct {
	store store.LogoStore
	cache *logocache.Cache
	log   zerolog.Logger
	rev   *store.Revision
}

func NewLogoService(logoStore store.LogoStore, cache *logocache.Cache, log zerolog.Logger) *LogoService {
	return &LogoService{
		store: logoStore,
		cache: cache,
		log:   log.With().Str("service", "logo").Logger(),
		rev:   store.NewRevision(),
	}
}

func (s *LogoService) ETag() string { return s.rev.ETag() }

func (s *LogoService) Create(ctx context.Context, logo *models.Logo) error {
	if err := s.store.Create(ctx, logo); err != nil {
		return err
	}
	s.rev.Bump()
	return nil
}

func (s *LogoService) Update(ctx context.Context, logo *models.Logo) error {
	if err := s.store.Update(ctx, logo); err != nil {
		return err
	}
	s.rev.Bump()
	return nil
}

func (s *LogoService) Delete(ctx context.Context, id string) error {
	if err := s.store.Delete(ctx, id); err != nil {
		return err
	}
	s.rev.Bump()
	return nil
}

func (s *LogoService) List(ctx context.Context) ([]models.Logo, error) {
	return s.store.List(ctx)
}

func (s *LogoService) GetByID(ctx context.Context, id string) (*models.Logo, error) {
	return s.store.GetByID(ctx, id)
}

func (s *LogoService) GetByURL(ctx context.Context, url string) (*models.Logo, error) {
	return s.store.GetByURL(ctx, url)
}

func (s *LogoService) Resolve(url string) string {
	return s.cache.Fetch(url)
}

func (s *LogoService) ResolveChannel(ch models.Channel) string {
	if ch.LogoID != nil {
		if logo, err := s.store.GetByID(context.Background(), *ch.LogoID); err == nil {
			return s.cache.Fetch(logo.URL)
		}
	}
	if ch.Logo != "" {
		return s.cache.Fetch(ch.Logo)
	}
	return logocache.Placeholder
}

func (s *LogoService) ResolveChannelLogos(channels []models.Channel) {
	for i := range channels {
		channels[i].Logo = s.ResolveChannel(channels[i])
	}
}
