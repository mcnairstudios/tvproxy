package decode

import (
	"testing"

	"github.com/asticode/go-astiav"
)

func TestNewAudioDecoderInvalidCodec(t *testing.T) {
	_, err := NewAudioDecoder(astiav.CodecID(0), nil)
	if err == nil {
		t.Fatal("expected error for invalid codec ID, got nil")
	}
}

func TestNewVideoDecoderInvalidCodec(t *testing.T) {
	_, err := NewVideoDecoder(astiav.CodecID(0), nil, DecodeOpts{})
	if err == nil {
		t.Fatal("expected error for invalid codec ID, got nil")
	}
}

func TestNewVideoDecoderBadName(t *testing.T) {
	_, err := NewVideoDecoder(astiav.CodecIDH264, nil, DecodeOpts{
		DecoderName: "this_decoder_does_not_exist",
	})
	if err == nil {
		t.Fatal("expected error for non-existent decoder name, got nil")
	}
}

func TestDecodeOptsDefaults(t *testing.T) {
	opts := DecodeOpts{}
	if opts.HWAccel != "" {
		t.Errorf("expected empty HWAccel, got %q", opts.HWAccel)
	}
	if opts.MaxBitDepth != 0 {
		t.Errorf("expected MaxBitDepth 0, got %d", opts.MaxBitDepth)
	}
	if opts.DecoderName != "" {
		t.Errorf("expected empty DecoderName, got %q", opts.DecoderName)
	}
}

func TestCloseIdempotent(t *testing.T) {
	d := &Decoder{}
	d.Close()
	d.Close()
}

func TestDecodeNilPacket(t *testing.T) {
	dec, err := NewAudioDecoder(astiav.CodecIDAac, nil)
	if err != nil {
		t.Skipf("AAC decoder not available: %v", err)
	}
	defer dec.Close()

	frames, err := dec.Decode(nil)
	if err != nil {
		t.Fatalf("unexpected error on nil packet decode: %v", err)
	}
	if len(frames) != 0 {
		for _, f := range frames {
			f.Free()
		}
		t.Fatalf("expected 0 frames on flush of empty decoder, got %d", len(frames))
	}
}

func TestHWAccelMap(t *testing.T) {
	expected := []string{"vaapi", "qsv", "videotoolbox", "cuda", "nvenc"}
	for _, name := range expected {
		if _, ok := hwAccelMap[name]; !ok {
			t.Errorf("hwAccelMap missing key %q", name)
		}
	}
}
