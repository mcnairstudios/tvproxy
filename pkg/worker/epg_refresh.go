package worker

import (
	"context"
	"time"

	"github.com/rs/zerolog"
)

type EPGRefresher interface {
	RefreshAllSources(ctx context.Context) error
}

type EPGRefreshWorker struct {
	service  EPGRefresher
	interval time.Duration
	log      zerolog.Logger
}

func NewEPGRefreshWorker(service EPGRefresher, interval time.Duration, log zerolog.Logger) *EPGRefreshWorker {
	return &EPGRefreshWorker{
		service:  service,
		interval: interval,
		log:      log,
	}
}

func (w *EPGRefreshWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.log.Info().Msg("starting EPG refresh cycle")
			if err := w.service.RefreshAllSources(ctx); err != nil {
				w.log.Error().Err(err).Msg("EPG refresh cycle failed")
			} else {
				w.log.Info().Msg("EPG refresh cycle completed")
			}
		}
	}
}
