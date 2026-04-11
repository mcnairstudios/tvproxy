package gstreamer

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

type Session struct {
	ID        string
	Pipeline  *Pipeline
	cmd       *exec.Cmd
	ctx       context.Context
	cancel    context.CancelFunc
	stdout    io.ReadCloser
	log       zerolog.Logger
	started   time.Time
	mu        sync.Mutex
	running   bool
	outputDir string
}

func NewSession(id string, pipeline *Pipeline, outputDir string, log zerolog.Logger) *Session {
	return &Session{
		ID:        id,
		Pipeline:  pipeline,
		outputDir: outputDir,
		log:       log.With().Str("session", id).Str("component", "gstreamer").Logger(),
	}
}

func (s *Session) Start(parentCtx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("session already running")
	}

	s.ctx, s.cancel = context.WithCancel(parentCtx)
	s.cmd = exec.CommandContext(s.ctx, s.Pipeline.Cmd, s.Pipeline.Args...)
	s.cmd.Stderr = &logWriter{log: s.log, level: "warn"}

	if s.outputDir != "" {
		os.MkdirAll(s.outputDir, 0755)
	}

	var err error
	s.stdout, err = s.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}

	if err := s.cmd.Start(); err != nil {
		return fmt.Errorf("start gst-launch: %w", err)
	}

	s.running = true
	s.started = time.Now()
	s.log.Info().Strs("args", s.Pipeline.Args).Msg("gstreamer session started")

	go func() {
		err := s.cmd.Wait()
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
		if err != nil && s.ctx.Err() == nil {
			s.log.Error().Err(err).Msg("gstreamer exited with error")
		} else {
			s.log.Info().Dur("duration", time.Since(s.started)).Msg("gstreamer session ended")
		}
	}()

	return nil
}

func (s *Session) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Signal(os.Interrupt)
		time.AfterFunc(3*time.Second, func() {
			if s.running {
				s.cmd.Process.Kill()
			}
		})
	}
}

func (s *Session) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *Session) Stdout() io.ReadCloser {
	return s.stdout
}

type logWriter struct {
	log   zerolog.Logger
	level string
}

func (w *logWriter) Write(p []byte) (n int, err error) {
	msg := string(p)
	if len(msg) > 0 && msg[len(msg)-1] == '\n' {
		msg = msg[:len(msg)-1]
	}
	if msg == "" {
		return len(p), nil
	}
	switch w.level {
	case "error":
		w.log.Error().Msg(msg)
	case "warn":
		w.log.Warn().Msg(msg)
	default:
		w.log.Debug().Msg(msg)
	}
	return len(p), nil
}
