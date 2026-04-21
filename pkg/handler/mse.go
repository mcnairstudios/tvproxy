package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/gavinmcnair/tvproxy/pkg/session"
)

type MSEHandler struct {
	getWatcher func(sessionID string) *session.Watcher
}

func NewMSEHandler(getWatcher func(sessionID string) *session.Watcher) *MSEHandler {
	return &MSEHandler{getWatcher: getWatcher}
}

func (h *MSEHandler) Probe(w http.ResponseWriter, r *http.Request) {
	watcher := h.resolveWatcher(w, r)
	if watcher == nil {
		return
	}

	probe := watcher.Probe()
	if probe == nil {
		respondError(w, http.StatusServiceUnavailable, "probe not yet available")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	resp := map[string]any{
		"video_codec":              probe.VideoCodec,
		"video_codec_string":       probe.VideoCodecString,
		"video_width":              probe.VideoWidth,
		"video_height":             probe.VideoHeight,
		"video_interlaced":         probe.VideoInterlaced,
		"audio_source_codec":       probe.AudioSourceCodec,
		"audio_source_channels":    probe.AudioSourceChannels,
		"audio_source_sample_rate": probe.AudioSourceSampleRate,
		"audio_output_codec":       probe.AudioOutputCodec,
		"audio_output_channels":    probe.AudioOutputChannels,
		"audio_output_sample_rate": probe.AudioOutputSampleRate,
	}
	if probe.VideoBitDepth > 0 {
		resp["video_bit_depth"] = probe.VideoBitDepth
	}
	if probe.VideoFramerateNum > 0 {
		resp["video_framerate_num"] = probe.VideoFramerateNum
		resp["video_framerate_den"] = probe.VideoFramerateDen
	}
	if probe.VideoBitrateKbps > 0 {
		resp["video_bitrate_kbps"] = probe.VideoBitrateKbps
	}
	json.NewEncoder(w).Encode(resp)
}

func (h *MSEHandler) Init(w http.ResponseWriter, r *http.Request) {
	watcher := h.resolveWatcher(w, r)
	if watcher == nil {
		return
	}
	track := chi.URLParam(r, "track")

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

func (h *MSEHandler) Segment(w http.ResponseWriter, r *http.Request) {
	watcher := h.resolveWatcher(w, r)
	if watcher == nil {
		return
	}
	track := chi.URLParam(r, "track")
	genStr := r.URL.Query().Get("gen")
	seqStr := r.URL.Query().Get("seq")

	gen, _ := strconv.ParseInt(genStr, 10, 64)
	seq, _ := strconv.Atoi(seqStr)

	if gen != watcher.Generation() {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusGone)
		return
	}

	var data []byte
	var ok bool
	switch track {
	case "video":
		data, ok = watcher.VideoSegment(seq)
	case "audio":
		data, ok = watcher.AudioSegment(seq)
	default:
		respondError(w, http.StatusBadRequest, "invalid track: "+track)
		return
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

func (h *MSEHandler) Status(w http.ResponseWriter, r *http.Request) {
	watcher := h.resolveWatcher(w, r)
	if watcher == nil {
		return
	}

	sig := watcher.Signal()
	sigData := map[string]any{}
	if sig != nil {
		sigData["strength"] = sig.Strength
		sigData["quality"] = sig.Quality
		sigData["snr"] = sig.Snr
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"gen":            watcher.Generation(),
		"video_segments": watcher.VideoSegmentCount(),
		"audio_segments": watcher.AudioSegmentCount(),
		"probe_ready":    watcher.Probe() != nil,
		"video_init":     watcher.VideoInit() != nil,
		"audio_init":     watcher.AudioInit() != nil,
		"signal":         sigData,
	})
}

func (h *MSEHandler) resolveWatcher(w http.ResponseWriter, r *http.Request) *session.Watcher {
	sessionID := chi.URLParam(r, "sessionID")
	watcher := h.getWatcher(sessionID)
	if watcher == nil {
		respondError(w, http.StatusNotFound, fmt.Sprintf("session %s not found", sessionID))
		return nil
	}
	return watcher
}
