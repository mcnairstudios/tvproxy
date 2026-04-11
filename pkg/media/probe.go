package media

type VideoInfo struct {
	Codec          string `json:"codec"`
	Profile        string `json:"profile,omitempty"`
	PixFmt         string `json:"pix_fmt,omitempty"`
	ColorSpace     string `json:"color_space,omitempty"`
	ColorTransfer  string `json:"color_transfer,omitempty"`
	ColorPrimaries string `json:"color_primaries,omitempty"`
	FieldOrder     string `json:"field_order,omitempty"`
	FPS            string `json:"fps,omitempty"`
	BitRate        string `json:"bit_rate,omitempty"`
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

func NormalizeVideoCodec(codec string) string {
	switch codec {
	case "hevc":
		return "h265"
	case "mpeg2video":
		return "mpeg2"
	}
	return codec
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
