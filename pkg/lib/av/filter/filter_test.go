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

func TestDeinterlacer_EAGAINOnFirstFrame(t *testing.T) {
	d, err := NewDeinterlacer(720, 576, astiav.PixelFormatYuv420P, astiav.NewRational(1, 25))
	if err != nil {
		t.Skipf("deinterlacer not available: %v", err)
	}
	defer d.Close()

	var outputFrames int
	for i := 0; i < 10; i++ {
		f := astiav.AllocFrame()
		f.SetWidth(720)
		f.SetHeight(576)
		f.SetPixelFormat(astiav.PixelFormatYuv420P)
		f.SetPts(int64(i))
		if err := f.AllocBuffer(0); err != nil {
			t.Fatalf("alloc: %v", err)
		}

		out, err := d.Process(f)
		f.Free()

		if err != nil {
			t.Fatalf("frame %d: unexpected fatal error: %v (EAGAIN should return nil, nil)", i, err)
		}
		if out == nil {
			if i == 0 {
				t.Logf("frame %d: nil (EAGAIN — buffering, expected for yadif)", i)
			}
			continue
		}
		outputFrames++
		out.Free()
	}

	if outputFrames == 0 {
		t.Error("deinterlacer produced 0 output frames from 10 inputs")
	} else {
		t.Logf("%d output frames from 10 inputs (first %d buffered)", outputFrames, 10-outputFrames)
	}
}

func TestDeinterlacer_576i(t *testing.T) {
	d, err := NewDeinterlacer(720, 576, astiav.PixelFormatYuv420P, astiav.NewRational(1, 25))
	if err != nil {
		t.Skipf("not available: %v", err)
	}
	defer d.Close()

	var out *astiav.Frame
	for i := 0; i < 5; i++ {
		f := astiav.AllocFrame()
		f.SetWidth(720)
		f.SetHeight(576)
		f.SetPixelFormat(astiav.PixelFormatYuv420P)
		f.SetPts(int64(i))
		f.AllocBuffer(0)
		out, _ = d.Process(f)
		f.Free()
		if out != nil {
			break
		}
	}
	if out == nil {
		t.Fatal("576i deinterlacer never produced output")
	}
	if out.Width() != 720 || out.Height() != 576 {
		t.Errorf("output dimensions: %dx%d, want 720x576", out.Width(), out.Height())
	}
	out.Free()
}
