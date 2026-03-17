package worker

import (
	"context"
	"time"

	"github.com/rs/zerolog"
)

type Checkpointer interface {
	Checkpoint(ctx context.Context)
}

type WALCheckpointWorker struct {
	db       Checkpointer
	interval time.Duration
	log      zerolog.Logger
}

func NewWALCheckpointWorker(db Checkpointer, interval time.Duration, log zerolog.Logger) *WALCheckpointWorker {
	return &WALCheckpointWorker{
		db:       db,
		interval: interval,
		log:      log,
	}
}

func (w *WALCheckpointWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.db.Checkpoint(ctx)
		}
	}
}
