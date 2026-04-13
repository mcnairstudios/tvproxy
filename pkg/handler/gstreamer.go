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
	Name  string `json:"name"`
	Codec string `json:"codec"`
	HW    bool   `json:"hw"`
}

type gstCapabilities struct {
	VideoEncoders []gstEncoder `json:"video_encoders"`
	AudioEncoders []gstEncoder `json:"audio_encoders"`
	HWAccel       string       `json:"hwaccel"`
}

var videoEncoders = []gstEncoder{
	{Name: "vaav1lpenc", Codec: "av1", HW: true},
	{Name: "vah265lpenc", Codec: "h265", HW: true},
	{Name: "vah264lpenc", Codec: "h264", HW: true},
	{Name: "svtav1enc", Codec: "av1", HW: false},
	{Name: "vtenc_h264", Codec: "h264", HW: true},
	{Name: "vtenc_h265", Codec: "h265", HW: true},
	{Name: "vtenc_av1", Codec: "av1", HW: true},
	{Name: "nvh264enc", Codec: "h264", HW: true},
	{Name: "nvh265enc", Codec: "h265", HW: true},
	{Name: "nvav1enc", Codec: "av1", HW: true},
	{Name: "x264enc", Codec: "h264", HW: false},
	{Name: "x265enc", Codec: "h265", HW: false},
}

var audioEncoders = []gstEncoder{
	{Name: "faac", Codec: "aac", HW: false},
	{Name: "fdkaacenc", Codec: "aac", HW: false},
	{Name: "voaacenc", Codec: "aac", HW: false},
	{Name: "avenc_aac", Codec: "aac", HW: false},
	{Name: "opusenc", Codec: "opus", HW: false},
}

func (h *GStreamerHandler) Capabilities(w http.ResponseWriter, r *http.Request) {
	caps := gstCapabilities{
		VideoEncoders: make([]gstEncoder, 0),
		AudioEncoders: make([]gstEncoder, 0),
	}

	for _, enc := range videoEncoders {
		if gst.Find(enc.Name) != nil {
			caps.VideoEncoders = append(caps.VideoEncoders, enc)
		}
	}

	for _, enc := range audioEncoders {
		if gst.Find(enc.Name) != nil {
			caps.AudioEncoders = append(caps.AudioEncoders, enc)
		}
	}

	caps.HWAccel = detectHWAccel(caps.VideoEncoders)

	respondJSON(w, http.StatusOK, caps)
}

func detectHWAccel(encoders []gstEncoder) string {
	for _, enc := range encoders {
		switch {
		case enc.Name == "vaav1lpenc" || enc.Name == "vah265lpenc" || enc.Name == "vah264lpenc":
			return "vaapi"
		case enc.Name == "vtenc_h264" || enc.Name == "vtenc_h265" || enc.Name == "vtenc_av1":
			return "videotoolbox"
		case enc.Name == "nvh264enc" || enc.Name == "nvh265enc" || enc.Name == "nvav1enc":
			return "nvenc"
		}
	}
	return "software"
}
