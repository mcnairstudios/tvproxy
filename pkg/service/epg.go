package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
	"github.com/gavinmcnair/tvproxy/pkg/xmltv"
)

// EPGService handles EPG source management and data synchronization.
type EPGService struct {
	epgSourceRepo   *repository.EPGSourceRepository
	epgDataRepo     *repository.EPGDataRepository
	programDataRepo *repository.ProgramDataRepository
	userAgentRepo   *repository.UserAgentRepository
	log             zerolog.Logger
}

// NewEPGService creates a new EPGService.
func NewEPGService(
	epgSourceRepo *repository.EPGSourceRepository,
	epgDataRepo *repository.EPGDataRepository,
	programDataRepo *repository.ProgramDataRepository,
	userAgentRepo *repository.UserAgentRepository,
	log zerolog.Logger,
) *EPGService {
	return &EPGService{
		epgSourceRepo:   epgSourceRepo,
		epgDataRepo:     epgDataRepo,
		programDataRepo: programDataRepo,
		userAgentRepo:   userAgentRepo,
		log:             log.With().Str("service", "epg").Logger(),
	}
}

// CreateSource creates a new EPG source.
func (s *EPGService) CreateSource(ctx context.Context, source *models.EPGSource) error {
	if err := s.epgSourceRepo.Create(ctx, source); err != nil {
		return fmt.Errorf("creating epg source: %w", err)
	}
	return nil
}

// GetSource returns an EPG source by ID.
func (s *EPGService) GetSource(ctx context.Context, id int64) (*models.EPGSource, error) {
	source, err := s.epgSourceRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("getting epg source: %w", err)
	}
	return source, nil
}

// ListSources returns all EPG sources.
func (s *EPGService) ListSources(ctx context.Context) ([]models.EPGSource, error) {
	sources, err := s.epgSourceRepo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing epg sources: %w", err)
	}
	return sources, nil
}

// UpdateSource updates an existing EPG source.
func (s *EPGService) UpdateSource(ctx context.Context, source *models.EPGSource) error {
	if err := s.epgSourceRepo.Update(ctx, source); err != nil {
		return fmt.Errorf("updating epg source: %w", err)
	}
	return nil
}

// DeleteSource deletes an EPG source by ID and its associated data.
func (s *EPGService) DeleteSource(ctx context.Context, id int64) error {
	// Delete associated EPG data and programs first
	epgDataList, err := s.epgDataRepo.ListBySourceID(ctx, id)
	if err != nil {
		return fmt.Errorf("listing epg data for source: %w", err)
	}
	for _, epgData := range epgDataList {
		if err := s.programDataRepo.DeleteByEPGDataID(ctx, epgData.ID); err != nil {
			s.log.Error().Err(err).Int64("epg_data_id", epgData.ID).Msg("failed to delete program data")
		}
	}
	if err := s.epgDataRepo.DeleteBySourceID(ctx, id); err != nil {
		return fmt.Errorf("deleting epg data for source: %w", err)
	}

	if err := s.epgSourceRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("deleting epg source: %w", err)
	}
	return nil
}

// RefreshSource fetches the XMLTV URL for the given source, parses it,
// and stores the EPG channel and program data.
func (s *EPGService) RefreshSource(ctx context.Context, sourceID int64) error {
	source, err := s.epgSourceRepo.GetByID(ctx, sourceID)
	if err != nil {
		return fmt.Errorf("getting source: %w", err)
	}

	s.log.Info().Int64("source_id", source.ID).Str("name", source.Name).Msg("refreshing epg source")

	body, err := s.fetchURL(ctx, source.URL)
	if err != nil {
		return fmt.Errorf("fetching xmltv url: %w", err)
	}
	defer body.Close()

	tv, err := xmltv.Parse(body)
	if err != nil {
		return fmt.Errorf("parsing xmltv: %w", err)
	}

	s.log.Info().
		Int("channels", len(tv.Channels)).
		Int("programmes", len(tv.Programmes)).
		Msg("parsed xmltv data")

	// Delete existing data for this source before inserting fresh data
	existingData, err := s.epgDataRepo.ListBySourceID(ctx, sourceID)
	if err != nil {
		return fmt.Errorf("listing existing epg data: %w", err)
	}
	for _, ed := range existingData {
		if err := s.programDataRepo.DeleteByEPGDataID(ctx, ed.ID); err != nil {
			s.log.Error().Err(err).Int64("epg_data_id", ed.ID).Msg("failed to delete existing program data")
		}
	}
	if err := s.epgDataRepo.DeleteBySourceID(ctx, sourceID); err != nil {
		return fmt.Errorf("deleting existing epg data: %w", err)
	}

	// Store new EPG channel data and build a channel ID to EPGData ID map
	channelIDMap := make(map[string]int64, len(tv.Channels))
	for _, ch := range tv.Channels {
		epgData := &models.EPGData{
			EPGSourceID: sourceID,
			ChannelID:   ch.ID,
			Name:        ch.DisplayName,
			Icon:        ch.Icon,
		}
		if err := s.epgDataRepo.Create(ctx, epgData); err != nil {
			s.log.Error().Err(err).Str("channel_id", ch.ID).Msg("failed to create epg data")
			continue
		}
		channelIDMap[ch.ID] = epgData.ID
	}

	// Store program data
	programCount := 0
	for _, prog := range tv.Programmes {
		epgDataID, ok := channelIDMap[prog.Channel]
		if !ok {
			continue
		}
		programData := &models.ProgramData{
			EPGDataID:   epgDataID,
			Title:       prog.Title,
			Description: prog.Description,
			Start:       prog.Start,
			Stop:        prog.Stop,
			Category:    prog.Category,
			EpisodeNum:  prog.EpisodeNum,
			Icon:        prog.Icon,
		}
		if err := s.programDataRepo.Create(ctx, programData); err != nil {
			s.log.Error().Err(err).Str("title", prog.Title).Msg("failed to create program data")
			continue
		}
		programCount++
	}

	// Update source metadata
	now := time.Now()
	source.LastRefreshed = &now
	source.ChannelCount = len(tv.Channels)
	source.ProgramCount = programCount
	if err := s.epgSourceRepo.Update(ctx, source); err != nil {
		return fmt.Errorf("updating epg source: %w", err)
	}

	s.log.Info().
		Int64("source_id", source.ID).
		Int("channels", len(tv.Channels)).
		Int("programs", programCount).
		Msg("source refresh complete")

	return nil
}

// RefreshAllSources refreshes all enabled EPG sources.
func (s *EPGService) RefreshAllSources(ctx context.Context) error {
	sources, err := s.epgSourceRepo.List(ctx)
	if err != nil {
		return fmt.Errorf("listing sources: %w", err)
	}

	var lastErr error
	for _, source := range sources {
		if !source.IsEnabled {
			continue
		}
		if err := s.RefreshSource(ctx, source.ID); err != nil {
			s.log.Error().Err(err).Int64("source_id", source.ID).Str("name", source.Name).Msg("failed to refresh source")
			lastErr = err
		}
	}

	if lastErr != nil {
		return fmt.Errorf("one or more sources failed to refresh: %w", lastErr)
	}
	return nil
}

// fetchURL retrieves the content from the given URL using the default user agent.
func (s *EPGService) fetchURL(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	ua, err := s.userAgentRepo.GetDefault(ctx)
	if err == nil && ua != nil {
		req.Header.Set("User-Agent", ua.UserAgent)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return resp.Body, nil
}
