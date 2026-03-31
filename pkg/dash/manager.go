package dash

import (
	"context"
	"io"
	"os"
	"sync"

	"github.com/rs/zerolog"
)

type Remuxing struct {
	remuxer *Remuxer
}

type Manager struct {
	remuxings map[string]*Remuxing
	mu        sync.Mutex
	log       zerolog.Logger
}

func NewManager(log zerolog.Logger) *Manager {
	return &Manager{
		remuxings: make(map[string]*Remuxing),
		log:       log.With().Str("component", "dash_manager").Logger(),
	}
}

func (m *Manager) GetOrStart(ctx context.Context, channelID, outputDir string, input io.Reader, audioOnly bool) (*Remuxer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if rx, ok := m.remuxings[channelID]; ok {
		if !rx.remuxer.IsDone() {
			return rx.remuxer, nil
		}
		delete(m.remuxings, channelID)
	}

	r := NewRemuxer(outputDir, m.log)
	if err := r.Start(ctx, input, audioOnly); err != nil {
		return nil, err
	}

	m.remuxings[channelID] = &Remuxing{remuxer: r}
	m.log.Info().Str("channel_id", channelID).Msg("dash remuxer started")
	return r, nil
}

func (m *Manager) Stop(channelID string) {
	m.mu.Lock()
	rx, ok := m.remuxings[channelID]
	if ok {
		delete(m.remuxings, channelID)
	}
	m.mu.Unlock()

	if ok {
		rx.remuxer.Stop()
		os.RemoveAll(rx.remuxer.OutputDir())
		m.log.Info().Str("channel_id", channelID).Msg("dash remuxer stopped")
	}
}

func (m *Manager) Shutdown() {
	m.mu.Lock()
	all := make(map[string]*Remuxing, len(m.remuxings))
	for k, v := range m.remuxings {
		all[k] = v
	}
	m.remuxings = make(map[string]*Remuxing)
	m.mu.Unlock()

	for _, rx := range all {
		rx.remuxer.Stop()
		os.RemoveAll(rx.remuxer.OutputDir())
	}
	m.log.Info().Int("count", len(all)).Msg("dash manager shutdown complete")
}
