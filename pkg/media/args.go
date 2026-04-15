package media

import (
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

var streamNamespace = uuid.MustParse("f47ac10b-58cc-4372-a567-0e02b2c3d479")

func StreamID(url string) string {
	return uuid.NewSHA1(streamNamespace, []byte(url)).String()
}

func IsHTTPURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func IsRTSPURL(s string) bool {
	return strings.HasPrefix(s, "rtsp://") || strings.HasPrefix(s, "rtsps://")
}

func ShellSplit(s string) []string {
	var args []string
	var current strings.Builder
	inQuote := false
	quoteChar := byte(0)

	for i := 0; i < len(s); i++ {
		c := s[i]
		if inQuote {
			if c == quoteChar {
				inQuote = false
			} else {
				current.WriteByte(c)
			}
		} else if c == '\'' || c == '"' {
			inQuote = true
			quoteChar = c
		} else if c == ' ' || c == '\t' {
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		} else {
			current.WriteByte(c)
		}
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}

func SanitizeFilename(title string, t time.Time) string {
	ts := t.Format("20060102_1504")
	clean := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' || r == '-' || r == '_' || r == '(' || r == ')' {
			return r
		}
		return '_'
	}, title)
	clean = strings.TrimSpace(clean)
	if clean == "" {
		clean = "recording"
	}
	return strings.ReplaceAll(clean, " ", "_") + "_" + ts
}

func IsFFmpegNoise(line string) bool {
	noisy := []string{
		"Last message repeated",
		"non-existing PPS",
		"mmco: unref short failure",
		"no frame!",
		"non-monotonic DTS",
		"Discarded",
	}
	for _, n := range noisy {
		if strings.Contains(line, n) {
			return true
		}
	}
	return false
}

func IsHEVC(codec string) bool {
	switch codec {
	case "libx265", "hevc", "hevc_vaapi", "hevc_qsv", "hevc_nvenc", "hevc_videotoolbox":
		return true
	}
	return false
}

func MapEncoderHW(codec, hwaccel string) string {
	switch hwaccel {
	case "vaapi":
		switch codec {
		case "h264":
			return "h264_vaapi"
		case "h265", "hevc":
			return "hevc_vaapi"
		case "av1":
			return "av1_vaapi"
		}
	case "qsv":
		switch codec {
		case "h264":
			return "h264_qsv"
		case "h265", "hevc":
			return "hevc_qsv"
		}
	case "nvenc", "cuda":
		switch codec {
		case "h264":
			return "h264_nvenc"
		case "h265", "hevc":
			return "hevc_nvenc"
		}
	case "videotoolbox":
		switch codec {
		case "h264":
			return "h264_videotoolbox"
		case "h265", "hevc":
			return "hevc_videotoolbox"
		}
	}
	switch codec {
	case "h264":
		return "libx264"
	case "h265", "hevc":
		return "libx265"
	case "av1":
		return "libsvtav1"
	}
	return codec
}

func DefaultContainer(videoCodec string) string {
	switch videoCodec {
	case "av1":
		return "matroska"
	default:
		return "mpegts"
	}
}
