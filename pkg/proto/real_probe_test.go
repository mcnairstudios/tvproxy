package proto

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestProbe_RealPluginOutput_v18(t *testing.T) {
	b64 := "CgRoMjY0EgthdmMxLmY0MDAwZBjAAiDwATIDYWFjOAJAgPcCSgNhYWNQAliA9wI="
	data, err := base64.StdEncoding.DecodeString(b64)
	require.NoError(t, err)

	probe := &Probe{}
	err = proto.Unmarshal(data, probe)
	require.NoError(t, err)

	assert.Equal(t, "h264", probe.VideoCodec)
	assert.Equal(t, "avc1.f4000d", probe.VideoCodecString)
	assert.Equal(t, int32(320), probe.VideoWidth)
	assert.Equal(t, int32(240), probe.VideoHeight)
	assert.Equal(t, int32(0), probe.VideoBitDepth, "v1.8 probe has no bit depth")
	assert.Equal(t, int32(0), probe.VideoFramerateNum, "v1.8 probe has no framerate")
}

func TestProbe_RealPluginOutput_v19(t *testing.T) {
	b64 := "CgRoMjY0EgthdmMxLmY0MDAwZBjAAiDwATIDYWFjOAJAgPcCSgNhYWNQAliA9wJgCGgZcAE="
	data, err := base64.StdEncoding.DecodeString(b64)
	require.NoError(t, err)
	require.Equal(t, 53, len(data))

	probe := &Probe{}
	err = proto.Unmarshal(data, probe)
	require.NoError(t, err)

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
	assert.Equal(t, int32(8), probe.VideoBitDepth)
	assert.Equal(t, int32(25), probe.VideoFramerateNum)
	assert.Equal(t, int32(1), probe.VideoFramerateDen)
}
