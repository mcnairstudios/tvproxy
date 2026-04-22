package tee

import (
	"bytes"
	"io"
	"testing"
)

func TestTeeReaderFromReader(t *testing.T) {
	sourceData := []byte("test media data stream content here, with enough bytes to exercise the tee")
	source := io.NopCloser(bytes.NewReader(sourceData))

	var recorded bytes.Buffer
	tr, err := NewFromReader(source, &recorded, 4096)
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()

	// Read all data through the exposed Read method
	// (simulates what libavformat would do via the AVIO read callback).
	buf := make([]byte, 16) // small buffer to exercise multiple reads
	var allRead []byte
	for {
		n, readErr := tr.Read(buf)
		if n > 0 {
			allRead = append(allRead, buf[:n]...)
		}
		if readErr != nil {
			if readErr != io.EOF {
				t.Fatalf("unexpected error: %v", readErr)
			}
			break
		}
	}

	if !bytes.Equal(allRead, sourceData) {
		t.Errorf("read data mismatch:\n  got:  %q\n  want: %q", allRead, sourceData)
	}
	if !bytes.Equal(recorded.Bytes(), sourceData) {
		t.Errorf("recorded data mismatch:\n  got:  %q\n  want: %q", recorded.Bytes(), sourceData)
	}
}

func TestTeeReaderIOContext(t *testing.T) {
	source := io.NopCloser(bytes.NewReader([]byte("hello")))
	var recorded bytes.Buffer

	tr, err := NewFromReader(source, &recorded, 4096)
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()

	if tr.IOContext() == nil {
		t.Error("expected non-nil IOContext")
	}
}

func TestTeeReaderDefaultBufSize(t *testing.T) {
	source := io.NopCloser(bytes.NewReader([]byte("data")))
	var recorded bytes.Buffer

	// Pass 0 for bufSize to exercise the default path.
	tr, err := NewFromReader(source, &recorded, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()

	if tr.IOContext() == nil {
		t.Error("expected non-nil IOContext with default buffer size")
	}
}

func TestTeeReaderCloseIdempotent(t *testing.T) {
	source := io.NopCloser(bytes.NewReader([]byte("data")))
	var recorded bytes.Buffer

	tr, err := NewFromReader(source, &recorded, 4096)
	if err != nil {
		t.Fatal(err)
	}

	// Close twice — should not panic.
	if err := tr.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestTeeReaderCloseNilSource(t *testing.T) {
	// Verify Close handles a zero-value TeeReader safely.
	tr := &TeeReader{}
	if err := tr.Close(); err != nil {
		t.Fatalf("close on zero-value TeeReader: %v", err)
	}
}
