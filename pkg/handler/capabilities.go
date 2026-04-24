package handler

import (
	"net/http"
	"strings"
	"sync"

	"github.com/asticode/go-astiav"
)

type CapabilitiesHandler struct{}

func NewCapabilitiesHandler() *CapabilitiesHandler {
	return &CapabilitiesHandler{}
}

type codecEntry struct {
	Name     string `json:"name"`
	Codec    string `json:"codec"`
	HWAccel  string `json:"hwaccel"`
	Platform string `json:"platform,omitempty"`
	HW       bool   `json:"hw"`
	Type     string `json:"type"`
}

type platformCapabilities struct {
	Platforms     []string     `json:"platforms"`
	VideoEncoders []codecEntry `json:"video_encoders"`
	VideoDecoders []codecEntry `json:"video_decoders"`
	AudioEncoders []codecEntry `json:"audio_encoders"`
}

var probeVideoEncoders = []struct {
	name    string
	codec   string
	hwaccel string
}{
	{"libx264", "h264", ""},
	{"libx265", "h265", ""},
	{"libsvtav1", "av1", ""},
	{"libvpx", "vp8", ""},
	{"libvpx-vp9", "vp9", ""},
	{"h264_videotoolbox", "h264", "videotoolbox"},
	{"hevc_videotoolbox", "h265", "videotoolbox"},
	{"h264_vaapi", "h264", "vaapi"},
	{"hevc_vaapi", "h265", "vaapi"},
	{"av1_vaapi", "av1", "vaapi"},
	{"h264_qsv", "h264", "qsv"},
	{"hevc_qsv", "h265", "qsv"},
	{"av1_qsv", "av1", "qsv"},
	{"h264_nvenc", "h264", "nvenc"},
	{"hevc_nvenc", "h265", "nvenc"},
	{"av1_nvenc", "av1", "nvenc"},
}

var probeVideoDecoders = []struct {
	name    string
	codec   string
	hwaccel string
}{
	{"h264", "h264", ""},
	{"hevc", "h265", ""},
	{"av1", "av1", ""},
	{"libdav1d", "av1", ""},
	{"mpeg2video", "mpeg2", ""},
	{"vp8", "vp8", ""},
	{"vp9", "vp9", ""},
}

var probeAudioEncoders = []struct {
	name  string
	codec string
}{
	{"aac", "aac"},
	{"libmp3lame", "mp3"},
	{"libopus", "opus"},
	{"libvorbis", "vorbis"},
	{"flac", "flac"},
	{"ac3", "ac3"},
	{"eac3", "eac3"},
	{"mp2", "mp2"},
}

var hwPlatformMap = map[string]astiav.HardwareDeviceType{
	"vaapi":        astiav.HardwareDeviceTypeVAAPI,
	"qsv":          astiav.HardwareDeviceTypeQSV,
	"cuda":         astiav.HardwareDeviceTypeCUDA,
	"nvenc":        astiav.HardwareDeviceTypeCUDA,
	"videotoolbox": astiav.HardwareDeviceTypeVideoToolbox,
	"vulkan":       astiav.HardwareDeviceTypeVulkan,
}

var (
	availablePlatforms     map[string]bool
	availablePlatformsOnce sync.Once
)

func probeAvailablePlatforms() map[string]bool {
	availablePlatformsOnce.Do(func() {
		availablePlatforms = make(map[string]bool)
		for name, hwType := range hwPlatformMap {
			if name == "nvenc" {
				continue
			}
			ctx, err := astiav.CreateHardwareDeviceContext(hwType, "", nil, 0)
			if err == nil {
				ctx.Free()
				availablePlatforms[name] = true
				if name == "cuda" {
					availablePlatforms["nvenc"] = true
				}
			}
		}
	})
	return availablePlatforms
}

func (h *CapabilitiesHandler) Capabilities(w http.ResponseWriter, r *http.Request) {
	available := probeAvailablePlatforms()
	platformSet := map[string]bool{}

	var videoEncoders []codecEntry
	for _, e := range probeVideoEncoders {
		if astiav.FindEncoderByName(e.name) == nil {
			continue
		}
		isHW := e.hwaccel != ""
		if isHW && !available[e.hwaccel] {
			continue
		}
		if isHW {
			platformSet[e.hwaccel] = true
		}
		videoEncoders = append(videoEncoders, codecEntry{
			Name:     e.name,
			Codec:    e.codec,
			HWAccel:  e.hwaccel,
			Platform: e.hwaccel,
			HW:       isHW,
			Type:     "video",
		})
	}

	var videoDecoders []codecEntry
	seen := map[string]bool{}
	for _, d := range probeVideoDecoders {
		c := astiav.FindDecoderByName(d.name)
		if c == nil {
			continue
		}
		videoDecoders = append(videoDecoders, codecEntry{
			Name:  d.name,
			Codec: d.codec,
			Type:  "video",
		})
		for _, hwc := range c.HardwareConfigs() {
			hwName := strings.ToLower(hwc.HardwareDeviceType().String())
			if hwName == "" || hwName == "none" {
				continue
			}
			if !available[hwName] {
				continue
			}
			key := d.codec + ":" + hwName
			if seen[key] {
				continue
			}
			seen[key] = true
			platformSet[hwName] = true
			videoDecoders = append(videoDecoders, codecEntry{
				Name:     d.name,
				Codec:    d.codec,
				HWAccel:  hwName,
				Platform: hwName,
				HW:       true,
				Type:     "video",
			})
		}
	}

	var audioEncoders []codecEntry
	for _, ae := range probeAudioEncoders {
		if astiav.FindEncoderByName(ae.name) != nil {
			audioEncoders = append(audioEncoders, codecEntry{
				Name:  ae.name,
				Codec: ae.codec,
				Type:  "audio",
			})
		}
	}

	var platforms []string
	for p := range platformSet {
		platforms = append(platforms, p)
	}

	respondJSON(w, http.StatusOK, platformCapabilities{
		Platforms:     platforms,
		VideoEncoders: videoEncoders,
		VideoDecoders: videoDecoders,
		AudioEncoders: audioEncoders,
	})
}
