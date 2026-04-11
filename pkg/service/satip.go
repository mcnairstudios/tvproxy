package service

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/media"
	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/store"
	"github.com/gavinmcnair/tvproxy/pkg/tvsatipscan"
)

type SatIPService struct {
	satipSourceStore store.SatIPSourceStore
	streamStore      store.StreamStore
	channelStore     store.ChannelStore
	probeCache       store.ProbeCache
	log              zerolog.Logger
	StatusTracker
}

func NewSatIPService(
	satipSourceStore store.SatIPSourceStore,
	streamStore store.StreamStore,
	channelStore store.ChannelStore,
	probeCache store.ProbeCache,
	log zerolog.Logger,
) *SatIPService {
	return &SatIPService{
		satipSourceStore: satipSourceStore,
		streamStore:      streamStore,
		channelStore:     channelStore,
		probeCache:       probeCache,
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

func satipTracks(comps []tvsatipscan.StreamComponent) []models.StreamTrack {
	if len(comps) == 0 {
		return nil
	}
	tracks := make([]models.StreamTrack, 0, len(comps))
	for _, c := range comps {
		tracks = append(tracks, models.StreamTrack{
			PID:       c.PID,
			Type:      c.TypeName,
			Category:  c.Category,
			Language:  c.Language,
			AudioType: c.AudioType,
			Label:     c.Label,
		})
	}
	return tracks
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
		SeedTimeout:     20 * time.Second,
		MuxTimeout:      60 * time.Second,
		Timeout:         60 * time.Second,
		Parallel:        2,
		Log:             s.log,
		TransmitterFile: source.TransmitterFile,
		OnMuxScanned: func(done, total int) {
			s.Set(source.ID, RefreshStatus{
				State:    "running",
				Progress: done,
				Total:    total,
				Message:  fmt.Sprintf("Scanning mux %d/%d...", done, total),
			})
		},
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
		group := satipStreamGroup(ch.ServiceType)
		tracks := satipTracks(ch.Streams)
		streams = append(streams, models.Stream{
			ID:            id,
			SatIPSourceID: source.ID,
			Name:          ch.Name,
			URL:           ch.RTSPURL(source.Host),
			Group:         group,
			ContentHash:   contentHash,
			IsActive:      true,
			Tracks:        tracks,
		})

		if s.probeCache != nil {
			hasVideo := group != "Radio"
			probe := &media.ProbeResult{
				HasVideo: hasVideo,
			}
			if hasVideo {
				for _, t := range tracks {
					if t.Category == "video" {
						probe.Video = &media.VideoInfo{Codec: t.Type}
						break
					}
				}
			}
			for _, t := range tracks {
				if t.Category == "audio" {
					probe.AudioTracks = append(probe.AudioTracks, media.AudioTrack{
						Language: t.Language,
						Codec:    t.Type,
					})
				}
			}
			s.probeCache.SaveProbeByStreamID(id, probe)
		}
	}

	noSignalKeys := make(map[string]bool, len(result.NoSignalMuxes)+len(result.ErrorMuxes))
	for _, tp := range result.NoSignalMuxes {
		noSignalKeys[tp.MuxKey()] = true
	}
	for _, tp := range result.ErrorMuxes {
		noSignalKeys[tp.MuxKey()] = true
	}
	if len(noSignalKeys) > 0 {
		existing, err := s.streamStore.ListBySatIPSourceID(ctx, source.ID)
		if err == nil {
			for _, st := range existing {
				if noSignalKeys[satipMuxKeyFromURL(st.URL)] {
					keepIDs = append(keepIDs, st.ID)
				}
			}
		}
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

func satipMuxKeyFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	q := u.Query()
	freq := q.Get("freq")
	msys := q.Get("msys")
	if freq == "" || msys == "" {
		return ""
	}
	switch msys {
	case "dvbt2":
		plp := q.Get("plp")
		if plp == "" {
			plp = "0"
		}
		return fmt.Sprintf("%s/%s/%s", freq, msys, plp)
	case "dvbt":
		return fmt.Sprintf("%s/%s", freq, msys)
	default:
		return fmt.Sprintf("%s/%s", freq, msys)
	}
}
