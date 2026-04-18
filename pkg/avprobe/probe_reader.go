package avprobe

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"os/exec"
	"strconv"
	"strings"

	"github.com/gavinmcnair/tvproxy/pkg/media"
)

func splitFraction(s string) []string {
	return strings.SplitN(s, "/", 2)
}

func ProbeReader(ctx context.Context, reader io.Reader) (*media.ProbeResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*secondDuration)
	defer cancel()

	args := []string{
		"-v", "error",
		"-analyzeduration", "2000000",
		"-probesize", "2000000",
		"-print_format", "json",
		"-show_entries", "stream=index,codec_name,codec_type,width,height,r_frame_rate,field_order,profile,pix_fmt,bits_per_raw_sample,color_space,color_transfer,color_primaries,start_time,duration,sample_rate,channels,bit_rate,disposition:stream_tags=language:format=duration,format_name",
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
	CodecType       string            `json:"codec_type"`
	CodecName       string            `json:"codec_name"`
	Profile         string            `json:"profile"`
	Index           int               `json:"index"`
	Width           int               `json:"width"`
	Height          int               `json:"height"`
	RFrameRate      string            `json:"r_frame_rate"`
	FieldOrder      string            `json:"field_order"`
	PixFmt          string            `json:"pix_fmt"`
	BitsPerRawSample string           `json:"bits_per_raw_sample"`
	ColorSpace       string            `json:"color_space"`
	ColorTransfer    string            `json:"color_transfer"`
	ColorPrimaries   string            `json:"color_primaries"`
	StreamStartTime  string            `json:"start_time"`
	StreamDuration   string            `json:"duration"`
	SampleRate       string            `json:"sample_rate"`
	Channels         int               `json:"channels"`
	BitRate          string                  `json:"bit_rate"`
	Disposition      media.StreamDisposition `json:"disposition"`
	Tags             map[string]string       `json:"tags"`
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
			interlaced := s.FieldOrder != "" && s.FieldOrder != "progressive" && s.FieldOrder != "unknown"
			bitDepth := 0
			if s.BitsPerRawSample != "" {
				bitDepth, _ = strconv.Atoi(s.BitsPerRawSample)
			}
			fps := ""
			if s.RFrameRate != "" {
				parts := splitFraction(s.RFrameRate)
				if len(parts) == 2 {
					num, _ := strconv.ParseFloat(parts[0], 64)
					den, _ := strconv.ParseFloat(parts[1], 64)
					if den > 0 {
						fpsVal := num / den
						if fpsVal > 0 && fpsVal < 300 {
							if fpsVal == float64(int(fpsVal)) {
								fps = strconv.Itoa(int(fpsVal))
							} else {
								fps = strconv.FormatFloat(fpsVal, 'f', 2, 64)
							}
						}
					}
				}
			}
			videoStartTime := 0.0
			if s.StreamStartTime != "" {
				videoStartTime, _ = strconv.ParseFloat(s.StreamStartTime, 64)
			}
			videoDuration := 0.0
			if s.StreamDuration != "" {
				videoDuration, _ = strconv.ParseFloat(s.StreamDuration, 64)
			}
			result.Video = &media.VideoInfo{
				Codec:          s.CodecName,
				Profile:        s.Profile,
				PixFmt:         s.PixFmt,
				BitDepth:       bitDepth,
				Interlaced:     interlaced,
				ColorSpace:     s.ColorSpace,
				ColorTransfer:  s.ColorTransfer,
				ColorPrimaries: s.ColorPrimaries,
				FieldOrder:     s.FieldOrder,
				FPS:            fps,
				BitRate:        s.BitRate,
				StartTime:      videoStartTime,
				Duration:       videoDuration,
			}
		}
		if s.CodecType == "audio" {
			lang := ""
			if s.Tags != nil {
				lang = s.Tags["language"]
			}
			audioStartTime := 0.0
			if s.StreamStartTime != "" {
				audioStartTime, _ = strconv.ParseFloat(s.StreamStartTime, 64)
			}
			audioDuration := 0.0
			if s.StreamDuration != "" {
				audioDuration, _ = strconv.ParseFloat(s.StreamDuration, 64)
			}
			result.AudioTracks = append(result.AudioTracks, media.AudioTrack{
				Index:       audioIdx,
				Language:    lang,
				Codec:       s.CodecName,
				Profile:     s.Profile,
				SampleRate:  s.SampleRate,
				Channels:    s.Channels,
				BitRate:     s.BitRate,
				StartTime:   audioStartTime,
				Duration:    audioDuration,
				Disposition: s.Disposition,
			})
			audioIdx++
		}
	}

	return result, nil
}
