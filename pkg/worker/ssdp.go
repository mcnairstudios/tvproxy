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

type SSDPWorker struct {
	hdhrDeviceRepo   *repository.HDHRDeviceRepository
	baseURL          string
	log              zerolog.Logger
	advertisers      map[string]*ssdpAdvertiser
	retryDelay       time.Duration
	announceInterval time.Duration
}

type ssdpAdvertiser struct {
	ad     *ssdp.Advertiser
	port   int
	cancel context.CancelFunc
}

func NewSSDPWorker(hdhrDeviceRepo *repository.HDHRDeviceRepository, baseURL string, retryDelay, announceInterval time.Duration, log zerolog.Logger) *SSDPWorker {
	if retryDelay <= 0 {
		retryDelay = 2 * time.Second
	}
	if announceInterval <= 0 {
		announceInterval = 30 * time.Second
	}
	return &SSDPWorker{
		hdhrDeviceRepo:   hdhrDeviceRepo,
		baseURL:          baseURL,
		log:              log.With().Str("worker", "ssdp").Logger(),
		advertisers:      make(map[string]*ssdpAdvertiser),
		retryDelay:       retryDelay,
		announceInterval: announceInterval,
	}
}

func (w *SSDPWorker) Run(ctx context.Context) {
	select {
	case <-time.After(w.retryDelay):
	case <-ctx.Done():
		return
	}

	w.syncAdvertisers(ctx)

	ticker := time.NewTicker(w.announceInterval)
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

	desired := make(map[string]*models.HDHRDevice)
	for i := range devices {
		if devices[i].IsEnabled && devices[i].Port > 0 {
			desired[devices[i].ID] = &devices[i]
		}
	}

	for id, adv := range w.advertisers {
		dev, ok := desired[id]
		if !ok || dev.Port != adv.port {
			w.log.Debug().Str("device_id", id).Msg("stopping SSDP advertiser")
			adv.ad.Bye()
			adv.ad.Close()
			adv.cancel()
			delete(w.advertisers, id)
		}
	}

	for id, dev := range desired {
		if _, exists := w.advertisers[id]; exists {
			continue
		}
		w.startAdvertiser(ctx, dev)
	}

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
		w.log.Debug().Str("device_id", id).Msg("stopping SSDP advertiser")
		adv.ad.Bye()
		adv.ad.Close()
		adv.cancel()
		delete(w.advertisers, id)
	}
	w.log.Debug().Msg("all SSDP advertisers stopped")
}
