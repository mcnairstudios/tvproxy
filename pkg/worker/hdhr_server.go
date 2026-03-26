package worker

import (
	"context"
	"encoding/xml"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/config"
	"github.com/gavinmcnair/tvproxy/pkg/handler"
	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/service"
	"github.com/gavinmcnair/tvproxy/pkg/store"
)

type deviceServer struct {
	port   int
	server *http.Server
	cancel context.CancelFunc
}

type HDHRServerWorker struct {
	hdhrStore       store.HDHRDeviceStore
	hdhrService     *service.HDHRService
	proxyService    *service.ProxyService
	settingsService *service.SettingsService
	outputService   *service.OutputService
	cfg             *config.Config
	log             zerolog.Logger
	mu              sync.Mutex
	servers         map[string]*deviceServer
	retryDelay      time.Duration
	syncInterval    time.Duration
	readTimeout     time.Duration
	idleTimeout     time.Duration
}

func NewHDHRServerWorker(
	hdhrStore store.HDHRDeviceStore,
	hdhrService *service.HDHRService,
	proxyService *service.ProxyService,
	settingsService *service.SettingsService,
	outputService *service.OutputService,
	cfg *config.Config,
	log zerolog.Logger,
) *HDHRServerWorker {
	retryDelay := 2 * time.Second
	syncInterval := 10 * time.Second
	readTimeout := 15 * time.Second
	idleTimeout := 60 * time.Second
	if cfg.Settings != nil {
		retryDelay = cfg.Settings.Workers.RetryDelay
		syncInterval = cfg.Settings.Workers.HDHRDiscoverInterval
		readTimeout = cfg.Settings.Server.HDHRReadTimeout
		idleTimeout = cfg.Settings.Server.HDHRIdleTimeout
	}
	return &HDHRServerWorker{
		hdhrStore:       hdhrStore,
		hdhrService:     hdhrService,
		proxyService:    proxyService,
		settingsService: settingsService,
		outputService:   outputService,
		cfg:             cfg,
		log:             log.With().Str("worker", "hdhr_servers").Logger(),
		servers:         make(map[string]*deviceServer),
		retryDelay:      retryDelay,
		syncInterval:    syncInterval,
		readTimeout:     readTimeout,
		idleTimeout:     idleTimeout,
	}
}

func (w *HDHRServerWorker) Run(ctx context.Context) {
	select {
	case <-time.After(w.retryDelay):
	case <-ctx.Done():
		return
	}

	w.sync(ctx)

	ticker := time.NewTicker(w.syncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.stopAll()
			return
		case <-ticker.C:
			w.sync(ctx)
		}
	}
}

func (w *HDHRServerWorker) sync(ctx context.Context) {
	devices, err := w.hdhrStore.List(ctx)
	if err != nil {
		w.log.Error().Err(err).Msg("failed to list devices for HDHR servers")
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	desired := make(map[string]int)
	for _, d := range devices {
		if d.IsEnabled && d.Port > 0 {
			desired[d.ID] = d.Port
		}
	}

	for id, ds := range w.servers {
		wantPort, ok := desired[id]
		if !ok || wantPort != ds.port {
			w.log.Info().Str("device_id", id).Int("port", ds.port).Msg("stopping HDHR device server")
			ds.cancel()
			ds.server.Close()
			delete(w.servers, id)
		}
	}

	for _, d := range devices {
		if !d.IsEnabled || d.Port <= 0 {
			continue
		}
		if _, exists := w.servers[d.ID]; exists {
			continue
		}
		w.startServer(ctx, d)
	}
}

func (w *HDHRServerWorker) startServer(parentCtx context.Context, device models.HDHRDevice) {
	host := w.extractHost()
	baseURL := fmt.Sprintf("http://%s:%d", host, device.Port)

	r := w.buildRouter(device, baseURL)

	srvCtx, cancel := context.WithCancel(parentCtx)
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", device.Port),
		Handler:      r,
		ReadTimeout:  w.readTimeout,
		WriteTimeout: 0,
		IdleTimeout:  w.idleTimeout,
		BaseContext:  func(_ net.Listener) context.Context { return srvCtx },
	}

	w.servers[device.ID] = &deviceServer{
		port:   device.Port,
		server: srv,
		cancel: cancel,
	}

	w.log.Info().
		Str("device", device.Name).
		Int("port", device.Port).
		Str("base_url", baseURL).
		Msg("starting HDHR device server")

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			w.log.Error().Err(err).Int("port", device.Port).Msg("HDHR device server error")
		}
	}()
}

func (w *HDHRServerWorker) buildRouter(device models.HDHRDevice, baseURL string) *chi.Mux {
	r := chi.NewRouter()
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Content-Type"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	r.Get("/discover.json", func(rw http.ResponseWriter, req *http.Request) {
		data, err := w.hdhrService.GetDiscoverDataForDevice(req.Context(), &device, baseURL)
		if err != nil {
			http.Error(rw, "failed to get discover info", http.StatusInternalServerError)
			return
		}
		handler.RespondJSONPublic(rw, http.StatusOK, data)
	})

	r.Get("/lineup.json", func(rw http.ResponseWriter, req *http.Request) {
		lineup, err := w.hdhrService.GetLineupForDevice(req.Context(), &device, baseURL)
		if err != nil {
			http.Error(rw, "failed to get lineup", http.StatusInternalServerError)
			return
		}
		handler.RespondJSONPublic(rw, http.StatusOK, lineup)
	})

	r.Get("/lineup_status.json", func(rw http.ResponseWriter, req *http.Request) {
		handler.RespondJSONPublic(rw, http.StatusOK, map[string]any{
			"ScanInProgress": 0,
			"ScanPossible":   1,
			"Source":         "Cable",
			"SourceList":     []string{"Cable"},
		})
	})

	r.Get("/device.xml", func(rw http.ResponseWriter, req *http.Request) {
		deviceXML, err := w.hdhrService.GetDeviceXMLForDevice(req.Context(), &device, baseURL)
		if err != nil {
			http.Error(rw, "failed to get device info", http.StatusInternalServerError)
			return
		}
		rw.Header().Set("Content-Type", "application/xml")
		rw.WriteHeader(http.StatusOK)
		xml.NewEncoder(rw).Encode(deviceXML)
	})

	r.Get("/capability", func(rw http.ResponseWriter, req *http.Request) {
		deviceXML, err := w.hdhrService.GetDeviceXMLForDevice(req.Context(), &device, baseURL)
		if err != nil {
			http.Error(rw, "failed to get device info", http.StatusInternalServerError)
			return
		}
		rw.Header().Set("Content-Type", "application/xml")
		rw.WriteHeader(http.StatusOK)
		xml.NewEncoder(rw).Encode(deviceXML)
	})

	proxyHandler := handler.NewProxyHandler(w.proxyService, w.settingsService, w.log)
	r.Get("/channel/{channelID}", proxyHandler.Stream)

	r.Get("/output/m3u", func(rw http.ResponseWriter, req *http.Request) {
		content, err := w.outputService.GenerateM3UForGroups(req.Context(), device.ChannelGroupIDs, baseURL)
		if err != nil {
			http.Error(rw, "failed to generate m3u", http.StatusInternalServerError)
			return
		}
		rw.Header().Set("Content-Type", "audio/x-mpegurl")
		rw.Header().Set("Content-Disposition", "attachment; filename=\"playlist.m3u\"")
		rw.WriteHeader(http.StatusOK)
		rw.Write([]byte(content))
	})
	r.Get("/output/epg", func(rw http.ResponseWriter, req *http.Request) {
		content, err := w.outputService.GenerateEPGForGroups(req.Context(), device.ChannelGroupIDs)
		if err != nil {
			http.Error(rw, "failed to generate epg", http.StatusInternalServerError)
			return
		}
		rw.Header().Set("Content-Type", "application/xml")
		rw.Header().Set("Content-Disposition", "attachment; filename=\"epg.xml\"")
		rw.WriteHeader(http.StatusOK)
		rw.Write([]byte(content))
	})

	return r
}

func (w *HDHRServerWorker) extractHost() string {
	u, err := url.Parse(w.cfg.BaseURL)
	if err != nil {
		return "localhost"
	}
	host := u.Hostname()
	if host == "" {
		return "localhost"
	}
	return host
}

func (w *HDHRServerWorker) stopAll() {
	w.mu.Lock()
	defer w.mu.Unlock()

	for id, ds := range w.servers {
		w.log.Info().Str("device_id", id).Int("port", ds.port).Msg("stopping HDHR device server")
		ds.cancel()
		ds.server.Close()
		delete(w.servers, id)
	}
}
