package worker

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/koron/go-ssdp"
	"github.com/rs/zerolog"
)

type DLNASettingsChecker interface {
	IsEnabled(ctx context.Context) bool
	UDN() string
}

type DLNAWorker struct {
	checker    DLNASettingsChecker
	baseURL    string
	port       int
	log        zerolog.Logger
	advertiser *ssdp.Advertiser
}

func NewDLNAWorker(checker DLNASettingsChecker, baseURL string, port int, log zerolog.Logger) *DLNAWorker {
	return &DLNAWorker{
		checker: checker,
		baseURL: baseURL,
		port:    port,
		log:     log.With().Str("worker", "dlna").Logger(),
	}
}

func (w *DLNAWorker) Run(ctx context.Context) {
	select {
	case <-time.After(2 * time.Second):
	case <-ctx.Done():
		return
	}

	w.sync(ctx)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.stop()
			return
		case <-ticker.C:
			w.sync(ctx)
		}
	}
}

func (w *DLNAWorker) sync(ctx context.Context) {
	enabled := w.checker.IsEnabled(ctx)

	if enabled && w.advertiser == nil {
		w.start()
	} else if !enabled && w.advertiser != nil {
		w.stop()
	} else if enabled && w.advertiser != nil {
		if err := w.advertiser.Alive(); err != nil {
			w.log.Warn().Err(err).Msg("DLNA SSDP alive failed")
		}
	}
}

func (w *DLNAWorker) start() {
	host := w.extractHost()
	location := fmt.Sprintf("http://%s:%d/dlna/device.xml", host, w.port)
	udn := w.checker.UDN()
	usn := udn + "::urn:schemas-upnp-org:device:MediaServer:1"

	w.log.Info().Str("location", location).Msg("starting DLNA SSDP advertiser")

	ad, err := ssdp.Advertise(
		"urn:schemas-upnp-org:device:MediaServer:1",
		usn,
		location,
		"TVProxy/1.0 UPnP/1.0 DLNA/1.50",
		1800,
	)
	if err != nil {
		w.log.Error().Err(err).Msg("failed to start DLNA SSDP advertiser")
		return
	}
	w.advertiser = ad

	if err := ad.Alive(); err != nil {
		w.log.Warn().Err(err).Msg("DLNA SSDP initial alive failed")
	}
}

func (w *DLNAWorker) stop() {
	if w.advertiser == nil {
		return
	}
	w.log.Info().Msg("stopping DLNA SSDP advertiser")
	w.advertiser.Bye()
	w.advertiser.Close()
	w.advertiser = nil
}

func (w *DLNAWorker) extractHost() string {
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
