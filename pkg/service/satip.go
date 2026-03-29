package service

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/store"
	"github.com/gavinmcnair/tvproxy/pkg/tvsatipscan"
)

type SatIPService struct {
	satipSourceStore store.SatIPSourceStore
	streamStore      store.StreamStore
	channelStore     store.ChannelStore
	log              zerolog.Logger
	StatusTracker
}

func NewSatIPService(
	satipSourceStore store.SatIPSourceStore,
	streamStore store.StreamStore,
	channelStore store.ChannelStore,
	log zerolog.Logger,
) *SatIPService {
	return &SatIPService{
		satipSourceStore: satipSourceStore,
		streamStore:      streamStore,
		channelStore:     channelStore,
		log:              log.With().Str("service", "satip").Logger(),
		StatusTracker:    NewStatusTracker(),
	}
}

func (s *SatIPService) Log() *zerolog.Logger { return &s.log }

func (s *SatIPService) CreateSource(ctx context.Context, source *models.SatIPSource) error {
	if err := s.satipSourceStore.Create(ctx, source); err != nil {
		return fmt.Errorf("creating satip source: %w", err)
	}
	return nil
}

func (s *SatIPService) GetSource(ctx context.Context, id string) (*models.SatIPSource, error) {
	source, err := s.satipSourceStore.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("getting satip source: %w", err)
	}
	return source, nil
}

func (s *SatIPService) ListSources(ctx context.Context) ([]models.SatIPSource, error) {
	sources, err := s.satipSourceStore.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing satip sources: %w", err)
	}
	return sources, nil
}

func (s *SatIPService) UpdateSource(ctx context.Context, source *models.SatIPSource) error {
	if err := s.satipSourceStore.Update(ctx, source); err != nil {
		return fmt.Errorf("updating satip source: %w", err)
	}
	return nil
}

func (s *SatIPService) DeleteSource(ctx context.Context, id string) error {
	if err := s.streamStore.DeleteBySatIPSourceID(ctx, id); err != nil {
		return fmt.Errorf("deleting streams for satip source: %w", err)
	}
	if err := s.streamStore.Save(); err != nil {
		s.log.Error().Err(err).Msg("failed to save stream store after satip source delete")
	}
	if err := s.satipSourceStore.Delete(ctx, id); err != nil {
		return fmt.Errorf("deleting satip source: %w", err)
	}
	return nil
}

func (s *SatIPService) ScanSource(ctx context.Context, sourceID string) error {
	source, err := s.satipSourceStore.GetByID(ctx, sourceID)
	if err != nil {
		return fmt.Errorf("getting satip source: %w", err)
	}

	s.Set(sourceID, RefreshStatus{State: "running", Message: "Scanning device capabilities..."})

	if err := s.scanSource(ctx, source); err != nil {
		s.satipSourceStore.UpdateLastError(ctx, source.ID, err.Error())
		s.Set(sourceID, RefreshStatus{State: "error", Message: err.Error()})
		return err
	}

	s.satipSourceStore.UpdateLastError(ctx, source.ID, "")
	s.Set(sourceID, RefreshStatus{State: "done", Message: "Scan complete"})
	return nil
}

func satipStreamGroup(serviceType uint8) string {
	switch serviceType {
	case 0x02, 0x07, 0x0A:
		return "Radio"
	case 0x11, 0x19, 0x1F, 0x20:
		return "HD"
	default:
		return "SD"
	}
}

func (s *SatIPService) scanSource(ctx context.Context, source *models.SatIPSource) error {
	s.log.Info().Str("source_id", source.ID).Str("name", source.Name).Msg("scanning satip source")
	s.Set(source.ID, RefreshStatus{State: "running", Message: "Discovering muxes via NIT..."})

	httpPort := source.HTTPPort
	if httpPort == 0 {
		httpPort = 8875
	}

	cfg := tvsatipscan.Config{
		SeedTimeout: 5 * time.Second,
		MuxTimeout:  20 * time.Second,
		Timeout:     15 * time.Second,
		Parallel:    4,
	}

	scanHost := source.Host
	if _, _, err := net.SplitHostPort(source.Host); err != nil {
		scanHost = net.JoinHostPort(source.Host, "554")
	}

	result, err := tvsatipscan.Scan(scanHost, httpPort, cfg)
	if err != nil {
		return fmt.Errorf("scanning satip device: %w", err)
	}

	s.log.Info().Int("channels", len(result.Channels)).Str("network", result.NetworkName).Msg("scan complete")
	s.Set(source.ID, RefreshStatus{State: "running", Message: fmt.Sprintf("Found %d channels, saving...", len(result.Channels))})

	streams := make([]models.Stream, 0, len(result.Channels))
	keepIDs := make([]string, 0, len(result.Channels))

	for _, ch := range result.Channels {
		contentHash := source.ID + ":" + strconv.Itoa(int(ch.ServiceID))
		id := deterministicStreamID(contentHash)
		keepIDs = append(keepIDs, id)
		streams = append(streams, models.Stream{
			ID:            id,
			SatIPSourceID: source.ID,
			Name:          ch.Name,
			URL:           ch.RTSPURL(source.Host),
			Group:         satipStreamGroup(ch.ServiceType),
			ContentHash:   contentHash,
			IsActive:      true,
		})
	}

	return s.upsertAndFinalizeSource(ctx, source, streams, keepIDs)
}

func (s *SatIPService) upsertAndFinalizeSource(ctx context.Context, source *models.SatIPSource, streams []models.Stream, keepIDs []string) error {
	if err := s.streamStore.BulkUpsert(ctx, streams); err != nil {
		return fmt.Errorf("upserting streams: %w", err)
	}

	deletedIDs, err := s.streamStore.DeleteStaleBySatIPSourceID(ctx, source.ID, keepIDs)
	if err != nil {
		return fmt.Errorf("deleting stale streams: %w", err)
	}

	if len(deletedIDs) > 0 && s.channelStore != nil {
		if err := s.channelStore.RemoveStreamMappings(ctx, deletedIDs); err != nil {
			s.log.Error().Err(err).Int("count", len(deletedIDs)).Msg("failed to clean up channel stream mappings")
		}
	}

	if err := s.streamStore.Save(); err != nil {
		s.log.Error().Err(err).Msg("failed to save stream store")
	}
	s.log.Info().Int("count", len(streams)).Msg("upserted satip streams")

	now := time.Now()
	if err := s.satipSourceStore.UpdateLastScanned(ctx, source.ID, now); err != nil {
		return fmt.Errorf("updating last scanned: %w", err)
	}
	if err := s.satipSourceStore.UpdateStreamCount(ctx, source.ID, len(streams)); err != nil {
		return fmt.Errorf("updating stream count: %w", err)
	}

	return nil
}

func (s *SatIPService) ClearSource(ctx context.Context, sourceID string) error {
	if _, err := s.satipSourceStore.GetByID(ctx, sourceID); err != nil {
		return fmt.Errorf("getting satip source: %w", err)
	}

	streams, err := s.streamStore.ListBySatIPSourceID(ctx, sourceID)
	if err != nil {
		return fmt.Errorf("listing satip streams: %w", err)
	}

	deletedIDs := make([]string, 0, len(streams))
	for _, st := range streams {
		deletedIDs = append(deletedIDs, st.ID)
	}

	if err := s.streamStore.DeleteBySatIPSourceID(ctx, sourceID); err != nil {
		return fmt.Errorf("deleting satip streams: %w", err)
	}
	if err := s.streamStore.Save(); err != nil {
		s.log.Error().Err(err).Msg("failed to save stream store after satip clear")
	}

	if len(deletedIDs) > 0 && s.channelStore != nil {
		if err := s.channelStore.RemoveStreamMappings(ctx, deletedIDs); err != nil {
			s.log.Error().Err(err).Msg("failed to clean up channel stream mappings on satip clear")
		}
	}

	if err := s.satipSourceStore.UpdateStreamCount(ctx, sourceID, 0); err != nil {
		s.log.Error().Err(err).Msg("failed to update stream count after clear")
	}
	return nil
}
