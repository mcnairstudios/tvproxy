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

type Remuxer struct {
	inputPath    string
	outputDir    string
	playlistPath string
	cmd          *exec.Cmd
	cancel       context.CancelFunc
	done         chan struct{}
	ready        chan struct{}
	readyOnce    sync.Once
	err          error
	log          zerolog.Logger
}

func NewRemuxer(inputPath, outputDir string, log zerolog.Logger) *Remuxer {
	return &Remuxer{
		inputPath:    inputPath,
		outputDir:    outputDir,
		playlistPath: filepath.Join(outputDir, "live.m3u8"),
		done:         make(chan struct{}),
		ready:        make(chan struct{}),
		log:          log,
	}
}

func (r *Remuxer) Start(ctx context.Context) error {
	if err := os.MkdirAll(r.outputDir, 0755); err != nil {
		return fmt.Errorf("creating hls output dir: %w", err)
	}

	rctx, cancel := context.WithCancel(ctx)
	r.cancel = cancel

	segPattern := filepath.Join(r.outputDir, "seg_%05d.mp4")

	r.cmd = exec.CommandContext(rctx, "ffmpeg",
		"-y", "-hide_banner", "-loglevel", "warning",
		"-re",
		"-i", r.inputPath,
		"-c", "copy",
		"-f", "hls",
		"-hls_segment_type", "fmp4",
		"-hls_time", "4",
		"-hls_list_size", "10",
		"-hls_flags", "delete_segments+append_list+program_date_time",
		"-hls_segment_filename", segPattern,
		r.playlistPath,
	)
	r.cmd.Cancel = func() error {
		return r.cmd.Process.Signal(syscall.SIGTERM)
	}
	r.cmd.WaitDelay = 5 * time.Second

	if err := r.cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("starting hls remuxer: %w", err)
	}

	go r.run()
	go r.waitForPlaylist()

	return nil
}

func (r *Remuxer) run() {
	defer close(r.done)
	err := r.cmd.Wait()
	if err != nil && r.cancel != nil {
		r.err = err
	}
	r.readyOnce.Do(func() { close(r.ready) })
}

func (r *Remuxer) waitForPlaylist() {
	for {
		select {
		case <-r.done:
			return
		default:
			if _, err := os.Stat(r.playlistPath); err == nil {
				r.readyOnce.Do(func() {
					r.log.Info().Str("playlist", r.playlistPath).Msg("hls playlist ready")
					close(r.ready)
				})
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
}

func (r *Remuxer) WaitReady(ctx context.Context) error {
	select {
	case <-r.ready:
		return r.err
	case <-ctx.Done():
		return ctx.Err()
	case <-r.done:
		if r.err != nil {
			return r.err
		}
		return fmt.Errorf("remuxer exited before playlist was ready")
	}
}

func (r *Remuxer) PlaylistPath() string { return r.playlistPath }
func (r *Remuxer) OutputDir() string    { return r.outputDir }

func (r *Remuxer) IsDone() bool {
	select {
	case <-r.done:
		return true
	default:
		return false
	}
}

func (r *Remuxer) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
	<-r.done
}
