package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const realPluginOutputDir = "/tmp/tvproxy_watcher_test"

func TestWatcher_RealPluginOutput(t *testing.T) {
	if _, err := os.Stat(filepath.Join(realPluginOutputDir, "probe.pb")); os.IsNotExist(err) {
		t.Skip("real plugin output not available at " + realPluginOutputDir)
	}

	log := zerolog.Nop()
	w, err := NewWatcher(realPluginOutputDir, log)
	require.NoError(t, err)
	defer w.Close()

	probe := w.Probe()
	require.NotNil(t, probe, "probe.pb should be loaded on startup via initial scan or pre-existing file")

	if probe == nil {
		t.Fatal("probe not loaded")
	}

	assert.Equal(t, "h264", probe.VideoCodec)
	assert.Equal(t, "avc1.f4000d", probe.VideoCodecString)
	assert.Equal(t, int32(320), probe.VideoWidth)
	assert.Equal(t, int32(240), probe.VideoHeight)
	assert.Equal(t, false, probe.VideoInterlaced)
	assert.Equal(t, "aac", probe.AudioSourceCodec)
	assert.Equal(t, int32(2), probe.AudioSourceChannels)
	assert.Equal(t, int32(48000), probe.AudioSourceSampleRate)
	assert.Equal(t, "aac", probe.AudioOutputCodec)
	assert.Equal(t, int32(2), probe.AudioOutputChannels)
	assert.Equal(t, int32(48000), probe.AudioOutputSampleRate)

	videoInit := w.VideoInit()
	require.NotNil(t, videoInit, "init_video.mp4 should be loaded")
	assert.Greater(t, len(videoInit), 100, "init_video.mp4 should have ftyp+moov")

	audioInit := w.AudioInit()
	require.NotNil(t, audioInit, "init_audio.mp4 should be loaded")
	assert.Greater(t, len(audioInit), 100, "init_audio.mp4 should have ftyp+moov")

	assert.GreaterOrEqual(t, w.VideoSegmentCount(), 1, "should have at least 1 video segment")
	assert.GreaterOrEqual(t, w.AudioSegmentCount(), 1, "should have at least 1 audio segment")

	vData, ok := w.VideoSegment(1)
	require.True(t, ok, "video segment 1 should exist")
	assert.Greater(t, len(vData), 1000, "video_0001.m4s should have moof+mdat")

	aData, ok := w.AudioSegment(1)
	require.True(t, ok, "audio segment 1 should exist")
	assert.Greater(t, len(aData), 1000, "audio_0001.m4s should have moof+mdat")

	assert.Equal(t, int64(1), w.Generation())
}
