package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/config"
	"github.com/gavinmcnair/tvproxy/pkg/m3u"
	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
)

// M3UService handles M3U account management and stream synchronization.
type M3UService struct {
	m3uAccountRepo *repository.M3UAccountRepository
	streamRepo     *repository.StreamRepository
	config         *config.Config
	log            zerolog.Logger
}

// NewM3UService creates a new M3UService.
func NewM3UService(
	m3uAccountRepo *repository.M3UAccountRepository,
	streamRepo *repository.StreamRepository,
	cfg *config.Config,
	log zerolog.Logger,
) *M3UService {
	return &M3UService{
		m3uAccountRepo: m3uAccountRepo,
		streamRepo:     streamRepo,
		config:         cfg,
		log:            log.With().Str("service", "m3u").Logger(),
	}
}

// Log returns the service logger for use by handlers.
func (s *M3UService) Log() *zerolog.Logger { return &s.log }

// CreateAccount creates a new M3U account.
func (s *M3UService) CreateAccount(ctx context.Context, account *models.M3UAccount) error {
	if err := s.m3uAccountRepo.Create(ctx, account); err != nil {
		return fmt.Errorf("creating m3u account: %w", err)
	}
	return nil
}

// GetAccount returns an M3U account by ID.
func (s *M3UService) GetAccount(ctx context.Context, id int64) (*models.M3UAccount, error) {
	account, err := s.m3uAccountRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("getting m3u account: %w", err)
	}
	return account, nil
}

// ListAccounts returns all M3U accounts.
func (s *M3UService) ListAccounts(ctx context.Context) ([]models.M3UAccount, error) {
	accounts, err := s.m3uAccountRepo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing m3u accounts: %w", err)
	}
	return accounts, nil
}

// UpdateAccount updates an existing M3U account.
func (s *M3UService) UpdateAccount(ctx context.Context, account *models.M3UAccount) error {
	if err := s.m3uAccountRepo.Update(ctx, account); err != nil {
		return fmt.Errorf("updating m3u account: %w", err)
	}
	return nil
}

// DeleteAccount deletes an M3U account and its associated streams.
func (s *M3UService) DeleteAccount(ctx context.Context, id int64) error {
	if err := s.streamRepo.DeleteByAccountID(ctx, id); err != nil {
		return fmt.Errorf("deleting streams for account: %w", err)
	}
	if err := s.m3uAccountRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("deleting m3u account: %w", err)
	}
	return nil
}

// RefreshAccount fetches the M3U URL for the given account, parses streams,
// and upserts them by matching on content hash. It also updates the account's
// last refresh time and stream count.
func (s *M3UService) RefreshAccount(ctx context.Context, accountID int64) error {
	account, err := s.m3uAccountRepo.GetByID(ctx, accountID)
	if err != nil {
		return fmt.Errorf("getting account: %w", err)
	}

	s.log.Info().Int64("account_id", account.ID).Str("name", account.Name).Msg("refreshing m3u account")

	body, err := s.fetchURL(ctx, account.URL)
	if err != nil {
		return fmt.Errorf("fetching m3u url: %w", err)
	}
	defer body.Close()

	entries, err := m3u.Parse(body)
	if err != nil {
		return fmt.Errorf("parsing m3u: %w", err)
	}

	s.log.Info().Int("entries", len(entries)).Msg("parsed m3u entries")

	// Build a set of content hashes from the new entries.
	// Hash is based on accountID + name only, so duplicate channel names
	// collapse into one stream (first occurrence wins).
	newHashes := make(map[string]struct{}, len(entries))
	streams := make([]models.Stream, 0, len(entries))
	for _, entry := range entries {
		hash := computeContentHash(entry.Name, account.ID)
		if _, dup := newHashes[hash]; dup {
			continue // skip duplicate name
		}
		newHashes[hash] = struct{}{}
		streams = append(streams, models.Stream{
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

	// Get existing streams for this account
	existingStreams, err := s.streamRepo.ListByAccountID(ctx, account.ID)
	if err != nil {
		return fmt.Errorf("listing existing streams: %w", err)
	}

	// Build a map of existing streams by content hash
	existingByHash := make(map[string]*models.Stream, len(existingStreams))
	for i := range existingStreams {
		existingByHash[existingStreams[i].ContentHash] = &existingStreams[i]
	}

	// Determine which streams to create, update, or deactivate
	var toCreate []models.Stream
	var toUpdate []*models.Stream
	for i := range streams {
		if existing, ok := existingByHash[streams[i].ContentHash]; ok {
			existing.Name = streams[i].Name
			existing.URL = streams[i].URL
			existing.Group = streams[i].Group
			existing.Logo = streams[i].Logo
			existing.TvgID = streams[i].TvgID
			existing.TvgName = streams[i].TvgName
			existing.IsActive = true
			toUpdate = append(toUpdate, existing)
		} else {
			toCreate = append(toCreate, streams[i])
		}
	}

	// Deactivate streams that are no longer in the M3U
	for i := range existingStreams {
		if _, ok := newHashes[existingStreams[i].ContentHash]; !ok {
			existingStreams[i].IsActive = false
			toUpdate = append(toUpdate, &existingStreams[i])
		}
	}

	// Bulk update existing streams (batched)
	if len(toUpdate) > 0 {
		if err := s.streamRepo.BulkUpdate(ctx, toUpdate); err != nil {
			return fmt.Errorf("bulk updating streams: %w", err)
		}
		s.log.Info().Int("count", len(toUpdate)).Msg("updated existing streams")
	}

	// Bulk create new streams (batched)
	if len(toCreate) > 0 {
		if err := s.streamRepo.BulkCreate(ctx, toCreate); err != nil {
			return fmt.Errorf("bulk creating streams: %w", err)
		}
		s.log.Info().Int("count", len(toCreate)).Msg("created new streams")
	}

	// Update account refresh time and stream count
	now := time.Now()
	if err := s.m3uAccountRepo.UpdateLastRefreshed(ctx, account.ID, now); err != nil {
		return fmt.Errorf("updating last refreshed: %w", err)
	}
	if err := s.m3uAccountRepo.UpdateStreamCount(ctx, account.ID, len(streams)); err != nil {
		return fmt.Errorf("updating stream count: %w", err)
	}

	s.log.Info().
		Int64("account_id", account.ID).
		Int("total", len(streams)).
		Int("new", len(toCreate)).
		Msg("account refresh complete")

	return nil
}

// RefreshAllAccounts refreshes all enabled M3U accounts.
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
			s.log.Error().Err(err).Int64("account_id", account.ID).Str("name", account.Name).Msg("failed to refresh account")
			lastErr = err
		}
	}

	if lastErr != nil {
		return fmt.Errorf("one or more accounts failed to refresh: %w", lastErr)
	}
	return nil
}

// fetchURL retrieves the content from the given URL using the default user agent.
func (s *M3UService) fetchURL(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("User-Agent", s.config.UserAgent)

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

// computeContentHash generates a SHA-256 hash from the account ID and stream name.
// URL is excluded so that duplicate channel names within the same account collapse
// into a single stream (common in IPTV M3U files with multiple server mirrors).
func computeContentHash(name string, accountID int64) string {
	h := sha256.New()
	h.Write([]byte(fmt.Sprintf("%d:%s", accountID, name)))
	return fmt.Sprintf("%x", h.Sum(nil))
}
