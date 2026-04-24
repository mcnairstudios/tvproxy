package jellyfin

import (
	"encoding/json"
	"os"
	"sync"
)

var stateFile = "jellyfin_state.json"

type persistedState struct {
	ServerID string            `json:"server_id"`
	Tokens   map[string]string `json:"tokens"`
}

func loadState() persistedState {
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return persistedState{
			ServerID: generateGUID(),
			Tokens:   map[string]string{},
		}
	}
	var s persistedState
	if err := json.Unmarshal(data, &s); err != nil || s.ServerID == "" {
		return persistedState{
			ServerID: generateGUID(),
			Tokens:   map[string]string{},
		}
	}
	if s.Tokens == nil {
		s.Tokens = map[string]string{}
	}
	return s
}

func (s *persistedState) syncTokens() sync.Map {
	var m sync.Map
	for k, v := range s.Tokens {
		m.Store(k, v)
	}
	return m
}

func (srv *Server) saveState() {
	tokens := map[string]string{}
	srv.tokens.Range(func(k, v any) bool {
		tokens[k.(string)] = v.(string)
		return true
	})
	state := persistedState{
		ServerID: srv.serverID,
		Tokens:   tokens,
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(stateFile, data, 0644)
}
