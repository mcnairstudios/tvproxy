package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/gavinmcnair/tvproxy/pkg/ffmpeg"
	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/store"
)

type StreamProfileHandler struct {
	repo store.ProfileStore
	rev  *store.Revision
}

func NewStreamProfileHandler(repo store.ProfileStore) *StreamProfileHandler {
	return &StreamProfileHandler{repo: repo, rev: store.NewRevision()}
}

var (
	validStreamModes = map[string]bool{"direct": true, "proxy": true, "ffmpeg": true}
	validHWAccels    = map[string]bool{"default": true, "none": true, "qsv": true, "nvenc": true, "vaapi": true, "videotoolbox": true}
	validVideoCodecs = map[string]bool{"default": true, "copy": true, "h264": true, "h265": true, "av1": true}
	validContainers  = map[string]bool{"mpegts": true, "matroska": true, "mp4": true, "webm": true}
	validDeliveries  = map[string]bool{"stream": true, "dash": true}
	validAudioCodecs = map[string]bool{"default": true, "copy": true, "aac": true, "opus": true}
	validFPSModes    = map[string]bool{"auto": true, "cfr": true}
)

type profileFields struct {
	StreamMode    string
	HWAccel       string
	VideoCodec    string
	Container     string
	Delivery      string
	AudioCodec    string
	FPSMode       string
	Deinterlace   bool
	UseCustomArgs bool
	CustomArgs    string
}

func validateProfileFields(f profileFields) string {
	if !validStreamModes[f.StreamMode] {
		return "invalid stream_mode"
	}
	if !validHWAccels[f.HWAccel] {
		return "invalid hwaccel"
	}
	if !validVideoCodecs[f.VideoCodec] {
		return "invalid video_codec"
	}
	if !validContainers[f.Container] {
		return "invalid container"
	}
	if !validDeliveries[f.Delivery] {
		return "invalid delivery"
	}
	if !validAudioCodecs[f.AudioCodec] {
		return "invalid audio_codec"
	}
	if !validFPSModes[f.FPSMode] {
		return "invalid fps_mode"
	}
	return ""
}

func composeArgs(f profileFields) string {
	if f.UseCustomArgs {
		return f.CustomArgs
	}
	return ""
}

func (h *StreamProfileHandler) List(w http.ResponseWriter, r *http.Request) {
	etag := h.rev.ETag()
	if r.Header.Get("If-None-Match") == etag {
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
		return
	}

	profiles, err := h.repo.List(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list stream profiles")
		return
	}

	respondCacheable(w, r, etag, http.StatusOK, profiles)
}

func (h *StreamProfileHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name          string `json:"name"`
		StreamMode    string `json:"stream_mode"`
		HWAccel       string `json:"hwaccel"`
		VideoCodec    string `json:"video_codec"`
		Container     string `json:"container"`
		Delivery      string `json:"delivery"`
		AudioCodec    string `json:"audio_codec"`
		Deinterlace   bool   `json:"deinterlace"`
		FPSMode       string `json:"fps_mode"`
		UseCustomArgs bool   `json:"use_custom_args"`
		CustomArgs    string `json:"custom_args"`
		IsDefault     bool   `json:"is_default"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		respondError(w, http.StatusBadRequest, "name is required")
		return
	}

	if existing, _ := h.repo.GetByName(r.Context(), req.Name); existing != nil {
		respondError(w, http.StatusConflict, "stream profile name already exists")
		return
	}

	if req.StreamMode == "" {
		req.StreamMode = "ffmpeg"
	}
	if req.HWAccel == "" {
		req.HWAccel = "none"
	}
	if req.VideoCodec == "" {
		req.VideoCodec = "copy"
	}
	if req.Container == "" {
		req.Container = ffmpeg.DefaultContainer(req.VideoCodec)
	}
	if req.Delivery == "" {
		req.Delivery = "stream"
	}
	if req.AudioCodec == "" {
		req.AudioCodec = "default"
	}
	if req.FPSMode == "" {
		req.FPSMode = "auto"
	}

	f := profileFields{
		StreamMode: req.StreamMode, HWAccel: req.HWAccel, Delivery: req.Delivery, AudioCodec: req.AudioCodec,
		VideoCodec: req.VideoCodec, Container: req.Container, FPSMode: req.FPSMode,
		Deinterlace: req.Deinterlace, UseCustomArgs: req.UseCustomArgs, CustomArgs: req.CustomArgs,
	}
	if msg := validateProfileFields(f); msg != "" {
		respondError(w, http.StatusBadRequest, msg)
		return
	}

	profile := &models.StreamProfile{
		Name:          req.Name,
		StreamMode:    req.StreamMode,
		HWAccel:       req.HWAccel,
		VideoCodec:    req.VideoCodec,
		Container:     req.Container,
		Delivery:      req.Delivery,
		AudioCodec:    req.AudioCodec,
		Deinterlace:   req.Deinterlace,
		FPSMode:       req.FPSMode,
		UseCustomArgs: req.UseCustomArgs,
		CustomArgs:    req.CustomArgs,
		Command:       "ffmpeg",
		Args:          composeArgs(f),
		IsDefault:     req.IsDefault,
	}

	if err := h.repo.Create(r.Context(), profile); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create stream profile")
		return
	}
	h.rev.Bump()

	respondJSON(w, http.StatusCreated, profile)
}

func (h *StreamProfileHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	profile, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "stream profile not found")
		return
	}

	respondJSON(w, http.StatusOK, profile)
}

func (h *StreamProfileHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	profile, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "stream profile not found")
		return
	}

	if profile.IsSystem {
		respondError(w, http.StatusForbidden, "cannot edit system profile")
		return
	}

	var req struct {
		Name          string `json:"name"`
		StreamMode    string `json:"stream_mode"`
		HWAccel       string `json:"hwaccel"`
		VideoCodec    string `json:"video_codec"`
		Container     string `json:"container"`
		Delivery      string `json:"delivery"`
		AudioCodec    string `json:"audio_codec"`
		Deinterlace   bool   `json:"deinterlace"`
		FPSMode       string `json:"fps_mode"`
		UseCustomArgs bool   `json:"use_custom_args"`
		CustomArgs    string `json:"custom_args"`
		IsDefault     bool   `json:"is_default"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name != "" && req.Name != profile.Name {
		if profile.IsClient {
			respondError(w, http.StatusForbidden, "cannot rename client profile")
			return
		}
		if existing, _ := h.repo.GetByName(r.Context(), req.Name); existing != nil {
			respondError(w, http.StatusConflict, "stream profile name already exists")
			return
		}
		profile.Name = req.Name
	}

	if req.StreamMode == "" {
		req.StreamMode = profile.StreamMode
	}
	if req.StreamMode == "" {
		req.StreamMode = "ffmpeg"
	}
	if req.HWAccel == "" {
		req.HWAccel = profile.HWAccel
	}
	if req.HWAccel == "" {
		req.HWAccel = "none"
	}
	if req.VideoCodec == "" {
		req.VideoCodec = profile.VideoCodec
	}
	if req.VideoCodec == "" {
		req.VideoCodec = "copy"
	}
	if req.Container == "" {
		req.Container = profile.Container
	}
	if req.Container == "" {
		req.Container = ffmpeg.DefaultContainer(req.VideoCodec)
	}
	if req.Delivery == "" {
		req.Delivery = profile.Delivery
	}
	if req.Delivery == "" {
		req.Delivery = "stream"
	}
	if req.AudioCodec == "" {
		req.AudioCodec = profile.AudioCodec
	}
	if req.AudioCodec == "" {
		req.AudioCodec = "default"
	}
	if req.FPSMode == "" {
		req.FPSMode = profile.FPSMode
	}
	if req.FPSMode == "" {
		req.FPSMode = "auto"
	}

	f := profileFields{
		StreamMode: req.StreamMode, HWAccel: req.HWAccel, Delivery: req.Delivery, AudioCodec: req.AudioCodec,
		VideoCodec: req.VideoCodec, Container: req.Container, FPSMode: req.FPSMode,
		Deinterlace: req.Deinterlace, UseCustomArgs: req.UseCustomArgs, CustomArgs: req.CustomArgs,
	}
	if msg := validateProfileFields(f); msg != "" {
		respondError(w, http.StatusBadRequest, msg)
		return
	}

	profile.StreamMode = req.StreamMode
	profile.HWAccel = req.HWAccel
	profile.VideoCodec = req.VideoCodec
	profile.Container = req.Container
	profile.Delivery = req.Delivery
	profile.AudioCodec = req.AudioCodec
	profile.Deinterlace = req.Deinterlace
	profile.FPSMode = req.FPSMode
	profile.IsDefault = req.IsDefault
	profile.UseCustomArgs = req.UseCustomArgs
	profile.CustomArgs = req.CustomArgs
	profile.Command = "ffmpeg"

	profile.Args = composeArgs(f)

	if err := h.repo.Update(r.Context(), profile); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update stream profile")
		return
	}
	h.rev.Bump()

	respondJSON(w, http.StatusOK, profile)
}

func (h *StreamProfileHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	profile, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "stream profile not found")
		return
	}

	if profile.IsSystem || profile.IsClient {
		respondError(w, http.StatusForbidden, "cannot delete system or client profile")
		return
	}

	if err := h.repo.Delete(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to delete stream profile")
		return
	}
	h.rev.Bump()

	w.WriteHeader(http.StatusNoContent)
}
