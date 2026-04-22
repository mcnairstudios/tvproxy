package handler

import (
	"net/http"
)

type GStreamerHandler struct{}

func NewGStreamerHandler() *GStreamerHandler {
	return &GStreamerHandler{}
}

type gstCapabilities struct {
	Platforms            []string `json:"platforms"`
	VideoEncoders        []any    `json:"video_encoders"`
	VideoDecoders        []any    `json:"video_decoders"`
	AudioEncoders        []any    `json:"audio_encoders"`
	Decode10BitAvailable bool     `json:"decode_10bit_available"`
}

func (h *GStreamerHandler) Capabilities(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, gstCapabilities{
		Platforms:     []string{},
		VideoEncoders: []any{},
		VideoDecoders: []any{},
		AudioEncoders: []any{},
	})
}
