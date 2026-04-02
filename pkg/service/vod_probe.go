package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/gavinmcnair/tvproxy/pkg/ffmpeg"
)

func (s *VODService) ProbeFile(ctx context.Context, streamURL, filePath string) (*ffmpeg.ProbeResult, error) {
	if streamURL != "" {
		cached, _ := s.recordingStore.GetProbe(ffmpeg.StreamHash(streamURL))
		if cached != nil {
			return cached, nil
		}
	}
	result, err := ffmpeg.Probe(ctx, filePath, "")
	if err != nil {
		return nil, err
	}
	if streamURL != "" && result != nil {
		s.recordingStore.SaveProbe(ffmpeg.StreamHash(streamURL), result)
	}
	return result, nil
}

func (s *VODService) ProbeStream(ctx context.Context, streamID string) (*ffmpeg.ProbeResult, error) {
	stream, err := s.streamStore.GetByID(ctx, streamID)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrStreamNotFound, err)
	}
	return ffmpeg.Probe(ctx, stream.URL, s.config.UserAgent)
}

func (s *VODService) TranscodeFile(ctx context.Context, filePath, profileName string) (io.ReadCloser, string, error) {
	sp, err := s.streamProfileRepo.GetByName(ctx, profileName)
	if err != nil {
		return nil, "", fmt.Errorf("profile %q not found: %w", profileName, err)
	}

	if sp.Args == "" {
		f, err := os.Open(filePath)
		if err != nil {
			return nil, "", err
		}
		return f, "video/mp4", nil
	}

	probe, probeErr := ffmpeg.Probe(ctx, filePath, "")
	if probeErr == nil && probe.Video != nil {
		fileCodec := ffmpeg.NormalizeVideoCodec(probe.Video.Codec)
		videoMatch := sp.VideoCodec == "copy" || sp.VideoCodec == fileCodec
		containerMatch := sp.Container == probe.FormatName
		if videoMatch && containerMatch {
			f, err := os.Open(filePath)
			if err != nil {
				return nil, "", err
			}
			return f, containerContentType(probe.FormatName), nil
		}
	}

	args := ffmpeg.ShellSplit(sp.Args)
	for i, arg := range args {
		if arg == "{input}" {
			args[i] = filePath
		}
	}
	args = append([]string{"-y"}, args...)

	cmd := exec.CommandContext(ctx, sp.Command, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, "", fmt.Errorf("creating stdout pipe: %w", err)
	}
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return nil, "", fmt.Errorf("starting ffmpeg transcode: %w", err)
	}

	contentType := containerContentType(sp.Container)
	return &cmdReadCloser{ReadCloser: stdout, cmd: cmd}, contentType, nil
}

func containerContentType(container string) string {
	switch container {
	case "mp4":
		return "video/mp4"
	case "mpegts":
		return "video/MP2T"
	case "matroska":
		return "video/x-matroska"
	case "webm":
		return "video/webm"
	default:
		return "video/mp4"
	}
}

type cmdReadCloser struct {
	io.ReadCloser
	cmd *exec.Cmd
}

func (c *cmdReadCloser) Close() error {
	c.ReadCloser.Close()
	return c.cmd.Wait()
}
