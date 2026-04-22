package scale

import (
	"testing"

	"github.com/asticode/go-astiav"
)

func TestNewScaler(t *testing.T) {
	s, err := NewScaler(
		1920, 1080, astiav.PixelFormatYuv420P,
		1280, 720, astiav.PixelFormatYuv420P,
	)
	if err != nil {
		t.Fatalf("NewScaler: %v", err)
	}
	defer s.Close()

	if s.swsCtx == nil {
		t.Fatal("expected non-nil swsCtx")
	}
	if s.dstW != 1280 || s.dstH != 720 {
		t.Fatalf("unexpected dst dims: %dx%d", s.dstW, s.dstH)
	}
}

func TestCloseIdempotent(t *testing.T) {
	s, err := NewScaler(
		640, 480, astiav.PixelFormatRgba,
		320, 240, astiav.PixelFormatRgba,
	)
	if err != nil {
		t.Fatalf("NewScaler: %v", err)
	}
	s.Close()
	s.Close() // must not panic
}

func TestScalerFields(t *testing.T) {
	s, err := NewScaler(
		1920, 1080, astiav.PixelFormatYuv420P,
		960, 540, astiav.PixelFormatRgba,
	)
	if err != nil {
		t.Fatalf("NewScaler: %v", err)
	}
	defer s.Close()

	if s.dstFmt != astiav.PixelFormatRgba {
		t.Fatalf("expected dstFmt Rgba, got %v", s.dstFmt)
	}
}
