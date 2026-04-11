package avprobe

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"os/exec"
	"strconv"

	"github.com/gavinmcnair/tvproxy/pkg/media"
)

func ProbeReader(ctx context.Context, reader io.Reader) (*media.ProbeResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*secondDuration)
	defer cancel()

	args := []string{
		"-v", "error",
		"-analyzeduration", "2000000",
		"-probesize", "2000000",
		"-print_format", "json",
		"-show_entries", "stream=index,codec_name,codec_type,width,height,r_frame_rate,field_order,profile,sample_rate,channels,bit_rate,disposition:stream_tags=language:format=duration,format_name",
		"pipe:0",
	}

	cmd := exec.CommandContext(ctx, "ffprobe", args...)
	cmd.Stdin = reader
	out, err := cmd.Output()
	if err != nil {
		return &media.ProbeResult{IsVOD: false}, nil
	}

	return parseFFprobeOutput(out)
}

const secondDuration = 1000000000

type ffprobeOutput struct {
	Format  ffprobeFormat   `json:"format"`
	Streams []ffprobeStream `json:"streams"`
}

type ffprobeFormat struct {
	FormatName string `json:"format_name"`
	Duration   string `json:"duration"`
}

type ffprobeStream struct {
	CodecType   string            `json:"codec_type"`
	CodecName   string            `json:"codec_name"`
	Profile     string            `json:"profile"`
	Index       int               `json:"index"`
	Width       int               `json:"width"`
	Height      int               `json:"height"`
	RFrameRate  string            `json:"r_frame_rate"`
	SampleRate  string            `json:"sample_rate"`
	Channels    int               `json:"channels"`
	BitRate     string            `json:"bit_rate"`
	Tags        map[string]string `json:"tags"`
}

func parseFFprobeOutput(out []byte) (*media.ProbeResult, error) {
	var probe ffprobeOutput
	if err := json.Unmarshal(out, &probe); err != nil {
		return &media.ProbeResult{IsVOD: false}, nil
	}

	result := &media.ProbeResult{
		FormatName: media.NormalizeContainer(probe.Format.FormatName),
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
			result.Video = &media.VideoInfo{
				Codec:   s.CodecName,
				Profile: s.Profile,
				BitRate: s.BitRate,
			}
		}
		if s.CodecType == "audio" {
			lang := ""
			if s.Tags != nil {
				lang = s.Tags["language"]
			}
			result.AudioTracks = append(result.AudioTracks, media.AudioTrack{
				Index:      audioIdx,
				Language:   lang,
				Codec:      s.CodecName,
				SampleRate: s.SampleRate,
				Channels:   s.Channels,
				BitRate:    s.BitRate,
			})
			audioIdx++
		}
	}

	return result, nil
}
