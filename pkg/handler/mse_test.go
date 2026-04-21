package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	tvproto "github.com/gavinmcnair/tvproxy/pkg/proto"
	"github.com/gavinmcnair/tvproxy/pkg/session"
)

func setupMSETest(t *testing.T) (*session.Watcher, string, chi.Router) {
	t.Helper()
	dir := t.TempDir()
	log := zerolog.Nop()

	w, err := session.NewWatcher(dir, log)
	require.NoError(t, err)
	t.Cleanup(func() { w.Close() })

	h := NewMSEHandler(func(sessionID string) *session.Watcher {
		if sessionID == "test-session" {
			return w
		}
		return nil
	})

	r := chi.NewRouter()
	r.Get("/mse/{sessionID}/probe", h.Probe)
	r.Get("/mse/{sessionID}/init/{track}", h.Init)
	r.Get("/mse/{sessionID}/segment/{track}", h.Segment)
	r.Get("/mse/{sessionID}/status", h.Status)

	return w, dir, r
}

func TestMSEHandler_SessionNotFound(t *testing.T) {
	_, _, r := setupMSETest(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/mse/nonexistent/probe", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestMSEHandler_ProbeNotReady(t *testing.T) {
	_, _, r := setupMSETest(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/mse/test-session/probe", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestMSEHandler_ProbeReady(t *testing.T) {
	w, dir, r := setupMSETest(t)

	probe := &tvproto.Probe{
		VideoCodec:       "h264",
		VideoCodecString: "avc1.640028",
		VideoWidth:       1920,
		VideoHeight:      1080,
	}
	data, _ := proto.Marshal(probe)
	os.WriteFile(filepath.Join(dir, "probe.pb"), data, 0644)

	require.Eventually(t, func() bool { return w.Probe() != nil }, 2*time.Second, 50*time.Millisecond)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/mse/test-session/probe", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	assert.Equal(t, "h264", resp["video_codec"])
	assert.Equal(t, "avc1.640028", resp["video_codec_string"])
}

func TestMSEHandler_InitNotReady(t *testing.T) {
	_, _, r := setupMSETest(t)


	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/mse/test-session/init/video", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestMSEHandler_SegmentGenMismatch(t *testing.T) {
	w, _, r := setupMSETest(t)
	_ = w

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/mse/test-session/segment/video?gen=999&seq=1", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusGone, rec.Code)
}

func TestMSEHandler_SegmentNotFound(t *testing.T) {
	w, _, r := setupMSETest(t)

	gen := w.Generation()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/mse/test-session/segment/video?gen="+
		itoa(gen)+"&seq=99", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestMSEHandler_Status(t *testing.T) {
	_, _, r := setupMSETest(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/mse/test-session/status", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	assert.Equal(t, float64(1), resp["gen"])
	assert.Equal(t, false, resp["probe_ready"])
	assert.Equal(t, false, resp["video_init"])
	assert.Equal(t, false, resp["audio_init"])
}

func TestMSEHandler_InvalidTrack(t *testing.T) {
	_, _, r := setupMSETest(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/mse/test-session/init/badtrack", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func itoa(n int64) string {
	return fmt.Sprintf("%d", n)
}
