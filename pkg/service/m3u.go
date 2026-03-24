package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/config"
	"github.com/gavinmcnair/tvproxy/pkg/httputil"
	"github.com/gavinmcnair/tvproxy/pkg/m3u"
	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
	"github.com/gavinmcnair/tvproxy/pkg/store"
	"github.com/gavinmcnair/tvproxy/pkg/xtream"
)

type RefreshStatus struct {
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
}

type M3UService struct {
	m3uAccountRepo *repository.M3UAccountRepository
	streamStore    store.StreamStore
	channelRepo    *repository.ChannelRepository
	logoService    *LogoService
	config         *config.Config
	httpClient     *http.Client
	log            zerolog.Logger

	statusMu sync.RWMutex
	statuses map[string]RefreshStatus
}

func NewM3UService(
	m3uAccountRepo *repository.M3UAccountRepository,
	streamStore store.StreamStore,
	channelRepo *repository.ChannelRepository,
	logoService *LogoService,
	cfg *config.Config,
	httpClient *http.Client,
	log zerolog.Logger,
) *M3UService {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &M3UService{
		m3uAccountRepo: m3uAccountRepo,
		streamStore:    streamStore,
		channelRepo:    channelRepo,
		logoService:    logoService,
		config:         cfg,
		httpClient:     httpClient,
		log:            log.With().Str("service", "m3u").Logger(),
		statuses:       make(map[string]RefreshStatus),
	}
}

func (s *M3UService) Log() *zerolog.Logger { return &s.log }

func (s *M3UService) CreateAccount(ctx context.Context, account *models.M3UAccount) error {
	if err := s.m3uAccountRepo.Create(ctx, account); err != nil {
		return fmt.Errorf("creating m3u account: %w", err)
	}
	return nil
}

func (s *M3UService) GetAccount(ctx context.Context, id string) (*models.M3UAccount, error) {
	account, err := s.m3uAccountRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("getting m3u account: %w", err)
	}
	return account, nil
}

func (s *M3UService) ListAccounts(ctx context.Context) ([]models.M3UAccount, error) {
	accounts, err := s.m3uAccountRepo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing m3u accounts: %w", err)
	}
	return accounts, nil
}

func (s *M3UService) UpdateAccount(ctx context.Context, account *models.M3UAccount) error {
	if err := s.m3uAccountRepo.Update(ctx, account); err != nil {
		return fmt.Errorf("updating m3u account: %w", err)
	}
	return nil
}

func (s *M3UService) DeleteAccount(ctx context.Context, id string) error {
	if err := s.streamStore.DeleteByAccountID(ctx, id); err != nil {
		return fmt.Errorf("deleting streams for account: %w", err)
	}
	if err := s.streamStore.Save(); err != nil {
		s.log.Error().Err(err).Msg("failed to save stream store after account delete")
	}
	if err := s.m3uAccountRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("deleting m3u account: %w", err)
	}
	return nil
}

func (s *M3UService) setStatus(id string, st RefreshStatus) {
	s.statusMu.Lock()
	s.statuses[id] = st
	s.statusMu.Unlock()
}

func (s *M3UService) GetStatus(id string) RefreshStatus {
	s.statusMu.RLock()
	st := s.statuses[id]
	s.statusMu.RUnlock()
	return st
}

func (s *M3UService) RefreshAccount(ctx context.Context, accountID string) error {
	account, err := s.m3uAccountRepo.GetByID(ctx, accountID)
	if err != nil {
		return fmt.Errorf("getting account: %w", err)
	}

	s.setStatus(accountID, RefreshStatus{State: "running", Message: "Refreshing..."})

	if err := s.refreshAccount(ctx, account); err != nil {
		s.m3uAccountRepo.UpdateLastError(ctx, account.ID, err.Error())
		s.setStatus(accountID, RefreshStatus{State: "error", Message: err.Error()})
		return err
	}

	s.m3uAccountRepo.UpdateLastError(ctx, account.ID, "")
	s.setStatus(accountID, RefreshStatus{State: "done", Message: "Refresh complete"})
	return nil
}

func (s *M3UService) refreshAccount(ctx context.Context, account *models.M3UAccount) error {
	if account.Type == "xtream" {
		return s.refreshXtreamAccount(ctx, account)
	}
	return s.refreshM3UAccount(ctx, account)
}

func (s *M3UService) refreshXtreamAccount(ctx context.Context, account *models.M3UAccount) error {
	s.log.Info().Str("account_id", account.ID).Str("name", account.Name).Msg("refreshing xtream account")

	xtreamTimeout := s.config.Settings.Network.XtreamAPITimeout
	client := xtream.NewClient(account.URL, account.Username, account.Password, s.config.UserAgent, s.config.BypassHeader, s.config.BypassSecret, xtreamTimeout, s.httpClient.Transport)

	if _, err := client.Authenticate(ctx); err != nil {
		return fmt.Errorf("xtream authentication failed: %w", err)
	}

	liveStreams, err := client.GetLiveStreams(ctx)
	if err != nil {
		return fmt.Errorf("getting xtream live streams: %w", err)
	}

	s.log.Info().Int("streams", len(liveStreams)).Msg("fetched xtream live streams")

	seen := make(map[string]struct{}, len(liveStreams))
	streams := make([]models.Stream, 0, len(liveStreams))
	keepIDs := make([]string, 0, len(liveStreams))
	for _, xs := range liveStreams {
		hash := computeContentHash(xs.Name, account.ID)
		if _, dup := seen[hash]; dup {
			continue
		}
		seen[hash] = struct{}{}
		id := deterministicStreamID(hash)
		keepIDs = append(keepIDs, id)
		streams = append(streams, models.Stream{
			ID:           id,
			M3UAccountID: account.ID,
			Name:         xs.Name,
			URL:          client.GetStreamURL(xs.StreamID, "ts"),
			Group:        xs.CategoryName,
			Logo:         xs.StreamIcon,
			TvgID:        xs.EPGChannelID,
			ContentHash:  hash,
			IsActive:     true,
		})
	}

	return s.upsertAndFinalize(ctx, account, streams, keepIDs)
}

func (s *M3UService) refreshM3UAccount(ctx context.Context, account *models.M3UAccount) error {
	s.log.Info().Str("account_id", account.ID).Str("name", account.Name).Msg("refreshing m3u account")

	body, err := httputil.FetchAndDecompress(ctx, s.httpClient, s.config, account.URL, s.log)
	if err != nil {
		return fmt.Errorf("fetching m3u url: %w", err)
	}
	defer body.Close()

	entries, err := m3u.Parse(body)
	if err != nil {
		return fmt.Errorf("parsing m3u: %w", err)
	}

	s.log.Info().Int("entries", len(entries)).Msg("parsed m3u entries")

	seen := make(map[string]struct{}, len(entries))
	streams := make([]models.Stream, 0, len(entries))
	keepIDs := make([]string, 0, len(entries))
	for _, entry := range entries {
		hash := computeContentHash(entry.Name, account.ID)
		if _, dup := seen[hash]; dup {
			continue
		}
		seen[hash] = struct{}{}
		id := deterministicStreamID(hash)
		keepIDs = append(keepIDs, id)
		streams = append(streams, models.Stream{
			ID:           id,
			M3UAccountID: account.ID,
			Name:         entry.Name,
			URL:          entry.URL,
			Group:        entry.Group,
			Logo:         entry.Logo,
			TvgID:        entry.TvgID,
			TvgName:      entry.TvgName,
			ContentHash:  hash,
			IsActive:     true,
		})
	}

	return s.upsertAndFinalize(ctx, account, streams, keepIDs)
}

func (s *M3UService) upsertAndFinalize(ctx context.Context, account *models.M3UAccount, streams []models.Stream, keepIDs []string) error {
	if err := s.streamStore.BulkUpsert(ctx, streams); err != nil {
		return fmt.Errorf("upserting streams: %w", err)
	}

	deletedIDs, err := s.streamStore.DeleteStaleByAccountID(ctx, account.ID, keepIDs)
	if err != nil {
		return fmt.Errorf("deleting stale streams: %w", err)
	}

	if len(deletedIDs) > 0 && s.channelRepo != nil {
		if err := s.channelRepo.RemoveStreamMappings(ctx, deletedIDs); err != nil {
			s.log.Error().Err(err).Int("count", len(deletedIDs)).Msg("failed to clean up channel stream mappings")
		}
	}

	if err := s.streamStore.Save(); err != nil {
		s.log.Error().Err(err).Msg("failed to save stream store")
	}
	s.log.Info().Int("count", len(streams)).Msg("upserted streams")

	now := time.Now()
	if err := s.m3uAccountRepo.UpdateLastRefreshed(ctx, account.ID, now); err != nil {
		return fmt.Errorf("updating last refreshed: %w", err)
	}
	if err := s.m3uAccountRepo.UpdateStreamCount(ctx, account.ID, len(streams)); err != nil {
		return fmt.Errorf("updating stream count: %w", err)
	}

	s.log.Info().
		Str("account_id", account.ID).
		Int("total", len(streams)).
		Msg("account refresh complete")

	if s.logoService != nil {
		go s.logoService.CacheStreamLogos(context.Background(), streams)
	}

	return nil
}

func (s *M3UService) RefreshAllAccounts(ctx context.Context) error {
	accounts, err := s.m3uAccountRepo.List(ctx)
	if err != nil {
		return fmt.Errorf("listing accounts: %w", err)
	}

	var lastErr error
	for _, account := range accounts {
		if !account.IsEnabled {
			continue
		}
		if err := s.RefreshAccount(ctx, account.ID); err != nil {
			s.log.Error().Err(err).Str("account_id", account.ID).Str("name", account.Name).Msg("failed to refresh account")
			lastErr = err
		}
	}

	if lastErr != nil {
		return fmt.Errorf("one or more accounts failed to refresh: %w", lastErr)
	}
	return nil
}


var streamNamespace = uuid.MustParse("f47ac10b-58cc-4372-a567-0e02b2c3d479")

func deterministicStreamID(contentHash string) string {
	return uuid.NewSHA1(streamNamespace, []byte(contentHash)).String()
}

func computeContentHash(name string, accountID string) string {
	h := sha256.New()
	h.Write([]byte(fmt.Sprintf("%s:%s", accountID, name)))
	return fmt.Sprintf("%x", h.Sum(nil))
}
