package dash

import (
	"bytes"
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

type logWriter struct {
	log    zerolog.Logger
	prefix string
	buf    bytes.Buffer
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.buf.Write(p)
	for {
		line, err := w.buf.ReadString('\n')
		if err != nil {
			w.buf.WriteString(line)
			break
		}
		w.log.Warn().Str("src", w.prefix).Msg(line[:len(line)-1])
	}
	return len(p), nil
}

type Remuxer struct {
	inputPath    string
	outputDir    string
	manifestPath string
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
		manifestPath: filepath.Join(outputDir, "manifest.mpd"),
		done:         make(chan struct{}),
		ready:        make(chan struct{}),
		log:          log,
	}
}

func (r *Remuxer) Start(ctx context.Context) error {
	if err := os.MkdirAll(r.outputDir, 0755); err != nil {
		return fmt.Errorf("creating dash output dir: %w", err)
	}

	// Wait for the upstream to write the fMP4 init segment before starting.
	// The file needs at least ftyp+moov (empty_moov) for ffmpeg to open it.
	waitCtx, waitCancel := context.WithTimeout(ctx, 30*time.Second)
	defer waitCancel()
	for {
		info, err := os.Stat(r.inputPath)
		if err == nil && info.Size() > 1024 {
			break
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("upstream file not ready: %w", waitCtx.Err())
		case <-time.After(500 * time.Millisecond):
		}
	}
	r.log.Debug().Str("input", r.inputPath).Msg("upstream file ready, starting dash remuxer")

	rctx, cancel := context.WithCancel(ctx)
	r.cancel = cancel

	r.cmd = exec.CommandContext(rctx, "ffmpeg",
		"-y", "-hide_banner", "-loglevel", "warning",
		"-f", "mp4", "-fflags", "+genpts+discardcorrupt+nofillin",
		"-i", r.inputPath,
		"-c", "copy",
		"-f", "dash",
		"-seg_duration", "2",
		"-window_size", "5",
		"-extra_window_size", "5",
		"-use_timeline", "1",
		"-use_template", "1",
		"-init_seg_name", "init-stream$RepresentationID$.$ext$",
		"-media_seg_name", "chunk-stream$RepresentationID$-$Number%05d$.$ext$",
		r.manifestPath,
	)
	r.cmd.Cancel = func() error {
		return r.cmd.Process.Signal(syscall.SIGTERM)
	}
	r.cmd.WaitDelay = 5 * time.Second
	r.cmd.Stderr = &logWriter{log: r.log, prefix: "dash-remuxer"}

	if err := r.cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("starting dash remuxer: %w", err)
	}

	go r.run()
	go r.waitForManifest()

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

func (r *Remuxer) waitForManifest() {
	for {
		select {
		case <-r.done:
			return
		default:
			if _, err := os.Stat(r.manifestPath); err == nil {
				r.readyOnce.Do(func() {
					r.log.Info().Str("manifest", r.manifestPath).Msg("dash manifest ready")
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
		return fmt.Errorf("remuxer exited before manifest was ready")
	}
}

func (r *Remuxer) ManifestPath() string { return r.manifestPath }
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
