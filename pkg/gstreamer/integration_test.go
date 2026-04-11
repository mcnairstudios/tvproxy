package gstreamer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestGStreamerAvailability(t *testing.T) {
	if !Available() {
		t.Skip("gst-launch-1.0 not installed")
	}
	t.Log("gst-launch-1.0 is available")
}

func TestGStreamerSession_TestPattern(t *testing.T) {
	if !Available() {
		t.Skip("gst-launch-1.0 not installed")
	}

	dir := t.TempDir()
	outFile := filepath.Join(dir, "test.ts")

	pipeline := &Pipeline{
		Cmd: "gst-launch-1.0",
		Args: []string{
			"-q", "-e",
			"videotestsrc", "num-buffers=50", "!",
			"video/x-raw,width=320,height=240,framerate=25/1", "!",
			"x264enc", "speed-preset=ultrafast", "tune=zerolatency", "!",
			"mpegtsmux", "!",
			"filesink", "location=" + outFile,
		},
	}

	log := zerolog.Nop()
	sess := NewSession("test-pattern", pipeline, dir, log)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := sess.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	time.Sleep(3 * time.Second)
	sess.Stop()
	time.Sleep(1 * time.Second)

	info, err := os.Stat(outFile)
	if err != nil {
		t.Fatalf("output file not found: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("output file is empty")
	}
	t.Logf("output: %d bytes", info.Size())
}

func TestGStreamerDiscoverer(t *testing.T) {
	if !DiscovererAvailable() {
		t.Skip("gst-discoverer-1.0 not installed")
	}

	_, err := exec.LookPath("gst-discoverer-1.0")
	if err != nil {
		t.Skip("gst-discoverer-1.0 not found")
	}

	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.ts")

	cmd := exec.Command("gst-launch-1.0", "-q", "-e",
		"videotestsrc", "num-buffers=25", "!",
		"video/x-raw,width=320,height=240,framerate=25/1", "!",
		"x264enc", "speed-preset=ultrafast", "!",
		"mpegtsmux", "!",
		"filesink", "location="+testFile,
	)
	if err := cmd.Run(); err != nil {
		t.Skipf("failed to create test file: %v", err)
	}

	out, err := exec.Command("gst-discoverer-1.0", testFile).CombinedOutput()
	if err != nil {
		t.Fatalf("discoverer failed: %v\n%s", err, out)
	}

	output := string(out)
	if len(output) == 0 {
		t.Fatal("discoverer returned empty output")
	}
	t.Logf("discoverer output length: %d bytes", len(output))
}
