package handler

import (
	"net/http"

	"github.com/go-gst/go-gst/gst"
)

type GStreamerHandler struct{}

func NewGStreamerHandler() *GStreamerHandler {
	return &GStreamerHandler{}
}

type gstEncoder struct {
	Name     string `json:"name"`
	Codec    string `json:"codec"`
	Platform string `json:"platform"`
	HW       bool   `json:"hw"`
}

type gstCapabilities struct {
	Platforms     []string     `json:"platforms"`
	VideoEncoders []gstEncoder `json:"video_encoders"`
	AudioEncoders []gstEncoder `json:"audio_encoders"`
}

var videoEncoderDefs = []gstEncoder{
	{Name: "vaav1lpenc", Codec: "av1", Platform: "Intel VA-API", HW: true},
	{Name: "vah265lpenc", Codec: "h265", Platform: "Intel VA-API", HW: true},
	{Name: "vah264lpenc", Codec: "h264", Platform: "Intel VA-API", HW: true},
	{Name: "vtenc_av1", Codec: "av1", Platform: "VideoToolbox", HW: true},
	{Name: "vtenc_h265", Codec: "h265", Platform: "VideoToolbox", HW: true},
	{Name: "vtenc_h264", Codec: "h264", Platform: "VideoToolbox", HW: true},
	{Name: "nvav1enc", Codec: "av1", Platform: "NVIDIA NVENC", HW: true},
	{Name: "nvh265enc", Codec: "h265", Platform: "NVIDIA NVENC", HW: true},
	{Name: "nvh264enc", Codec: "h264", Platform: "NVIDIA NVENC", HW: true},
	{Name: "svtav1enc", Codec: "av1", Platform: "Software", HW: false},
	{Name: "x265enc", Codec: "h265", Platform: "Software", HW: false},
	{Name: "x264enc", Codec: "h264", Platform: "Software", HW: false},
	{Name: "rav1enc", Codec: "av1", Platform: "Software", HW: false},
}

var audioEncoderDefs = []gstEncoder{
	{Name: "fdkaacenc", Codec: "aac", Platform: "Software", HW: false},
	{Name: "faac", Codec: "aac", Platform: "Software", HW: false},
	{Name: "voaacenc", Codec: "aac", Platform: "Software", HW: false},
	{Name: "avenc_aac", Codec: "aac", Platform: "Software", HW: false},
	{Name: "opusenc", Codec: "opus", Platform: "Software", HW: false},
}

func (h *GStreamerHandler) Capabilities(w http.ResponseWriter, r *http.Request) {
	caps := gstCapabilities{
		Platforms:     make([]string, 0),
		VideoEncoders: make([]gstEncoder, 0),
		AudioEncoders: make([]gstEncoder, 0),
	}

	platformSeen := make(map[string]bool)

	for _, enc := range videoEncoderDefs {
		if gst.Find(enc.Name) != nil {
			caps.VideoEncoders = append(caps.VideoEncoders, enc)
			if enc.HW && !platformSeen[enc.Platform] {
				platformSeen[enc.Platform] = true
				caps.Platforms = append(caps.Platforms, enc.Platform)
			}
		}
	}

	for _, enc := range audioEncoderDefs {
		if gst.Find(enc.Name) != nil {
			caps.AudioEncoders = append(caps.AudioEncoders, enc)
		}
	}

	respondJSON(w, http.StatusOK, caps)
}
