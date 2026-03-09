package worker

import (
	"context"
	"sync"

	"github.com/rs/zerolog"
)

type Worker interface {
	Run(ctx context.Context)
}

type Manager struct {
	workers map[string]Worker
	log     zerolog.Logger
	wg      sync.WaitGroup
	cancel  context.CancelFunc
}

func NewManager(log zerolog.Logger) *Manager {
	return &Manager{
		workers: make(map[string]Worker),
		log:     log,
	}
}

func (m *Manager) Add(name string, w Worker) {
	m.workers[name] = w
}

func (m *Manager) Start(ctx context.Context) {
	workerCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel

	for name, w := range m.workers {
		m.wg.Add(1)
		go func(name string, w Worker) {
			defer m.wg.Done()
			m.log.Info().Str("worker", name).Msg("worker started")
			w.Run(workerCtx)
			m.log.Info().Str("worker", name).Msg("worker stopped")
		}(name, w)
	}
}

func (m *Manager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()
	m.log.Info().Msg("all workers stopped")
}
