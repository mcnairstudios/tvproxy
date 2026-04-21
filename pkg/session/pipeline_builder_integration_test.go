package session

import (
	"testing"

	"github.com/gavinmcnair/tvproxy/pkg/gstreamer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMSEPipeline_RealSatIPScenario(t *testing.T) {
	opts := gstreamer.SessionOpts{
		SourceURL:     "rtsp://192.168.1.100/?freq=586&msys=dvbt2&plp=0",
		IsLive:        true,
		VideoCodec:    "mpeg2video",
		ContainerHint: "mpegts",
		NeedsTranscode: true,
		HWAccel:        "vaapi",
		DecodeHWAccel:  "vaapi",
		OutputCodec:    "h264",
		Bitrate:        5000,
		AudioChannels:  2,
		OutputDir:      "/record/ch42/uuid1",
	}

	spec := BuildMSEPipeline(opts)

	require.True(t, spec.HasElement("src"))
	require.True(t, spec.HasElement("tee"))
	require.True(t, spec.HasElement("q_demux"))
	require.True(t, spec.HasElement("q_raw"))
	require.True(t, spec.HasElement("rawsink"))
	require.True(t, spec.HasElement("d"))
	require.True(t, spec.HasElement("dec"))
	require.True(t, spec.HasElement("enc"))
	require.True(t, spec.HasElement("fmp4"))

	src := spec.ElementByName("src")
	assert.Equal(t, "tvproxysrc", src.Factory)
	assert.Equal(t, "rtsp://192.168.1.100/?freq=586&msys=dvbt2&plp=0", src.Properties["location"])
	assert.Equal(t, true, src.Properties["is-live"])

	dec := spec.ElementByName("dec")
	assert.Equal(t, "tvproxydecode", dec.Factory)
	assert.Equal(t, "vaapi", dec.Properties["hw-accel"])

	enc := spec.ElementByName("enc")
	assert.Equal(t, "tvproxyencode", enc.Factory)
	assert.Equal(t, "h264", enc.Properties["codec"])
	assert.Equal(t, 5000, enc.Properties["bitrate"])
	assert.Equal(t, "vaapi", enc.Properties["hw-accel"])

	fmp4 := spec.ElementByName("fmp4")
	assert.Equal(t, "tvproxyfmp4", fmp4.Factory)
	assert.Equal(t, "h264", fmp4.Properties["video-codec"])
	assert.Equal(t, "/record/ch42/uuid1", fmp4.Properties["output-dir"])

	demux := spec.ElementByName("d")
	assert.Equal(t, "tvproxydemux", demux.Factory)
	assert.Equal(t, 2, demux.Properties["audio-channels"])
	assert.Equal(t, "mpegts", demux.Properties["container-hint"])

	rawsink := spec.ElementByName("rawsink")
	assert.Equal(t, false, rawsink.Properties["async"])
}

func TestMSEPipeline_RealIPTVCopyScenario(t *testing.T) {
	opts := gstreamer.SessionOpts{
		SourceURL:     "http://provider.example.com/live/ch1.ts",
		IsLive:        true,
		VideoCodec:    "h264",
		ContainerHint: "mpegts",
		NeedsTranscode: false,
		AudioChannels:  2,
		OutputDir:      "/record/ch99/uuid2",
	}

	spec := BuildMSEPipeline(opts)

	assert.False(t, spec.HasElement("dec"), "copy mode should not have decoder")
	assert.False(t, spec.HasElement("enc"), "copy mode should not have encoder")

	fmp4 := spec.ElementByName("fmp4")
	assert.Equal(t, "h264", fmp4.Properties["video-codec"])

	assertHasLink(t, spec, "d", "video", "fmp4", "video")
	assertHasLink(t, spec, "d", "audio", "fmp4", "audio")
}

func TestStreamPipeline_RealJellyfinPassthrough(t *testing.T) {
	opts := gstreamer.SessionOpts{
		SourceURL:     "http://provider.example.com/live/ch1.ts",
		IsLive:        true,
		AudioPassthrough: true,
		OutputDir:      "/record/ch99/uuid3",
		MuxOutputPath:  "/record/ch99/uuid3/output.ts",
	}

	spec := BuildStreamPipeline(opts)

	demux := spec.ElementByName("d")
	assert.Equal(t, "copy", demux.Properties["audio-codec"])
	assert.Equal(t, 0, demux.Properties["audio-channels"])

	assert.True(t, spec.HasElement("tee"), "live source needs tee for raw recording")
	assert.True(t, spec.HasElement("mux"))
	assert.True(t, spec.HasElement("sink"))

	sink := spec.ElementByName("sink")
	assert.Equal(t, "filesink", sink.Factory)
	assert.Equal(t, "/record/ch99/uuid3/output.ts", sink.Properties["location"])
}

func TestStreamTranscodePipeline_RealDLNAScenario(t *testing.T) {
	opts := gstreamer.SessionOpts{
		SourceURL:      "rtsp://192.168.1.100/?freq=586&msys=dvbt2",
		IsLive:         true,
		VideoCodec:     "mpeg2video",
		NeedsTranscode: true,
		HWAccel:        "vaapi",
		DecodeHWAccel:  "vaapi",
		OutputCodec:    "h264",
		Bitrate:        3000,
		AudioChannels:  2,
		OutputDir:      "/record/dlna/uuid4",
		MuxOutputPath:  "/record/dlna/uuid4/output.ts",
	}

	spec := BuildStreamTranscodePipeline(opts)

	assert.True(t, spec.HasElement("dec"))
	assert.True(t, spec.HasElement("enc"))
	assert.True(t, spec.HasElement("mux"))
	assert.True(t, spec.HasElement("sink"))
	assert.True(t, spec.HasElement("tee"), "network source needs tee")
	assert.True(t, spec.HasElement("rawsink"), "raw recording alongside transcode")

	enc := spec.ElementByName("enc")
	assert.Equal(t, "h264", enc.Properties["codec"])
	assert.Equal(t, 3000, enc.Properties["bitrate"])

	demux := spec.ElementByName("d")
	assert.Equal(t, 2, demux.Properties["audio-channels"])
}

func TestRecordingPlayback_RealScenario(t *testing.T) {
	spec := BuildRecordingPlaybackPipeline(
		"/config/recordings/stream1/recorded/Show_2026-04-21.ts",
		"/record/playback/uuid5",
	)

	src := spec.ElementByName("src")
	assert.Equal(t, "/config/recordings/stream1/recorded/Show_2026-04-21.ts", src.Properties["location"])
	assert.Equal(t, false, src.Properties["is-live"])

	assert.False(t, spec.HasElement("tee"), "file playback has no tee")
	assert.False(t, spec.HasElement("rawsink"), "file playback has no raw recording")
	assert.True(t, spec.HasElement("fmp4"))

	assertHasLink(t, spec, "src", "", "d", "")
}
