package worker

import (
	"context"
	"time"

	"github.com/rs/zerolog"
)

type WireGuardSyncer interface {
	SyncIfChanged(ctx context.Context)
}

type WireGuardWorker struct {
	syncer   WireGuardSyncer
	interval time.Duration
	log      zerolog.Logger
}

func NewWireGuardWorker(syncer WireGuardSyncer, interval time.Duration, log zerolog.Logger) *WireGuardWorker {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &WireGuardWorker{
		syncer:   syncer,
		interval: interval,
		log:      log.With().Str("worker", "wireguard").Logger(),
	}
}

func (w *WireGuardWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.syncer.SyncIfChanged(ctx)
		}
	}
}
