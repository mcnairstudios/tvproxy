package probe

import (
	"os"
	"testing"
)

// testFile returns the path from AVPROBE_TEST_FILE env var, or skips the test.
func testFile(t *testing.T) string {
	t.Helper()
	f := os.Getenv("AVPROBE_TEST_FILE")
	if f == "" {
		t.Skip("set AVPROBE_TEST_FILE to a media file path to run probe tests")
	}
	return f
}

func TestProbe(t *testing.T) {
	url := testFile(t)

	info, err := Probe(url, 5)
	if err != nil {
		t.Fatalf("Probe(%q) error: %v", url, err)
	}

	if info.DurationMs <= 0 && !info.IsLive {
		t.Errorf("expected positive duration for non-live content, got %d ms", info.DurationMs)
	}
}

func TestProbeVideoStream(t *testing.T) {
	url := testFile(t)

	info, err := Probe(url, 5)
	if err != nil {
		t.Fatalf("Probe(%q) error: %v", url, err)
	}
	if info.Video == nil {
		t.Skip("test file has no video stream")
	}

	v := info.Video
	if v.Width <= 0 || v.Height <= 0 {
		t.Errorf("expected positive dimensions, got %dx%d", v.Width, v.Height)
	}
	if v.Codec == "" {
		t.Error("expected non-empty video codec")
	}
	if v.FramerateN <= 0 || v.FramerateD <= 0 {
		t.Errorf("expected positive framerate, got %d/%d", v.FramerateN, v.FramerateD)
	}
}

func TestProbeAudioTracks(t *testing.T) {
	url := testFile(t)

	info, err := Probe(url, 5)
	if err != nil {
		t.Fatalf("Probe(%q) error: %v", url, err)
	}
	if len(info.AudioTracks) == 0 {
		t.Skip("test file has no audio tracks")
	}

	a := info.AudioTracks[0]
	if a.Codec == "" {
		t.Error("expected non-empty audio codec")
	}
	if a.Channels <= 0 {
		t.Errorf("expected positive channel count, got %d", a.Channels)
	}
	if a.SampleRate <= 0 {
		t.Errorf("expected positive sample rate, got %d", a.SampleRate)
	}
}

func TestProbeInvalidURL(t *testing.T) {
	_, err := Probe("/nonexistent/file.mp4", 2)
	if err == nil {
		t.Error("expected error for invalid URL, got nil")
	}
}
