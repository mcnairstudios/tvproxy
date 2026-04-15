package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/fmp4"
	"github.com/gavinmcnair/tvproxy/pkg/hls"
	"github.com/gavinmcnair/tvproxy/pkg/middleware"
	"github.com/gavinmcnair/tvproxy/pkg/service"
)

type VODHandler struct {
	vodService    *service.VODService
	clientService *service.ClientService
	hlsManager    *hls.Manager
	log           zerolog.Logger
}

func NewVODHandler(vodService *service.VODService, clientService *service.ClientService, hlsManager *hls.Manager, log zerolog.Logger) *VODHandler {
	return &VODHandler{
		vodService:    vodService,
		clientService: clientService,
		hlsManager:    hlsManager,
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

func setStreamHeaders(w http.ResponseWriter, videoCodec string) {
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

	delivery := "stream"
	if sess := h.vodService.GetSession(sessionID); sess != nil {
		if sess.ProfileName == "Browser" {
			delivery = "mse"
		} else if sess.HLSOutputDir != "" {
			delivery = "hls"
		}
	}

	resp := map[string]any{
		"session_id":      sessionID,
		"consumer_id":     consumerID,
		"channel_id":      streamID,
		"container":       container,
		"delivery":        delivery,
		"request_headers": clientHeaders(r),
	}
	_, _, duration := h.vodService.GetProbeInfo(sessionID)
	if duration < 1 {
		if sess := h.vodService.GetSession(sessionID); sess != nil && sess.Duration > 0 {
			duration = sess.Duration
		}
	}
	if duration > 0 {
		resp["duration"] = duration
	}
	respondJSON(w, http.StatusOK, resp)
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

	delivery := "stream"
	if sess := h.vodService.GetSession(sessionID); sess != nil {
		if sess.ProfileName == "Browser" {
			delivery = "mse"
		} else if sess.HLSOutputDir != "" {
			delivery = "hls"
		}
	}

	resp := map[string]any{
		"session_id":      sessionID,
		"consumer_id":     consumerID,
		"channel_id":      channelID,
		"container":       container,
		"delivery":        delivery,
		"audio_only":      audioOnly,
		"request_headers": clientHeaders(r),
	}
	_, _, duration := h.vodService.GetProbeInfo(sessionID)
	if duration >= 30 {
		resp["duration"] = duration
	}
	respondJSON(w, http.StatusOK, resp)
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

	fileSize := h.vodService.GetFileSize(channelID)
	var bitrateKbps int64
	if buffered > 1 && fileSize > 0 {
		bitrateKbps = fileSize * 8 / int64(buffered) / 1000
	}

	resp := map[string]any{
		"buffered":           buffered,
		"ready":              done,
		"error":              errMsg,
		"recording":          h.vodService.IsRecording(channelID),
		"profile":            sess.ProfileName,
		"stream_url":         sess.StreamURL,
		"output_video_codec": sess.OutputVideoCodec,
		"output_audio_codec": sess.OutputAudioCodec,
		"output_container":   sess.OutputContainer,
		"output_hwaccel":     sess.OutputHWAccel,
		"file_size":          fileSize,
		"bitrate_kbps":       bitrateKbps,
	}
	if video != nil {
		resp["video"] = video
	}
	if len(audioTracks) > 0 {
		resp["audio_tracks"] = audioTracks
	}
	if duration < 30 && sess.Duration >= 30 {
		duration = sess.Duration
	}
	if duration >= 30 {
		resp["duration"] = duration
	}
	if sess.SeekOffset > 0 {
		resp["seek_offset"] = sess.SeekOffset
	}
	respondJSON(w, http.StatusOK, resp)
}

func (h *VODHandler) Seek(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "sessionID")
	posStr := r.URL.Query().Get("position")
	if posStr == "" {
		respondError(w, http.StatusBadRequest, "position required")
		return
	}
	var position float64
	if _, err := fmt.Sscanf(posStr, "%f", &position); err != nil || position < 0 {
		respondError(w, http.StatusBadRequest, "invalid position")
		return
	}

	if err := h.vodService.SeekSession(r.Context(), channelID, position); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"position": position})
}

func (h *VODHandler) Stream(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "sessionID")

	sess := h.vodService.GetSession(channelID)
	if sess == nil {
		respondError(w, http.StatusNotFound, "session not found")
		return
	}

	filePath := sess.FilePath
	for i := 0; i < 50; i++ {
		info, err := os.Stat(filePath)
		if err == nil && info.Size() > 0 {
			break
		}
		if h.vodService.IsDone(channelID) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	http.ServeFile(w, r, filePath)
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

	http.ServeFile(w, r, fullPath)
}

func (h *VODHandler) PlayCompletedRecording(w http.ResponseWriter, r *http.Request) {
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
	title := filename
	if profileName == "" {
		profileName = "Browser"
	}

	sessionID, consumerID, container, duration, audioOnly, err := h.vodService.StartWatchingFile(r.Context(), fullPath, title, profileName, r.UserAgent(), r.RemoteAddr)
	if err != nil {
		h.log.Error().Err(err).Str("filename", filename).Msg("failed to create file session")
		respondError(w, http.StatusInternalServerError, "session creation failed")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"session_id":  sessionID,
		"consumer_id": consumerID,
		"container":   container,
		"duration":    duration,
		"audio_only":  audioOnly,
	})
}

func (h *VODHandler) HLSMaster(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "sessionID")
	sess := h.vodService.GetSession(channelID)
	if sess == nil {
		respondError(w, http.StatusNotFound, "session not found")
		return
	}

	if sess.HLSOutputDir != "" {
		for i := 0; i < 30; i++ {
			if data, err := os.ReadFile(filepath.Join(sess.HLSOutputDir, "playlist.m3u8")); err == nil && len(data) > 50 {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		w.Header().Set("Content-Type", "application/x-mpegURL")
		w.Header().Set("Cache-Control", "no-cache, no-store")
		fmt.Fprintln(w, "#EXTM3U")
		fmt.Fprintf(w, "#EXT-X-STREAM-INF:BANDWIDTH=10000000\n")
		fmt.Fprintf(w, "/vod/%s/hls/playlist.m3u8\n", channelID)
		return
	}

	var duration float64
	for i := 0; i < 20; i++ {
		_, _, d := h.vodService.GetProbeInfo(channelID)
		if d > 0 {
			duration = d
			break
		}
		if sess.Duration > 0 {
			duration = sess.Duration
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	var durationTicks int64
	if duration > 0 {
		durationTicks = int64(duration * 10000000)
	}

	streamURL := sess.StreamURL
	if streamURL == "" {
		respondError(w, http.StatusInternalServerError, "no stream URL")
		return
	}

	profile := hls.ProfileSettings{
		VideoCodec:   sess.OutputVideoCodec,
		AudioCodec:   sess.OutputAudioCodec,
		HWAccel:      sess.OutputHWAccel,
		Container:    sess.OutputContainer,
		UseWireGuard: sess.UseWireGuard,
	}
	if profile.VideoCodec == "" {
		profile.VideoCodec = "copy"
	}
	if profile.AudioCodec == "" {
		profile.AudioCodec = "aac"
	}

	hlsSess := h.hlsManager.GetOrCreateSession(channelID, streamURL, 6, durationTicks, duration == 0, profile)
	playlistURL := fmt.Sprintf("/vod/%s/hls/playlist.m3u8", channelID)
	hls.ServeMasterPlaylist(w, hlsSess, playlistURL)
}

func (h *VODHandler) HLSPlaylist(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "sessionID")

	sess := h.vodService.GetSession(channelID)
	if sess != nil && sess.HLSOutputDir != "" {
		playlistPath := filepath.Join(sess.HLSOutputDir, "playlist.m3u8")
		var data []byte
		var err error
		for i := 0; i < 30; i++ {
			data, err = os.ReadFile(playlistPath)
			if err == nil && len(data) > 20 {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		if err != nil || len(data) < 20 {
			respondError(w, http.StatusNotFound, "playlist not ready")
			return
		}
		w.Header().Set("Content-Type", "application/x-mpegURL")
		w.Header().Set("Cache-Control", "no-cache, no-store")
		w.Write(data)
		return
	}

	hlsSess := h.hlsManager.GetSession(channelID)
	if hlsSess == nil {
		respondError(w, http.StatusNotFound, "hls session not found")
		return
	}

	if hlsSess.IsDone() && hlsSess.CurrentTranscodeIndex() == -1 {
		if err := hlsSess.StartTranscode(context.Background(), 0, 0); err != nil {
			h.log.Error().Err(err).Str("session", channelID).Msg("failed to start hls transcode")
		}
	}

	hls.ServeMediaPlaylist(w, hlsSess, "")
}

func (h *VODHandler) HLSSegment(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "sessionID")
	segmentFile := chi.URLParam(r, "segment")

	sess := h.vodService.GetSession(channelID)
	if sess != nil && sess.HLSOutputDir != "" {
		segPath := filepath.Join(sess.HLSOutputDir, segmentFile)
		for i := 0; i < 50; i++ {
			if info, err := os.Stat(segPath); err == nil && info.Size() > 0 {
				hls.ServeSegment(w, r, segPath)
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
		respondError(w, http.StatusNotFound, "segment not available")
		return
	}

	hlsSess := h.hlsManager.GetSession(channelID)
	if hlsSess == nil {
		respondError(w, http.StatusNotFound, "hls session not found")
		return
	}

	if segmentFile == "init.mp4" {
		if err := h.hlsManager.RequestSegment(context.Background(), hlsSess, 0, 0); err != nil {
			respondError(w, http.StatusNotFound, "init segment not available")
			return
		}
		hls.ServeSegment(w, r, hlsSess.InitSegmentPath())
		return
	}

	var segmentIndex int
	name := segmentFile
	name = strings.TrimSuffix(name, ".mp4")
	name = strings.TrimSuffix(name, ".ts")
	fmt.Sscanf(name, "seg%d", &segmentIndex)

	var runtimeTicks int64
	if rt := r.URL.Query().Get("runtimeTicks"); rt != "" {
		fmt.Sscanf(rt, "%d", &runtimeTicks)
	}

	if err := h.hlsManager.RequestSegment(context.Background(), hlsSess, segmentIndex, runtimeTicks); err != nil {
		h.log.Error().Err(err).Int("segment", segmentIndex).Msg("hls segment not available")
		respondError(w, http.StatusNotFound, "segment not available")
		return
	}

	hls.ServeSegment(w, r, hlsSess.SegmentPath(segmentIndex))
}

func (h *VODHandler) MSEInit(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "sessionID")
	track := chi.URLParam(r, "track")

	sess := h.vodService.GetSession(channelID)
	if sess == nil {
		respondError(w, http.StatusNotFound, "session not found")
		return
	}

	var store *fmp4.TrackStore
	if track == "video" {
		store = sess.VideoStore
	} else {
		store = sess.AudioStore
	}
	if store == nil {
		respondError(w, http.StatusNotFound, "no MSE store for track")
		return
	}

	data, gen := store.GetInit()
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("X-Gen", strconv.FormatInt(gen, 10))
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Expose-Headers", "X-Gen")
	w.Write(data)
}

func (h *VODHandler) MSESegment(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "sessionID")
	track := chi.URLParam(r, "track")

	sess := h.vodService.GetSession(channelID)
	if sess == nil {
		respondError(w, http.StatusNotFound, "session not found")
		return
	}

	var store *fmp4.TrackStore
	if track == "video" {
		store = sess.VideoStore
	} else {
		store = sess.AudioStore
	}
	if store == nil {
		respondError(w, http.StatusNotFound, "no MSE store")
		return
	}

	genStr := r.URL.Query().Get("gen")
	seqStr := r.URL.Query().Get("seq")
	gen, _ := strconv.ParseInt(genStr, 10, 64)
	seq, _ := strconv.Atoi(seqStr)

	data, ok := store.GetSegment(gen, seq)
	if !ok {
		w.WriteHeader(http.StatusGone)
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write(data)
}

func (h *VODHandler) MSESeek(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "sessionID")
	posStr := r.URL.Query().Get("position")
	pos, err := strconv.ParseFloat(posStr, 64)
	if err != nil || pos < 0 {
		respondError(w, http.StatusBadRequest, "invalid position")
		return
	}

	if err := h.vodService.SeekSession(r.Context(), channelID, pos); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	sess := h.vodService.GetSession(channelID)
	gen := int64(0)
	if sess != nil && sess.VideoStore != nil {
		gen = sess.VideoStore.Generation()
	}

	respondJSON(w, http.StatusOK, map[string]any{"gen": gen, "pos": pos})
}

func (h *VODHandler) MSEDebug(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "sessionID")
	sess := h.vodService.GetSession(channelID)
	if sess == nil {
		respondError(w, http.StatusNotFound, "session not found")
		return
	}

	resp := map[string]any{
		"duration": sess.Duration,
	}
	if sess.VideoStore != nil {
		resp["video_segments"] = sess.VideoStore.SegmentCount()
		resp["gen"] = sess.VideoStore.Generation()
	}
	respondJSON(w, http.StatusOK, resp)
}

func (h *VODHandler) MSEWorkerJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript")
	w.Header().Set("Cache-Control", "no-cache")
	fmt.Fprint(w, mseWorkerJS)
}

const mseWorkerJS = `
let ac = null;

async function fetchTrack(sessionId, name, gen, signal) {
  let seq = 0;
  let backoff = 0;
  while (!signal.aborted) {
    try {
      const resp = await fetch('/vod/' + sessionId + '/mse/' + name + '/segment?gen=' + gen + '&seq=' + seq, {signal});
      if (resp.status === 410) {
        self.postMessage({type: 'genChanged', track: name});
        return;
      }
      if (!resp.ok) {
        throw new Error('HTTP ' + resp.status);
      }
      const data = await resp.arrayBuffer();
      self.postMessage({type: 'segment', track: name, data: data, seq: seq}, [data]);
      seq++;
      backoff = 0;
    } catch(e) {
      if (e.name === 'AbortError') return;
      const delay = Math.pow(2, backoff) * 1000;
      backoff = Math.min(backoff + 1, 5);
      self.postMessage({type: 'error', track: name, msg: e.toString() + ' (retry in ' + (delay/1000) + 's)'});
      await new Promise(r => setTimeout(r, delay));
    }
  }
}

self.onmessage = function(e) {
  const msg = e.data;
  if (msg.type === 'start' || msg.type === 'seek') {
    if (ac) ac.abort();
    ac = new AbortController();
    const signal = ac.signal;
    fetchTrack(msg.sessionId, 'video', msg.videoGen, signal);
    fetchTrack(msg.sessionId, 'audio', msg.audioGen, signal);
  } else if (msg.type === 'stop') {
    if (ac) ac.abort();
    ac = null;
  }
};
`
