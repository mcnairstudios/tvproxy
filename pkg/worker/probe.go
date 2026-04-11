package worker

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/gstreamer"
	"github.com/gavinmcnair/tvproxy/pkg/store"
)

type ProbeWorker struct {
	scheduler  *gstreamer.ProbeScheduler
	streamStore store.StreamReader
	log        zerolog.Logger
	interval   time.Duration
}

func NewProbeWorker(
	probeCache store.ProbeCache,
	streamStore store.StreamReader,
	interval time.Duration,
	log zerolog.Logger,
) *ProbeWorker {
	return &ProbeWorker{
		scheduler:   gstreamer.NewProbeScheduler(probeCache, log),
		streamStore: streamStore,
		log:         log.With().Str("worker", "probe").Logger(),
		interval:    interval,
	}
}

func (w *ProbeWorker) Run(ctx context.Context) {
	w.log.Info().Msg("probe worker started")
	defer w.log.Info().Msg("probe worker stopped")

	w.queueUnprobed(ctx)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		if w.scheduler.QueueCount() > 0 {
			w.scheduler.Run(ctx)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.queueUnprobed(ctx)
		}
	}
}

func (w *ProbeWorker) queueUnprobed(ctx context.Context) {
	streams, err := w.streamStore.List(ctx)
	if err != nil {
		w.log.Error().Err(err).Msg("failed to list streams for probing")
		return
	}

	var jobs []gstreamer.ProbeJob
	for _, s := range streams {
		if s.URL == "" || !s.IsActive {
			continue
		}
		jobs = append(jobs, gstreamer.ProbeJob{
			StreamID:  s.ID,
			StreamURL: s.URL,
		})
	}

	if len(jobs) > 0 {
		w.scheduler.QueueStreams(jobs)
	}
}

func (w *ProbeWorker) Scheduler() *gstreamer.ProbeScheduler {
	return w.scheduler
}
