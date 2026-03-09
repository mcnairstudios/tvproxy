package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
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
)

func main() {
	cfg := config.Load()

	log := setupLogger(cfg)
	log.Info().Msg("starting tvproxy")

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
	userAgentRepo := repository.NewUserAgentRepository(db)

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
	m3uService := service.NewM3UService(m3uAccountRepo, streamRepo, userAgentRepo, log)
	channelService := service.NewChannelService(channelRepo, channelGroupRepo, streamRepo, log)
	epgService := service.NewEPGService(epgSourceRepo, epgDataRepo, programDataRepo, userAgentRepo, log)
	settingsService := service.NewSettingsService(settingsRepo)
	proxyService := service.NewProxyService(channelRepo, streamRepo, m3uAccountRepo, userAgentRepo, log)
	hdhrService := service.NewHDHRService(hdhrDeviceRepo, channelRepo, channelProfileRepo, cfg, log)
	outputService := service.NewOutputService(channelRepo, channelGroupRepo, epgDataRepo, programDataRepo, cfg, log)

	// Auth middleware
	authMW := middleware.NewAuthMiddleware(authService, cfg.APIKey)

	// Handlers
	authHandler := handler.NewAuthHandler(authService)
	userHandler := handler.NewUserHandler(authService)
	m3uAccountHandler := handler.NewM3UAccountHandler(m3uService)
	streamHandler := handler.NewStreamHandler(streamRepo)
	channelHandler := handler.NewChannelHandler(channelService)
	channelGroupHandler := handler.NewChannelGroupHandler(channelGroupRepo)
	channelProfileHandler := handler.NewChannelProfileHandler(channelProfileRepo)
	logoHandler := handler.NewLogoHandler(logoRepo)
	streamProfileHandler := handler.NewStreamProfileHandler(streamProfileRepo)
	epgSourceHandler := handler.NewEPGSourceHandler(epgService)
	epgDataHandler := handler.NewEPGDataHandler(epgDataRepo, programDataRepo)
	hdhrHandler := handler.NewHDHRHandler(hdhrService, proxyService, cfg)
	outputHandler := handler.NewOutputHandler(outputService)
	proxyHandler := handler.NewProxyHandler(proxyService)
	settingsHandler := handler.NewSettingsHandler(settingsService)
	userAgentHandler := handler.NewUserAgentHandler(userAgentRepo)

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

	// HDHomeRun routes (no auth - Plex/Emby/Jellyfin need direct access)
	r.Get("/hdhr/discover.json", hdhrHandler.Discover)
	r.Get("/hdhr/lineup_status.json", hdhrHandler.LineupStatus)
	r.Get("/hdhr/lineup.json", hdhrHandler.Lineup)
	r.Get("/hdhr/device.xml", hdhrHandler.DeviceXML)

	// Output routes (no auth for player access)
	r.Get("/output/m3u", outputHandler.M3U)
	r.Get("/output/epg", outputHandler.EPG)

	// Proxy routes (no auth for player access)
	r.Get("/proxy/stream/{channelID}", proxyHandler.Stream)

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

		r.Route("/api/user-agents", func(r chi.Router) {
			r.Get("/", userAgentHandler.List)
			r.Post("/", userAgentHandler.Create)
			r.Get("/{id}", userAgentHandler.Get)
			r.Put("/{id}", userAgentHandler.Update)
			r.Delete("/{id}", userAgentHandler.Delete)
		})
	})

	// Workers
	wm := worker.NewManager(log)
	wm.Add("m3u_refresh", worker.NewM3URefreshWorker(m3uService, cfg.M3URefreshInterval, log))
	wm.Add("epg_refresh", worker.NewEPGRefreshWorker(epgService, cfg.EPGRefreshInterval, log))
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

