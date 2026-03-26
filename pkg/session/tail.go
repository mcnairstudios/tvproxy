package session

import (
	"context"
	"io"
	"os"
	"time"
)

type TailReader struct {
	file    *os.File
	ctx     context.Context
	session *Session
}

func newTailReader(ctx context.Context, f *os.File, s *Session) *TailReader {
	return &TailReader{file: f, ctx: ctx, session: s}
}

func (r *TailReader) Read(p []byte) (int, error) {
	for {
		n, err := r.file.Read(p)
		if n > 0 {
			return n, nil
		}
		if err != io.EOF {
			return 0, err
		}

		if r.session.isDone() {
			return 0, io.EOF
		}

		select {
		case <-r.ctx.Done():
			return 0, r.ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (r *TailReader) Close() error {
	return r.file.Close()
}
