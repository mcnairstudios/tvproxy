package handler

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/middleware"
	"github.com/gavinmcnair/tvproxy/pkg/service"
)

type VODHandler struct {
	vodService *service.VODService
	log        zerolog.Logger
}

func NewVODHandler(vodService *service.VODService, log zerolog.Logger) *VODHandler {
	return &VODHandler{
		vodService: vodService,
		log:        log.With().Str("handler", "vod").Logger(),
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

func parseAudioIndex(r *http.Request) int {
	raw := r.URL.Query().Get("audio")
	if raw == "" {
		return 0
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 {
		return 0
	}
	return v
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

func (h *VODHandler) ProbeStream(w http.ResponseWriter, r *http.Request) {
	streamID := chi.URLParam(r, "streamID")

	result, err := h.vodService.ProbeStream(r.Context(), streamID)
	if err != nil {
		h.log.Error().Err(err).Str("stream_id", streamID).Msg("probe failed")
		respondError(w, http.StatusNotFound, "stream not found")
		return
	}

	if result.IsVOD {
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"type":     "vod",
			"duration": result.Duration,
			"width":    result.Width,
			"height":   result.Height,
		})
	} else {
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"type": "live",
		})
	}
}

func (h *VODHandler) CreateSession(w http.ResponseWriter, r *http.Request) {
	streamID := chi.URLParam(r, "streamID")

	profileName := r.URL.Query().Get("profile")
	audioIndex := parseAudioIndex(r)

	session, err := h.vodService.CreateSession(r.Context(), streamID, profileName, audioIndex, r.UserAgent(), r.RemoteAddr)
	if err != nil {
		h.log.Error().Err(err).Str("stream_id", streamID).Msg("create VOD session failed")
		if errors.Is(err, service.ErrStreamNotFound) {
			respondError(w, http.StatusNotFound, err.Error())
			return
		}
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"session_id": session.ID,
		"duration":   h.vodService.GetDuration(session.ID),
	})
}

func (h *VODHandler) CreateChannelSession(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelID")

	profileName := r.URL.Query().Get("profile")
	audioIndex := parseAudioIndex(r)

	session, err := h.vodService.CreateSessionForChannel(r.Context(), channelID, profileName, audioIndex, r.UserAgent(), r.RemoteAddr)
	if err != nil {
		h.log.Error().Err(err).Str("channel_id", channelID).Msg("create channel VOD session failed")
		if errors.Is(err, service.ErrStreamNotFound) {
			respondError(w, http.StatusNotFound, err.Error())
			return
		}
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"session_id": session.ID,
		"duration":   h.vodService.GetDuration(session.ID),
	})
}

func (h *VODHandler) Status(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")

	if _, ok := h.vodService.GetSession(sessionID); !ok {
		respondError(w, http.StatusNotFound, "session not found")
		return
	}

	buffered := h.vodService.GetBufferedSecs(sessionID)
	ready := h.vodService.IsProcessReady(sessionID)

	errMsg := ""
	if procErr := h.vodService.GetProcessError(sessionID); procErr != nil {
		errMsg = procErr.Error()
	}

	segments := h.vodService.GetSegments(sessionID)
	if segments == nil {
		segments = []service.SegmentInfo{}
	}

	resp := map[string]interface{}{
		"buffered":    buffered,
		"duration":    h.vodService.GetDuration(sessionID),
		"ready":       ready,
		"error":       errMsg,
		"recording":   h.vodService.IsRecording(sessionID),
		"segments":    segments,
		"audio_index": h.vodService.GetAudioIndex(sessionID),
		"profile":     h.vodService.GetProfileName(sessionID),
	}
	if vi := h.vodService.GetVideoInfo(sessionID); vi != nil {
		resp["video"] = vi
	}
	if tracks := h.vodService.GetAudioTracks(sessionID); len(tracks) > 0 {
		resp["audio_tracks"] = tracks
	}
	respondJSON(w, http.StatusOK, resp)
}

func (h *VODHandler) Seek(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")

	session, ok := h.vodService.GetSession(sessionID)
	if !ok {
		respondError(w, http.StatusNotFound, "session not found")
		return
	}

	tStr := r.URL.Query().Get("t")
	offset, err := strconv.ParseFloat(tStr, 64)
	if err != nil || offset < 0 {
		respondError(w, http.StatusBadRequest, "invalid time offset")
		return
	}

	reader, err := h.vodService.StreamSeek(r.Context(), session, offset)
	if err != nil {
		h.log.Error().Err(err).Str("session_id", sessionID).Float64("offset", offset).Msg("seek failed")
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer reader.Close()

	setStreamHeaders(w)
	streamResponse(w, reader)
}

func (h *VODHandler) Stream(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")

	session, ok := h.vodService.GetSession(sessionID)
	if !ok {
		respondError(w, http.StatusNotFound, "session not found")
		return
	}

	reader, err := h.vodService.StreamFile(r.Context(), session)
	if err != nil {
		h.log.Error().Err(err).Str("session_id", sessionID).Msg("stream failed")
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer reader.Close()

	setStreamHeaders(w)
	streamResponse(w, reader)
}

func (h *VODHandler) DeleteSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	user := requireUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	h.vodService.DeleteSession(sessionID)
	w.WriteHeader(http.StatusNoContent)
}

type recordRequest struct {
	ProgramTitle string  `json:"program_title"`
	ChannelName  string  `json:"channel_name"`
	StopAt       string  `json:"stop_at"`
	StartOffset  float64 `json:"start_offset"`
	EndOffset    float64 `json:"end_offset"`
}

func (h *VODHandler) MarkRecording(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
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

	seg, err := h.vodService.CreateSegment(sessionID, req.ProgramTitle, req.ChannelName, user.UserID, req.StartOffset, req.EndOffset, stopAt)
	if err != nil {
		if errors.Is(err, service.ErrSessionNotFound) {
			respondError(w, http.StatusNotFound, err.Error())
			return
		}
		if errors.Is(err, service.ErrActiveSegmentExists) {
			h.log.Warn().Str("session_id", sessionID).Msg("segment creation rejected: active segment exists")
			respondError(w, http.StatusConflict, err.Error())
			return
		}
		h.log.Error().Err(err).Str("session_id", sessionID).Msg("create segment failed")
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	resp := map[string]interface{}{"status": "recording"}
	if seg != nil {
		segResp := map[string]interface{}{
			"id":           seg.ID,
			"start_offset": seg.StartOffset,
			"status":       string(seg.Status),
		}
		if seg.EndOffset != nil {
			segResp["end_offset"] = *seg.EndOffset
		}
		resp["segment"] = segResp
	}
	respondJSON(w, http.StatusOK, resp)
}

func (h *VODHandler) CreateRecording(w http.ResponseWriter, r *http.Request) {
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

	session, seg, err := h.vodService.CreateRecordingSession(r.Context(), channelID, req.ProgramTitle, req.ChannelName, user.UserID, stopAt)
	if err != nil {
		h.log.Error().Err(err).Str("channel_id", channelID).Msg("create recording failed")
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	resp := map[string]interface{}{
		"session_id": session.ID,
		"status":     "recording",
	}
	if seg != nil {
		resp["segment"] = map[string]interface{}{
			"id":           seg.ID,
			"start_offset": seg.StartOffset,
			"status":       string(seg.Status),
		}
	}
	respondJSON(w, http.StatusOK, resp)
}

func (h *VODHandler) ListRecordings(w http.ResponseWriter, r *http.Request) {
	user := requireUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	list := h.vodService.ListRecordings(user.UserID, user.IsAdmin)
	if list == nil {
		list = []service.RecordingInfo{}
	}
	respondJSON(w, http.StatusOK, list)
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
	filename := chi.URLParam(r, "filename")
	user := requireUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	targetUserID := user.UserID
	if user.IsAdmin && r.URL.Query().Has("user_id") {
		targetUserID = r.URL.Query().Get("user_id")
	}

	if err := h.vodService.DeleteCompletedRecording(filename, targetUserID); err != nil {
		if errors.Is(err, service.ErrInvalidFilename) {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		if errors.Is(err, service.ErrFileNotFound) {
			respondError(w, http.StatusNotFound, err.Error())
			return
		}
		h.log.Error().Err(err).Str("filename", filename).Msg("delete recording failed")
		respondError(w, http.StatusInternalServerError, "failed to delete recording")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *VODHandler) StopRecording(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	user := requireUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	extract := r.URL.Query().Get("extract") == "true"
	var err error
	if extract {
		err = h.vodService.CloseAndExtract(sessionID, user.UserID, user.IsAdmin)
	} else {
		err = h.vodService.CloseSegment(sessionID, user.UserID, user.IsAdmin)
	}
	if err != nil {
		if errors.Is(err, service.ErrSessionNotFound) {
			respondError(w, http.StatusNotFound, err.Error())
			return
		}
		if errors.Is(err, service.ErrNotAuthorized) {
			h.log.Warn().Str("session_id", sessionID).Str("user_id", user.UserID).Msg("unauthorized stop recording attempt")
			respondError(w, http.StatusForbidden, err.Error())
			return
		}
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *VODHandler) CancelRecording(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	user := requireUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if err := h.vodService.CancelSegment(sessionID, user.UserID, user.IsAdmin); err != nil {
		if errors.Is(err, service.ErrSessionNotFound) {
			respondError(w, http.StatusNotFound, err.Error())
			return
		}
		if errors.Is(err, service.ErrNotAuthorized) {
			h.log.Warn().Str("session_id", sessionID).Str("user_id", user.UserID).Msg("unauthorized cancel recording attempt")
			respondError(w, http.StatusForbidden, err.Error())
			return
		}
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *VODHandler) UpdateSegment(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	segmentID := chi.URLParam(r, "segmentID")
	user := requireUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var req struct {
		StartOffset *float64 `json:"start_offset"`
		EndOffset   *float64 `json:"end_offset"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.vodService.UpdateSegment(sessionID, segmentID, user.UserID, user.IsAdmin, req.StartOffset, req.EndOffset); err != nil {
		if errors.Is(err, service.ErrSessionNotFound) || errors.Is(err, service.ErrSegmentNotFound) {
			respondError(w, http.StatusNotFound, err.Error())
			return
		}
		if errors.Is(err, service.ErrNotAuthorized) {
			h.log.Warn().Str("session_id", sessionID).Str("segment_id", segmentID).Str("user_id", user.UserID).Msg("unauthorized update segment attempt")
			respondError(w, http.StatusForbidden, err.Error())
			return
		}
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *VODHandler) DeleteSegment(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	segmentID := chi.URLParam(r, "segmentID")
	user := requireUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	if err := h.vodService.DeleteSegment(sessionID, segmentID, user.UserID, user.IsAdmin); err != nil {
		if errors.Is(err, service.ErrSessionNotFound) || errors.Is(err, service.ErrSegmentNotFound) {
			respondError(w, http.StatusNotFound, err.Error())
			return
		}
		if errors.Is(err, service.ErrNotAuthorized) {
			h.log.Warn().Str("session_id", sessionID).Str("segment_id", segmentID).Str("user_id", user.UserID).Msg("unauthorized delete segment attempt")
			respondError(w, http.StatusForbidden, err.Error())
			return
		}
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *VODHandler) StreamCompletedRecording(w http.ResponseWriter, r *http.Request) {
	filename := chi.URLParam(r, "filename")
	user := requireUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	targetUserID := user.UserID
	if user.IsAdmin && r.URL.Query().Has("user_id") {
		targetUserID = r.URL.Query().Get("user_id")
	}

	fullPath, err := h.vodService.GetCompletedRecordingPath(filename, targetUserID)
	if err != nil {
		if errors.Is(err, service.ErrInvalidFilename) {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondError(w, http.StatusNotFound, err.Error())
		return
	}

	http.ServeFile(w, r, fullPath)
}
