package main

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/config"
	"github.com/gavinmcnair/tvproxy/pkg/database"
	"github.com/gavinmcnair/tvproxy/pkg/defaults"
	"github.com/gavinmcnair/tvproxy/pkg/handler"
	"github.com/gavinmcnair/tvproxy/pkg/hls"
	"github.com/gavinmcnair/tvproxy/pkg/jellyfin"
	"github.com/gavinmcnair/tvproxy/pkg/logocache"
	"github.com/gavinmcnair/tvproxy/pkg/middleware"
	"github.com/gavinmcnair/tvproxy/pkg/service"
	"github.com/gavinmcnair/tvproxy/pkg/session"
	"github.com/gavinmcnair/tvproxy/pkg/store"
	"github.com/gavinmcnair/tvproxy/pkg/tmdb"
	"github.com/gavinmcnair/tvproxy/pkg/worker"
	"github.com/gavinmcnair/tvproxy/pkg/xtream"
	"github.com/gavinmcnair/tvproxy/web"
)

var buildVersion = "dev"

func main() {
	cfg := config.Load()

	log := setupLogger(cfg)

	if cfg.BaseURL == "" {
		log.Fatal().Msg("TVPROXY_BASE_URL is required (e.g. http://192.168.1.149:8888)")
	}

	log.Info().Str("base_url", cfg.BaseURL).Msg("starting tvproxy")

	go func() {
		log.Info().Msg("pprof listening on :6060")
		http.ListenAndServe(":6060", nil)
	}()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	dataDir := filepath.Dir(cfg.DatabasePath)

	db, err := database.New(ctx, cfg.DatabasePath, log)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to initialize database")
	}
	defer db.Close()

	tuningSettings, err := defaults.LoadSettings(filepath.Join(dataDir, "settings.json"))
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load settings")
	}
	cfg.Settings = tuningSettings

	profileStore := store.NewProfileStore(filepath.Join(dataDir, "profiles.json"), log)
	if err := profileStore.Load(); err != nil {
		log.Fatal().Err(err).Msg("failed to load profile store")
	}
	profileStore.SeedSystemProfiles()

	sourceProfileStore := store.NewSourceProfileStore(filepath.Join(dataDir, "source_profiles.json"), log)
	if err := sourceProfileStore.Load(); err != nil {
		log.Fatal().Err(err).Msg("failed to load source profile store")
	}
	sourceProfileStore.SeedDefaults()

	clientStore := store.NewClientStore(filepath.Join(dataDir, "clients_data.json"))
	if err := clientStore.Load(); err != nil {
		log.Fatal().Err(err).Msg("failed to load client store")
	}

	clientDefs, err := defaults.LoadClientDefaults(filepath.Join(dataDir, "clients.json"))
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load client defaults")
	}
	settingsStore := store.NewSettingsStore(filepath.Join(dataDir, "core_settings.json"))
	if err := settingsStore.Load(); err != nil {
		log.Fatal().Err(err).Msg("failed to load settings store")
	}

	if err := service.SeedClientDefaults(ctx, clientDefs, profileStore, clientStore, settingsStore); err != nil {
		log.Fatal().Err(err).Msg("failed to seed client defaults")
	}

	streamStore, err := store.NewBoltStreamStore(dataDir, log)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to open stream bolt store")
	}
	epgStore := store.NewEPGStore(filepath.Join(dataDir, "epg.gob"), log)
	{
		streamErr := make(chan error, 1)
		epgErr := make(chan error, 1)
		go func() { streamErr <- streamStore.Load() }()
		go func() { epgErr <- epgStore.Load() }()
		if err := <-streamErr; err != nil {
			log.Fatal().Err(err).Msg("failed to load stream store")
		}
		if err := <-epgErr; err != nil {
			log.Fatal().Err(err).Msg("failed to load epg store")
		}
	}

	userStore := store.NewUserStore(filepath.Join(dataDir, "users.json"))
	if err := userStore.Load(); err != nil {
		log.Fatal().Err(err).Msg("failed to load user store")
	}
	m3uAccountStore := store.NewM3UAccountStore(filepath.Join(dataDir, "m3u_accounts.json"))
	if err := m3uAccountStore.Load(); err != nil {
		log.Fatal().Err(err).Msg("failed to load m3u account store")
	}
	channelStore := store.NewChannelStore(filepath.Join(dataDir, "channels.json"))
	if err := channelStore.Load(); err != nil {
		log.Fatal().Err(err).Msg("failed to load channel store")
	}
	channelGroupStore := store.NewChannelGroupStore(filepath.Join(dataDir, "channel_groups.json"))
	if err := channelGroupStore.Load(); err != nil {
		log.Fatal().Err(err).Msg("failed to load channel group store")
	}
	favoriteStore := store.NewFavoriteStore(dataDir)
	logoStore := store.NewLogoStore(filepath.Join(dataDir, "logos.json"))
	if err := logoStore.Load(); err != nil {
		log.Fatal().Err(err).Msg("failed to load logo store")
	}
	epgSourceStore := store.NewEPGSourceStore(filepath.Join(dataDir, "epg_sources.json"))
	if err := epgSourceStore.Load(); err != nil {
		log.Fatal().Err(err).Msg("failed to load epg source store")
	}
	hdhrStore := store.NewHDHRDeviceStore(filepath.Join(dataDir, "hdhr_devices.json"))
	if err := hdhrStore.Load(); err != nil {
		log.Fatal().Err(err).Msg("failed to load hdhr device store")
	}
	scheduledRecStore := store.NewScheduledRecordingStore(filepath.Join(dataDir, "scheduled_recordings.json"))
	if err := scheduledRecStore.Load(); err != nil {
		log.Fatal().Err(err).Msg("failed to load scheduled recording store")
	}
	satipSourceStore := store.NewSatIPSourceStore(filepath.Join(dataDir, "satip_sources.json"))
	if err := satipSourceStore.Load(); err != nil {
		log.Fatal().Err(err).Msg("failed to load satip source store")
	}

	satipSources, _ := satipSourceStore.List(ctx)
	satipIDs := make([]string, len(satipSources))
	for i, s := range satipSources {
		satipIDs[i] = s.ID
	}
	if deleted, err := streamStore.DeleteOrphanedSatIPStreams(ctx, satipIDs); err == nil && len(deleted) > 0 {
		log.Info().Int("count", len(deleted)).Msg("cleaned up orphaned satip streams")
		streamStore.Save()
	}

	hdhrSourceStore := store.NewHDHRSourceStore(filepath.Join(dataDir, "hdhr_sources.json"))
	if err := hdhrSourceStore.Load(); err != nil {
		log.Fatal().Err(err).Msg("failed to load hdhr source store")
	}

	hdhrSources, _ := hdhrSourceStore.List(ctx)
	hdhrSourceIDs := make([]string, len(hdhrSources))
	for i, s := range hdhrSources {
		hdhrSourceIDs[i] = s.ID
	}
	if deleted, err := streamStore.DeleteOrphanedHDHRStreams(ctx, hdhrSourceIDs); err == nil && len(deleted) > 0 {
		log.Info().Int("count", len(deleted)).Msg("cleaned up orphaned hdhr source streams")
		streamStore.Save()
	}

	authService := service.NewAuthService(userStore, cfg.JWTSecret, cfg.AccessTokenExpiry, cfg.RefreshTokenExpiry)
	authService.SetInviteExpiry(cfg.Settings.Auth.InviteTokenExpiry)

	users, err := userStore.List(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to check existing users")
	}
	if len(users) == 0 {
		log.Info().Msg("no users found, creating default admin user (admin/admin)")
		if _, err := authService.CreateUser(ctx, "admin", "admin", true); err != nil {
			log.Fatal().Err(err).Msg("failed to create default admin user")
		}
	}

	adminUser, err := authService.FindFirstAdmin(ctx)
	var adminUserID string
	if err == nil && adminUser != nil {
		adminUserID = adminUser.ID
	}
	settingsService := service.NewSettingsService(settingsStore, log)
	settingsService.LoadDebugFlag(ctx)

	wgService := service.NewWireGuardService(settingsService, log)
	if err := wgService.Start(ctx); err != nil {
		log.Error().Err(err).Msg("failed to start wireguard tunnel (continuing without VPN)")
	}

	wgProfileStore := store.NewWireGuardProfileStore(filepath.Join(dataDir, "wireguard_profiles.json"))
	if err := wgProfileStore.Load(); err != nil {
		log.Fatal().Err(err).Msg("failed to load wireguard profile store")
	}
	wgMultiService := service.NewMultiWireGuardService(wgProfileStore, settingsService, log)
	if err := wgMultiService.Start(ctx); err != nil {
		log.Error().Err(err).Msg("failed to start multi wireguard (continuing)")
	}

	wgHTTPClient := wgService.HTTPClient()

	logoTimeout := 10 * time.Second
	if cfg.Settings != nil {
		logoTimeout = cfg.Settings.Network.LogoDownloadTimeout
	}
	logoCache := logocache.New(filepath.Join(dataDir, "static", "logocache"), cfg, logoTimeout)
	logoService := service.NewLogoService(logoStore, epgStore, logoCache, log)

	xtreamCache := xtream.NewCache(filepath.Join(dataDir, "cache", "xtream"))
	recordingStore := store.NewRecordingStore(cfg.RecordDir, log)
	probeCache, err := store.NewBoltProbeCache(filepath.Join(dataDir, "cache"))
	if err != nil {
		log.Fatal().Err(err).Msg("failed to open probe cache")
	}
	m3uService := service.NewM3UService(m3uAccountStore, streamStore, channelStore, logoService, probeCache, cfg, dataDir, wgHTTPClient, log)
	m3uService.SetXtreamCache(xtreamCache)
	m3uService.SetSourceProfileStore(sourceProfileStore)
	m3uService.CleanupOrphanedStreams(ctx)
	channelService := service.NewChannelService(channelStore, channelGroupStore, streamStore, log)
	epgService := service.NewEPGService(epgSourceStore, epgStore, cfg, wgHTTPClient, log)
	epgService.CleanupOrphanedEPGData(ctx)
	activityService := service.NewActivityService()
	clientService := service.NewClientService(clientStore, profileStore, settingsService, log)
	proxyService := service.NewProxyService(channelStore, streamStore, profileStore, clientService, activityService, settingsService, probeCache, cfg, wgHTTPClient, log)
	hdhrService := service.NewHDHRService(hdhrStore, channelStore, cfg)
	outputService := service.NewOutputService(channelStore, channelGroupStore, epgStore, logoService, cfg, log)
	satipService := service.NewSatIPService(satipSourceStore, streamStore, channelStore, probeCache, log)
	hdhrSourceService := service.NewHDHRSourceService(hdhrSourceStore, streamStore, channelStore, probeCache, log)
	wgMultiClient := wgMultiService.HTTPClient()
	m3uService.SetWGClient(wgMultiClient)
	sessionMgr := session.NewManager(cfg, wgHTTPClient, wgMultiClient, probeCache, log)

	wgPool := session.NewWGPool(log)
	for profileID, client := range wgMultiService.ConnectedTransports() {
		name := wgMultiService.ProfileName(profileID)
		if err := wgPool.AddProxy(profileID, name, client, cfg); err != nil {
			log.Error().Err(err).Str("profile", name).Msg("failed to start wireguard proxy")
		}
	}
	wgPool.AddDirect("default", "Direct", cfg, log)
	m3uService.SetWGClient(wgPool.Client())
	log.Info().Int("proxies", wgPool.Count()).Msg("wireguard pool active with failover")

	vodService := service.NewVODService(channelStore, streamStore, profileStore, sourceProfileStore, m3uAccountStore, satipSourceStore, settingsService, sessionMgr, recordingStore, activityService, cfg, log)
	vodService.SetProbeCache(probeCache)
	go vodService.RecoverRecordings(ctx)
	schedulerService := service.NewSchedulerService(scheduledRecStore, channelStore, vodService, cfg, log)
	dlnaService := service.NewDLNAService(channelStore, channelGroupStore, userStore, favoriteStore, streamStore, settingsService, logoService, vodService, cfg, log)

	authMW := middleware.NewAuthMiddleware(authService, activityService, cfg.APIKey, adminUserID)

	wgClientForSessions := wgMultiClient
	if wgPool.Count() > 0 {
		wgClientForSessions = wgPool.Client()
	}
	hlsManager := hls.NewManager(hls.TempDir(), wgClientForSessions, cfg, log)
	if wgClientForSessions != nil {
		hlsManager.WGProxyFunc = func(streamURL string) string {
			proxy, err := sessionMgr.WGProxy("default", wgClientForSessions, cfg, log)
			if err != nil {
				log.Error().Err(err).Msg("failed to create wg proxy for hls")
				return streamURL
			}
			return proxy.ProxyURL(streamURL)
		}
	}
	go hlsManager.StartCleanupWorker(ctx)
	sessionMgr.SetOnCleanup(func(channelID string) {
		hlsManager.StopSession(channelID)
	})

	exportService := service.NewExportService(channelStore, channelGroupStore, profileStore, sourceProfileStore, clientStore, m3uAccountStore, epgSourceStore, settingsService, authService)
	dataResetter := service.NewDataResetter(
		profileStore, settingsStore, clientStore, logoStore, m3uAccountStore,
		epgSourceStore, hdhrStore, userStore, channelStore, channelGroupStore,
		scheduledRecStore, clientDefs, func() {
			service.ForceSeedClientDefaults(ctx, clientDefs, profileStore, clientStore, settingsStore)
		},
	)

	r := setupRouter(cfg, log, settingsService)
	tmdbClient := tmdb.NewClient(
		filepath.Join(filepath.Dir(cfg.DatabasePath), "cache", "tmdb"),
		func() string { v, _ := settingsService.Get(ctx, "tmdb_api_key"); return v },
		log,
	)
	gstreamerHandler := handler.NewGStreamerHandler()
	tmdbHandler := handler.NewTMDBHandler(tmdbClient, streamStore)
	wgMultiHandler := handler.NewMultiWireGuardHandler(wgMultiService, wgProfileStore, log)
	wgMultiHandler.SetPool(wgPool)
	registerRoutes(r, routeHandlers{
		auth:           handler.NewAuthHandler(authService),
		user:           handler.NewUserHandler(authService),
		m3uAccount:     handler.NewM3UAccountHandler(m3uService, dataDir),
		satip:          handler.NewSatIPHandler(satipService),
		hdhrSource:     handler.NewHDHRSourceHandler(hdhrSourceService),
		stream:         handler.NewStreamHandler(streamStore, streamStore, logoService, tmdbClient, xtreamCache),
		channel:        handler.NewChannelHandler(channelService, logoService),
		channelGroup:   handler.NewChannelGroupHandler(channelService),
		logo:           handler.NewLogoHandler(logoService),
		profile:        handler.NewStreamProfileHandler(profileStore),
		sourceProfile:  handler.NewSourceProfileHandler(sourceProfileStore),
		epgSource:      handler.NewEPGSourceHandler(epgService),
		epgData:        handler.NewEPGDataHandler(epgStore, epgStore),
		hdhr:           handler.NewHDHRHandler(hdhrService, proxyService, cfg),
		output:         handler.NewOutputHandler(outputService),
		proxy:          handler.NewProxyHandler(proxyService, settingsService, log),
		vod:            handler.NewVODHandler(vodService, clientService, log),
		activity:       handler.NewActivityHandler(activityService),
		favorite:       handler.NewFavoriteHandler(favoriteStore),
		settings:       handler.NewSettingsHandler(settingsService, exportService, dataResetter, authService, streamStore, epgStore),
		client:         handler.NewClientHandler(clientService),
		scheduler:      handler.NewSchedulerHandler(schedulerService, log),
		dlna:           handler.NewDLNAHandler(dlnaService, authService, settingsService, cfg, log),
		wireguard:      handler.NewWireGuardHandler(wgService, log),
		wireguardMulti: wgMultiHandler,
		tmdb:           tmdbHandler,
		gstreamer:      gstreamerHandler,
		logoCache:      logoCache,
		log:            log,
	}, authMW)

	distFS, err := fs.Sub(web.Assets, "dist")
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load embedded web assets")
	}
	versionedIndexBytes := buildVersionedIndex(distFS)
	staticRoot := filepath.Join(filepath.Dir(cfg.DatabasePath), "static")
	registerStaticRoutes(r, staticRoot, distFS, versionedIndexBytes)

	jellyfinServer := jellyfin.NewServer("TVProxy", cfg.BaseURL, authService, activityService, favoriteStore, channelStore, channelGroupStore, streamStore, epgStore, logoService, tmdbClient, hlsManager, log)
	go func() {
		jfRouter := chi.NewRouter()
		jfRouter.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Access-Control-Allow-Origin", "*")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, HEAD")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Emby-Authorization, X-MediaBrowser-Token")
				if r.Method == "OPTIONS" {
					w.WriteHeader(http.StatusOK)
					return
				}
				next.ServeHTTP(w, r)
			})
		})
		jfRouter.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer func() {
					if err := recover(); err != nil {
						log.Error().Interface("panic", err).Str("path", r.URL.Path).Msg("jellyfin panic recovered")
						w.WriteHeader(http.StatusInternalServerError)
					}
				}()
				auth := r.Header.Get("Authorization")
				if len(auth) > 120 {
					auth = auth[:120]
				}
				log.Info().Str("method", r.Method).Str("path", r.URL.Path).Str("query", r.URL.RawQuery).Str("auth", auth).Msg("jellyfin request")
				next.ServeHTTP(w, r)
			})
		})
		jfRouter.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.URL.Path = canonicalJellyfinPath(r.URL.Path)
				next.ServeHTTP(w, r)
			})
		})
		jfRouter.NotFound(func(w http.ResponseWriter, r *http.Request) {
			log.Warn().Str("method", r.Method).Str("path", r.URL.Path).Msg("jellyfin 404 - unhandled endpoint")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"Items":[],"TotalRecordCount":0}`))
		})
		jfRouter.Mount("/", jellyfinServer.Router())
		jfAddr := ":8096"
		log.Info().Str("addr", jfAddr).Msg("Jellyfin API server listening")
		if err := http.ListenAndServe(jfAddr, jfRouter); err != nil {
			log.Error().Err(err).Msg("Jellyfin API server error")
		}
	}()

	onTMDBResolved := func(streamID string, tmdbID int) {
		if st, err := streamStore.GetByID(ctx, streamID); err == nil && st != nil && st.TMDBID > 0 {
			return
		}
		if err := streamStore.UpdateTMDBID(ctx, streamID, tmdbID); err != nil {
			log.Debug().Err(err).Str("stream_id", streamID).Int("tmdb_id", tmdbID).Msg("failed to update stream TMDB ID")
		}
	}

	syncTMDB := func() {
		summaries, err := streamStore.ListSummaries(ctx)
		if err != nil {
			log.Error().Err(err).Msg("failed to list streams for TMDB sync")
			return
		}
		var items []tmdb.VODItem
		seen := make(map[string]bool)
		for _, s := range summaries {
			if s.VODType == "" {
				continue
			}
			name := s.Name
			mediaType := "movie"
			if s.VODType == "series" {
				if s.VODSeries != "" {
					name = s.VODSeries
				}
				mediaType = "tv"
			}
			key := name + "_" + mediaType
			if seen[key] {
				continue
			}
			seen[key] = true
			items = append(items, tmdb.VODItem{StreamID: s.ID, Name: name, MediaType: mediaType, TMDBID: s.TMDBID})
		}
		if len(items) > 0 {
			tmdbClient.PopulateMetadataFromCache(items)
			tmdbClient.Sync(items, onTMDBResolved)
		}
	}

	go syncTMDB()

	m3uWorker := worker.NewM3URefreshWorker(m3uService, cfg.M3URefreshInterval, log)
	m3uWorker.SetOnRefreshDone(syncTMDB)

	wm := worker.NewManager(log)
	wm.Add("m3u_refresh", m3uWorker)
	wm.Add("epg_refresh", worker.NewEPGRefreshWorker(epgService, cfg.EPGRefreshInterval, log))
	wm.Add("ssdp", worker.NewSSDPWorker(hdhrStore, cfg.BaseURL, cfg.Settings.Workers.RetryDelay, cfg.Settings.Workers.SSDPAnnounceInterval, log))
	wm.Add("hdhr_discover", worker.NewHDHRDiscoverWorker(hdhrStore, cfg, cfg.BaseURL, cfg.Settings.Workers.RetryDelay, log))
	wm.Add("hdhr_servers", worker.NewHDHRServerWorker(hdhrStore, hdhrService, proxyService, settingsService, outputService, cfg, log))
	wm.Add("dlna", worker.NewDLNAWorker(dlnaService, cfg.BaseURL, cfg.Port, cfg.Settings.Workers.RetryDelay, cfg.Settings.Workers.DLNAAnnounceInterval, log))
	wm.Add("recording_scheduler", worker.NewSchedulerWorker(schedulerService, 30*time.Second, log))
	wm.Add("wal_checkpoint", worker.NewWALCheckpointWorker(db, 5*time.Minute, log))
	wm.Add("wireguard", worker.NewWireGuardWorker(wgService, 30*time.Second, log))
	wm.Add("wireguard_multi", worker.NewMultiWireGuardWorker(wgMultiService, 30*time.Second, 60*time.Second, log))
	wm.Start(ctx)

	srv := &http.Server{
		Addr:         cfg.ListenAddr(),
		Handler:      r,
		ReadTimeout:  cfg.Settings.Server.HTTPReadTimeout,
		WriteTimeout: 0,
		IdleTimeout:  cfg.Settings.Server.HTTPIdleTimeout,
	}

	go func() {
		log.Info().Str("addr", cfg.ListenAddr()).Msg("HTTP server listening")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("HTTP server error")
		}
	}()

	<-ctx.Done()
	log.Info().Msg("shutting down")

	wgService.Stop()
	vodService.Shutdown()
	hlsManager.StopAll()
	sessionMgr.Shutdown()
	wgPool.StopAll()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("HTTP server shutdown error")
	}

	wm.Stop()
	log.Info().Msg("shutdown complete")
}

func setupLogger(cfg *config.Config) zerolog.Logger {
	level, err := zerolog.ParseLevel(cfg.LogLevel)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	if cfg.LogJSON {
		return zerolog.New(os.Stdout).With().Timestamp().Logger()
	}
	return zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}).
		With().Timestamp().Logger()
}

func setupRouter(cfg *config.Config, log zerolog.Logger, settingsService *service.SettingsService) *chi.Mux {
	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(middleware.RequestLogger(log, settingsService.IsDebug))
	r.Use(middleware.Recovery(log))

	corsMiddleware := cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-API-Key"},
		ExposedHeaders:   []string{"Link", "ETag", "X-Language-Counts"},
		AllowCredentials: true,
		MaxAge:           300,
	})
	r.Use(func(next http.Handler) http.Handler {
		withCORS := corsMiddleware(next)
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if strings.HasPrefix(req.URL.Path, "/api/") {
				withCORS.ServeHTTP(w, req)
				return
			}
			next.ServeHTTP(w, req)
		})
	})

	bodyLimit := cfg.Settings.Server.RequestBodyLimitBytes
	if bodyLimit <= 0 {
		bodyLimit = 1 << 20
	}
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, bodyLimit)
			}
			next.ServeHTTP(w, r)
		})
	})

	return r
}

func canonicalJellyfinPath(path string) string {
	lower := strings.ToLower(path)

	segments := map[string]string{
		"system": "System", "info": "Info", "public": "Public",
		"branding": "Branding", "configuration": "Configuration", "css": "Css",
		"splashscreen": "Splashscreen", "quickconnect": "QuickConnect",
		"enabled": "Enabled", "users": "Users", "authenticatebyname": "AuthenticateByName",
		"me": "Me", "userviews": "UserViews", "userimage": "UserImage",
		"items": "Items", "latest": "Latest", "resume": "Resume",
		"filters": "Filters", "counts": "Counts", "useritems": "UserItems",
		"shows": "Shows", "nextup": "NextUp", "seasons": "Seasons",
		"episodes": "Episodes", "videos": "Videos", "stream": "stream",
		"livetv": "LiveTv", "channels": "Channels", "programs": "Programs",
		"guideinfo": "GuideInfo", "sessions": "Sessions", "capabilities": "Capabilities",
		"full": "Full", "playing": "Playing", "progress": "Progress",
		"stopped": "Stopped", "playback": "Playback", "bitratetest": "BitrateTest",
		"displaypreferences": "DisplayPreferences", "userplayeditems": "UserPlayedItems",
		"userfavoriteitems": "UserFavoriteItems", "images": "Images",
		"similar": "Similar", "localtrailers": "LocalTrailers",
		"specialfeatures": "SpecialFeatures", "thememedia": "ThemeMedia",
		"themesongs": "ThemeSongs", "themevideos": "ThemeVideos",
		"instantmix": "InstantMix", "playbackinfo": "PlaybackInfo",
		"persons": "Persons", "studios": "Studios", "artists": "Artists",
		"genres": "Genres", "ping": "Ping",
	}

	parts := strings.Split(path, "/")
	for i, part := range parts {
		lp := strings.ToLower(part)
		if mapped, ok := segments[lp]; ok {
			parts[i] = mapped
		}
	}

	result := strings.Join(parts, "/")
	_ = lower
	return result
}

func buildVersionedIndex(distFS fs.FS) []byte {
	indexHTML, err := fs.ReadFile(distFS, "index.html")
	if err != nil {
		panic("failed to read embedded index.html: " + err.Error())
	}
	ver := buildVersion
	if ver == "dev" {
		ver = fmt.Sprintf("dev.%d", time.Now().Unix())
	}
	idx := string(indexHTML)
	idx = strings.ReplaceAll(idx, `app.css"`, `app.css?v=`+ver+`"`)
	idx = strings.ReplaceAll(idx, `app.css?v=dev"`, `app.css?v=`+ver+`"`)
	idx = strings.ReplaceAll(idx, `app.js"`, `app.js?v=`+ver+`"`)
	idx = strings.ReplaceAll(idx, `app.js?v=dev"`, `app.js?v=`+ver+`"`)
	versionedIndex := idx
	return []byte(versionedIndex)
}
