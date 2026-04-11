package service

import (
	"fmt"
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
	SkipProbe         bool
	HLSOutputDir      string
	SourceInputArgs   string
	SourceDeinterlace bool
	SourceAudioResync bool
	SourceFPSMode     string

	VideoCodec string
	AudioCodec string
	HWAccel    string
	Container  string

	FFmpegArgs string
	Command    string
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
	Delivery   string
	VideoCodec string
	AudioCodec string
	HWAccel    string
	Container  string
	Command    string
	Args       string
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

func resolveSessionStrategy(in StrategyInput, out StrategyOutput, outputDir string) SessionStrategy {
	cat := classifyStream(in)

	switch cat {
	case CategoryLiveIPTV, CategoryLiveSatIP:
		return liveStrategy(in, out, cat)
	case CategoryVODRemote:
		return vodRemoteStrategy(in, out)
	case CategoryVODLocal:
		return vodLocalStrategy(in, out)
	default:
		return vodLocalStrategy(in, out)
	}
}

func liveStrategy(in StrategyInput, out StrategyOutput, cat StreamCategory) SessionStrategy {
	sp := in.SourceProfile

	sourceVideo := "h264"
	sourceAudio := "aac"
	if in.StreamVCodec != "" {
		sourceVideo = strings.ToLower(in.StreamVCodec)
	} else if sp != nil && sp.VideoCodec != "" {
		sourceVideo = sp.VideoCodec
	}
	if in.StreamACodec != "" {
		sourceAudio = strings.ToLower(in.StreamACodec)
	} else if sp != nil && sp.AudioCodec != "" {
		sourceAudio = sp.AudioCodec
	}

	s := SessionStrategy{
		Category:        cat,
		VideoCodec:      resolveVideoAction(sourceVideo, out.VideoCodec),
		AudioCodec:      resolveAudioAction(sourceAudio, out.AudioCodec, out.Container),
		HWAccel:         out.HWAccel,
		Container:       out.Container,
		Command:         out.Command,
		SourceInputArgs: buildSourceInputArgs(sp),
	}
	if sp != nil {
		s.SourceDeinterlace = sp.Deinterlace
		s.SourceAudioResync = sp.AudioResync
		s.SourceFPSMode = sp.FPSMode
		if sp.ProbeMode == "none" || sp.ProbeMode == "declared" {
			s.SkipProbe = true
		}
	}

	s.MetadataOnly = false
	s.FFmpegArgs = out.Args

	return s
}

func vodRemoteStrategy(in StrategyInput, out StrategyOutput) SessionStrategy {
	sp := in.SourceProfile
	s := SessionStrategy{
		Category:        CategoryVODRemote,
		VideoCodec:      out.VideoCodec,
		AudioCodec:      out.AudioCodec,
		HWAccel:         out.HWAccel,
		Container:       out.Container,
		Command:         out.Command,
		FFmpegArgs:      out.Args,
		SourceInputArgs: buildSourceInputArgs(sp),
		MetadataOnly:    out.Delivery == "hls",
	}
	if sp != nil {
		s.SourceDeinterlace = sp.Deinterlace
		s.SourceAudioResync = sp.AudioResync
		s.SourceFPSMode = sp.FPSMode
		if sp.ProbeMode == "none" || sp.ProbeMode == "declared" {
			s.SkipProbe = true
		}
	}
	return s
}

func vodLocalStrategy(in StrategyInput, out StrategyOutput) SessionStrategy {
	s := SessionStrategy{
		Category:     CategoryVODLocal,
		VideoCodec:   out.VideoCodec,
		AudioCodec:   out.AudioCodec,
		HWAccel:      out.HWAccel,
		Container:    out.Container,
		Command:      out.Command,
		FFmpegArgs:   out.Args,
		MetadataOnly: out.Delivery == "hls",
	}
	return s
}

func resolveVideoAction(sourceCodec, clientCodec string) string {
	if clientCodec == "" || clientCodec == "default" || clientCodec == "copy" {
		return "copy"
	}
	if sourceCodec == clientCodec {
		return "copy"
	}
	return clientCodec
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

func buildSourceInputArgs(sp *models.SourceProfile) string {
	if sp == nil {
		return ""
	}
	var parts []string

	if sp.RTSPTransport != "" {
		parts = append(parts, "-rtsp_transport", sp.RTSPTransport)
	}
	if sp.InputFormat != "" {
		parts = append(parts, "-f", sp.InputFormat)
	}
	if sp.AnalyzeDuration > 0 {
		parts = append(parts, "-analyzeduration", fmt.Sprintf("%d", sp.AnalyzeDuration))
	}
	if sp.ProbeSize > 0 {
		parts = append(parts, "-probesize", fmt.Sprintf("%d", sp.ProbeSize))
	}
	if sp.MaxDelay > 0 {
		parts = append(parts, "-max_delay", fmt.Sprintf("%d", sp.MaxDelay))
	}
	if sp.ErrDetect != "" {
		parts = append(parts, "-err_detect", sp.ErrDetect)
	}
	if sp.FFlags != "" {
		parts = append(parts, "-fflags", sp.FFlags)
	}

	return strings.Join(parts, " ")
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
