package handler

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/middleware"
	"github.com/gavinmcnair/tvproxy/pkg/service"
	"github.com/gavinmcnair/tvproxy/pkg/session"
)

type VODHandler struct {
	vodService    *service.VODService
	clientService *service.ClientService
	log           zerolog.Logger
}

func NewVODHandler(vodService *service.VODService, clientService *service.ClientService, log zerolog.Logger) *VODHandler {
	return &VODHandler{
		vodService:    vodService,
		clientService: clientService,
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

	resp := map[string]any{
		"duration": result.Duration,
		"width":    result.Width,
		"height":   result.Height,
	}
	if result.IsVOD {
		resp["type"] = "vod"
	} else {
		resp["type"] = "live"
	}
	if result.Video != nil {
		resp["video"] = result.Video
	}
	if len(result.AudioTracks) > 0 {
		resp["audio_tracks"] = result.AudioTracks
	}
	if result.FormatName != "" {
		resp["format"] = result.FormatName
	}
	respondJSON(w, http.StatusOK, resp)
}

func (h *VODHandler) DeleteProbe(w http.ResponseWriter, r *http.Request) {
	streamID := chi.URLParam(r, "streamID")
	if err := h.vodService.DeleteProbe(r.Context(), streamID); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
		if sess.Delivery == "mse" {
			delivery = "mse"
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
		if sess.Delivery == "mse" {
			delivery = "mse"
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

	if sess.Duration > 0 {
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
		return
	}

	reader, err := h.vodService.TailSession(r.Context(), channelID)
	if err != nil {
		respondError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "close")
	w.WriteHeader(http.StatusOK)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	io.Copy(w, reader)
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

	delivery := "stream"
	if sess := h.vodService.GetSession(sessionID); sess != nil && sess.Delivery == "mse" {
		delivery = "mse"
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"session_id":  sessionID,
		"consumer_id": consumerID,
		"container":   container,
		"duration":    duration,
		"audio_only":  audioOnly,
		"delivery":    delivery,
	})
}

func (h *VODHandler) acquireWatcher(channelID string, timeout time.Duration) (*session.Watcher, string) {
	deadline := time.Now().Add(timeout)
	sessionSeen := false
	for time.Now().Before(deadline) {
		sess := h.vodService.GetSession(channelID)
		if sess == nil {
			if sessionSeen {
				return nil, "session ended"
			}
			return nil, "session not found"
		}
		sessionSeen = true
		if sess.Delivery != "mse" {
			return nil, "session delivery is not MSE"
		}
		if sess.SessionWatcher != nil {
			return sess.SessionWatcher, ""
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil, "pipeline not ready (timeout)"
}

func (h *VODHandler) MSEInit(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "sessionID")
	track := chi.URLParam(r, "track")

	watcher, reason := h.acquireWatcher(channelID, 30*time.Second)
	if watcher == nil {
		code := http.StatusServiceUnavailable
		if reason == "session not found" {
			code = http.StatusNotFound
		}
		respondError(w, code, reason)
		return
	}

	var data []byte
	switch track {
	case "video":
		data = watcher.VideoInit()
	case "audio":
		data = watcher.AudioInit()
	default:
		respondError(w, http.StatusBadRequest, "invalid track: "+track)
		return
	}

	if data == nil {
		w.Header().Set("Cache-Control", "no-store")
		respondError(w, http.StatusServiceUnavailable, "init segment not yet available")
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Gen", strconv.FormatInt(watcher.Generation(), 10))
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Expose-Headers", "X-Gen")
	w.Write(data)
}

func (h *VODHandler) MSESegment(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "sessionID")
	track := chi.URLParam(r, "track")

	genStr := r.URL.Query().Get("gen")
	seqStr := r.URL.Query().Get("seq")
	gen, _ := strconv.ParseInt(genStr, 10, 64)
	seq, _ := strconv.Atoi(seqStr)

	watcher, reason := h.acquireWatcher(channelID, 10*time.Second)
	if watcher == nil {
		code := http.StatusServiceUnavailable
		if reason == "session not found" {
			code = http.StatusNotFound
		}
		respondError(w, code, reason)
		return
	}

	if gen != watcher.Generation() {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusGone)
		return
	}

	if track != "video" && track != "audio" {
		respondError(w, http.StatusBadRequest, "invalid track: "+track)
		return
	}

	var data []byte
	var ok bool
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if track == "video" {
			data, ok = watcher.VideoSegment(seq)
		} else {
			data, ok = watcher.AudioSegment(seq)
		}
		if ok {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !ok {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Cache-Control", "no-store")
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
	if sess != nil && sess.SessionWatcher != nil {
		gen = sess.SessionWatcher.Generation()
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
		"duration":    sess.Duration,
		"video_codec": sess.OutputVideoCodec,
	}
	if sess.SessionWatcher != nil {
		w := sess.SessionWatcher
		resp["gen"] = w.Generation()
		resp["video_segments"] = w.VideoSegmentCount()
		resp["audio_segments"] = w.AudioSegmentCount()
		resp["probe_ready"] = w.Probe() != nil
		if probe := w.Probe(); probe != nil {
			if probe.AudioOutputCodec != "" {
				resp["audio_codec"] = "mp4a.40.2"
			}
			probeData := map[string]any{
				"video_codec":        probe.VideoCodec,
				"video_codec_string": probe.VideoCodecString,
				"video_width":        probe.VideoWidth,
				"video_height":       probe.VideoHeight,
				"audio_source_codec": probe.AudioSourceCodec,
				"audio_output_codec": probe.AudioOutputCodec,
			}
			if probe.VideoBitDepth > 0 {
				probeData["video_bit_depth"] = probe.VideoBitDepth
			}
			if probe.VideoFramerateNum > 0 {
				probeData["video_framerate_num"] = probe.VideoFramerateNum
				probeData["video_framerate_den"] = probe.VideoFramerateDen
			}
			if probe.VideoBitrateKbps > 0 {
				probeData["video_bitrate_kbps"] = probe.VideoBitrateKbps
			}
			resp["probe"] = probeData
		}
	}
	if sess.Video != nil {
		resp["source_video"] = sess.Video
	}
	if len(sess.AudioTracks) > 0 {
		resp["source_audio"] = sess.AudioTracks[0]
	}
	if errMsg := sess.GetError(); errMsg != "" {
		resp["error"] = errMsg
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
  let seq = 1;
  while (!signal.aborted) {
    try {
      const resp = await fetch('/vod/' + sessionId + '/mse/' + name + '/segment?gen=' + gen + '&seq=' + seq, {signal});
      if (resp.status === 410) {
        self.postMessage({type: 'genChanged', track: name});
        return;
      }
      if (resp.status === 404) {
        await new Promise(r => setTimeout(r, 500));
        continue;
      }
      if (!resp.ok) {
        throw new Error('HTTP ' + resp.status);
      }
      const startTime = parseFloat(resp.headers.get('X-Start-Time') || '-1');
      const data = await resp.arrayBuffer();
      self.postMessage({type: 'segment', track: name, data: data, seq: seq, startTime: startTime}, [data]);
      seq++;
    } catch(e) {
      if (e.name === 'AbortError') return;
      self.postMessage({type: 'error', track: name, msg: e.toString()});
      await new Promise(r => setTimeout(r, 1000));
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
    if (!msg.audioDisabled) fetchTrack(msg.sessionId, 'audio', msg.audioGen, signal);
  } else if (msg.type === 'stop') {
    if (ac) ac.abort();
    ac = null;
  }
};
`
