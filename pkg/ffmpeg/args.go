package ffmpeg

import (
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

func IsHTTPURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func IsRTSPURL(s string) bool {
	return strings.HasPrefix(s, "rtsp://") || strings.HasPrefix(s, "rtsps://")
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

func MapEncoder(codec string) string {
	switch codec {
	case "", "copy":
		return "copy"
	case "h264":
		return "libx264"
	case "h265":
		return "libx265"
	case "av1":
		return "libsvtav1"
	default:
		return codec
	}
}
