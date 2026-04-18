package handler

import (
	"net/http"
	"strings"

	"github.com/go-gst/go-gst/gst"
)

type GStreamerHandler struct{}

func NewGStreamerHandler() *GStreamerHandler {
	return &GStreamerHandler{}
}

type gstEncoder struct {
	Name      string `json:"name"`
	Codec     string `json:"codec"`
	Platform  string `json:"platform"`
	HW        bool   `json:"hw"`
	Supports10Bit bool `json:"supports_10bit,omitempty"`
}

type gstCapabilities struct {
	Platforms      []string     `json:"platforms"`
	VideoEncoders  []gstEncoder `json:"video_encoders"`
	VideoDecoders  []gstEncoder `json:"video_decoders"`
	AudioEncoders  []gstEncoder `json:"audio_encoders"`
	Decode10BitAvailable bool   `json:"decode_10bit_available"`
}

var videoDecoderDefs = []gstEncoder{
	{Name: "vtdec", Codec: "h264", Platform: "VideoToolbox", HW: true},
	{Name: "vtdec", Codec: "h265", Platform: "VideoToolbox", HW: true},
	{Name: "vtdec", Codec: "av1", Platform: "VideoToolbox", HW: true},
	{Name: "vah264dec", Codec: "h264", Platform: "Intel VA-API", HW: true},
	{Name: "vah265dec", Codec: "h265", Platform: "Intel VA-API", HW: true},
	{Name: "vaav1dec", Codec: "av1", Platform: "Intel VA-API", HW: true},
	{Name: "vampeg2dec", Codec: "mpeg2", Platform: "Intel VA-API", HW: true},
	{Name: "qsvh264dec", Codec: "h264", Platform: "Intel QSV", HW: true},
	{Name: "qsvh265dec", Codec: "h265", Platform: "Intel QSV", HW: true},
	{Name: "qsvav1dec", Codec: "av1", Platform: "Intel QSV", HW: true},
	{Name: "nvh264dec", Codec: "h264", Platform: "NVIDIA NVDEC", HW: true},
	{Name: "nvh265dec", Codec: "h265", Platform: "NVIDIA NVDEC", HW: true},
	{Name: "nvav1dec", Codec: "av1", Platform: "NVIDIA NVDEC", HW: true},
	{Name: "avdec_h264", Codec: "h264", Platform: "Software", HW: false},
	{Name: "avdec_h265", Codec: "h265", Platform: "Software", HW: false},
	{Name: "dav1ddec", Codec: "av1", Platform: "Software", HW: false},
	{Name: "avdec_av1", Codec: "av1", Platform: "Software", HW: false},
	{Name: "avdec_mpeg2video", Codec: "mpeg2", Platform: "Software", HW: false},
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

func decoderSupports10Bit(elementName string) bool {
	el, _ := gst.NewElement(elementName)
	if el == nil {
		return false
	}
	for _, tmpl := range el.GetPadTemplates() {
		if tmpl.Direction() != gst.PadDirectionSource {
			continue
		}
		capsStr := tmpl.Caps().String()
		for _, fmt := range []string{"P010", "P012", "Y410", "Y412", "P016", "10LE", "10BE", "12LE", "12BE"} {
			if strings.Contains(capsStr, fmt) {
				return true
			}
		}
	}
	return false
}

func (h *GStreamerHandler) Capabilities(w http.ResponseWriter, r *http.Request) {
	caps := gstCapabilities{
		Platforms:     make([]string, 0),
		VideoEncoders: make([]gstEncoder, 0),
		VideoDecoders: make([]gstEncoder, 0),
		AudioEncoders: make([]gstEncoder, 0),
	}

	platformSeen := make(map[string]bool)
	decoderSeen := make(map[string]bool)

	for _, enc := range videoEncoderDefs {
		if gst.Find(enc.Name) != nil {
			caps.VideoEncoders = append(caps.VideoEncoders, enc)
			if enc.HW && !platformSeen[enc.Platform] {
				platformSeen[enc.Platform] = true
				caps.Platforms = append(caps.Platforms, enc.Platform)
			}
		}
	}

	for _, dec := range videoDecoderDefs {
		key := dec.Name + ":" + dec.Codec
		if decoderSeen[key] {
			continue
		}
		if gst.Find(dec.Name) != nil {
			dec.Supports10Bit = decoderSupports10Bit(dec.Name)
			caps.VideoDecoders = append(caps.VideoDecoders, dec)
			decoderSeen[key] = true
			if dec.HW && !platformSeen[dec.Platform] {
				platformSeen[dec.Platform] = true
				caps.Platforms = append(caps.Platforms, dec.Platform)
			}
		}
	}

	for _, dec := range caps.VideoDecoders {
		if dec.HW && dec.Supports10Bit {
			caps.Decode10BitAvailable = true
			break
		}
	}

	for _, enc := range audioEncoderDefs {
		if gst.Find(enc.Name) != nil {
			caps.AudioEncoders = append(caps.AudioEncoders, enc)
		}
	}

	respondJSON(w, http.StatusOK, caps)
}
