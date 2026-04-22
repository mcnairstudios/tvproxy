package media

import "strings"

type VideoInfo struct {
	Codec          string  `json:"codec"`
	Profile        string  `json:"profile,omitempty"`
	PixFmt         string  `json:"pix_fmt,omitempty"`
	BitDepth       int     `json:"bit_depth,omitempty"`
	Interlaced     bool    `json:"interlaced,omitempty"`
	ColorSpace     string  `json:"color_space,omitempty"`
	ColorTransfer  string  `json:"color_transfer,omitempty"`
	ColorPrimaries string  `json:"color_primaries,omitempty"`
	FieldOrder     string  `json:"field_order,omitempty"`
	FPS            string  `json:"fps,omitempty"`
	BitRate        string  `json:"bit_rate,omitempty"`
	StartTime      float64 `json:"start_time,omitempty"`
	Duration       float64 `json:"duration,omitempty"`
}

type StreamDisposition struct {
	VisualImpaired int `json:"visual_impaired"`
	Dependent      int `json:"dependent"`
	Descriptions   int `json:"descriptions"`
}

func (d StreamDisposition) IsSkippable() bool {
	return d.VisualImpaired != 0 || d.Dependent != 0 || d.Descriptions != 0
}

type AudioTrack struct {
	Index       int                `json:"index"`
	Language    string             `json:"language"`
	Codec       string             `json:"codec"`
	Profile     string             `json:"profile,omitempty"`
	SampleRate  string             `json:"sample_rate,omitempty"`
	Channels    int                `json:"channels,omitempty"`
	BitRate     string             `json:"bit_rate,omitempty"`
	StartTime   float64            `json:"start_time,omitempty"`
	Duration    float64            `json:"duration,omitempty"`
	Disposition StreamDisposition  `json:"disposition,omitempty"`
}

type ProbeResult struct {
	Duration    float64      `json:"duration"`
	IsVOD       bool         `json:"is_vod"`
	Width       int          `json:"width"`
	Height      int          `json:"height"`
	HasVideo    bool         `json:"has_video"`
	FormatName  string       `json:"format_name,omitempty"`
	Video       *VideoInfo   `json:"video,omitempty"`
	AudioTracks []AudioTrack `json:"audio_tracks,omitempty"`
}

func NormalizeCodec(codec string) string {
	c := strings.ToLower(strings.TrimSpace(codec))
	switch {
	case c == "hevc" || strings.Contains(c, "h265") || strings.Contains(c, "h.265") || strings.Contains(c, "hevc"):
		return "h265"
	case c == "avc" || c == "mpeg-4 avc" || strings.Contains(c, "h264") || strings.Contains(c, "h.264"):
		return "h264"
	case c == "mpeg2" || c == "mpeg2video" || strings.Contains(c, "mpeg-2"):
		return "mpeg2video"
	case c == "mpeg4" || strings.Contains(c, "mpeg-4 visual"):
		return "mpeg4"
	case c == "aac_latm" || c == "mp4a-latm":
		return "aac_latm"
	case c == "aac" || c == "aac audio":
		return "aac"
	case c == "mp2" || strings.Contains(c, "mpeg audio"):
		return "mp2"
	case c == "ac3" || c == "ac-3" || c == "a_ac3":
		return "ac3"
	case c == "eac3" || c == "e-ac-3" || c == "a_eac3":
		return "eac3"
	case c == "dts" || c == "dca" || strings.Contains(c, "dts"):
		return "dts"
	case c == "truehd" || c == "mlp" || strings.Contains(c, "truehd"):
		return "truehd"
	case c == "opus" || c == "libopus":
		return "opus"
	case c == "flac":
		return "flac"
	case c == "vorbis" || c == "libvorbis":
		return "vorbis"
	case c == "av1 video" || c == "av1":
		return "av1"
	case c == "vp8":
		return "vp8"
	case c == "vp9":
		return "vp9"
	}
	return c
}

func NormalizeContainer(formatName string) string {
	switch formatName {
	case "mpegts":
		return "mpegts"
	case "matroska,webm":
		return "matroska"
	case "mov,mp4,m4a,3gp,3g2,mj2":
		return "mp4"
	}
	return formatName
}
