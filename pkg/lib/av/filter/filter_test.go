package filter

import (
	"testing"

	"github.com/asticode/go-astiav"
)

func TestNewDeinterlacer(t *testing.T) {
	tb := astiav.NewRational(1, 25)
	d, err := NewDeinterlacer(1920, 1080, astiav.PixelFormatYuv420P, tb)
	if err != nil {
		t.Skipf("NewDeinterlacer not available (missing ffmpeg libs?): %v", err)
	}
	defer d.Close()

	if d.graph == nil {
		t.Fatal("expected non-nil graph")
	}
	if d.bufferSrc == nil {
		t.Fatal("expected non-nil bufferSrc")
	}
	if d.bufferSink == nil {
		t.Fatal("expected non-nil bufferSink")
	}
}

func TestCloseIdempotent(t *testing.T) {
	tb := astiav.NewRational(1, 30)
	d, err := NewDeinterlacer(1280, 720, astiav.PixelFormatYuv420P, tb)
	if err != nil {
		t.Skipf("NewDeinterlacer not available (missing ffmpeg libs?): %v", err)
	}
	d.Close()
	d.Close()
}

func TestDeinterlacerStruct(t *testing.T) {
	d := &Deinterlacer{}
	d.Close()
}
