package gstreamer

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/avprobe"
	"github.com/gavinmcnair/tvproxy/pkg/media"
	"github.com/gavinmcnair/tvproxy/pkg/store"
)

type ProbeJob struct {
	StreamID  string
	StreamURL string
}

type ProbeScheduler struct {
	probeCache store.ProbeCache
	log        zerolog.Logger
	queue      []ProbeJob
	mu         sync.Mutex
	running    bool
	interval   time.Duration
}

func NewProbeScheduler(probeCache store.ProbeCache, log zerolog.Logger) *ProbeScheduler {
	return &ProbeScheduler{
		probeCache: probeCache,
		log:        log.With().Str("component", "probe_scheduler").Logger(),
		interval:   3 * time.Second,
	}
}

func (s *ProbeScheduler) QueueStreams(jobs []ProbeJob) {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing := make(map[string]bool)
	for _, j := range s.queue {
		existing[j.StreamID] = true
	}

	added := 0
	for _, j := range jobs {
		if existing[j.StreamID] {
			continue
		}
		cached, _ := s.probeCache.GetProbe(j.StreamID)
		if cached != nil {
			continue
		}
		s.queue = append(s.queue, j)
		added++
	}
	if added > 0 {
		s.log.Info().Int("queued", added).Int("total", len(s.queue)).Msg("probe jobs queued")
	}
}

func (s *ProbeScheduler) QueueCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.queue)
}

func (s *ProbeScheduler) Run(ctx context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		job, ok := s.nextJob()
		if !ok {
			return
		}

		s.probeOne(ctx, job)

		select {
		case <-ctx.Done():
			return
		case <-time.After(s.interval):
		}
	}
}

func (s *ProbeScheduler) nextJob() (ProbeJob, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) == 0 {
		return ProbeJob{}, false
	}
	job := s.queue[0]
	s.queue = s.queue[1:]
	return job, true
}

func (s *ProbeScheduler) probeOne(ctx context.Context, job ProbeJob) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Error().Interface("panic", r).Str("stream", job.StreamID).Msg("probe panic recovered")
		}
	}()

	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	result, err := avprobe.Probe(probeCtx, job.StreamURL, "")
	if err != nil {
		s.log.Debug().Err(err).Str("stream", job.StreamID).Msg("probe failed")
		return
	}

	if result == nil {
		return
	}

	s.probeCache.SaveProbe(job.StreamID, result)

	if media.IsHTTPURL(job.StreamURL) {
		header, err := media.CaptureTPSHeader(probeCtx, job.StreamURL, 5*time.Second)
		if err == nil && len(header) > 0 {
			s.probeCache.SaveTSHeader(job.StreamID, header)
		}
	}

	s.log.Info().
		Str("url", job.StreamURL).
		Msg("probed stream")
}
