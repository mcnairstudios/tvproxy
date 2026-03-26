package service

import "sync"

type RefreshStatus struct {
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
}

type StatusTracker struct {
	mu       sync.RWMutex
	statuses map[string]RefreshStatus
}

func NewStatusTracker() StatusTracker {
	return StatusTracker{statuses: make(map[string]RefreshStatus)}
}

func (t *StatusTracker) Set(id string, st RefreshStatus) {
	t.mu.Lock()
	t.statuses[id] = st
	t.mu.Unlock()
}

func (t *StatusTracker) Get(id string) RefreshStatus {
	t.mu.RLock()
	st := t.statuses[id]
	t.mu.RUnlock()
	return st
}
