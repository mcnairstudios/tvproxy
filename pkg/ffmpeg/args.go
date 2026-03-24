package ffmpeg

import (
	"fmt"
	"regexp"
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
