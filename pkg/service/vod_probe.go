package service

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/gavinmcnair/tvproxy/pkg/media"
)

func (s *VODService) ProbeFile(ctx context.Context, streamURL, filePath string) (*media.ProbeResult, error) {
	if streamURL != "" {
		id := media.StreamID(streamURL)
		cached, _ := s.probeCache.GetProbe(id)
		if cached != nil {
			return cached, nil
		}
	}
	id := media.StreamID(filePath)
	cached, _ := s.probeCache.GetProbe(id)
	if cached != nil {
		return cached, nil
	}
	return &media.ProbeResult{}, nil
}

func (s *VODService) ProbeStream(ctx context.Context, streamID string) (*media.ProbeResult, error) {
	_, err := s.streamStore.GetByID(ctx, streamID)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrStreamNotFound, err)
	}
	if s.probeCache != nil {
		cached, _ := s.probeCache.GetProbe(streamID)
		if cached != nil {
			return cached, nil
		}
	}
	return &media.ProbeResult{}, nil
}

func (s *VODService) DeleteProbe(ctx context.Context, streamID string) error {
	if s.probeCache != nil {
		return s.probeCache.DeleteProbe(streamID)
	}
	return nil
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

	probe, _ := s.cachedOrFreshProbe(ctx, filePath)
	if probe != nil && probe.Video != nil {
		fileCodec := media.NormalizeCodec(probe.Video.Codec)
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

	format := "mp4"
	if sp.Container == "mpegts" {
		format = "mpegts"
	}
	videoCodec := sp.VideoCodec
	if videoCodec == "" {
		videoCodec = "copy"
	}

	reader, err := StartAVPipeline(ctx, AVPipelineOpts{
		URL:        filePath,
		Format:     format,
		VideoCodec: videoCodec,
		AudioCodec: "aac",
		HWAccel:    sp.HWAccel,
		IsLive:     false,
		Log:        s.log,
	})
	if err != nil {
		return nil, "", fmt.Errorf("starting transcode: %w", err)
	}

	contentType := containerContentType(sp.Container)
	return reader, contentType, nil
}

func (s *VODService) cachedOrFreshProbe(ctx context.Context, filePath string) (*media.ProbeResult, error) {
	id := media.StreamID(filePath)
	if cached, err := s.probeCache.GetProbe(id); err == nil && cached != nil {
		return cached, nil
	}
	return nil, nil
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

