package ffmpeg

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func ShellSplit(s string) []string {
	var args []string
	var current strings.Builder
	inDouble, inSingle := false, false

	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case (c == ' ' || c == '\n' || c == '\r' || c == '\t') && !inDouble && !inSingle:
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteByte(c)
		}
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}

func InjectReconnect(args []string, inputURL string, delayMax, rwTimeout int) []string {
	if !strings.HasPrefix(inputURL, "http://") && !strings.HasPrefix(inputURL, "https://") {
		return args
	}
	for _, arg := range args {
		if arg == "-reconnect" {
			return args
		}
	}
	if delayMax <= 0 {
		delayMax = 30
	}
	if rwTimeout <= 0 {
		rwTimeout = 30000000
	}
	for i, arg := range args {
		if arg == "-i" {
			reconnectArgs := []string{
				"-reconnect", "1",
				"-reconnect_streamed", "1",
				"-reconnect_delay_max", fmt.Sprintf("%d", delayMax),
				"-reconnect_on_network_error", "1",
				"-rw_timeout", fmt.Sprintf("%d", rwTimeout),
			}
			newArgs := make([]string, 0, len(args)+len(reconnectArgs))
			newArgs = append(newArgs, args[:i]...)
			newArgs = append(newArgs, reconnectArgs...)
			newArgs = append(newArgs, args[i:]...)
			return newArgs
		}
	}
	return args
}

// SoftenMaps converts hard stream map args ("-map A:B") to optional ("-map A:B?")
// so ffmpeg silently skips missing stream types (e.g. no video in a radio stream).
func SoftenMaps(args []string) []string {
	result := make([]string, len(args))
	copy(result, args)
	for i := 0; i < len(result)-1; i++ {
		if result[i] == "-map" {
			v := result[i+1]
			if len(v) > 0 && v[len(v)-1] != '?' {
				result[i+1] = v + "?"
			}
		}
	}
	return result
}

// InjectRTSPTransport adds "-rtsp_transport tcp" before the first "-i" if not already present.
// TCP transport eliminates UDP packet loss and "RTP: missed N packets" errors.
func InjectRTSPTransport(args []string) []string {
	for _, arg := range args {
		if arg == "-rtsp_transport" {
			return args
		}
	}
	for i, arg := range args {
		if arg == "-i" {
			newArgs := make([]string, 0, len(args)+2)
			newArgs = append(newArgs, args[:i]...)
			newArgs = append(newArgs, "-rtsp_transport", "tcp")
			newArgs = append(newArgs, args[i:]...)
			return newArgs
		}
	}
	return args
}

// InjectFPSMode adds "-fps_mode cfr -r 25" after the output path if transcoding video
// (i.e. -c:v is not "copy"). CFR output eliminates stutter from dropped/corrupt input frames.
func InjectFPSMode(args []string) []string {
	for _, arg := range args {
		if arg == "-fps_mode" {
			return args
		}
	}
	// Only apply when transcoding video (not copy).
	transcoding := false
	for i, arg := range args {
		if arg == "-c:v" && i+1 < len(args) && args[i+1] != "copy" {
			transcoding = true
			break
		}
	}
	if !transcoding {
		return args
	}
	return append(args, "-fps_mode", "cfr")
}

func ReplaceAudioMap(args []string, audioIndex int) []string {
	if audioIndex <= 0 {
		return args
	}
	target := fmt.Sprintf("0:a:%d", audioIndex)
	for i, arg := range args {
		if arg == "0:a:0" {
			result := make([]string, len(args))
			copy(result, args)
			result[i] = target
			return result
		}
	}
	return args
}

func InjectUserAgent(args []string, userAgent string) []string {
	for _, arg := range args {
		if arg == "-user_agent" {
			return args
		}
	}
	for i, arg := range args {
		if arg == "-i" {
			newArgs := make([]string, 0, len(args)+2)
			newArgs = append(newArgs, args[:i]...)
			newArgs = append(newArgs, "-user_agent", userAgent)
			newArgs = append(newArgs, args[i:]...)
			return newArgs
		}
	}
	return args
}

func IsHTTPURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func IsRTSPURL(s string) bool {
	return strings.HasPrefix(s, "rtsp://") || strings.HasPrefix(s, "rtsps://")
}

// InjectAudioResync adds "-af aresample=async=1000" before the output if not present.
// This corrects A/V sync drift that occurs when MPEG-TS audio and video have different
// start timestamps (common with live DVB streams joined mid-stream).
func InjectAudioResync(args []string) []string {
	for _, arg := range args {
		if arg == "-af" || arg == "aresample=async=1000" {
			return args
		}
	}
	// Find output path (after last -f or just before -progress).
	for i, arg := range args {
		if arg == "-progress" {
			newArgs := make([]string, 0, len(args)+2)
			newArgs = append(newArgs, args[:i]...)
			newArgs = append(newArgs, "-af", "aresample=async=1000")
			newArgs = append(newArgs, args[i:]...)
			return newArgs
		}
	}
	return args
}

// InjectRTSPProbe ensures RTSP inputs have sufficient probesize (10 MB) and a
// reasonable analyzeduration (3 s) to probe DVB MPEG-2 and H.264 streams.
// probesize=10MB ensures video dimensions are resolved for high-bitrate muxes.
// analyzeduration=3s is short enough for radio streams to start quickly.
func InjectRTSPProbe(args []string) []string {
	const minProbeSize = 10_000_000
	const minAnalyzeDuration = 3_000_000 // 3 seconds

	result := make([]string, len(args))
	copy(result, args)

	hasAnalyze, hasProbe := false, false
	for i, arg := range result {
		switch arg {
		case "-analyzeduration":
			hasAnalyze = true
			if i+1 < len(result) {
				if v, err := strconv.Atoi(result[i+1]); err == nil && v < minAnalyzeDuration {
					result[i+1] = strconv.Itoa(minAnalyzeDuration)
				}
			}
		case "-probesize":
			hasProbe = true
			if i+1 < len(result) {
				if v, err := strconv.Atoi(result[i+1]); err == nil && v < minProbeSize {
					result[i+1] = strconv.Itoa(minProbeSize)
				}
			}
		}
	}

	var inject []string
	if !hasAnalyze {
		inject = append(inject, "-analyzeduration", strconv.Itoa(minAnalyzeDuration))
	}
	if !hasProbe {
		inject = append(inject, "-probesize", strconv.Itoa(minProbeSize))
	}
	if len(inject) == 0 {
		return result
	}

	for i, arg := range result {
		if arg == "-i" {
			newArgs := make([]string, 0, len(result)+len(inject))
			newArgs = append(newArgs, result[:i]...)
			newArgs = append(newArgs, inject...)
			newArgs = append(newArgs, result[i:]...)
			return newArgs
		}
	}
	return result
}

var ffmpegNoisePatterns = []string{
	"non-existing PPS",
	"non-existing SPS",
	"no frame!",
	"skipping",
	"Skipping",
	"missing picture",
	"concealing",
	"decode_slice_header",
	"error while decoding",
	"missing reference picture",
	"reference picture reordering",
	"Last message repeated",
	"undecodable NALU",
}

func IsFFmpegNoise(line string) bool {
	for _, pattern := range ffmpegNoisePatterns {
		if strings.Contains(line, pattern) {
			return true
		}
	}
	return false
}

var nonAlphanumRe = regexp.MustCompile(`[^a-zA-Z0-9]+`)

func SanitizeFilename(title string, t time.Time) string {
	name := nonAlphanumRe.ReplaceAllString(title, "_")
	name = strings.Trim(name, "_")
	if len(name) > 60 {
		name = name[:60]
	}
	if name == "" {
		name = "recording"
	}
	return name + "_" + t.Format("20060102_1504")
}
