package dash

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	"github.com/rs/zerolog"
)

func TempDir() string {
	return filepath.Join(os.TempDir(), "tvproxy-dash")
}

func ChannelDir(channelID string) string {
	return filepath.Join(TempDir(), channelID)
}

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

func (m *Manager) GetOrStart(ctx context.Context, channelID, inputPath, outputDir string, isVOD bool, duration float64) (*Remuxer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if r, ok := m.remuxers[channelID]; ok {
		if !r.IsDone() && r.inputPath == inputPath {
			return r, nil
		}
		r.Stop()
		os.RemoveAll(r.OutputDir())
		delete(m.remuxers, channelID)
	}

	r := NewRemuxer(inputPath, outputDir, isVOD, duration, m.log)
	if err := r.Start(ctx); err != nil {
		return nil, err
	}

	m.remuxers[channelID] = r
	m.log.Info().Str("channel_id", channelID).Msg("dash remuxer started")
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
		os.RemoveAll(r.OutputDir())
		m.log.Info().Str("channel_id", channelID).Msg("dash remuxer stopped")
	}
}

func (m *Manager) Shutdown() {
	m.mu.Lock()
	all := make(map[string]*Remuxer, len(m.remuxers))
	for k, v := range m.remuxers {
		all[k] = v
	}
	m.remuxers = make(map[string]*Remuxer)
	m.mu.Unlock()

	for _, r := range all {
		r.Stop()
		os.RemoveAll(r.OutputDir())
	}
	m.log.Info().Int("count", len(all)).Msg("dash manager shutdown complete")
}
