package main

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/config"
	"github.com/gavinmcnair/tvproxy/pkg/database"
	"github.com/gavinmcnair/tvproxy/pkg/handler"
	"github.com/gavinmcnair/tvproxy/pkg/middleware"
	"github.com/gavinmcnair/tvproxy/pkg/openapi"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
	"github.com/gavinmcnair/tvproxy/pkg/service"
	"github.com/gavinmcnair/tvproxy/pkg/worker"
	"github.com/gavinmcnair/tvproxy/web"
)

func main() {
	cfg := config.Load()

	log := setupLogger(cfg)

	if cfg.BaseURL == "" {
		log.Fatal().Msg("TVPROXY_BASE_URL is required (e.g. http://192.168.1.149:8888)")
	}

	log.Info().Str("base_url", cfg.BaseURL).Msg("starting tvproxy")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	db, err := database.New(ctx, cfg.DatabasePath, log)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to initialize database")
	}
	defer db.Close()

	// Repositories
	userRepo := repository.NewUserRepository(db)
	m3uAccountRepo := repository.NewM3UAccountRepository(db)
	streamRepo := repository.NewStreamRepository(db)
	channelRepo := repository.NewChannelRepository(db)
	channelGroupRepo := repository.NewChannelGroupRepository(db)
	channelProfileRepo := repository.NewChannelProfileRepository(db)
	logoRepo := repository.NewLogoRepository(db)
	streamProfileRepo := repository.NewStreamProfileRepository(db)
	epgSourceRepo := repository.NewEPGSourceRepository(db)
	epgDataRepo := repository.NewEPGDataRepository(db)
	programDataRepo := repository.NewProgramDataRepository(db)
	hdhrDeviceRepo := repository.NewHDHRDeviceRepository(db)
	settingsRepo := repository.NewCoreSettingsRepository(db)
	clientRepo := repository.NewClientRepository(db)

	// Create default admin user if no users exist
	users, err := userRepo.List(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to check existing users")
	}
	if len(users) == 0 {
		log.Info().Msg("no users found, creating default admin user (admin/admin)")
		tmpAuth := service.NewAuthService(userRepo, cfg.JWTSecret, cfg.AccessTokenExpiry, cfg.RefreshTokenExpiry)
		if _, err := tmpAuth.CreateUser(ctx, "admin", "admin", true); err != nil {
			log.Fatal().Err(err).Msg("failed to create default admin user")
		}
	}

	// Services
	authService := service.NewAuthService(userRepo, cfg.JWTSecret, cfg.AccessTokenExpiry, cfg.RefreshTokenExpiry)
	m3uService := service.NewM3UService(m3uAccountRepo, streamRepo, cfg, log)
	channelService := service.NewChannelService(channelRepo, channelGroupRepo, streamRepo, log)
	epgService := service.NewEPGService(epgSourceRepo, epgDataRepo, programDataRepo, cfg, log)
	settingsService := service.NewSettingsService(settingsRepo)
	clientService := service.NewClientService(clientRepo, streamProfileRepo, log)
	proxyService := service.NewProxyService(channelRepo, streamRepo, m3uAccountRepo, channelProfileRepo, streamProfileRepo, clientService, cfg, log)
	hdhrService := service.NewHDHRService(hdhrDeviceRepo, channelRepo, streamRepo, channelProfileRepo, streamProfileRepo, cfg, log)
	outputService := service.NewOutputService(channelRepo, channelGroupRepo, streamRepo, channelProfileRepo, streamProfileRepo, epgDataRepo, programDataRepo, cfg, log)

	// Auth middleware
	authMW := middleware.NewAuthMiddleware(authService, cfg.APIKey)

	// Handlers
	authHandler := handler.NewAuthHandler(authService)
	userHandler := handler.NewUserHandler(authService)
	m3uAccountHandler := handler.NewM3UAccountHandler(m3uService)
	streamHandler := handler.NewStreamHandler(streamRepo)
	channelHandler := handler.NewChannelHandler(channelService, logoRepo)
	channelGroupHandler := handler.NewChannelGroupHandler(channelGroupRepo)
	channelProfileHandler := handler.NewChannelProfileHandler(channelProfileRepo)
	logoHandler := handler.NewLogoHandler(logoRepo)
	streamProfileHandler := handler.NewStreamProfileHandler(streamProfileRepo)
	epgSourceHandler := handler.NewEPGSourceHandler(epgService)
	epgDataHandler := handler.NewEPGDataHandler(epgDataRepo, programDataRepo)
	hdhrHandler := handler.NewHDHRHandler(hdhrService, hdhrDeviceRepo, proxyService, cfg)
	outputHandler := handler.NewOutputHandler(outputService)
	proxyHandler := handler.NewProxyHandler(proxyService, log)
	settingsHandler := handler.NewSettingsHandler(settingsService)
	clientHandler := handler.NewClientHandler(clientService)

	// Router
	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(middleware.RequestLogger(log))
	r.Use(middleware.Recovery(log))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-API-Key"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// OpenAPI spec
	r.Get("/api/openapi.yaml", openapi.SpecHandler())

	// Public routes
	r.Post("/api/auth/login", authHandler.Login)
	r.Post("/api/auth/refresh", authHandler.Refresh)

	// HDHomeRun routes at root (no auth - Plex/Emby/Jellyfin need direct access)
	// Matches Threadfin's route layout exactly: /device.xml, /discover.json, etc.
	r.Get("/discover.json", hdhrHandler.Discover)
	r.Get("/lineup_status.json", hdhrHandler.LineupStatus)
	r.Get("/lineup.json", hdhrHandler.Lineup)
	r.Get("/device.xml", hdhrHandler.DeviceXML)
	r.Get("/capability", hdhrHandler.DeviceXML) // alias used by some clients

	// Output routes (no auth for player access)
	r.Get("/output/m3u", outputHandler.M3U)
	r.Get("/output/epg", outputHandler.EPG)

	// Stream routes (no auth for player access)
	r.Get("/channel/{channelID}", proxyHandler.Stream)
	r.Get("/stream/{streamID}", proxyHandler.RawStream)

	// Authenticated API routes
	r.Group(func(r chi.Router) {
		r.Use(authMW.Authenticate)

		r.Post("/api/auth/logout", authHandler.Logout)
		r.Get("/api/auth/me", authHandler.Me)

		r.Route("/api/users", func(r chi.Router) {
			r.Use(authMW.RequireAdmin)
			r.Get("/", userHandler.List)
			r.Post("/", userHandler.Create)
			r.Get("/{id}", userHandler.Get)
			r.Put("/{id}", userHandler.Update)
			r.Delete("/{id}", userHandler.Delete)
		})

		r.Route("/api/m3u/accounts", func(r chi.Router) {
			r.Get("/", m3uAccountHandler.List)
			r.Post("/", m3uAccountHandler.Create)
			r.Get("/{id}", m3uAccountHandler.Get)
			r.Put("/{id}", m3uAccountHandler.Update)
			r.Delete("/{id}", m3uAccountHandler.Delete)
			r.Post("/{id}/refresh", m3uAccountHandler.Refresh)
		})

		r.Route("/api/streams", func(r chi.Router) {
			r.Get("/", streamHandler.List)
			r.Get("/{id}", streamHandler.Get)
			r.Delete("/{id}", streamHandler.Delete)
		})

		r.Route("/api/channels", func(r chi.Router) {
			r.Get("/", channelHandler.List)
			r.Post("/", channelHandler.Create)
			r.Get("/{id}", channelHandler.Get)
			r.Put("/{id}", channelHandler.Update)
			r.Delete("/{id}", channelHandler.Delete)
			r.Get("/{id}/streams", channelHandler.GetStreams)
			r.Post("/{id}/streams", channelHandler.AssignStreams)
		})

		r.Route("/api/channel-groups", func(r chi.Router) {
			r.Get("/", channelGroupHandler.List)
			r.Post("/", channelGroupHandler.Create)
			r.Get("/{id}", channelGroupHandler.Get)
			r.Put("/{id}", channelGroupHandler.Update)
			r.Delete("/{id}", channelGroupHandler.Delete)
		})

		r.Route("/api/channel-profiles", func(r chi.Router) {
			r.Get("/", channelProfileHandler.List)
			r.Post("/", channelProfileHandler.Create)
			r.Get("/{id}", channelProfileHandler.Get)
			r.Put("/{id}", channelProfileHandler.Update)
			r.Delete("/{id}", channelProfileHandler.Delete)
		})

		r.Route("/api/logos", func(r chi.Router) {
			r.Get("/", logoHandler.List)
			r.Post("/", logoHandler.Create)
			r.Get("/{id}", logoHandler.Get)
			r.Put("/{id}", logoHandler.Update)
			r.Delete("/{id}", logoHandler.Delete)
		})

		r.Route("/api/stream-profiles", func(r chi.Router) {
			r.Get("/", streamProfileHandler.List)
			r.Post("/", streamProfileHandler.Create)
			r.Get("/{id}", streamProfileHandler.Get)
			r.Put("/{id}", streamProfileHandler.Update)
			r.Delete("/{id}", streamProfileHandler.Delete)
		})

		r.Route("/api/epg", func(r chi.Router) {
			r.Get("/sources", epgSourceHandler.List)
			r.Post("/sources", epgSourceHandler.Create)
			r.Get("/sources/{id}", epgSourceHandler.Get)
			r.Put("/sources/{id}", epgSourceHandler.Update)
			r.Delete("/sources/{id}", epgSourceHandler.Delete)
			r.Post("/sources/{id}/refresh", epgSourceHandler.Refresh)
			r.Get("/data", epgDataHandler.List)
			r.Get("/now", epgDataHandler.NowPlaying)
			r.Get("/guide", epgDataHandler.Guide)
		})

		r.Route("/api/hdhr/devices", func(r chi.Router) {
			r.Get("/", hdhrHandler.ListDevices)
			r.Post("/", hdhrHandler.CreateDevice)
			r.Get("/{id}", hdhrHandler.GetDevice)
			r.Put("/{id}", hdhrHandler.UpdateDevice)
			r.Delete("/{id}", hdhrHandler.DeleteDevice)
		})

		r.Route("/api/settings", func(r chi.Router) {
			r.Get("/", settingsHandler.List)
			r.Put("/", settingsHandler.Update)
		})

		r.Route("/api/clients", func(r chi.Router) {
			r.Get("/", clientHandler.List)
			r.Post("/", clientHandler.Create)
			r.Get("/{id}", clientHandler.Get)
			r.Put("/{id}", clientHandler.Update)
			r.Delete("/{id}", clientHandler.Delete)
		})
	})

	// Embedded web frontend
	distFS, err := fs.Sub(web.Assets, "dist")
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load embedded web assets")
	}
	fileServer := http.FileServer(http.FS(distFS))
	r.Get("/*", func(w http.ResponseWriter, req *http.Request) {
		// Try to serve the static file first
		path := strings.TrimPrefix(req.URL.Path, "/")
		if f, err := distFS.Open(path); err == nil {
			f.Close()
			fileServer.ServeHTTP(w, req)
			return
		}
		// Fall back to index.html for SPA routing
		req.URL.Path = "/"
		fileServer.ServeHTTP(w, req)
	})

	// Workers
	wm := worker.NewManager(log)
	wm.Add("m3u_refresh", worker.NewM3URefreshWorker(m3uService, cfg.M3URefreshInterval, log))
	wm.Add("epg_refresh", worker.NewEPGRefreshWorker(epgService, cfg.EPGRefreshInterval, log))

	// SSDP discovery worker — BaseURL is portless (e.g. http://192.168.1.149).
	// Workers extract the host and append per-device ports.
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = fmt.Sprintf("http://%s", cfg.Host)
	}
	wm.Add("ssdp", worker.NewSSDPWorker(hdhrDeviceRepo, baseURL, log))
	wm.Add("hdhr_discover", worker.NewHDHRDiscoverWorker(hdhrDeviceRepo, baseURL, log))
	wm.Add("hdhr_servers", worker.NewHDHRServerWorker(hdhrDeviceRepo, hdhrService, proxyService, outputService, cfg, log))

	wm.Start(ctx)

	// HTTP Server
	srv := &http.Server{
		Addr:         cfg.ListenAddr(),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // Disabled for streaming
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Info().Str("addr", cfg.ListenAddr()).Msg("HTTP server listening")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("HTTP server error")
		}
	}()

	<-ctx.Done()
	log.Info().Msg("shutting down")

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

