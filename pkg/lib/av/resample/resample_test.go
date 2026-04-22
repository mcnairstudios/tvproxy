package resample

import (
	"testing"

	"github.com/asticode/go-astiav"
)

func TestNewResampler(t *testing.T) {
	r, err := NewResampler(2, 44100, astiav.SampleFormatFltp,
		2, 48000, astiav.SampleFormatS16)
	if err != nil {
		t.Fatalf("NewResampler: unexpected error: %v", err)
	}
	defer r.Close()

	if r.swrCtx == nil {
		t.Fatal("expected non-nil swrCtx")
	}
	if r.dstRate != 48000 {
		t.Errorf("dstRate = %d, want 48000", r.dstRate)
	}
}

func TestNewResamplerUnsupportedChannels(t *testing.T) {
	_, err := NewResampler(3, 44100, astiav.SampleFormatFltp,
		2, 48000, astiav.SampleFormatS16)
	if err == nil {
		t.Fatal("expected error for unsupported source channel count")
	}

	_, err = NewResampler(2, 44100, astiav.SampleFormatFltp,
		3, 48000, astiav.SampleFormatS16)
	if err == nil {
		t.Fatal("expected error for unsupported destination channel count")
	}
}

func TestCloseIdempotent(t *testing.T) {
	r, err := NewResampler(1, 16000, astiav.SampleFormatS16,
		2, 48000, astiav.SampleFormatFltp)
	if err != nil {
		t.Fatalf("NewResampler: unexpected error: %v", err)
	}
	r.Close()
	r.Close() // must not panic
}
