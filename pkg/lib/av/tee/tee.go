// Package avtee provides a custom AVIO context that tees raw bytes to disk
// while feeding them to libavformat for demuxing. This enables raw source
// recording (e.g., saving raw MPEG-TS from a live TV stream while
// simultaneously demuxing it).
package tee

import (
	"io"
	"net/http"

	"github.com/asticode/go-astiav"
)

const defaultBufSize = 32768

// TeeReader wraps an io.Reader and tees all bytes read to a writer (e.g., a file).
// It provides a custom AVIO context for libavformat to read from.
type TeeReader struct {
	source   io.ReadCloser
	recorder io.Writer
	ioCtx    *astiav.IOContext
	tee      io.Reader
}

// Config configures the tee reader.
type Config struct {
	URL      string    // source URL to fetch
	Recorder io.Writer // where to write raw bytes (e.g., os.File)
	BufSize  int       // AVIO buffer size (default 32768)
}

// New creates a TeeReader that fetches from the URL and tees to the recorder.
// The caller must call Close() when done.
func New(cfg Config) (*TeeReader, error) {
	resp, err := http.Get(cfg.URL)
	if err != nil {
		return nil, err
	}

	bufSize := cfg.BufSize
	if bufSize <= 0 {
		bufSize = defaultBufSize
	}

	return NewFromReader(resp.Body, cfg.Recorder, bufSize)
}

// NewFromReader creates a TeeReader from an existing io.ReadCloser.
// Useful when the caller manages the HTTP connection themselves.
func NewFromReader(source io.ReadCloser, recorder io.Writer, bufSize int) (*TeeReader, error) {
	if bufSize <= 0 {
		bufSize = defaultBufSize
	}

	tee := io.TeeReader(source, recorder)

	t := &TeeReader{
		source:   source,
		recorder: recorder,
		tee:      tee,
	}

	ioCtx, err := astiav.AllocIOContext(
		bufSize,
		false, // not writable — this is an input context
		func(b []byte) (int, error) {
			return tee.Read(b)
		},
		nil, // no seek — live streams are not seekable
		nil, // no write callback
	)
	if err != nil {
		return nil, err
	}

	t.ioCtx = ioCtx
	return t, nil
}

// Read reads from the tee reader, which simultaneously writes to the recorder.
// This is exposed so callers (and tests) can read without going through AVIO.
func (t *TeeReader) Read(b []byte) (int, error) {
	return t.tee.Read(b)
}

// IOContext returns the underlying AVIO context.
func (t *TeeReader) IOContext() *astiav.IOContext {
	return t.ioCtx
}

// SetupFormatContext attaches this TeeReader's AVIO context to a FormatContext.
// After calling this, use fc.OpenInput("", nil, nil) with an empty URL.
func (t *TeeReader) SetupFormatContext(fc *astiav.FormatContext) {
	fc.SetPb(t.ioCtx)
}

// Close releases the AVIO context and closes the source reader.
func (t *TeeReader) Close() error {
	if t.ioCtx != nil {
		t.ioCtx.Free()
		t.ioCtx = nil
	}
	if t.source != nil {
		return t.source.Close()
	}
	return nil
}
