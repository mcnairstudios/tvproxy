package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/dash"
	"github.com/gavinmcnair/tvproxy/pkg/middleware"
	"github.com/gavinmcnair/tvproxy/pkg/service"
)

type VODHandler struct {
	vodService    *service.VODService
	clientService *service.ClientService
	dashManager   *dash.Manager
	log           zerolog.Logger
}

func NewVODHandler(vodService *service.VODService, clientService *service.ClientService, dashManager *dash.Manager, log zerolog.Logger) *VODHandler {
	return &VODHandler{
		vodService:    vodService,
		clientService: clientService,
		dashManager:   dashManager,
		log:           log.With().Str("handler", "vod").Logger(),
	}
}

func requireUser(r *http.Request) *middleware.ContextUser {
	return middleware.UserFromContext(r.Context())
}

func parseStopAt(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, raw)
}

func clientHeaders(r *http.Request) map[string]string {
	skip := map[string]bool{
		"Cookie": true, "Authorization": true, "Connection": true,
		"Upgrade": true, "Sec-Websocket-Key": true, "Sec-Websocket-Version": true,
	}
	out := make(map[string]string, len(r.Header))
	for k, v := range r.Header {
		if skip[k] {
			continue
		}
		out[k] = v[0]
	}
	return out
}

func setStreamHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.WriteHeader(http.StatusOK)
}

func streamResponse(w http.ResponseWriter, reader io.Reader) {
	buf := make([]byte, 32*1024)
	for {
		n, readErr := reader.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		if readErr != nil {
			return
		}
	}
}

func serveTranscodedOrFile(w http.ResponseWriter, r *http.Request, reader io.ReadCloser, contentType string) {
	if f, ok := reader.(*os.File); ok {
		reader.Close()
		http.ServeFile(w, r, f.Name())
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.WriteHeader(http.StatusOK)
	streamResponse(w, reader)
}

func (h *VODHandler) ProbeStream(w http.ResponseWriter, r *http.Request) {
	streamID := chi.URLParam(r, "streamID")

	result, err := h.vodService.ProbeStream(r.Context(), streamID)
	if err != nil {
		h.log.Error().Err(err).Str("stream_id", streamID).Msg("probe failed")
		respondError(w, http.StatusNotFound, "stream not found")
		return
	}

	if result.IsVOD {
		respondJSON(w, http.StatusOK, map[string]any{
			"type":     "vod",
			"duration": result.Duration,
			"width":    result.Width,
			"height":   result.Height,
		})
	} else {
		respondJSON(w, http.StatusOK, map[string]any{
			"type": "live",
		})
	}
}

func (h *VODHandler) CreateSession(w http.ResponseWriter, r *http.Request) {
	streamID := chi.URLParam(r, "streamID")
	profileName := r.URL.Query().Get("profile")

	sessionID, consumerID, container, err := h.vodService.StartWatchingStream(r.Context(), streamID, profileName, r.UserAgent(), r.RemoteAddr)
	if err != nil {
		h.log.Error().Err(err).Str("stream_id", streamID).Msg("create VOD session failed")
		if errors.Is(err, service.ErrStreamNotFound) {
			respondError(w, http.StatusNotFound, err.Error())
			return
		}
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"session_id":      sessionID,
		"consumer_id":     consumerID,
		"channel_id":      streamID,
		"container":       container,
		"request_headers": clientHeaders(r),
	})
}

func (h *VODHandler) CreateChannelSession(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelID")
	profileName := r.URL.Query().Get("profile")

	sessionID, consumerID, container, audioOnly, err := h.vodService.StartWatching(r.Context(), channelID, profileName, r.UserAgent(), r.RemoteAddr)
	if err != nil {
		h.log.Error().Err(err).Str("channel_id", channelID).Msg("create channel VOD session failed")
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"session_id":      sessionID,
		"consumer_id":     consumerID,
		"channel_id":      channelID,
		"container":       container,
		"audio_only":      audioOnly,
		"request_headers": clientHeaders(r),
	})
}

func (h *VODHandler) Status(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "sessionID")

	sess := h.vodService.GetSession(channelID)
	if sess == nil {
		respondError(w, http.StatusNotFound, "session not found")
		return
	}

	buffered := h.vodService.GetBufferedSecs(channelID)
	done := h.vodService.IsDone(channelID)

	errMsg := ""
	if procErr := h.vodService.GetError(channelID); procErr != nil {
		errMsg = procErr.Error()
	}

	video, audioTracks, duration := h.vodService.GetProbeInfo(channelID)

	resp := map[string]any{
		"buffered":   buffered,
		"ready":      done,
		"error":      errMsg,
		"recording":  h.vodService.IsRecording(channelID),
		"profile":    sess.ProfileName,
		"stream_url": sess.StreamURL,
	}
	if video != nil {
		resp["video"] = video
	}
	if len(audioTracks) > 0 {
		resp["audio_tracks"] = audioTracks
	}
	if duration > 0 {
		resp["duration"] = duration
	}
	respondJSON(w, http.StatusOK, resp)
}

func (h *VODHandler) Stream(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "sessionID")

	sess := h.vodService.GetSession(channelID)
	if sess == nil {
		respondError(w, http.StatusNotFound, "session not found")
		return
	}

	reader, err := h.vodService.TailSession(r.Context(), channelID)
	if err != nil {
		h.log.Error().Err(err).Str("channel_id", channelID).Msg("stream failed")
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer reader.Close()

	setStreamHeaders(w)
	streamResponse(w, reader)
}

func (h *VODHandler) DeleteSession(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "sessionID")
	consumerID := r.URL.Query().Get("consumer_id")
	user := requireUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	h.vodService.StopWatching(channelID, consumerID)
	w.WriteHeader(http.StatusNoContent)
}

type recordRequest struct {
	ProgramTitle string `json:"program_title"`
	ChannelName  string `json:"channel_name"`
	StopAt       string `json:"stop_at"`
}

func (h *VODHandler) StartRecording(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelID")
	user := requireUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var req recordRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	stopAt, err := parseStopAt(req.StopAt)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid stop_at time format")
		return
	}

	if err := h.vodService.StartRecording(r.Context(), channelID, req.ProgramTitle, req.ChannelName, user.UserID, stopAt); err != nil {
		if errors.Is(err, service.ErrAlreadyRecording) {
			respondError(w, http.StatusConflict, err.Error())
			return
		}
		h.log.Error().Err(err).Str("channel_id", channelID).Msg("start recording failed")
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"status": "recording"})
}

func (h *VODHandler) StopRecording(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelID")
	user := requireUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	if err := h.vodService.StopRecording(channelID); err != nil {
		h.log.Error().Err(err).Str("channel_id", channelID).Msg("stop recording failed")
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *VODHandler) ListCompletedRecordings(w http.ResponseWriter, r *http.Request) {
	user := requireUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	list, err := h.vodService.ListCompletedRecordings(user.UserID, user.IsAdmin)
	if err != nil {
		h.log.Error().Err(err).Msg("list completed recordings failed")
		respondError(w, http.StatusInternalServerError, "failed to list recordings")
		return
	}
	respondJSON(w, http.StatusOK, list)
}

func (h *VODHandler) DeleteCompletedRecording(w http.ResponseWriter, r *http.Request) {
	streamID := chi.URLParam(r, "streamID")
	filename := chi.URLParam(r, "filename")
	user := requireUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	if err := h.vodService.DeleteCompletedRecording(streamID, filename, user.UserID, user.IsAdmin); err != nil {
		if errors.Is(err, service.ErrNotAuthorized) {
			respondError(w, http.StatusForbidden, "not authorized")
			return
		}
		h.log.Error().Err(err).Str("stream_id", streamID).Str("filename", filename).Msg("delete recording failed")
		respondError(w, http.StatusInternalServerError, "failed to delete recording")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *VODHandler) StreamRecordingDLNA(w http.ResponseWriter, r *http.Request) {
	streamID := chi.URLParam(r, "streamID")
	filename := chi.URLParam(r, "filename")

	fullPath, err := h.vodService.GetCompletedRecordingPath(streamID, filename, "", true)
	if err != nil {
		respondError(w, http.StatusNotFound, "recording not found")
		return
	}

	profile, _, _ := h.clientService.MatchClient(r.Context(), r)
	if profile == nil {
		http.ServeFile(w, r, fullPath)
		return
	}

	reader, contentType, err := h.vodService.TranscodeFile(r.Context(), fullPath, profile.Name)
	if err != nil {
		h.log.Error().Err(err).Str("filename", filename).Str("profile", profile.Name).Msg("dlna transcode failed")
		http.ServeFile(w, r, fullPath)
		return
	}
	defer reader.Close()

	serveTranscodedOrFile(w, r, reader, contentType)
}

func (h *VODHandler) ProbeCompletedRecording(w http.ResponseWriter, r *http.Request) {
	streamID := chi.URLParam(r, "streamID")
	filename := chi.URLParam(r, "filename")
	user := requireUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	fullPath, err := h.vodService.GetCompletedRecordingPath(streamID, filename, user.UserID, user.IsAdmin)
	if err != nil {
		if errors.Is(err, service.ErrNotAuthorized) {
			respondError(w, http.StatusForbidden, "not authorized")
			return
		}
		respondError(w, http.StatusNotFound, "recording not found")
		return
	}

	result, err := h.vodService.ProbeFile(r.Context(), streamID, fullPath)
	if err != nil {
		h.log.Error().Err(err).Str("filename", filename).Msg("probe completed recording failed")
		respondError(w, http.StatusInternalServerError, "probe failed")
		return
	}

	resp := map[string]any{
		"duration": result.Duration,
	}
	if result.Video != nil {
		resp["video"] = result.Video
	}
	if len(result.AudioTracks) > 0 {
		resp["audio_tracks"] = result.AudioTracks
	}
	respondJSON(w, http.StatusOK, resp)
}

func (h *VODHandler) StreamCompletedRecording(w http.ResponseWriter, r *http.Request) {
	streamID := chi.URLParam(r, "streamID")
	filename := chi.URLParam(r, "filename")
	user := requireUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	fullPath, err := h.vodService.GetCompletedRecordingPath(streamID, filename, user.UserID, user.IsAdmin)
	if err != nil {
		if errors.Is(err, service.ErrNotAuthorized) {
			respondError(w, http.StatusForbidden, "not authorized")
			return
		}
		respondError(w, http.StatusNotFound, "recording not found")
		return
	}

	profileName := r.URL.Query().Get("profile")
	if profileName == "" {
		http.ServeFile(w, r, fullPath)
		return
	}

	reader, contentType, err := h.vodService.TranscodeFile(r.Context(), fullPath, profileName)
	if err != nil {
		h.log.Error().Err(err).Str("filename", filename).Str("profile", profileName).Msg("transcode failed")
		respondError(w, http.StatusInternalServerError, "transcode failed")
		return
	}
	defer reader.Close()

	serveTranscodedOrFile(w, r, reader, contentType)
}

func (h *VODHandler) DASHManifest(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "sessionID")

	if h.dashManager == nil {
		respondError(w, http.StatusNotImplemented, "dash not available")
		return
	}

	sess := h.vodService.GetSession(channelID)
	if sess == nil {
		respondError(w, http.StatusNotFound, "session not found")
		return
	}

	dashDir := filepath.Join(os.TempDir(), "tvproxy-dash", channelID)
	remuxer, err := h.dashManager.GetOrStart(context.Background(), channelID, sess.FilePath, dashDir)
	if err != nil {
		h.log.Error().Err(err).Str("channel_id", channelID).Msg("failed to start dash remuxer")
		respondError(w, http.StatusInternalServerError, "dash remuxer failed")
		return
	}

	waitCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := remuxer.WaitReady(waitCtx); err != nil {
		h.log.Error().Err(err).Str("channel_id", channelID).Msg("dash manifest not ready")
		respondError(w, http.StatusServiceUnavailable, "dash not ready")
		return
	}

	data, err := os.ReadFile(remuxer.ManifestPath())
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, "manifest not readable")
		return
	}
	w.Header().Set("Content-Type", "application/dash+xml")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write(data)
}

func (h *VODHandler) DASHSegment(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "sessionID")
	segment := chi.URLParam(r, "segment")

	if strings.Contains(segment, "/") || strings.Contains(segment, "..") {
		respondError(w, http.StatusBadRequest, "invalid segment name")
		return
	}

	sess := h.vodService.GetSession(channelID)
	if sess == nil {
		respondError(w, http.StatusNotFound, "session not found")
		return
	}

	segPath := filepath.Join(os.TempDir(), "tvproxy-dash", channelID, segment)
	if _, err := os.Stat(segPath); err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	http.ServeFile(w, r, segPath)
}
