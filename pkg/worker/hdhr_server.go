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
	"github.com/gavinmcnair/tvproxy/pkg/repository"
	"github.com/gavinmcnair/tvproxy/pkg/service"
)

type deviceServer struct {
	port   int
	server *http.Server
	cancel context.CancelFunc
}

// HDHRServerWorker manages per-device HTTP listeners on unique ports.
type HDHRServerWorker struct {
	hdhrDeviceRepo *repository.HDHRDeviceRepository
	hdhrService    *service.HDHRService
	proxyService   *service.ProxyService
	outputService  *service.OutputService
	cfg            *config.Config
	log            zerolog.Logger
	mu             sync.Mutex
	servers        map[int64]*deviceServer
}

// NewHDHRServerWorker creates a new per-device HDHR HTTP server worker.
func NewHDHRServerWorker(
	hdhrDeviceRepo *repository.HDHRDeviceRepository,
	hdhrService *service.HDHRService,
	proxyService *service.ProxyService,
	outputService *service.OutputService,
	cfg *config.Config,
	log zerolog.Logger,
) *HDHRServerWorker {
	return &HDHRServerWorker{
		hdhrDeviceRepo: hdhrDeviceRepo,
		hdhrService:    hdhrService,
		proxyService:   proxyService,
		outputService:  outputService,
		cfg:            cfg,
		log:            log.With().Str("worker", "hdhr_servers").Logger(),
		servers:        make(map[int64]*deviceServer),
	}
}

// Run implements the Worker interface. Syncs device servers every 10 seconds.
func (w *HDHRServerWorker) Run(ctx context.Context) {
	// Wait for HTTP server to start
	select {
	case <-time.After(2 * time.Second):
	case <-ctx.Done():
		return
	}

	w.sync(ctx)

	ticker := time.NewTicker(10 * time.Second)
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
	devices, err := w.hdhrDeviceRepo.List(ctx)
	if err != nil {
		w.log.Error().Err(err).Msg("failed to list devices for HDHR servers")
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	// Build set of desired device ID → port
	desired := make(map[int64]int)
	for _, d := range devices {
		if d.IsEnabled && d.Port > 0 {
			desired[d.ID] = d.Port
		}
	}

	// Stop servers for removed/disabled/port-changed devices
	for id, ds := range w.servers {
		wantPort, ok := desired[id]
		if !ok || wantPort != ds.port {
			w.log.Info().Int64("device_id", id).Int("port", ds.port).Msg("stopping HDHR device server")
			ds.cancel()
			ds.server.Close()
			delete(w.servers, id)
		}
	}

	// Start servers for new devices
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
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0,
		IdleTimeout:  60 * time.Second,
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

	// Device-specific HDHR endpoints
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
		handler.RespondJSONPublic(rw, http.StatusOK, map[string]interface{}{
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

	// Shared channel proxy route
	proxyHandler := handler.NewProxyHandler(w.proxyService, w.log)
	r.Get("/channel/{channelID}", proxyHandler.Stream)

	// Device-specific filtered M3U/EPG output
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
		w.log.Info().Int64("device_id", id).Int("port", ds.port).Msg("stopping HDHR device server")
		ds.cancel()
		ds.server.Close()
		delete(w.servers, id)
	}
}
