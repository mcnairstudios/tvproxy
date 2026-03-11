package worker

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/koron/go-ssdp"
	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
)

// SSDPWorker advertises HDHR devices via SSDP so Plex/Emby/Jellyfin
// can auto-discover them on the local network.
type SSDPWorker struct {
	hdhrDeviceRepo *repository.HDHRDeviceRepository
	baseURL        string
	log            zerolog.Logger
	advertisers    map[int64]*ssdpAdvertiser
}

type ssdpAdvertiser struct {
	ad     *ssdp.Advertiser
	port   int
	cancel context.CancelFunc
}

// NewSSDPWorker creates a new SSDP discovery worker.
func NewSSDPWorker(hdhrDeviceRepo *repository.HDHRDeviceRepository, baseURL string, log zerolog.Logger) *SSDPWorker {
	return &SSDPWorker{
		hdhrDeviceRepo: hdhrDeviceRepo,
		baseURL:        baseURL,
		log:            log.With().Str("worker", "ssdp").Logger(),
		advertisers:    make(map[int64]*ssdpAdvertiser),
	}
}

// Run starts SSDP advertisers for all enabled HDHR devices.
func (w *SSDPWorker) Run(ctx context.Context) {
	// Wait briefly for the HTTP server to start
	select {
	case <-time.After(2 * time.Second):
	case <-ctx.Done():
		return
	}

	w.syncAdvertisers(ctx)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.stopAll()
			return
		case <-ticker.C:
			w.syncAdvertisers(ctx)
		}
	}
}

func (w *SSDPWorker) syncAdvertisers(ctx context.Context) {
	devices, err := w.hdhrDeviceRepo.List(ctx)
	if err != nil {
		w.log.Error().Err(err).Msg("failed to list HDHR devices for SSDP")
		return
	}

	// Build desired set
	desired := make(map[int64]*models.HDHRDevice)
	for i := range devices {
		if devices[i].IsEnabled && devices[i].Port > 0 {
			desired[devices[i].ID] = &devices[i]
		}
	}

	// Stop advertisers for removed/disabled/port-changed devices
	for id, adv := range w.advertisers {
		dev, ok := desired[id]
		if !ok || dev.Port != adv.port {
			w.log.Debug().Int64("device_id", id).Msg("stopping SSDP advertiser")
			adv.ad.Bye()
			adv.ad.Close()
			adv.cancel()
			delete(w.advertisers, id)
		}
	}

	// Start advertisers for new devices
	for id, dev := range desired {
		if _, exists := w.advertisers[id]; exists {
			continue
		}
		w.startAdvertiser(ctx, dev)
	}

	// Send alive for all active advertisers
	for _, adv := range w.advertisers {
		if err := adv.ad.Alive(); err != nil {
			w.log.Warn().Err(err).Msg("SSDP alive failed")
		}
	}
}

func (w *SSDPWorker) startAdvertiser(ctx context.Context, device *models.HDHRDevice) {
	host := w.extractHost()
	location := fmt.Sprintf("http://%s:%d/device.xml", host, device.Port)
	usn := fmt.Sprintf("uuid:%s::upnp:rootdevice", device.DeviceID)

	w.log.Debug().
		Str("device", device.Name).
		Str("device_id", device.DeviceID).
		Str("location", location).
		Msg("starting SSDP advertiser")

	ad, err := ssdp.Advertise(
		"upnp:rootdevice",
		usn,
		location,
		"HDHomeRun/1.0 UPnP/1.0",
		1800,
	)
	if err != nil {
		w.log.Error().Err(err).Str("device", device.Name).Msg("failed to start SSDP advertiser")
		return
	}

	advCtx, cancel := context.WithCancel(ctx)
	w.advertisers[device.ID] = &ssdpAdvertiser{
		ad:     ad,
		port:   device.Port,
		cancel: cancel,
	}

	// Monitor context for cleanup
	go func() {
		<-advCtx.Done()
	}()
}

func (w *SSDPWorker) extractHost() string {
	u, err := url.Parse(w.baseURL)
	if err != nil {
		return "localhost"
	}
	host := u.Hostname()
	if host == "" {
		return "localhost"
	}
	return host
}

func (w *SSDPWorker) stopAll() {
	for id, adv := range w.advertisers {
		w.log.Debug().Int64("device_id", id).Msg("stopping SSDP advertiser")
		adv.ad.Bye()
		adv.ad.Close()
		adv.cancel()
		delete(w.advertisers, id)
	}
	w.log.Debug().Msg("all SSDP advertisers stopped")
}
