package service

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/config"
	"github.com/gavinmcnair/tvproxy/pkg/ffmpeg"
	"github.com/gavinmcnair/tvproxy/pkg/httputil"
	"github.com/gavinmcnair/tvproxy/pkg/m3u"
	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/mtls"
	"github.com/gavinmcnair/tvproxy/pkg/store"
	"github.com/gavinmcnair/tvproxy/pkg/xtream"
)

type M3UService struct {
	m3uAccountStore store.M3UAccountStore
	streamStore     store.StreamStore
	channelStore    store.ChannelStore
	logoService     *LogoService
	probeCache      store.ProbeCache
	config          *config.Config
	configDir       string
	httpClient      *http.Client
	wgClient        *http.Client
	xtreamCache     *xtream.Cache
	log             zerolog.Logger
	StatusTracker
}

func NewM3UService(
	m3uAccountStore store.M3UAccountStore,
	streamStore store.StreamStore,
	channelStore store.ChannelStore,
	logoService *LogoService,
	probeCache store.ProbeCache,
	cfg *config.Config,
	configDir string,
	httpClient *http.Client,
	log zerolog.Logger,
) *M3UService {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &M3UService{
		m3uAccountStore: m3uAccountStore,
		streamStore:     streamStore,
		channelStore:    channelStore,
		logoService:     logoService,
		probeCache:      probeCache,
		config:          cfg,
		configDir:       configDir,
		httpClient:      httpClient,
		log:             log.With().Str("service", "m3u").Logger(),
		StatusTracker:   NewStatusTracker(),
	}
}

func (s *M3UService) Log() *zerolog.Logger { return &s.log }

func (s *M3UService) SetXtreamCache(c *xtream.Cache) { s.xtreamCache = c }

func (s *M3UService) ResumeSeriesSync(ctx context.Context) {
	s.log.Info().Msg("resume series sync: starting")
	if s.xtreamCache == nil {
		s.log.Info().Msg("resume series sync: no xtream cache, skipping")
		return
	}
	accounts, err := s.m3uAccountStore.List(ctx)
	if err != nil {
		s.log.Warn().Err(err).Msg("resume series sync: failed to list accounts")
		return
	}
	for _, acct := range accounts {
		if acct.Type != "xtream" || !acct.IsEnabled {
			continue
		}
		s.log.Info().Str("account", acct.Name).Msg("checking series sync status")
		xtreamTimeout := s.config.Settings.Network.XtreamAPITimeout
		if xtreamTimeout <= 0 {
			xtreamTimeout = 30 * time.Second
		}
		client := xtream.NewClient(acct.URL, acct.Username, acct.Password, s.config.UserAgent, s.config.BypassHeader, s.config.BypassSecret, xtreamTimeout, s.transportForAccount(&acct))

		fetchCtx, fetchCancel := context.WithTimeout(ctx, 60*time.Second)
		seriesList, err := client.GetSeries(fetchCtx)
		fetchCancel()
		if err != nil {
			s.log.Warn().Err(err).Str("account", acct.Name).Msg("failed to fetch series list for sync resume")
			continue
		}

		seriesWithEpisodes := make(map[string]bool)
		existing, _ := s.streamStore.ListByAccountID(ctx, acct.ID)
		for _, st := range existing {
			if st.VODType == "series" && st.VODEpisode > 0 {
				seriesWithEpisodes[st.VODSeries] = true
			}
		}

		needSync := 0
		for _, sr := range seriesList {
			cleanName, _ := extractLanguage(sr.Name)
			if !seriesWithEpisodes[cleanName] {
				needSync++
			}
		}

		if needSync > 0 {
			s.log.Info().Str("account", acct.Name).Int("pending", needSync).Msg("resuming series sync")
			a := acct
			go s.syncXtreamSeries(&a, seriesList)
		}
	}
}

func (s *M3UService) CreateAccount(ctx context.Context, account *models.M3UAccount) error {
	if err := s.m3uAccountStore.Create(ctx, account); err != nil {
		return fmt.Errorf("creating m3u account: %w", err)
	}
	return nil
}

func (s *M3UService) GetAccount(ctx context.Context, id string) (*models.M3UAccount, error) {
	account, err := s.m3uAccountStore.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("getting m3u account: %w", err)
	}
	return account, nil
}

func (s *M3UService) ListAccounts(ctx context.Context) ([]models.M3UAccount, error) {
	accounts, err := s.m3uAccountStore.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing m3u accounts: %w", err)
	}
	return accounts, nil
}

func (s *M3UService) UpdateAccount(ctx context.Context, account *models.M3UAccount) error {
	if err := s.m3uAccountStore.Update(ctx, account); err != nil {
		return fmt.Errorf("updating m3u account: %w", err)
	}
	s.streamStore.UpdateWireGuardByAccountID(ctx, account.ID, account.UseWireGuard)
	s.streamStore.Save()
	return nil
}

func (s *M3UService) DeleteAccount(ctx context.Context, id string) error {
	if err := s.streamStore.DeleteByAccountID(ctx, id); err != nil {
		return fmt.Errorf("deleting streams for account: %w", err)
	}
	if err := s.streamStore.Save(); err != nil {
		s.log.Error().Err(err).Msg("failed to save stream store after account delete")
	}
	if err := s.m3uAccountStore.Delete(ctx, id); err != nil {
		return fmt.Errorf("deleting m3u account: %w", err)
	}
	return nil
}

func (s *M3UService) HardRefreshAccount(ctx context.Context, accountID string) error {
	s.streamStore.ClearAutoTMDBByAccountID(ctx, accountID)
	s.streamStore.Save()

	account, err := s.m3uAccountStore.GetByID(ctx, accountID)
	if err != nil {
		return fmt.Errorf("getting account: %w", err)
	}
	account.ETag = ""
	s.m3uAccountStore.Update(ctx, account)

	return s.RefreshAccount(ctx, accountID)
}

func (s *M3UService) RefreshAccount(ctx context.Context, accountID string) error {
	account, err := s.m3uAccountStore.GetByID(ctx, accountID)
	if err != nil {
		return fmt.Errorf("getting account: %w", err)
	}

	s.Set(accountID, RefreshStatus{State: "running", Message: "Refreshing..."})

	if err := s.refreshAccount(ctx, account); err != nil {
		s.m3uAccountStore.UpdateLastError(ctx, account.ID, err.Error())
		s.Set(accountID, RefreshStatus{State: "error", Message: err.Error()})
		return err
	}

	s.m3uAccountStore.UpdateLastError(ctx, account.ID, "")
	s.Set(accountID, RefreshStatus{State: "done", Message: "Refresh complete"})
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
	client := xtream.NewClient(account.URL, account.Username, account.Password, s.config.UserAgent, s.config.BypassHeader, s.config.BypassSecret, xtreamTimeout, s.transportForAccount(account))

	if _, err := client.Authenticate(ctx); err != nil {
		return fmt.Errorf("xtream authentication failed: %w", err)
	}

	liveStreams, err := client.GetLiveStreams(ctx)
	if err != nil {
		return fmt.Errorf("getting xtream live streams: %w", err)
	}
	s.log.Info().Int("live", len(liveStreams)).Msg("fetched xtream live streams")

	vodStreams, err := client.GetVODStreams(ctx)
	if err != nil {
		s.log.Warn().Err(err).Msg("failed to fetch xtream VOD streams")
	} else {
		s.log.Info().Int("vod", len(vodStreams)).Msg("fetched xtream VOD streams")
	}

	seriesList, err := client.GetSeries(ctx)
	if err != nil {
		s.log.Warn().Err(err).Msg("failed to fetch xtream series")
	} else {
		s.log.Info().Int("series", len(seriesList)).Msg("fetched xtream series")
	}

	seen := make(map[string]struct{})
	var streams []models.Stream
	var keepIDs []string

	for _, xs := range liveStreams {
		streamURL := client.GetStreamURL(xs.StreamID, "ts")
		hash := computeContentHash(streamURL)
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
			URL:          streamURL,
			Group:        xs.CategoryName,
			Logo:         xs.Icon(),
			TvgID:        xs.EPGChannelID,
			ContentHash:  hash,
			UseWireGuard: account.UseWireGuard,
			IsActive:     true,
		})
	}

	for _, vs := range vodStreams {
		streamURL := client.GetVODStreamURL(vs.StreamID, vs.ContainerExt)
		hash := computeContentHash(streamURL)
		if _, dup := seen[hash]; dup {
			continue
		}
		seen[hash] = struct{}{}
		id := deterministicStreamID(hash)
		keepIDs = append(keepIDs, id)
		cleanName, lang := extractLanguage(vs.Name)
		if lang == "" {
			cleanName, lang = vs.Name, extractLangFromCategory(vs.CategoryName)
		}
		streams = append(streams, models.Stream{
			ID:           id,
			M3UAccountID: account.ID,
			Name:         cleanName,
			URL:          streamURL,
			Group:        vs.CategoryName,
			Logo:         vs.Icon(),
			ContentHash:  hash,
			VODType:      "movie",
			CacheType:    "xtream",
			CacheKey:     vs.StreamID,
			Language:     lang,
			UseWireGuard: account.UseWireGuard,
			IsActive:     true,
		})
		if s.xtreamCache != nil {
			s.xtreamCache.SetMovie(vs.StreamID, &xtream.MovieMeta{
				StreamID:     vs.StreamID,
				Name:         vs.Name,
				PosterURL:    vs.Icon(),
				Rating:       vs.RatingStr(),
				IsAdult:      vs.IsAdult == "1",
				Container:    vs.ContainerExt,
				CategoryName: vs.CategoryName,
			})
		}
	}

	for _, sr := range seriesList {
		hash := computeContentHash(fmt.Sprintf("xtream-series-%d", sr.SeriesID))
		if _, dup := seen[hash]; dup {
			continue
		}
		seen[hash] = struct{}{}
		id := deterministicStreamID(hash)
		keepIDs = append(keepIDs, id)
		cleanName, lang := extractLanguage(sr.Name)
		if lang == "" {
			lang = extractLangFromCategory(sr.CategoryName)
		}
		streams = append(streams, models.Stream{
			ID:           id,
			M3UAccountID: account.ID,
			Name:         cleanName,
			URL:          fmt.Sprintf("%s/series/%s/%s", account.URL, account.Username, account.Password),
			Group:        sr.CategoryName,
			Logo:         sr.Cover,
			ContentHash:  hash,
			VODType:      "series",
			VODSeries:    cleanName,
			CacheType:    "xtream",
			CacheKey:     sr.SeriesID,
			Language:     lang,
			UseWireGuard: account.UseWireGuard,
			IsActive:     true,
		})

		if s.xtreamCache != nil {
			sm := &xtream.SeriesMeta{
				SeriesID:     sr.SeriesID,
				Name:         sr.Name,
				Plot:         sr.Plot,
				Cast:         sr.Cast,
				Director:     sr.Director,
				Genre:        sr.Genre,
				ReleaseDate:  sr.ReleaseDate,
				Rating:       sr.Rating,
				PosterURL:    sr.Cover,
				Trailer:      sr.YouTubeTrailer,
				CategoryName: sr.CategoryName,
			}
			if len(sr.BackdropPath) > 0 {
				sm.BackdropURL = sr.BackdropPath[0]
			}
			s.xtreamCache.SetSeries(sr.SeriesID, sm)
		}
	}

	s.log.Info().Int("total", len(streams)).Msg("xtream refresh complete")
	if s.xtreamCache != nil {
		s.xtreamCache.Save()
		s.logoService.QueuePrefetch(s.xtreamCache.PosterURLs())
		go s.syncXtreamSeries(account, seriesList)
	}
	return s.upsertAndFinalize(ctx, account, streams, keepIDs)
}

func (s *M3UService) httpClientForAccount(account *models.M3UAccount) *http.Client {
	if account.TLSEnrolled && s.configDir != "" {
		if tlsClient, err := mtls.TLSClient(s.configDir, account.ID); err == nil {
			return tlsClient
		}
	}
	if account.UseWireGuard && s.wgClient != nil {
		return s.wgClient
	}
	return s.httpClient
}

func (s *M3UService) SetWGClient(c *http.Client) { s.wgClient = c }

func (s *M3UService) transportForAccount(account *models.M3UAccount) http.RoundTripper {
	if account.UseWireGuard && s.wgClient != nil {
		return s.wgClient.Transport
	}
	return s.httpClientForAccount(account).Transport
}

func (s *M3UService) refreshM3UAccount(ctx context.Context, account *models.M3UAccount) error {
	s.log.Info().Str("account_id", account.ID).Str("name", account.Name).Msg("refreshing m3u account")
	s.Set(account.ID, RefreshStatus{State: "running", Message: "Downloading playlist..."})

	client := s.httpClientForAccount(account)
	result, err := httputil.FetchConditional(ctx, client, s.config, account.URL, account.ETag, s.log)
	if err != nil {
		return fmt.Errorf("fetching m3u url: %w", err)
	}
	if !result.Changed {
		s.log.Info().Str("account_id", account.ID).Msg("m3u unchanged (etag match)")
		return nil
	}
	defer result.Body.Close()

	if result.ETag != account.ETag {
		s.m3uAccountStore.UpdateETag(ctx, account.ID, result.ETag)
	}

	s.Set(account.ID, RefreshStatus{State: "running", Message: "Parsing entries..."})
	entries, err := m3u.Parse(result.Body)
	if err != nil {
		return fmt.Errorf("parsing m3u: %w", err)
	}

	s.log.Info().Int("entries", len(entries)).Msg("parsed m3u entries")
	s.Set(account.ID, RefreshStatus{State: "running", Message: "Processing streams...", Total: len(entries)})

	m3uSvc := s
	seen := make(map[string]struct{}, len(entries))
	streams := make([]models.Stream, 0, len(entries))
	keepIDs := make([]string, 0, len(entries))
	for _, entry := range entries {
		hash := computeContentHash(entry.URL)
		if _, dup := seen[hash]; dup {
			continue
		}
		seen[hash] = struct{}{}
		id := deterministicStreamID(hash)
		keepIDs = append(keepIDs, id)
		s := models.Stream{
			ID:            id,
			M3UAccountID:  account.ID,
			Name:          entry.Name,
			URL:           entry.URL,
			Group:         entry.Group,
			Logo:          entry.Logo,
			TvgID:         entry.TvgID,
			TvgName:       entry.TvgName,
			ContentHash:   hash,
			UseWireGuard:  account.UseWireGuard,
			IsActive:      true,
			VODType:       entry.TVPType,
			VODSeries:     entry.TVPSeries,
			VODCollection: entry.TVPCollection,
			VODVCodec:     entry.TVPVCodec,
			VODACodec:     entry.TVPACodec,
			VODRes:        entry.TVPRes,
			VODAudio:      entry.TVPAudio,
		}
		if entry.TVPSeason != "" {
			fmt.Sscanf(entry.TVPSeason, "%d", &s.VODSeason)
		}
		if entry.TVPEpisode != "" {
			fmt.Sscanf(entry.TVPEpisode, "%d", &s.VODEpisode)
		}
		if entry.TVPDur != "" {
			fmt.Sscanf(entry.TVPDur, "%f", &s.VODDuration)
		}
		s.VODSeasonName = entry.TVPSeasonName
		s.VODYear = extractYearFromName(entry.Name)
		streams = append(streams, s)

		if entry.TVPVCodec != "" && m3uSvc.probeCache != nil {
			probe := &ffmpeg.ProbeResult{
				HasVideo: true,
				Video: &ffmpeg.VideoInfo{
					Codec: strings.ToLower(entry.TVPVCodec),
				},
			}
			if entry.TVPDur != "" {
				fmt.Sscanf(entry.TVPDur, "%f", &probe.Duration)
			}
			if entry.TVPACodec != "" {
				probe.AudioTracks = append(probe.AudioTracks, ffmpeg.AudioTrack{
					Codec: strings.ToLower(entry.TVPACodec),
				})
			}
			m3uSvc.probeCache.SaveProbe(ffmpeg.StreamHash(entry.URL), probe)
		}
	}

	return s.upsertAndFinalize(ctx, account, streams, keepIDs)
}

func (s *M3UService) upsertAndFinalize(ctx context.Context, account *models.M3UAccount, streams []models.Stream, keepIDs []string) error {
	s.Set(account.ID, RefreshStatus{State: "running", Message: "Saving streams...", Total: len(streams), Progress: len(streams)})
	if err := s.streamStore.BulkUpsert(ctx, streams); err != nil {
		return fmt.Errorf("upserting streams: %w", err)
	}

	existing, _ := s.streamStore.ListByAccountID(ctx, account.ID)
	for _, st := range existing {
		if st.VODType == "series" && st.VODEpisode > 0 {
			keepIDs = append(keepIDs, st.ID)
		}
	}

	deletedIDs, err := s.streamStore.DeleteStaleByAccountID(ctx, account.ID, keepIDs)
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
	s.log.Info().Int("count", len(streams)).Msg("upserted streams")

	now := time.Now()
	if err := s.m3uAccountStore.UpdateLastRefreshed(ctx, account.ID, now); err != nil {
		return fmt.Errorf("updating last refreshed: %w", err)
	}
	if err := s.m3uAccountStore.UpdateStreamCount(ctx, account.ID, len(streams)); err != nil {
		return fmt.Errorf("updating stream count: %w", err)
	}
	s.log.Info().
		Str("account_id", account.ID).
		Int("total", len(streams)).
		Msg("account refresh complete")

	return nil
}

func (s *M3UService) syncXtreamSeries(account *models.M3UAccount, seriesList []xtream.Series) {
	xtreamTimeout := s.config.Settings.Network.XtreamAPITimeout
	client := xtream.NewClient(account.URL, account.Username, account.Password, s.config.UserAgent, s.config.BypassHeader, s.config.BypassSecret, xtreamTimeout, s.transportForAccount(account))

	s.log.Info().Int("series", len(seriesList)).Msg("starting background xtream series sync")

	ctx := context.Background()

	seriesWithEpisodes := make(map[string]bool)
	existing, _ := s.streamStore.ListByAccountID(ctx, account.ID)
	for _, st := range existing {
		if st.VODType == "series" && st.VODEpisode > 0 {
			seriesWithEpisodes[st.VODSeries] = true
		}
	}

	synced := 0
	for _, sr := range seriesList {
		if sm := s.xtreamCache.GetSeries(sr.SeriesID); sm != nil && len(sm.Seasons) > 0 {
			cleanName, _ := extractLanguage(sr.Name)
			if seriesWithEpisodes[cleanName] {
				synced++
				continue
			}
		}

		info, err := client.GetSeriesInfo(ctx, sr.SeriesID)
		if err != nil {
			continue
		}

		sm := s.xtreamCache.GetSeries(sr.SeriesID)
		if sm == nil {
			continue
		}
		for _, rawSeason := range info.RawSeasons {
			sm.Seasons = append(sm.Seasons, xtream.SeasonMeta{
				SeasonNumber: rawSeason.SeasonNumber,
				Name:         rawSeason.Name,
				AirDate:      rawSeason.AirDate,
				EpisodeCount: rawSeason.EpisodeCount,
				CoverURL:     rawSeason.Cover,
			})
		}
		s.xtreamCache.SetSeries(sr.SeriesID, sm)

		cleanName, lang := extractLanguage(sr.Name)
		if lang == "" {
			lang = extractLangFromCategory(sr.CategoryName)
		}

		var episodeStreams []models.Stream
		for seasonNum, episodes := range info.Seasons {
			var season int
			fmt.Sscanf(seasonNum, "%d", &season)
			for _, ep := range episodes {
				var epID int
				fmt.Sscanf(ep.ID, "%d", &epID)
				if epID == 0 {
					continue
				}
				epSeason := season
				if ep.Info.Season > 0 {
					epSeason = ep.Info.Season
				}
				streamURL := client.GetSeriesStreamURL(epID, ep.ContainerExt)
				hash := computeContentHash(streamURL)
				id := deterministicStreamID(hash)

				episodeStreams = append(episodeStreams, models.Stream{
					ID:           id,
					M3UAccountID: account.ID,
					Name:         ep.Title,
					URL:          streamURL,
					Group:        sr.CategoryName,
					Logo:         sr.Cover,
					ContentHash:  hash,
					VODType:      "series",
					VODSeries:    cleanName,
					VODSeason:    epSeason,
					VODEpisode:   ep.EpisodeNum,
					VODDuration:  float64(ep.Info.DurationSecs),
					CacheType:    "xtream",
					CacheKey:     epID,
					Language:     lang,
					UseWireGuard: account.UseWireGuard,
					IsActive:     true,
				})

				s.xtreamCache.SetEpisode(epID, &xtream.EpisodeMeta{
					ID:         epID,
					EpisodeNum: ep.EpisodeNum,
					Season:     epSeason,
					Title:      ep.Title,
					Duration:   ep.Info.DurationSecs,
					Container:  ep.ContainerExt,
					CoverURL:   ep.Info.MovieImage,
				})
			}
		}

		if len(episodeStreams) > 0 {
			s.streamStore.BulkUpsert(ctx, episodeStreams)
			placeholderHash := computeContentHash(fmt.Sprintf("xtream-series-%d", sr.SeriesID))
			placeholderID := deterministicStreamID(placeholderHash)
			s.streamStore.Delete(ctx, placeholderID)
		}

		synced++
		if synced%200 == 0 {
			s.xtreamCache.Save()
			s.streamStore.Save()
			s.log.Info().Int("synced", synced).Int("total", len(seriesList)).Msg("xtream series sync progress")
		}
		time.Sleep(100 * time.Millisecond)
	}

	s.xtreamCache.Save()
	s.streamStore.Save()
	s.logoService.QueuePrefetch(s.xtreamCache.PosterURLs())
	s.log.Info().Int("synced", synced).Msg("xtream series sync complete")
}

func (s *M3UService) CleanupOrphanedStreams(ctx context.Context) {
	accounts, err := s.m3uAccountStore.List(ctx)
	if err != nil {
		s.log.Error().Err(err).Msg("orphan cleanup: failed to list accounts")
		return
	}
	ids := make([]string, len(accounts))
	for i, a := range accounts {
		ids[i] = a.ID
	}
	deletedIDs, err := s.streamStore.DeleteOrphanedM3UStreams(ctx, ids)
	if err != nil {
		s.log.Error().Err(err).Msg("orphan cleanup: failed to delete orphaned streams")
		return
	}
	if len(deletedIDs) == 0 {
		return
	}
	s.log.Info().Int("deleted", len(deletedIDs)).Msg("cleaned up orphaned M3U streams")
	if err := s.streamStore.Save(); err != nil {
		s.log.Error().Err(err).Msg("orphan cleanup: failed to save stream store")
	}
	if s.channelStore != nil {
		if err := s.channelStore.RemoveStreamMappings(ctx, deletedIDs); err != nil {
			s.log.Error().Err(err).Msg("orphan cleanup: failed to remove stream mappings from channels")
		}
	}
}

func (s *M3UService) RefreshAllAccounts(ctx context.Context) error {
	accounts, err := s.m3uAccountStore.List(ctx)
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

func extractLanguage(name string) (cleanName, lang string) {
	idx := strings.Index(name, ":")
	if idx >= 2 && idx <= 6 {
		prefix := strings.TrimSpace(name[:idx])
		isLang := len(prefix) >= 2
		for _, c := range prefix {
			if !((c >= 'A' && c <= 'Z') || c == '-') {
				isLang = false
				break
			}
		}
		if isLang && prefix[0] != '-' {
			return strings.TrimSpace(name[idx+1:]), prefix
		}
	}
	return name, ""
}

func extractLangFromCategory(cat string) string {
	if len(cat) >= 2 && cat[0] >= 'A' && cat[0] <= 'Z' && cat[1] >= 'A' && cat[1] <= 'Z' {
		if len(cat) == 2 || cat[2] == ' ' || cat[2] == '-' || cat[2] == ':' {
			return cat[:2]
		}
	}
	return ""
}

func deterministicStreamID(contentHash string) string {
	return uuid.NewSHA1(streamNamespace, []byte(contentHash)).String()
}

func computeContentHash(streamURL string) string {
	u, err := url.Parse(streamURL)
	if err != nil {
		return streamURL
	}
	return u.Path
}

func extractYearFromName(name string) int {
	for i := len(name) - 1; i >= 5; i-- {
		if name[i] == ')' && i >= 5 && name[i-5] == '(' {
			var year int
			if _, err := fmt.Sscanf(name[i-4:i], "%d", &year); err == nil && year >= 1900 && year <= 2099 {
				return year
			}
		}
	}
	return 0
}
