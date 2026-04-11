package ffmpeg

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os/exec"
	"strconv"
	"strings"

	"github.com/gavinmcnair/tvproxy/pkg/defaults"
)

func StreamHash(url string) string {
	sum := sha256.Sum256([]byte(url))
	return fmt.Sprintf("%x", sum)[:16]
}

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

type ffprobeOutput struct {
	Format  ffprobeFormat   `json:"format"`
	Streams []ffprobeStream `json:"streams"`
}

type ffprobeFormat struct {
	FormatName string `json:"format_name"`
	Duration   string `json:"duration"`
}

type ffprobeStream struct {
	CodecType      string             `json:"codec_type"`
	CodecName      string             `json:"codec_name"`
	Profile        string             `json:"profile"`
	Index          int                `json:"index"`
	Width          int                `json:"width"`
	Height         int                `json:"height"`
	PixFmt         string             `json:"pix_fmt"`
	ColorSpace     string             `json:"color_space"`
	ColorTransfer  string             `json:"color_transfer"`
	ColorPrimaries string             `json:"color_primaries"`
	FieldOrder     string             `json:"field_order"`
	RFrameRate     string             `json:"r_frame_rate"`
	SampleRate     string             `json:"sample_rate"`
	Channels       int                `json:"channels"`
	BitRate        string             `json:"bit_rate"`
	Tags           map[string]string  `json:"tags"`
	Disposition    StreamDisposition  `json:"disposition"`
}

func NormalizeVideoCodec(ffprobeCodec string) string {
	switch ffprobeCodec {
	case "hevc":
		return "h265"
	default:
		return ffprobeCodec
	}
}

func NormalizeContainer(formatName string) string {
	switch {
	case strings.Contains(formatName, "mp4") || strings.Contains(formatName, "mov"):
		return "mp4"
	case strings.Contains(formatName, "mpegts"):
		return "mpegts"
	case strings.Contains(formatName, "matroska"):
		return "matroska"
	case strings.Contains(formatName, "webm"):
		return "webm"
	default:
		return formatName
	}
}

func simplifyFrameRate(rate string) string {
	parts := strings.SplitN(rate, "/", 2)
	if len(parts) != 2 {
		return rate
	}
	num, err1 := strconv.ParseFloat(parts[0], 64)
	den, err2 := strconv.ParseFloat(parts[1], 64)
	if err1 != nil || err2 != nil || den == 0 {
		return rate
	}
	fps := num / den
	if fps <= 0 || fps > 300 {
		return ""
	}
	if fps == float64(int(fps)) {
		return strconv.Itoa(int(fps))
	}
	return strconv.FormatFloat(fps, 'f', 2, 64)
}

func Probe(ctx context.Context, url, userAgent string, extraHeaders ...string) (*ProbeResult, error) {
	s := settings()
	ctx, cancel := context.WithTimeout(ctx, s.ProbeTimeout)
	defer cancel()

	args := probeArgs(s, url)
	if userAgent != "" {
		args = append(args, "-user_agent", userAgent)
	}
	if len(extraHeaders) > 0 {
		var headerStr string
		for _, h := range extraHeaders {
			headerStr += h + "\r\n"
		}
		args = append(args, "-headers", headerStr)
	}
	args = append(args, url)

	cmd := exec.CommandContext(ctx, "ffprobe", args...)
	out, err := cmd.Output()
	if err != nil {
		return &ProbeResult{IsVOD: false}, nil
	}

	return parseProbeOutput(out)
}

func ProbeReader(ctx context.Context, reader io.Reader) (*ProbeResult, error) {
	s := settings()
	ctx, cancel := context.WithTimeout(ctx, s.ProbeTimeout)
	defer cancel()

	args := probeArgs(s, "")
	args = append(args, "pipe:0")

	cmd := exec.CommandContext(ctx, "ffprobe", args...)
	cmd.Stdin = reader
	out, err := cmd.Output()
	if err != nil {
		return &ProbeResult{IsVOD: false}, nil
	}

	return parseProbeOutput(out)
}

func probeArgs(s *defaults.FFmpegSettings, url string) []string {
	analyzeDuration := s.AnalyzeDuration
	probeSize := s.ProbeSize

	args := []string{"-v", "error"}

	if strings.HasPrefix(url, "rtsp://") || strings.HasPrefix(url, "rtsps://") {
		args = append(args, "-rtsp_transport", "tcp")
		analyzeDuration = 3000000
		probeSize = 3000000
	} else if IsHTTPURL(url) {
		analyzeDuration = 0
		probeSize = 32
	}

	args = append(args,
		"-analyzeduration", strconv.Itoa(analyzeDuration),
		"-probesize", strconv.Itoa(probeSize),
		"-print_format", "json",
		"-show_entries", "stream=index,codec_name,codec_type,width,height,r_frame_rate,field_order,pix_fmt,color_space,color_transfer,color_primaries,profile,sample_rate,channels,bit_rate,disposition:stream_tags=language:format=duration,format_name",
	)
	return args
}

func parseProbeOutput(out []byte) (*ProbeResult, error) {
	var probe ffprobeOutput
	if err := json.Unmarshal(out, &probe); err != nil {
		return &ProbeResult{IsVOD: false}, nil
	}

	result := &ProbeResult{
		FormatName: NormalizeContainer(probe.Format.FormatName),
	}

	if probe.Format.Duration != "" {
		d, err := strconv.ParseFloat(probe.Format.Duration, 64)
		if err == nil && d > 0 && !math.IsInf(d, 0) && !math.IsNaN(d) {
			result.Duration = d
			result.IsVOD = true
		}
	}

	audioIdx := 0
	for _, s := range probe.Streams {
		if s.CodecType == "video" && s.Width > 0 && result.Width == 0 {
			result.Width = s.Width
			result.Height = s.Height
			result.HasVideo = true
			result.Video = &VideoInfo{
				Codec:          s.CodecName,
				Profile:        s.Profile,
				PixFmt:         s.PixFmt,
				ColorSpace:     s.ColorSpace,
				ColorTransfer:  s.ColorTransfer,
				ColorPrimaries: s.ColorPrimaries,
				FieldOrder:     s.FieldOrder,
				FPS:            simplifyFrameRate(s.RFrameRate),
				BitRate:        s.BitRate,
			}
		}
		if s.CodecType == "audio" && !s.Disposition.IsSkippable() {
			lang := ""
			if s.Tags != nil {
				lang = s.Tags["language"]
			}
			result.AudioTracks = append(result.AudioTracks, AudioTrack{
				Index:       audioIdx,
				Language:    lang,
				Codec:       s.CodecName,
				Profile:     s.Profile,
				SampleRate:  s.SampleRate,
				Channels:    s.Channels,
				BitRate:     s.BitRate,
				Disposition: s.Disposition,
			})
			audioIdx++
		}
	}

	return result, nil
}
