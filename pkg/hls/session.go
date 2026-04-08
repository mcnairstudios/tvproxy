package hls

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog"
)

type ProfileSettings struct {
	VideoCodec  string
	AudioCodec  string
	HWAccel     string
	Container   string
	Deinterlace bool
	AutoDetect  bool
}

type Session struct {
	ID            string
	StreamURL     string
	OutputDir     string
	SegmentLength int
	DurationTicks int64
	IsLive        bool
	Profile       ProfileSettings
	mu            sync.Mutex
	cmd           *exec.Cmd
	cancel        context.CancelFunc
	done          chan struct{}
	startNumber   int
	lastAccess    time.Time
	log           zerolog.Logger
}

func NewSession(id, streamURL, outputDir string, segmentLength int, durationTicks int64, isLive bool, profile ProfileSettings, log zerolog.Logger) *Session {
	return &Session{
		ID:            id,
		StreamURL:     streamURL,
		OutputDir:     outputDir,
		SegmentLength: segmentLength,
		DurationTicks: durationTicks,
		IsLive:        isLive,
		Profile:       profile,
		lastAccess:    time.Now(),
		log:           log,
	}
}

func (s *Session) StartTranscode(ctx context.Context, startNumber int, startTimeTicks int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.stopLocked()

	if err := os.MkdirAll(s.OutputDir, 0755); err != nil {
		return fmt.Errorf("creating hls output dir: %w", err)
	}

	rctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.startNumber = startNumber
	s.done = make(chan struct{})

	args := s.buildFFmpegArgs(startNumber, startTimeTicks)

	s.log.Info().
		Str("session", s.ID).
		Int("start_number", startNumber).
		Int64("start_ticks", startTimeTicks).
		Msg("starting hls transcode")

	s.cmd = exec.CommandContext(rctx, "ffmpeg", args...)
	s.cmd.Cancel = func() error {
		return s.cmd.Process.Signal(syscall.SIGTERM)
	}
	s.cmd.WaitDelay = 5 * time.Second
	s.cmd.Stderr = os.Stderr

	if err := s.cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("starting ffmpeg: %w", err)
	}

	done := s.done
	go func() {
		defer close(done)
		s.cmd.Wait()
	}()

	return nil
}

func (s *Session) segmentExt() string {
	if s.Profile.Container == "mpegts" {
		return ".ts"
	}
	return ".mp4"
}

func (s *Session) buildFFmpegArgs(startNumber int, startTimeTicks int64) []string {
	var args []string

	args = append(args, "-hide_banner", "-loglevel", "warning")

	hwaccel := s.Profile.HWAccel
	if hwaccel != "" && hwaccel != "none" && hwaccel != "default" {
		switch hwaccel {
		case "vaapi":
			args = append(args,
				"-init_hw_device", "vaapi=va:/dev/dri/renderD128",
				"-filter_hw_device", "va",
			)
		case "qsv":
			args = append(args,
				"-init_hw_device", "qsv=qsv:hw",
				"-filter_hw_device", "qsv",
			)
		case "cuda", "nvenc":
			args = append(args, "-hwaccel", "cuda", "-hwaccel_output_format", "cuda")
		case "videotoolbox":
			args = append(args, "-hwaccel", "videotoolbox")
		}
	}

	if startTimeTicks > 0 {
		secs := float64(startTimeTicks) / 10000000.0
		args = append(args, "-ss", fmt.Sprintf("%.3f", secs))
	}

	args = append(args,
		"-analyzeduration", "3000000",
		"-probesize", "3000000",
		"-i", s.StreamURL,
	)

	videoCodec := s.Profile.VideoCodec
	if videoCodec == "" || videoCodec == "copy" {
		videoCodec = "copy"
	}
	audioCodec := s.Profile.AudioCodec
	if audioCodec == "" || audioCodec == "copy" {
		audioCodec = "copy"
	}

	if s.Profile.Deinterlace && videoCodec == "copy" {
		videoCodec = "libx264"
	}

	args = append(args,
		"-map_metadata", "-1",
		"-map_chapters", "-1",
		"-threads", "0",
		"-map", "0:v:0?",
		"-map", "0:a:0?",
		"-c:v", videoCodec,
	)

	if videoCodec != "copy" && videoCodec != "libx264" {
		args = append(args, "-tag:v:0", "hvc1")
	}

	if videoCodec == "copy" {
		args = append(args, "-start_at_zero")
	}

	if s.Profile.Deinterlace && videoCodec != "copy" {
		args = append(args, "-vf", "yadif")
	}

	args = append(args, "-c:a", audioCodec)
	if audioCodec != "copy" {
		args = append(args, "-b:a", "192k", "-ac", "2")
	}

	args = append(args,
		"-copyts",
		"-avoid_negative_ts", "disabled",
		"-max_muxing_queue_size", "2048",
		"-f", "hls",
		"-max_delay", "5000000",
		"-hls_time", fmt.Sprintf("%d", s.SegmentLength),
	)

	ext := s.segmentExt()
	if s.Profile.Container == "mpegts" {
		args = append(args, "-hls_segment_type", "mpegts")
	} else {
		args = append(args,
			"-hls_segment_type", "fmp4",
			"-hls_fmp4_init_filename", "init.mp4",
			"-hls_segment_options", "movflags=+frag_discont",
		)
	}

	args = append(args,
		"-start_number", fmt.Sprintf("%d", startNumber),
		"-hls_playlist_type", "event",
		"-hls_list_size", "0",
	)

	segPattern := filepath.Join(s.OutputDir, "seg%d"+ext)
	playlistPath := filepath.Join(s.OutputDir, "playlist.m3u8")

	args = append(args,
		"-hls_segment_filename", segPattern,
		"-y", playlistPath,
	)

	return args
}

func (s *Session) SegmentPath(index int) string {
	return filepath.Join(s.OutputDir, fmt.Sprintf("seg%d%s", index, s.segmentExt()))
}

func (s *Session) InitSegmentPath() string {
	return filepath.Join(s.OutputDir, "init.mp4")
}

func (s *Session) WaitForSegment(index int, timeout time.Duration) error {
	segPath := s.SegmentPath(index)
	nextPath := s.SegmentPath(index + 1)
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if _, err := os.Stat(segPath); err == nil {
			if s.IsDone() {
				return nil
			}
			if _, err := os.Stat(nextPath); err == nil {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	if _, err := os.Stat(segPath); err == nil {
		return nil
	}

	return fmt.Errorf("segment %d not ready after %v", index, timeout)
}

func (s *Session) CurrentTranscodeIndex() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	highest := -1
	entries, err := os.ReadDir(s.OutputDir)
	if err != nil {
		return -1
	}
	ext := s.segmentExt()
	pattern := "seg%d" + ext
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		var idx int
		if _, err := fmt.Sscanf(e.Name(), pattern, &idx); err == nil {
			if idx > highest {
				highest = idx
			}
		}
	}
	return highest
}

func (s *Session) Touch() {
	s.mu.Lock()
	s.lastAccess = time.Now()
	s.mu.Unlock()
}

func (s *Session) IdleSince() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Since(s.lastAccess)
}

func (s *Session) IsDone() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done == nil {
		return true
	}
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

func (s *Session) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
}

func (s *Session) stopLocked() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.done != nil {
		<-s.done
		s.done = nil
	}
	s.cmd = nil
}
