package jellyfin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestQuickConnectInitiate_Returns400(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/QuickConnect/Initiate", nil)
	w := httptest.NewRecorder()

	srv.quickConnectInitiate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestQuickConnectInitiate_JSONResponse(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/QuickConnect/Initiate", nil)
	w := httptest.NewRecorder()

	srv.quickConnectInitiate(w, req)

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if _, ok := body["Message"]; !ok {
		t.Error("expected Message field in response")
	}
}

func TestScheduledTasks_ReturnsEmptyArray(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/ScheduledTasks", nil)
	w := httptest.NewRecorder()

	srv.scheduledTasks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var tasks []any
	if err := json.NewDecoder(w.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected empty array, got %d items", len(tasks))
	}
}

func TestScheduledTasks_ContentType(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/ScheduledTasks", nil)
	w := httptest.NewRecorder()

	srv.scheduledTasks(w, req)

	ct := w.Header().Get("Content-Type")
	if ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type: got %q want application/json; charset=utf-8", ct)
	}
}

func TestSystemInfoStorage_Returns200(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/System/Info/Storage", nil)
	w := httptest.NewRecorder()

	srv.systemInfoStorage(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestSystemInfoStorage_DrivesList(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/System/Info/Storage", nil)
	w := httptest.NewRecorder()

	srv.systemInfoStorage(w, req)

	var info StorageInfo
	if err := json.NewDecoder(w.Body).Decode(&info); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if info.Drives == nil {
		t.Error("Drives should not be nil")
	}
	if len(info.Drives) != 0 {
		t.Errorf("expected empty Drives slice, got %d", len(info.Drives))
	}
}

func TestSystemInfoStorage_JSONStructure(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/System/Info/Storage", nil)
	w := httptest.NewRecorder()

	srv.systemInfoStorage(w, req)

	var raw map[string]any
	if err := json.NewDecoder(w.Body).Decode(&raw); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if _, ok := raw["Drives"]; !ok {
		t.Error("response missing Drives field")
	}
}

func TestWebConfigurationPages_ReturnsEmptyArray(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/web/ConfigurationPages", nil)
	req = withURLParam(req, "file", "ConfigurationPages")
	w := httptest.NewRecorder()

	srv.webFile(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var pages []any
	if err := json.NewDecoder(w.Body).Decode(&pages); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(pages) != 0 {
		t.Errorf("expected empty array, got %d items", len(pages))
	}
}

func TestWebConfigurationPages_ContentType(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/web/ConfigurationPages", nil)
	req = withURLParam(req, "file", "ConfigurationPages")
	w := httptest.NewRecorder()

	srv.webFile(w, req)

	ct := w.Header().Get("Content-Type")
	if ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type: got %q want application/json; charset=utf-8", ct)
	}
}

func TestSessionsGet_ReturnsEmptyArray(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/Sessions", nil)
	w := httptest.NewRecorder()

	srv.sessionsGet(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var sessions []SessionInfo
	if err := json.NewDecoder(w.Body).Decode(&sessions); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected empty array, got %d items", len(sessions))
	}
}

func TestBitrateTest_DefaultSize(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/Playback/BitrateTest", nil)
	w := httptest.NewRecorder()

	srv.bitrateTest(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.Len() != 1000000 {
		t.Errorf("expected 1000000 bytes, got %d", w.Body.Len())
	}
}

func TestBitrateTest_CustomSize(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/Playback/BitrateTest?size=512000", nil)
	w := httptest.NewRecorder()

	srv.bitrateTest(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.Len() != 512000 {
		t.Errorf("expected 512000 bytes, got %d", w.Body.Len())
	}
}

func TestBitrateTest_ContentType(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/Playback/BitrateTest?size=1024", nil)
	w := httptest.NewRecorder()

	srv.bitrateTest(w, req)

	ct := w.Header().Get("Content-Type")
	if ct != "application/octet-stream" {
		t.Errorf("Content-Type: got %q want application/octet-stream", ct)
	}
}

func TestThemeMedia_ReturnsEmptyArray(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/Items/abc123/ThemeMedia", nil)
	req = withURLParam(req, "itemId", "abc123")
	w := httptest.NewRecorder()

	srv.listSpecialFeatures(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var items []BaseItemDto
	if err := json.NewDecoder(w.Body).Decode(&items); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected empty array, got %d items", len(items))
	}
}

func TestClientLog_Returns200(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/ClientLog/Document", nil)
	w := httptest.NewRecorder()

	srv.clientLog(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestSessionsCapabilities_NoContent(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/Sessions/Capabilities", nil)
	w := httptest.NewRecorder()

	noContent(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

func TestSessionsCapabilitiesFull_NoContent(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/Sessions/Capabilities/Full", nil)
	w := httptest.NewRecorder()

	noContent(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}
