package jellyfin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type persistedState struct {
	mu     sync.Mutex
	path   string
	Tokens map[string]string `json:"tokens"`
}

func loadState(stateDir string) *persistedState {
	ps := &persistedState{
		path:   filepath.Join(stateDir, "jellyfin_state.json"),
		Tokens: make(map[string]string),
	}
	data, err := os.ReadFile(ps.path)
	if err == nil {
		json.Unmarshal(data, ps)
	}
	return ps
}

func (ps *persistedState) saveState() {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	data, err := json.Marshal(ps)
	if err != nil {
		return
	}
	os.WriteFile(ps.path, data, 0600)
}

func (ps *persistedState) syncTokens(m *sync.Map) {
	for token, userID := range ps.Tokens {
		m.Store(token, userID)
	}
}

func (ps *persistedState) storeToken(m *sync.Map, token, userID string) {
	m.Store(token, userID)
	ps.mu.Lock()
	ps.Tokens[token] = userID
	ps.mu.Unlock()
	ps.saveState()
}
