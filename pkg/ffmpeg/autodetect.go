package ffmpeg

import (
	"strings"
)

func isRTSPURL(url string) bool {
	return strings.HasPrefix(url, "rtsp://") || strings.HasPrefix(url, "rtsps://")
}

func isHTTPURL(url string) bool {
	return strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://")
}

func isInterlaced(fieldOrder string) bool {
	switch fieldOrder {
	case "tt", "bb", "tb", "bt":
		return true
	}
	return false
}

func audioEncoder(probe *ProbeResult) []string {
	s := settings()
	if probe == nil || len(probe.AudioTracks) == 0 {
		return []string{"-c:a", "aac", "-ac", "2", "-b:a", s.AudioBitrate}
	}
	isStereoOrMono := probe.AudioTracks[0].Channels <= 2
	switch probe.AudioTracks[0].Codec {
	case "aac":
		if isStereoOrMono {
			return []string{"-c:a", "copy"}
		}
		return []string{"-c:a", "aac", "-ac", "2", "-b:a", s.AudioBitrate}
	default:
		return []string{"-c:a", "aac", "-ac", "2", "-b:a", s.AudioBitrate}
	}
}
