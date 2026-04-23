package mux

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/asticode/go-astiav"
)

// ---------------------------------------------------------------------------
// MuxOpts
// ---------------------------------------------------------------------------

func TestMuxOptsFields(t *testing.T) {
	opts := MuxOpts{
		OutputDir:         "/tmp/segments",
		SegmentDurationMs: 4000,
		AudioFragmentMs:   2048,
		VideoCodecID:      astiav.CodecIDH264,
		VideoExtradata:    []byte{0x00, 0x00, 0x00, 0x01},
		VideoWidth:        1920,
		VideoHeight:       1080,
		VideoTimeBase:     astiav.NewRational(1, 90000),
		AudioCodecID:      astiav.CodecIDAac,
		AudioExtradata:    []byte{0x12, 0x10},
		AudioChannels:     2,
		AudioSampleRate:   48000,
	}

	if opts.OutputDir != "/tmp/segments" {
		t.Errorf("OutputDir = %q, want /tmp/segments", opts.OutputDir)
	}
	if opts.AudioFragmentMs != 2048 {
		t.Errorf("AudioFragmentMs = %d, want 2048", opts.AudioFragmentMs)
	}
}

// ---------------------------------------------------------------------------
// FragmentedMuxer
// ---------------------------------------------------------------------------

func TestNewFragmentedMuxer_EmptyDir(t *testing.T) {
	_, err := NewFragmentedMuxer(MuxOpts{})
	if err == nil {
		t.Fatal("expected error for empty OutputDir")
	}
}

func TestNewFragmentedMuxer_NoTracks(t *testing.T) {
	dir := t.TempDir()
	m, err := NewFragmentedMuxer(MuxOpts{OutputDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	// No video or audio configured — write should fail
	if err := m.WriteVideoPacket(nil); err == nil {
		t.Error("expected error for no video track")
	}
	if err := m.WriteAudioPacket(nil); err == nil {
		t.Error("expected error for no audio track")
	}
}

func TestNewFragmentedMuxer_VideoTrack(t *testing.T) {
	dir := t.TempDir()

	// Minimal H.264 avcC extradata (configurationVersion=1, profile=66 Baseline, level=30)
	extradata := []byte{
		0x01, 0x42, 0xC0, 0x1E, 0xFF, 0xE1,
		0x00, 0x04, 0x67, 0x42, 0xC0, 0x1E, // SPS
		0x01,
		0x00, 0x02, 0x68, 0xCE, // PPS
	}

	m, err := NewFragmentedMuxer(MuxOpts{
		OutputDir:      dir,
		VideoCodecID:   astiav.CodecIDH264,
		VideoExtradata: extradata,
		VideoWidth:     640,
		VideoHeight:    480,
		VideoTimeBase:  astiav.NewRational(1, 90000),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	// Init segment should exist
	initPath := filepath.Join(dir, "init_video.mp4")
	data, err := os.ReadFile(initPath)
	if err != nil {
		t.Fatalf("init_video.mp4 not found: %v", err)
	}
	if len(data) == 0 {
		t.Error("init_video.mp4 is empty")
	}

	// Check for ftyp box
	if len(data) >= 8 && string(data[4:8]) != "ftyp" {
		t.Errorf("init segment should start with ftyp, got %q", string(data[4:8]))
	}
}

func TestFragmentedMuxer_CloseIdempotent(t *testing.T) {
	dir := t.TempDir()
	m, err := NewFragmentedMuxer(MuxOpts{OutputDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal("second Close should not error")
	}
}

func TestFragmentedMuxer_WriteAfterClose(t *testing.T) {
	dir := t.TempDir()
	m, err := NewFragmentedMuxer(MuxOpts{OutputDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	m.Close()

	if err := m.WriteVideoPacket(nil); err == nil {
		t.Error("expected error writing video after close")
	}
	if err := m.WriteAudioPacket(nil); err == nil {
		t.Error("expected error writing audio after close")
	}
}

// ---------------------------------------------------------------------------
// Codec string extraction
// ---------------------------------------------------------------------------

func TestExtractCodecString_H264(t *testing.T) {
	dir := t.TempDir()
	extradata := []byte{
		0x01, 0x42, 0xC0, 0x1E, 0xFF, 0xE1,
		0x00, 0x04, 0x67, 0x42, 0xC0, 0x1E,
		0x01,
		0x00, 0x02, 0x68, 0xCE,
	}
	m, err := NewFragmentedMuxer(MuxOpts{
		OutputDir:      dir,
		VideoCodecID:   astiav.CodecIDH264,
		VideoExtradata: extradata,
		VideoWidth:     640,
		VideoHeight:    480,
		VideoTimeBase:  astiav.NewRational(1, 90000),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	cs := m.VideoCodecString()
	if cs == "" {
		t.Error("H.264 codec string is empty")
		initPath := filepath.Join(dir, "init_video.mp4")
		data, _ := os.ReadFile(initPath)
		t.Logf("init segment: %d bytes", len(data))
		if len(data) > 0 {
			t.Logf("first 32 bytes: %x", data[:min(32, len(data))])
		}
	} else {
		t.Logf("H.264 codec string: %s", cs)
		if cs != "avc1.42C01E" {
			t.Errorf("expected avc1.42C01E, got %s", cs)
		}
	}
}

func TestExtractCodecString_HEVC(t *testing.T) {
	dir := t.TempDir()
	// Minimal hvcC: version=1, profile=Main(1), tier=0, level=120
	extradata := []byte{
		0x01, 0x01, 0x60, 0x00, 0x00, 0x00,
		0x90, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x78, 0xF0, 0x00, 0xFC, 0xFD, 0xFA,
		0xFA, 0x00, 0x00, 0x0F, 0x00,
	}
	m, err := NewFragmentedMuxer(MuxOpts{
		OutputDir:      dir,
		VideoCodecID:   astiav.CodecIDHevc,
		VideoExtradata: extradata,
		VideoWidth:     1920,
		VideoHeight:    1080,
		VideoTimeBase:  astiav.NewRational(1, 90000),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	cs := m.VideoCodecString()
	if cs == "" {
		t.Error("HEVC codec string is empty")
	} else {
		t.Logf("HEVC codec string: %s", cs)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestFindBox(t *testing.T) {
	// Construct a minimal box: size(4) + type(4) + content
	box := []byte{
		0x00, 0x00, 0x00, 0x0C, // size = 12
		'f', 't', 'y', 'p', // type = ftyp
		0xAA, 0xBB, 0xCC, 0xDD, // content
	}
	content := findBox(box, "ftyp")
	if content == nil {
		t.Fatal("expected to find ftyp box")
	}
	if len(content) != 4 {
		t.Errorf("expected 4 bytes content, got %d", len(content))
	}
}

func TestFindBox_NotFound(t *testing.T) {
	box := []byte{
		0x00, 0x00, 0x00, 0x08,
		'f', 't', 'y', 'p',
	}
	content := findBox(box, "moov")
	if content != nil {
		t.Error("expected nil for missing box")
	}
}

func TestAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.bin")
	data := []byte("hello world")

	if err := atomicWrite(path, data); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello world" {
		t.Errorf("got %q, want %q", got, "hello world")
	}

	// .tmp should not exist
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error(".tmp file should not exist after rename")
	}
}

// ---------------------------------------------------------------------------
// StreamMuxer
// ---------------------------------------------------------------------------

func TestNewStreamMuxer_EmptyFormat(t *testing.T) {
	var buf bytes.Buffer
	_, err := NewStreamMuxer("", &buf)
	if err == nil {
		t.Fatal("expected error for empty format")
	}
}

func TestNewStreamMuxer_NilWriter(t *testing.T) {
	_, err := NewStreamMuxer("mpegts", nil)
	if err == nil {
		t.Fatal("expected error for nil writer")
	}
}

func TestNewStreamMuxer_MPEGTS(t *testing.T) {
	var buf bytes.Buffer
	m, err := NewStreamMuxer("mpegts", &buf)
	if err != nil {
		t.Fatal(err)
	}
	if m.fc == nil {
		t.Error("expected non-nil FormatContext")
	}
	_ = m.Close()
}

func TestStreamMuxer_AddStreamNilParams(t *testing.T) {
	var buf bytes.Buffer
	m, err := NewStreamMuxer("mpegts", &buf)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	_, err = m.AddStream(nil)
	if err == nil {
		t.Error("expected error for nil codec params")
	}
}

func TestStreamMuxer_CloseIdempotent(t *testing.T) {
	var buf bytes.Buffer
	m, err := NewStreamMuxer("mpegts", &buf)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestStreamMuxer_WriteAfterClose(t *testing.T) {
	var buf bytes.Buffer
	m, err := NewStreamMuxer("mpegts", &buf)
	if err != nil {
		t.Fatal(err)
	}
	_ = m.Close()

	if err := m.WriteHeader(); err == nil {
		t.Error("expected error on WriteHeader after close")
	}
	if err := m.WritePacket(nil); err == nil {
		t.Error("expected error on WritePacket after close")
	}
}
