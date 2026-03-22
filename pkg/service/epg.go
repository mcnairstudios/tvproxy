package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/config"
	"github.com/gavinmcnair/tvproxy/pkg/httputil"
	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
	"github.com/gavinmcnair/tvproxy/pkg/store"
	"github.com/gavinmcnair/tvproxy/pkg/xmltv"
)

type EPGService struct {
	epgSourceRepo *repository.EPGSourceRepository
	epgStore      store.EPGStore
	config        *config.Config
	httpClient    *http.Client
	log           zerolog.Logger
}

func NewEPGService(
	epgSourceRepo *repository.EPGSourceRepository,
	epgStore store.EPGStore,
	cfg *config.Config,
	httpClient *http.Client,
	log zerolog.Logger,
) *EPGService {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &EPGService{
		epgSourceRepo: epgSourceRepo,
		epgStore:      epgStore,
		config:        cfg,
		httpClient:    httpClient,
		log:           log.With().Str("service", "epg").Logger(),
	}
}

func (s *EPGService) Log() *zerolog.Logger { return &s.log }

func (s *EPGService) CreateSource(ctx context.Context, source *models.EPGSource) error {
	if err := s.epgSourceRepo.Create(ctx, source); err != nil {
		return fmt.Errorf("creating epg source: %w", err)
	}
	return nil
}

func (s *EPGService) GetSource(ctx context.Context, id string) (*models.EPGSource, error) {
	source, err := s.epgSourceRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("getting epg source: %w", err)
	}
	return source, nil
}

func (s *EPGService) ListSources(ctx context.Context) ([]models.EPGSource, error) {
	sources, err := s.epgSourceRepo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing epg sources: %w", err)
	}
	return sources, nil
}

func (s *EPGService) UpdateSource(ctx context.Context, source *models.EPGSource) error {
	if err := s.epgSourceRepo.Update(ctx, source); err != nil {
		return fmt.Errorf("updating epg source: %w", err)
	}
	return nil
}

func (s *EPGService) DeleteSource(ctx context.Context, id string) error {
	epgDataList, err := s.epgStore.ListBySourceID(ctx, id)
	if err != nil {
		return fmt.Errorf("listing epg data for source: %w", err)
	}
	for _, epgData := range epgDataList {
		if err := s.epgStore.DeleteProgramsByEPGDataID(ctx, epgData.ID); err != nil {
			s.log.Error().Err(err).Str("epg_data_id", epgData.ID).Msg("failed to delete program data")
		}
	}
	if err := s.epgStore.DeleteBySourceID(ctx, id); err != nil {
		return fmt.Errorf("deleting epg data for source: %w", err)
	}
	if err := s.epgStore.Save(); err != nil {
		s.log.Error().Err(err).Msg("failed to save epg store after source delete")
	}

	if err := s.epgSourceRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("deleting epg source: %w", err)
	}
	return nil
}

func (s *EPGService) RefreshSource(ctx context.Context, sourceID string) error {
	source, err := s.epgSourceRepo.GetByID(ctx, sourceID)
	if err != nil {
		return fmt.Errorf("getting source: %w", err)
	}

	if err := s.refreshSource(ctx, source); err != nil {
		s.epgSourceRepo.UpdateLastError(ctx, source.ID, err.Error())
		return err
	}

	s.epgSourceRepo.UpdateLastError(ctx, source.ID, "")
	return nil
}

func (s *EPGService) refreshSource(ctx context.Context, source *models.EPGSource) error {
	s.log.Info().Str("source_id", source.ID).Str("name", source.Name).Msg("refreshing epg source")

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

	existingData, err := s.epgStore.ListBySourceID(ctx, source.ID)
	if err != nil {
		return fmt.Errorf("listing existing epg data: %w", err)
	}
	for _, ed := range existingData {
		if err := s.epgStore.DeleteProgramsByEPGDataID(ctx, ed.ID); err != nil {
			s.log.Error().Err(err).Str("epg_data_id", ed.ID).Msg("failed to delete existing program data")
		}
	}
	if err := s.epgStore.DeleteBySourceID(ctx, source.ID); err != nil {
		return fmt.Errorf("deleting existing epg data: %w", err)
	}

	epgDataItems := make([]models.EPGData, 0, len(tv.Channels))
	for _, ch := range tv.Channels {
		epgDataItems = append(epgDataItems, models.EPGData{
			EPGSourceID: source.ID,
			ChannelID:   ch.ID,
			Name:        ch.DisplayName,
			Icon:        ch.Icon,
		})
	}
	if err := s.epgStore.BulkCreateEPGData(ctx, epgDataItems); err != nil {
		return fmt.Errorf("bulk creating epg data: %w", err)
	}

	channelIDMap := make(map[string]string, len(epgDataItems))
	for _, d := range epgDataItems {
		channelIDMap[d.ChannelID] = d.ID
	}

	programs := make([]models.ProgramData, 0, len(tv.Programmes))
	for _, prog := range tv.Programmes {
		epgDataID, ok := channelIDMap[prog.Channel]
		if !ok {
			continue
		}
		programs = append(programs, models.ProgramData{
			EPGDataID:         epgDataID,
			Title:             prog.Title,
			Description:       prog.Description,
			Start:             prog.Start,
			Stop:              prog.Stop,
			Category:          prog.Category,
			EpisodeNum:        prog.EpisodeNum,
			Icon:              prog.Icon,
			Subtitle:          prog.Subtitle,
			Date:              prog.Date,
			Language:          prog.Language,
			IsNew:             prog.IsNew,
			IsPreviouslyShown: prog.IsPreviouslyShown,
			Credits:           prog.Credits,
			Rating:            prog.Rating,
			RatingIcon:        prog.RatingIcon,
			StarRating:        prog.StarRating,
			SubCategories:     prog.SubCategories,
			EpisodeNumSystem:  prog.EpisodeNumSystem,
		})
	}

	batchSize := 5000
	if s.config.Settings != nil && s.config.Settings.EPG.BatchSize > 0 {
		batchSize = s.config.Settings.EPG.BatchSize
	}
	programCount := 0
	for i := 0; i < len(programs); i += batchSize {
		end := i + batchSize
		if end > len(programs) {
			end = len(programs)
		}
		batch := programs[i:end]
		if err := s.epgStore.BulkCreatePrograms(ctx, batch); err != nil {
			return fmt.Errorf("bulk creating program data (batch %d-%d): %w", i, end, err)
		}
		programCount += len(batch)
		s.log.Debug().Int("inserted", programCount).Int("total", len(programs)).Msg("program data bulk insert progress")
	}

	if err := s.epgStore.Save(); err != nil {
		s.log.Error().Err(err).Msg("failed to save epg store")
	}

	now := time.Now()
	source.LastRefreshed = &now
	source.ChannelCount = len(tv.Channels)
	source.ProgramCount = programCount
	if err := s.epgSourceRepo.Update(ctx, source); err != nil {
		return fmt.Errorf("updating epg source: %w", err)
	}

	s.log.Info().
		Str("source_id", source.ID).
		Int("channels", len(tv.Channels)).
		Int("programs", programCount).
		Msg("source refresh complete")

	return nil
}

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
			s.log.Error().Err(err).Str("source_id", source.ID).Str("name", source.Name).Msg("failed to refresh source")
			lastErr = err
		}
	}

	if lastErr != nil {
		return fmt.Errorf("one or more sources failed to refresh: %w", lastErr)
	}
	return nil
}

func (s *EPGService) fetchURL(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("User-Agent", s.config.UserAgent)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return httputil.DecompressReader(resp.Body, url)
}
