package proto

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestProbe_RoundTrip(t *testing.T) {
	original := &Probe{
		VideoCodec:            "h265",
		VideoCodecString:      "hev1.2.4.L150",
		VideoWidth:            3840,
		VideoHeight:           2160,
		VideoInterlaced:       false,
		AudioSourceCodec:      "eac3",
		AudioSourceChannels:   6,
		AudioSourceSampleRate: 48000,
		AudioOutputCodec:      "aac",
		AudioOutputChannels:   2,
		AudioOutputSampleRate: 48000,
	}

	data, err := proto.Marshal(original)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	decoded := &Probe{}
	err = proto.Unmarshal(data, decoded)
	require.NoError(t, err)

	require.Equal(t, original.VideoCodec, decoded.VideoCodec)
	require.Equal(t, original.VideoCodecString, decoded.VideoCodecString)
	require.Equal(t, original.VideoWidth, decoded.VideoWidth)
	require.Equal(t, original.VideoHeight, decoded.VideoHeight)
	require.Equal(t, original.VideoInterlaced, decoded.VideoInterlaced)
	require.Equal(t, original.AudioSourceCodec, decoded.AudioSourceCodec)
	require.Equal(t, original.AudioSourceChannels, decoded.AudioSourceChannels)
	require.Equal(t, original.AudioSourceSampleRate, decoded.AudioSourceSampleRate)
	require.Equal(t, original.AudioOutputCodec, decoded.AudioOutputCodec)
	require.Equal(t, original.AudioOutputChannels, decoded.AudioOutputChannels)
	require.Equal(t, original.AudioOutputSampleRate, decoded.AudioOutputSampleRate)
}

func TestSignal_RoundTrip(t *testing.T) {
	original := &Signal{
		Strength:  0.85,
		Quality:   0.92,
		Snr:       12.5,
		Timestamp: 1713657600,
	}

	data, err := proto.Marshal(original)
	require.NoError(t, err)

	decoded := &Signal{}
	err = proto.Unmarshal(data, decoded)
	require.NoError(t, err)

	require.InDelta(t, float64(original.Strength), float64(decoded.Strength), 0.001)
	require.InDelta(t, float64(original.Quality), float64(decoded.Quality), 0.001)
	require.InDelta(t, float64(original.Snr), float64(decoded.Snr), 0.001)
	require.Equal(t, original.Timestamp, decoded.Timestamp)
}

func TestProbe_EmptyFields(t *testing.T) {
	original := &Probe{
		VideoCodec: "h264",
	}

	data, err := proto.Marshal(original)
	require.NoError(t, err)

	decoded := &Probe{}
	err = proto.Unmarshal(data, decoded)
	require.NoError(t, err)

	require.Equal(t, "h264", decoded.VideoCodec)
	require.Equal(t, "", decoded.VideoCodecString)
	require.Equal(t, int32(0), decoded.VideoWidth)
	require.Equal(t, int32(0), decoded.AudioSourceChannels)
}
