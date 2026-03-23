package ffmpeg

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"os/exec"
	"strconv"
	"strings"

	"github.com/gavinmcnair/tvproxy/pkg/defaults"
)

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

type AudioTrack struct {
	Index      int    `json:"index"`
	Language   string `json:"language"`
	Codec      string `json:"codec"`
	Profile    string `json:"profile,omitempty"`
	SampleRate string `json:"sample_rate,omitempty"`
	Channels   int    `json:"channels,omitempty"`
	BitRate    string `json:"bit_rate,omitempty"`
}

type ProbeResult struct {
	Duration    float64      `json:"duration"`
	IsVOD       bool         `json:"is_vod"`
	Width       int          `json:"width"`
	Height      int          `json:"height"`
	Video       *VideoInfo   `json:"video,omitempty"`
	AudioTracks []AudioTrack `json:"audio_tracks,omitempty"`
}

type ffprobeOutput struct {
	Format  ffprobeFormat   `json:"format"`
	Streams []ffprobeStream `json:"streams"`
}

type ffprobeFormat struct {
	Duration string `json:"duration"`
}

type ffprobeStream struct {
	CodecType      string            `json:"codec_type"`
	CodecName      string            `json:"codec_name"`
	Profile        string            `json:"profile"`
	Index          int               `json:"index"`
	Width          int               `json:"width"`
	Height         int               `json:"height"`
	PixFmt         string            `json:"pix_fmt"`
	ColorSpace     string            `json:"color_space"`
	ColorTransfer  string            `json:"color_transfer"`
	ColorPrimaries string            `json:"color_primaries"`
	FieldOrder     string            `json:"field_order"`
	RFrameRate     string            `json:"r_frame_rate"`
	SampleRate     string            `json:"sample_rate"`
	Channels       int               `json:"channels"`
	BitRate        string            `json:"bit_rate"`
	Tags           map[string]string `json:"tags"`
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
	if fps == float64(int(fps)) {
		return strconv.Itoa(int(fps))
	}
	return strconv.FormatFloat(fps, 'f', 2, 64)
}

func Probe(ctx context.Context, url, userAgent string) (*ProbeResult, error) {
	s := settings()
	ctx, cancel := context.WithTimeout(ctx, s.ProbeTimeout)
	defer cancel()

	args := probeArgs(s)
	if userAgent != "" {
		args = append(args, "-user_agent", userAgent)
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

	args := probeArgs(s)
	args = append(args, "pipe:0")

	cmd := exec.CommandContext(ctx, "ffprobe", args...)
	cmd.Stdin = reader
	out, err := cmd.Output()
	if err != nil {
		return &ProbeResult{IsVOD: false}, nil
	}

	return parseProbeOutput(out)
}

func probeArgs(s *defaults.FFmpegSettings) []string {
	return []string{
		"-v", "quiet",
		"-analyzeduration", strconv.Itoa(s.AnalyzeDuration),
		"-probesize", strconv.Itoa(s.ProbeSize),
		"-print_format", "json",
		"-show_format",
		"-show_streams",
	}
}

func parseProbeOutput(out []byte) (*ProbeResult, error) {
	var probe ffprobeOutput
	if err := json.Unmarshal(out, &probe); err != nil {
		return &ProbeResult{IsVOD: false}, nil
	}

	result := &ProbeResult{}

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
		if s.CodecType == "audio" {
			lang := ""
			if s.Tags != nil {
				lang = s.Tags["language"]
			}
			result.AudioTracks = append(result.AudioTracks, AudioTrack{
				Index:      audioIdx,
				Language:   lang,
				Codec:      s.CodecName,
				Profile:    s.Profile,
				SampleRate: s.SampleRate,
				Channels:   s.Channels,
				BitRate:    s.BitRate,
			})
			audioIdx++
		}
	}

	return result, nil
}
