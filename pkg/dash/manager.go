package dash

import (
	"context"
	"sync"

	"github.com/rs/zerolog"
)

type Manager struct {
	remuxers map[string]*Remuxer
	mu       sync.Mutex
	log      zerolog.Logger
}

func NewManager(log zerolog.Logger) *Manager {
	return &Manager{
		remuxers: make(map[string]*Remuxer),
		log:      log.With().Str("component", "dash_manager").Logger(),
	}
}

func (m *Manager) GetOrStart(ctx context.Context, channelID, inputPath, outputDir string) (*Remuxer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if r, ok := m.remuxers[channelID]; ok {
		if !r.IsDone() {
			return r, nil
		}
		delete(m.remuxers, channelID)
	}

	r := NewRemuxer(inputPath, outputDir, m.log)
	if err := r.Start(ctx); err != nil {
		return nil, err
	}

	m.remuxers[channelID] = r
	m.log.Info().Str("channel_id", channelID).Str("input", inputPath).Msg("dash remuxer started")
	return r, nil
}

func (m *Manager) Stop(channelID string) {
	m.mu.Lock()
	r, ok := m.remuxers[channelID]
	if ok {
		delete(m.remuxers, channelID)
	}
	m.mu.Unlock()

	if ok {
		r.Stop()
		m.log.Info().Str("channel_id", channelID).Msg("dash remuxer stopped")
	}
}

func (m *Manager) Shutdown() {
	m.mu.Lock()
	remuxers := make([]*Remuxer, 0, len(m.remuxers))
	for _, r := range m.remuxers {
		remuxers = append(remuxers, r)
	}
	m.remuxers = make(map[string]*Remuxer)
	m.mu.Unlock()

	for _, r := range remuxers {
		r.Stop()
	}
	m.log.Info().Int("count", len(remuxers)).Msg("dash manager shutdown complete")
}
