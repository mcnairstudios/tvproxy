package dash

import (
	"bytes"
	"context"
	"fmt"
	"io"
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

func NewRemuxer(outputDir string, log zerolog.Logger) *Remuxer {
	return &Remuxer{
		outputDir:    outputDir,
		manifestPath: filepath.Join(outputDir, "manifest.mpd"),
		done:         make(chan struct{}),
		ready:        make(chan struct{}),
		log:          log,
	}
}

func (r *Remuxer) Start(ctx context.Context, input io.Reader) error {
	if err := os.MkdirAll(r.outputDir, 0755); err != nil {
		return fmt.Errorf("creating dash output dir: %w", err)
	}

	rctx, cancel := context.WithCancel(ctx)
	r.cancel = cancel

	packagerBin := "packager"
	if _, err := exec.LookPath(packagerBin); err != nil {
		packagerBin = "/usr/local/bin/packager"
	}

	args := []string{
		fmt.Sprintf("in=/dev/stdin,stream=video,init_segment=%s,segment_template=%s",
			filepath.Join(r.outputDir, "init_v.mp4"),
			filepath.Join(r.outputDir, "seg_v_$Number$.m4s")),
		fmt.Sprintf("in=/dev/stdin,stream=audio,init_segment=%s,segment_template=%s",
			filepath.Join(r.outputDir, "init_a.mp4"),
			filepath.Join(r.outputDir, "seg_a_$Number$.m4s")),
		"--mpd_output", r.manifestPath, "--segment_duration", "2", "--io_block_size", "65536",
	}
	r.cmd = exec.CommandContext(rctx, packagerBin, args...)
	r.cmd.Stdin = input
	r.cmd.Cancel = func() error {
		return r.cmd.Process.Signal(syscall.SIGTERM)
	}
	r.cmd.WaitDelay = 5 * time.Second
	r.cmd.Stderr = &logWriter{log: r.log, prefix: "shaka-packager"}
	r.cmd.Stdout = &logWriter{log: r.log, prefix: "shaka-packager"}

	if err := r.cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("starting shaka packager: %w", err)
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
		return fmt.Errorf("packager exited before manifest was ready")
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
