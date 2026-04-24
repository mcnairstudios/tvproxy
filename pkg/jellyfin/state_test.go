package jellyfin

import (
	"os"
	"sync"
	"testing"
)

func TestLoadState_NoFile(t *testing.T) {
	old := stateFile
	defer func() { stateFile = old }()
	stateFile = "/tmp/jf_test_nonexistent.json"
	os.Remove(stateFile)

	s := loadState()
	if s.ServerID == "" {
		t.Error("expected generated ServerID")
	}
	if len(s.ServerID) != 32 {
		t.Errorf("expected 32-char ServerID, got %d: %s", len(s.ServerID), s.ServerID)
	}
	if s.Tokens == nil {
		t.Error("expected non-nil Tokens map")
	}
}

func TestSaveAndLoadState(t *testing.T) {
	old := stateFile
	defer func() { stateFile = old }()
	stateFile = "/tmp/jf_test_state.json"
	defer os.Remove(stateFile)

	srv := &Server{serverID: "aabbccdd11223344aabbccdd11223344"}
	srv.tokens.Store("testtoken1234567", "user-id-1")
	srv.tokens.Store("testtoken7654321", "user-id-2")
	srv.saveState()

	s := loadState()
	if s.ServerID != "aabbccdd11223344aabbccdd11223344" {
		t.Errorf("ServerID mismatch: %s", s.ServerID)
	}
	if len(s.Tokens) != 2 {
		t.Errorf("expected 2 tokens, got %d", len(s.Tokens))
	}
	if s.Tokens["testtoken1234567"] != "user-id-1" {
		t.Errorf("token1 user mismatch: %s", s.Tokens["testtoken1234567"])
	}
}

func TestSyncTokens(t *testing.T) {
	s := persistedState{
		ServerID: "test",
		Tokens:   map[string]string{"tok1": "user1", "tok2": "user2"},
	}
	m := s.syncTokens()

	v, ok := m.Load("tok1")
	if !ok || v.(string) != "user1" {
		t.Error("tok1 not found or wrong value")
	}
	v, ok = m.Load("tok2")
	if !ok || v.(string) != "user2" {
		t.Error("tok2 not found or wrong value")
	}
	_, ok = m.Load("tok3")
	if ok {
		t.Error("tok3 should not exist")
	}
}

func TestServerID_PersistsAcrossRestarts(t *testing.T) {
	old := stateFile
	defer func() { stateFile = old }()
	stateFile = "/tmp/jf_test_serverid.json"
	defer os.Remove(stateFile)

	srv := &Server{serverID: "persistent_server_id_1234567890ab"}
	srv.tokens = sync.Map{}
	srv.saveState()

	s := loadState()
	if s.ServerID != "persistent_server_id_1234567890ab" {
		t.Errorf("ServerID not persisted: got %s", s.ServerID)
	}
}

func TestLoadState_CorruptFile(t *testing.T) {
	old := stateFile
	defer func() { stateFile = old }()
	stateFile = "/tmp/jf_test_corrupt.json"
	defer os.Remove(stateFile)

	os.WriteFile(stateFile, []byte("not json"), 0644)
	s := loadState()
	if s.ServerID == "" {
		t.Error("expected generated ServerID on corrupt file")
	}
	if s.Tokens == nil {
		t.Error("expected non-nil Tokens")
	}
}

func TestAutoRegisterToken(t *testing.T) {
	old := stateFile
	defer func() { stateFile = old }()
	stateFile = "/tmp/jf_test_autoreg.json"
	defer os.Remove(stateFile)

	srv := &Server{serverID: "testserver"}
	srv.tokens = sync.Map{}

	srv.tokens.Store("known_token", "user-123")

	_, ok := srv.tokens.Load("unknown_token")
	if ok {
		t.Fatal("unknown_token should not exist yet")
	}

	srv.tokens.Store("unknown_token", "user-123")
	srv.saveState()

	s := loadState()
	if s.Tokens["unknown_token"] != "user-123" {
		t.Error("auto-registered token not persisted")
	}
	if s.Tokens["known_token"] != "user-123" {
		t.Error("known token lost")
	}
}
