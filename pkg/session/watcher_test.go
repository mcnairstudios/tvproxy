package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	tvproto "github.com/gavinmcnair/tvproxy/pkg/proto"
)

func TestWatcher_ProbeDetection(t *testing.T) {
	dir := t.TempDir()
	log := zerolog.Nop()

	w, err := NewWatcher(dir, log)
	require.NoError(t, err)
	defer w.Close()

	probe := &tvproto.Probe{
		VideoCodec:       "h264",
		VideoCodecString: "avc1.640028",
		VideoWidth:       1920,
		VideoHeight:      1080,
		AudioSourceCodec: "ac3",
		AudioSourceChannels:   6,
		AudioSourceSampleRate: 48000,
		AudioOutputCodec:      "aac",
		AudioOutputChannels:   2,
		AudioOutputSampleRate: 48000,
	}
	data, err := proto.Marshal(probe)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(dir, "probe.pb"), data, 0644)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return w.Probe() != nil
	}, 2*time.Second, 50*time.Millisecond)

	got := w.Probe()
	require.Equal(t, "h264", got.VideoCodec)
	require.Equal(t, "avc1.640028", got.VideoCodecString)
	require.Equal(t, int32(1920), got.VideoWidth)
	require.Equal(t, int32(1080), got.VideoHeight)
	require.Equal(t, "ac3", got.AudioSourceCodec)
	require.Equal(t, int32(6), got.AudioSourceChannels)
	require.Equal(t, "aac", got.AudioOutputCodec)
	require.Equal(t, int32(2), got.AudioOutputChannels)
}

func TestWatcher_SignalDetection(t *testing.T) {
	dir := t.TempDir()
	log := zerolog.Nop()

	w, err := NewWatcher(dir, log)
	require.NoError(t, err)
	defer w.Close()

	sig := &tvproto.Signal{
		Strength:  0.85,
		Quality:   0.92,
		Snr:       12.5,
		Timestamp: 1234567890,
	}
	data, err := proto.Marshal(sig)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(dir, "signal.pb"), data, 0644)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return w.Signal() != nil
	}, 2*time.Second, 50*time.Millisecond)

	got := w.Signal()
	require.InDelta(t, 0.85, float64(got.Strength), 0.01)
	require.InDelta(t, 0.92, float64(got.Quality), 0.01)
}

func TestWatcher_InitSegments(t *testing.T) {
	dir := t.TempDir()
	log := zerolog.Nop()

	w, err := NewWatcher(dir, log)
	require.NoError(t, err)
	defer w.Close()

	segDir := filepath.Join(dir, "segments")

	videoInit := []byte{0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p'}
	audioInit := []byte{0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p', 'a'}

	err = os.WriteFile(filepath.Join(segDir, "init_video.mp4"), videoInit, 0644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(segDir, "init_audio.mp4"), audioInit, 0644)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return w.VideoInit() != nil && w.AudioInit() != nil
	}, 2*time.Second, 50*time.Millisecond)

	require.Equal(t, videoInit, w.VideoInit())
	require.Equal(t, audioInit, w.AudioInit())
}

func TestWatcher_MediaSegments(t *testing.T) {
	dir := t.TempDir()
	log := zerolog.Nop()

	w, err := NewWatcher(dir, log)
	require.NoError(t, err)
	defer w.Close()

	segDir := filepath.Join(dir, "segments")

	seg1 := []byte("video_segment_1")
	seg2 := []byte("video_segment_2")
	aseg1 := []byte("audio_segment_1")

	err = os.WriteFile(filepath.Join(segDir, "video_0001.m4s"), seg1, 0644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(segDir, "video_0002.m4s"), seg2, 0644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(segDir, "audio_0001.m4s"), aseg1, 0644)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return w.VideoSegmentCount() >= 2 && w.AudioSegmentCount() >= 1
	}, 2*time.Second, 50*time.Millisecond)

	data, ok := w.VideoSegment(1)
	require.True(t, ok)
	require.Equal(t, seg1, data)

	data, ok = w.VideoSegment(2)
	require.True(t, ok)
	require.Equal(t, seg2, data)

	data, ok = w.AudioSegment(1)
	require.True(t, ok)
	require.Equal(t, aseg1, data)

	_, ok = w.VideoSegment(99)
	require.False(t, ok)
}

func TestWatcher_Generation(t *testing.T) {
	dir := t.TempDir()
	log := zerolog.Nop()

	w, err := NewWatcher(dir, log)
	require.NoError(t, err)
	defer w.Close()

	require.Equal(t, int64(1), w.Generation())

	w.Reset()
	require.Equal(t, int64(2), w.Generation())

	w.Reset()
	require.Equal(t, int64(3), w.Generation())
}

func TestWatcher_ResetClearsState(t *testing.T) {
	dir := t.TempDir()
	log := zerolog.Nop()

	w, err := NewWatcher(dir, log)
	require.NoError(t, err)
	defer w.Close()

	segDir := filepath.Join(dir, "segments")

	probe := &tvproto.Probe{VideoCodec: "h264"}
	data, _ := proto.Marshal(probe)
	os.WriteFile(filepath.Join(dir, "probe.pb"), data, 0644)
	os.WriteFile(filepath.Join(segDir, "init_video.mp4"), []byte("init"), 0644)
	os.WriteFile(filepath.Join(segDir, "video_0001.m4s"), []byte("seg"), 0644)

	require.Eventually(t, func() bool {
		return w.Probe() != nil && w.VideoInit() != nil && w.VideoSegmentCount() >= 1
	}, 2*time.Second, 50*time.Millisecond)

	w.Reset()

	require.Nil(t, w.Probe())
	require.Nil(t, w.VideoInit())
	require.Nil(t, w.AudioInit())
	require.Equal(t, 0, w.VideoSegmentCount())
	require.Equal(t, 0, w.AudioSegmentCount())
}

func TestWatcher_WaitProbeChannel(t *testing.T) {
	dir := t.TempDir()
	log := zerolog.Nop()

	w, err := NewWatcher(dir, log)
	require.NoError(t, err)
	defer w.Close()

	go func() {
		time.Sleep(100 * time.Millisecond)
		probe := &tvproto.Probe{VideoCodec: "h265", VideoCodecString: "hev1.2.4.L150"}
		data, _ := proto.Marshal(probe)
		os.WriteFile(filepath.Join(dir, "probe.pb"), data, 0644)
	}()

	select {
	case pb := <-w.WaitProbe():
		require.Equal(t, "h265", pb.VideoCodec)
		require.Equal(t, "hev1.2.4.L150", pb.VideoCodecString)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for probe")
	}
}

func TestSegmentSeqFromFilename(t *testing.T) {
	tests := []struct {
		name string
		want int
	}{
		{"video_0001.m4s", 1},
		{"video_0042.m4s", 42},
		{"audio_0100.m4s", 100},
		{"invalid.m4s", 0},
		{"video.m4s", 0},
	}
	for _, tt := range tests {
		got := SegmentSeqFromFilename(tt.name)
		if got != tt.want {
			t.Errorf("SegmentSeqFromFilename(%q) = %d, want %d", tt.name, got, tt.want)
		}
	}
}
