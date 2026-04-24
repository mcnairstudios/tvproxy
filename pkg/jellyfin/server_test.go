package jellyfin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func newTestServer() *Server {
	return &Server{
		serverID:   "testserverid1234567890abcdef1234",
		serverName: "Test",
	}
}

func withURLParam(r *http.Request, key, val string) *http.Request {
	rctx := chi.RouteContext(r.Context())
	if rctx == nil {
		rctx = chi.NewRouteContext()
		r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	}
	rctx.URLParams.Add(key, val)
	return r
}

func TestDisplayPreferences_AllFields(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/DisplayPreferences/usersettings?client=emby", nil)
	req = withURLParam(req, "id", "usersettings")
	w := httptest.NewRecorder()

	srv.displayPreferences(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var prefs DisplayPreferences
	if err := json.NewDecoder(w.Body).Decode(&prefs); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if prefs.ID != "usersettings" {
		t.Errorf("Id: got %q want %q", prefs.ID, "usersettings")
	}
	if prefs.SortBy != "SortName" {
		t.Errorf("SortBy: got %q want %q", prefs.SortBy, "SortName")
	}
	if prefs.RememberIndexing {
		t.Error("RememberIndexing should be false")
	}
	if prefs.PrimaryImageHeight != 250 {
		t.Errorf("PrimaryImageHeight: got %d want 250", prefs.PrimaryImageHeight)
	}
	if prefs.PrimaryImageWidth != 250 {
		t.Errorf("PrimaryImageWidth: got %d want 250", prefs.PrimaryImageWidth)
	}
	if prefs.ScrollDirection != "Horizontal" {
		t.Errorf("ScrollDirection: got %q want %q", prefs.ScrollDirection, "Horizontal")
	}
	if !prefs.ShowBackdrop {
		t.Error("ShowBackdrop should be true")
	}
	if prefs.RememberSorting {
		t.Error("RememberSorting should be false")
	}
	if prefs.SortOrder != "Ascending" {
		t.Errorf("SortOrder: got %q want %q", prefs.SortOrder, "Ascending")
	}
	if prefs.ShowSidebar {
		t.Error("ShowSidebar should be false")
	}
	if prefs.Client != "emby" {
		t.Errorf("Client: got %q want %q", prefs.Client, "emby")
	}
}

func TestDisplayPreferences_CustomPrefs(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/DisplayPreferences/usersettings?client=emby", nil)
	req = withURLParam(req, "id", "usersettings")
	w := httptest.NewRecorder()

	srv.displayPreferences(w, req)

	var prefs DisplayPreferences
	json.NewDecoder(w.Body).Decode(&prefs)

	cp := prefs.CustomPrefs
	if cp.ChromecastVersion != "stable" {
		t.Errorf("chromecastVersion: got %q want %q", cp.ChromecastVersion, "stable")
	}
	if cp.SkipForwardLength != "30000" {
		t.Errorf("skipForwardLength: got %q want %q", cp.SkipForwardLength, "30000")
	}
	if cp.SkipBackLength != "10000" {
		t.Errorf("skipBackLength: got %q want %q", cp.SkipBackLength, "10000")
	}
	if cp.EnableNextVideoInfoOverlay != "False" {
		t.Errorf("enableNextVideoInfoOverlay: got %q want %q", cp.EnableNextVideoInfoOverlay, "False")
	}
	if cp.TVHome != nil {
		t.Errorf("tvhome: expected nil, got %v", cp.TVHome)
	}
	if cp.DashboardTheme != nil {
		t.Errorf("dashboardTheme: expected nil, got %v", cp.DashboardTheme)
	}
}

func TestDisplayPreferences_ClientFromQueryParam(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/DisplayPreferences/usersettings?client=Infuse", nil)
	req = withURLParam(req, "id", "usersettings")
	w := httptest.NewRecorder()

	srv.displayPreferences(w, req)

	var prefs DisplayPreferences
	json.NewDecoder(w.Body).Decode(&prefs)

	if prefs.Client != "Infuse" {
		t.Errorf("Client: got %q want %q", prefs.Client, "Infuse")
	}
}

func TestDisplayPreferences_ClientFallsBackToAuthHeader(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/DisplayPreferences/usersettings", nil)
	req.Header.Set("X-Emby-Authorization", `MediaBrowser Client="Jellyfin Web", Device="Chrome", DeviceId="abc", Version="10.10.6", Token="tok"`)
	req = withURLParam(req, "id", "usersettings")
	w := httptest.NewRecorder()

	srv.displayPreferences(w, req)

	var prefs DisplayPreferences
	json.NewDecoder(w.Body).Decode(&prefs)

	if prefs.Client != "Jellyfin Web" {
		t.Errorf("Client: got %q want %q", prefs.Client, "Jellyfin Web")
	}
}

func TestDisplayPreferences_JSONStructure(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/DisplayPreferences/usersettings?client=emby", nil)
	req = withURLParam(req, "id", "usersettings")
	w := httptest.NewRecorder()

	srv.displayPreferences(w, req)

	var raw map[string]any
	if err := json.NewDecoder(w.Body).Decode(&raw); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	requiredKeys := []string{
		"Id", "SortBy", "RememberIndexing", "PrimaryImageHeight", "PrimaryImageWidth",
		"CustomPrefs", "ScrollDirection", "ShowBackdrop", "RememberSorting",
		"SortOrder", "ShowSidebar", "Client",
	}
	for _, k := range requiredKeys {
		if _, ok := raw[k]; !ok {
			t.Errorf("missing field %q in response", k)
		}
	}

	customPrefs, ok := raw["CustomPrefs"].(map[string]any)
	if !ok {
		t.Fatal("CustomPrefs is not an object")
	}
	customPrefsKeys := []string{
		"chromecastVersion", "skipForwardLength", "skipBackLength",
		"enableNextVideoInfoOverlay", "tvhome", "dashboardTheme",
	}
	for _, k := range customPrefsKeys {
		if _, ok := customPrefs[k]; !ok {
			t.Errorf("missing CustomPrefs field %q", k)
		}
	}
}

func TestSystemEndpoint(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/System/Endpoint", nil)
	w := httptest.NewRecorder()

	srv.systemEndpoint(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var info EndpointInfo
	if err := json.NewDecoder(w.Body).Decode(&info); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if info.IsLocal {
		t.Error("IsLocal should be false")
	}
	if !info.IsInNetwork {
		t.Error("IsInNetwork should be true")
	}
}

func TestSystemEndpoint_ContentType(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/System/Endpoint", nil)
	w := httptest.NewRecorder()

	srv.systemEndpoint(w, req)

	ct := w.Header().Get("Content-Type")
	if ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type: got %q want application/json; charset=utf-8", ct)
	}
}
