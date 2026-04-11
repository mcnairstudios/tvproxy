package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/gavinmcnair/tvproxy/pkg/avprobe"
	"github.com/gavinmcnair/tvproxy/pkg/gstreamer"
	"github.com/gavinmcnair/tvproxy/pkg/media"
)

func (s *VODService) ProbeFile(ctx context.Context, streamURL, filePath string) (*media.ProbeResult, error) {
	if streamURL != "" {
		cached, _ := s.probeCache.GetProbe(media.StreamHash(streamURL))
		if cached != nil {
			return cached, nil
		}
	}
	result, err := avprobe.Probe(ctx, filePath, "")
	if err != nil {
		return nil, err
	}
	if streamURL != "" && result != nil {
		s.probeCache.SaveProbe(media.StreamHash(streamURL), result)
	}
	return result, nil
}

func (s *VODService) ProbeStream(ctx context.Context, streamID string) (*media.ProbeResult, error) {
	stream, err := s.streamStore.GetByID(ctx, streamID)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrStreamNotFound, err)
	}
	return avprobe.Probe(ctx, stream.URL, s.config.UserAgent)
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

	probe, probeErr := s.cachedOrFreshProbe(ctx, filePath)
	if probeErr == nil && probe != nil && probe.Video != nil {
		fileCodec := media.NormalizeVideoCodec(probe.Video.Codec)
		fileContainer := media.NormalizeContainer(probe.FormatName)
		videoMatch := sp.VideoCodec == "copy" || sp.VideoCodec == fileCodec
		containerMatch := sp.Container == "" || sp.Container == fileContainer
		if videoMatch && containerMatch {
			f, err := os.Open(filePath)
			if err != nil {
				return nil, "", err
			}
			return f, containerContentType(fileContainer), nil
		}
	}

	var command string
	var args []string

	if gstreamer.Available() {
		outFormat := gstreamer.OutputMP4
		if sp.Container == "mpegts" {
			outFormat = gstreamer.OutputMPEGTS
		}
		outVideo := sp.VideoCodec
		if outVideo == "" {
			outVideo = "copy"
		}
		pipeline := gstreamer.BuildFromProbe(probe, filePath, gstreamer.PipelineOpts{
			InputType:        "file",
			IsLive:           false,
			OutputVideoCodec: outVideo,
			OutputAudioCodec: "aac",
			OutputFormat:     outFormat,
			HWAccel:          gstreamer.HWAccel(sp.HWAccel),
		})
		command = pipeline.Cmd
		args = pipeline.Args
	} else {
		args = media.ShellSplit(sp.Args)
		for i, arg := range args {
			if arg == "{input}" {
				args[i] = filePath
			}
		}
		args = append([]string{"-y"}, args...)
		command = sp.Command
		if command == "" {
			command = "ffmpeg"
		}
	}

	cmd := exec.CommandContext(ctx, command, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, "", fmt.Errorf("creating stdout pipe: %w", err)
	}
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return nil, "", fmt.Errorf("starting transcode: %w", err)
	}

	contentType := containerContentType(sp.Container)
	return &cmdReadCloser{ReadCloser: stdout, cmd: cmd}, contentType, nil
}

func (s *VODService) cachedOrFreshProbe(ctx context.Context, filePath string) (*media.ProbeResult, error) {
	hash := media.StreamHash(filePath)
	if cached, err := s.probeCache.GetProbe(hash); err == nil && cached != nil {
		return cached, nil
	}
	result, err := avprobe.Probe(ctx, filePath, "")
	if err == nil && result != nil {
		s.probeCache.SaveProbe(hash, result)
	}
	return result, err
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
