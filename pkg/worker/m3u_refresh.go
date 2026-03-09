package worker

import (
	"context"
	"time"

	"github.com/rs/zerolog"
)

type M3URefresher interface {
	RefreshAllAccounts(ctx context.Context) error
}

type M3URefreshWorker struct {
	service  M3URefresher
	interval time.Duration
	log      zerolog.Logger
}

func NewM3URefreshWorker(service M3URefresher, interval time.Duration, log zerolog.Logger) *M3URefreshWorker {
	return &M3URefreshWorker{
		service:  service,
		interval: interval,
		log:      log,
	}
}

func (w *M3URefreshWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.log.Info().Msg("starting M3U refresh cycle")
			if err := w.service.RefreshAllAccounts(ctx); err != nil {
				w.log.Error().Err(err).Msg("M3U refresh cycle failed")
			} else {
				w.log.Info().Msg("M3U refresh cycle completed")
			}
		}
	}
}
