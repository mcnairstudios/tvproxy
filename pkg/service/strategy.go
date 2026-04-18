package service

import (
	"strings"

	"github.com/gavinmcnair/tvproxy/pkg/models"
)

type StreamCategory int

const (
	CategoryLiveIPTV   StreamCategory = iota // Live IPTV channel (xtream/m3u, no VOD type)
	CategoryLiveSatIP                        // SAT>IP live (RTSP)
	CategoryVODRemote                        // Fixed-length VOD from remote provider (xtream movie/series)
	CategoryVODLocal                         // Fixed-length VOD from local server (tvproxy-streams)
	CategoryFile                             // Local file playback (completed recordings)
)

type SessionStrategy struct {
	Category StreamCategory

	MetadataOnly      bool

	VideoCodec string
	AudioCodec string
	HWAccel    string
	Container  string
}

type StrategyInput struct {
	StreamURL     string
	VODType       string
	VODDuration   float64
	UseWireGuard  bool
	SatIPSource   bool
	StreamGroup   string
	StreamID      string
	StreamVCodec  string
	StreamACodec  string
	SourceProfile *models.SourceProfile
}

type StrategyOutput struct {
	VideoCodec   string
	AudioCodec   string
	HWAccel      string
	Container    string
	OutputHeight int
}

func classifyStream(in StrategyInput) StreamCategory {
	if in.SatIPSource {
		return CategoryLiveSatIP
	}
	if in.VODType == "movie" || in.VODType == "series" {
		if isLocalURL(in.StreamURL) {
			return CategoryVODLocal
		}
		return CategoryVODRemote
	}
	return CategoryLiveIPTV
}

func resolveSessionStrategy(in StrategyInput, out StrategyOutput) SessionStrategy {
	cat := classifyStream(in)

	var s SessionStrategy
	switch cat {
	case CategoryLiveIPTV, CategoryLiveSatIP:
		s = liveStrategy(in, out, cat)
	case CategoryVODRemote:
		s = vodRemoteStrategy(in, out)
	case CategoryVODLocal:
		s = vodLocalStrategy(in, out)
	default:
		s = vodLocalStrategy(in, out)
	}

	return s
}

func liveStrategy(in StrategyInput, out StrategyOutput, cat StreamCategory) SessionStrategy {
	sourceVideo := "h264"
	sourceAudio := "aac"
	if in.StreamVCodec != "" {
		sourceVideo = strings.ToLower(in.StreamVCodec)
	}
	if in.StreamACodec != "" {
		sourceAudio = strings.ToLower(in.StreamACodec)
	}

	videoCodec := resolveVideoActionWithHeight(sourceVideo, out.VideoCodec, out.OutputHeight)

	audioCodec := resolveAudioAction(sourceAudio, out.AudioCodec, out.Container)
	if audioCodec == "copy" && sourceAudio != "aac" {
		audioCodec = "aac"
	}
	if audioCodec == "copy" && (cat == CategoryLiveSatIP || cat == CategoryLiveIPTV) {
		audioCodec = "aac"
	}

	return SessionStrategy{
		Category:     cat,
		VideoCodec:   videoCodec,
		AudioCodec:   audioCodec,
		HWAccel:      out.HWAccel,
		Container:    out.Container,
		MetadataOnly: false,
	}
}

func vodRemoteStrategy(in StrategyInput, out StrategyOutput) SessionStrategy {
	sourceAudio := "aac"
	if in.StreamACodec != "" {
		sourceAudio = strings.ToLower(in.StreamACodec)
	}

	audioCodec := resolveAudioAction(sourceAudio, out.AudioCodec, out.Container)
	if audioCodec == "copy" && sourceAudio != "aac" {
		audioCodec = "aac"
	}

	return SessionStrategy{
		Category:     CategoryVODRemote,
		VideoCodec:   out.VideoCodec,
		AudioCodec:   audioCodec,
		HWAccel:      out.HWAccel,
		Container:    out.Container,
		MetadataOnly: false,
	}
}

func vodLocalStrategy(in StrategyInput, out StrategyOutput) SessionStrategy {
	sourceAudio := "aac"
	if in.StreamACodec != "" {
		sourceAudio = strings.ToLower(in.StreamACodec)
	}

	audioCodec := resolveAudioAction(sourceAudio, out.AudioCodec, out.Container)
	if audioCodec == "copy" && sourceAudio != "aac" {
		audioCodec = "aac"
	}

	return SessionStrategy{
		Category:     CategoryVODLocal,
		VideoCodec:   out.VideoCodec,
		AudioCodec:   audioCodec,
		HWAccel:      out.HWAccel,
		Container:    out.Container,
		MetadataOnly: false,
	}
}

func resolveVideoAction(sourceCodec, clientCodec string) string {
	return resolveVideoActionWithHeight(sourceCodec, clientCodec, 0)
}

func resolveVideoActionWithHeight(sourceCodec, clientCodec string, outputHeight int) string {
	if clientCodec == "" || clientCodec == "default" || clientCodec == "copy" {
		if outputHeight > 0 {
			if sourceCodec != "" {
				return normalizeVideoCodecName(sourceCodec)
			}
			return "h265"
		}
		return "copy"
	}
	src := normalizeVideoCodecName(sourceCodec)
	dst := normalizeVideoCodecName(clientCodec)
	if src == dst && outputHeight == 0 {
		return "copy"
	}
	return clientCodec
}

func normalizeVideoCodecName(c string) string {
	switch strings.ToLower(c) {
	case "hevc", "h265", "h.265":
		return "h265"
	case "avc", "h264", "h.264":
		return "h264"
	}
	return strings.ToLower(c)
}

func resolveAudioAction(sourceCodec, clientCodec, outputContainer string) string {
	if clientCodec == "" || clientCodec == "default" {
		if sourceCodec == "aac" && outputContainer != "webm" {
			return "copy"
		}
		if outputContainer == "webm" {
			return "opus"
		}
		return "aac"
	}
	if clientCodec == "copy" || sourceCodec == clientCodec {
		return "copy"
	}
	return clientCodec
}

func isLocalURL(u string) bool {
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return true
	}
	u = strings.TrimPrefix(u, "http://")
	u = strings.TrimPrefix(u, "https://")
	return strings.HasPrefix(u, "192.168.") ||
		strings.HasPrefix(u, "10.") ||
		strings.HasPrefix(u, "172.") ||
		strings.HasPrefix(u, "localhost") ||
		strings.HasPrefix(u, "127.")
}
