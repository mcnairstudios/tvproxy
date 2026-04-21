package session

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gavinmcnair/tvproxy/pkg/gstreamer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func specToGstLaunch(spec gstreamer.PipelineSpec) string {
	elements := map[string]gstreamer.ElementSpec{}
	for _, el := range spec.Elements {
		elements[el.Name] = el
	}

	var parts []string
	for _, el := range spec.Elements {
		s := el.Factory
		if el.Name != el.Factory {
			s += " name=" + el.Name
		}
		for k, v := range el.Properties {
			s += fmt.Sprintf(" %s=%v", k, v)
		}
		parts = append(parts, s)
	}

	var links []string
	for _, link := range spec.Links {
		if link.FromPad != "" && link.ToPad != "" {
			links = append(links, fmt.Sprintf("%s.%s ! %s.%s", link.FromElement, link.FromPad, link.ToElement, link.ToPad))
		} else {
			links = append(links, fmt.Sprintf("%s ! %s", link.FromElement, link.ToElement))
		}
	}

	return fmt.Sprintf("Elements: %s\nLinks: %s", strings.Join(parts, " | "), strings.Join(links, ", "))
}

func TestPhase1_MSECopySpec_MatchesProvenPipeline(t *testing.T) {
	opts := gstreamer.SessionOpts{
		SourceURL:     "/tmp/test.ts",
		IsLive:        false,
		IsFileSource:  true,
		VideoCodec:    "h264",
		ContainerHint: "mpegts",
		AudioChannels: 2,
		OutputDir:     "/tmp/out",
	}

	spec := BuildMSEPipeline(opts)

	t.Log("Spec:\n" + specToGstLaunch(spec))

	src := spec.ElementByName("src")
	require.NotNil(t, src)
	assert.Equal(t, "tvproxysrc", src.Factory)
	assert.Equal(t, "/tmp/test.ts", src.Properties["location"])
	assert.Equal(t, false, src.Properties["is-live"])

	d := spec.ElementByName("d")
	require.NotNil(t, d)
	assert.Equal(t, "tvproxydemux", d.Factory)
	assert.Equal(t, 2, d.Properties["audio-channels"])
	assert.Equal(t, "mpegts", d.Properties["container-hint"])

	fmp4 := spec.ElementByName("fmp4")
	require.NotNil(t, fmp4)
	assert.Equal(t, "tvproxyfmp4", fmp4.Factory)
	assert.Equal(t, "/tmp/out", fmp4.Properties["output-dir"])
	assert.Equal(t, "h264", fmp4.Properties["video-codec"])
	assert.Equal(t, uint(2000), fmp4.Properties["segment-duration-ms"])

	assert.False(t, spec.HasElement("tee"), "file source — no tee")
	assert.False(t, spec.HasElement("dec"), "copy mode — no decoder")
	assert.False(t, spec.HasElement("enc"), "copy mode — no encoder")

	assertHasLink(t, spec, "src", "", "d", "")
	assertHasLink(t, spec, "d", "video", "fmp4", "video")
	assertHasLink(t, spec, "d", "audio", "fmp4", "audio")
}

func TestPhase1_MSETranscodeSpec_MatchesExpected(t *testing.T) {
	opts := gstreamer.SessionOpts{
		SourceURL:      "/tmp/test.ts",
		IsLive:         false,
		IsFileSource:   true,
		VideoCodec:     "mpeg2video",
		ContainerHint:  "mpegts",
		NeedsTranscode: true,
		HWAccel:        "vaapi",
		DecodeHWAccel:  "vaapi",
		OutputCodec:    "h264",
		Bitrate:        5000,
		AudioChannels:  2,
		OutputDir:      "/tmp/out",
	}

	spec := BuildMSEPipeline(opts)

	t.Log("Spec:\n" + specToGstLaunch(spec))

	dec := spec.ElementByName("dec")
	require.NotNil(t, dec)
	assert.Equal(t, "tvproxydecode", dec.Factory)
	assert.Equal(t, "vaapi", dec.Properties["hw-accel"])

	enc := spec.ElementByName("enc")
	require.NotNil(t, enc)
	assert.Equal(t, "tvproxyencode", enc.Factory)
	assert.Equal(t, "vaapi", enc.Properties["hw-accel"])
	assert.Equal(t, "h264", enc.Properties["codec"])
	assert.Equal(t, 5000, enc.Properties["bitrate"])

	assertHasLink(t, spec, "d", "video", "dec", "sink")
	assertHasLink(t, spec, "dec", "", "enc", "")
	assertHasLink(t, spec, "enc", "src", "fmp4", "video")
	assertHasLink(t, spec, "d", "audio", "fmp4", "audio")
}

func TestPhase1_LiveMSESpec_HasTeeAndRawRecording(t *testing.T) {
	opts := gstreamer.SessionOpts{
		SourceURL:     "http://provider.example.com/live/ch1.ts",
		IsLive:        true,
		VideoCodec:    "h264",
		ContainerHint: "mpegts",
		AudioChannels: 2,
		OutputDir:     "/record/ch1/uuid1",
	}

	spec := BuildMSEPipeline(opts)

	t.Log("Spec:\n" + specToGstLaunch(spec))

	require.True(t, spec.HasElement("tee"))
	require.True(t, spec.HasElement("q_demux"))
	require.True(t, spec.HasElement("q_raw"))
	require.True(t, spec.HasElement("rawsink"))

	rawsink := spec.ElementByName("rawsink")
	assert.Equal(t, "filesink", rawsink.Factory)
	assert.Equal(t, "/record/ch1/uuid1/source.ts", rawsink.Properties["location"])
	assert.Equal(t, false, rawsink.Properties["async"])

	qDemux := spec.ElementByName("q_demux")
	assert.Equal(t, "queue", qDemux.Factory)
	assert.Equal(t, uint(0), qDemux.Properties["max-size-buffers"])
	assert.Equal(t, uint64(0), qDemux.Properties["max-size-time"])

	assertHasLink(t, spec, "src", "", "tee", "")
	assertHasLink(t, spec, "tee", "src_0", "q_demux", "sink")
	assertHasLink(t, spec, "tee", "src_1", "q_raw", "sink")
	assertHasLink(t, spec, "q_demux", "", "d", "")
	assertHasLink(t, spec, "q_raw", "", "rawsink", "")
}

func TestPhase1_ExecutorElementOrder(t *testing.T) {
	opts := gstreamer.SessionOpts{
		SourceURL:     "/tmp/test.ts",
		IsLive:        false,
		IsFileSource:  true,
		VideoCodec:    "h264",
		ContainerHint: "mpegts",
		AudioChannels: 2,
		OutputDir:     "/tmp/out",
	}

	spec := BuildMSEPipeline(opts)

	names := make([]string, len(spec.Elements))
	for i, el := range spec.Elements {
		names[i] = el.Name
	}

	srcIdx := indexOf(names, "src")
	dIdx := indexOf(names, "d")
	fmp4Idx := indexOf(names, "fmp4")

	assert.Less(t, srcIdx, dIdx, "src must be before demux")
	assert.Less(t, dIdx, fmp4Idx, "demux must be before fmp4")
}

func TestPhase10_MaxBitDepthProperty(t *testing.T) {
	opts := gstreamer.SessionOpts{
		SourceURL:      "/tmp/test.ts",
		IsLive:         false,
		IsFileSource:   true,
		VideoCodec:     "h265",
		NeedsTranscode: true,
		HWAccel:        "vaapi",
		DecodeHWAccel:  "vaapi",
		OutputCodec:    "h264",
		MaxBitDepth:    8,
		OutputDir:      "/tmp/out",
	}

	spec := BuildMSEPipeline(opts)

	dec := spec.ElementByName("dec")
	require.NotNil(t, dec)
	assert.Equal(t, 8, dec.Properties["max-bit-depth"])
}

func TestPhase10_MaxBitDepthZeroOmitted(t *testing.T) {
	opts := gstreamer.SessionOpts{
		SourceURL:      "/tmp/test.ts",
		IsLive:         false,
		IsFileSource:   true,
		VideoCodec:     "h264",
		NeedsTranscode: true,
		HWAccel:        "none",
		OutputCodec:    "h264",
		MaxBitDepth:    0,
		OutputDir:      "/tmp/out",
	}

	spec := BuildMSEPipeline(opts)

	dec := spec.ElementByName("dec")
	require.NotNil(t, dec)
	_, hasMaxBitDepth := dec.Properties["max-bit-depth"]
	assert.False(t, hasMaxBitDepth, "max-bit-depth=0 should not be set")
}

func TestPhase10_ElementOverrides(t *testing.T) {
	opts := gstreamer.SessionOpts{
		SourceURL:           "/tmp/test.ts",
		IsLive:              false,
		IsFileSource:        true,
		VideoCodec:          "h264",
		NeedsTranscode:      true,
		HWAccel:             "vaapi",
		DecodeHWAccel:       "vaapi",
		OutputCodec:         "h264",
		VideoDecoderElement: "vah264dec",
		VideoEncoderElement: "vah264lpenc",
		OutputDir:           "/tmp/out",
	}

	spec := BuildMSEPipeline(opts)

	dec := spec.ElementByName("dec")
	require.NotNil(t, dec)
	assert.Equal(t, "vah264dec", dec.Properties["element-override"])

	enc := spec.ElementByName("enc")
	require.NotNil(t, enc)
	assert.Equal(t, "vah264lpenc", enc.Properties["element-override"])
}

func TestPhase10_NoOverridesOmitted(t *testing.T) {
	opts := gstreamer.SessionOpts{
		SourceURL:      "/tmp/test.ts",
		IsLive:         false,
		IsFileSource:   true,
		VideoCodec:     "mpeg2video",
		NeedsTranscode: true,
		HWAccel:        "none",
		OutputCodec:    "h264",
		OutputDir:      "/tmp/out",
	}

	spec := BuildMSEPipeline(opts)

	dec := spec.ElementByName("dec")
	require.NotNil(t, dec)
	_, hasOverride := dec.Properties["element-override"]
	assert.False(t, hasOverride, "no override should not set element-override")

	enc := spec.ElementByName("enc")
	require.NotNil(t, enc)
	_, hasEncOverride := enc.Properties["element-override"]
	assert.False(t, hasEncOverride, "no override should not set element-override")
}

func TestPhase11_SourceProfileProperties(t *testing.T) {
	opts := gstreamer.SessionOpts{
		SourceURL:     "rtsp://192.168.1.100/?freq=586&msys=dvbt2",
		IsLive:        true,
		UserAgent:     "TVProxy/1.0",
		HTTPTimeout:   10,
		RTSPLatency:   200,
		RTSPTransport: "tcp",
		VideoCodec:    "h264",
		AudioChannels: 2,
		OutputDir:     "/tmp/out",
	}

	spec := BuildMSEPipeline(opts)

	src := spec.ElementByName("src")
	require.NotNil(t, src)
	assert.Equal(t, "TVProxy/1.0", src.Properties["user-agent"])
	assert.Equal(t, 10, src.Properties["timeout"])
	assert.Equal(t, 200, src.Properties["rtsp-latency"])
	assert.Equal(t, "tcp", src.Properties["rtsp-transport"])
}

func TestPhase11_SourceProfileDefaults(t *testing.T) {
	opts := gstreamer.SessionOpts{
		SourceURL:     "http://example.com/stream.ts",
		IsLive:        true,
		VideoCodec:    "h264",
		AudioChannels: 2,
		OutputDir:     "/tmp/out",
	}

	spec := BuildMSEPipeline(opts)

	src := spec.ElementByName("src")
	require.NotNil(t, src)
	_, hasUA := src.Properties["user-agent"]
	assert.False(t, hasUA, "empty user-agent should not be set")
	_, hasTimeout := src.Properties["timeout"]
	assert.False(t, hasTimeout, "zero timeout should not be set")
	_, hasLatency := src.Properties["rtsp-latency"]
	assert.False(t, hasLatency, "zero rtsp-latency should not be set")
	_, hasTransport := src.Properties["rtsp-transport"]
	assert.False(t, hasTransport, "empty rtsp-transport should not be set")
}

func indexOf(slice []string, item string) int {
	for i, s := range slice {
		if s == item {
			return i
		}
	}
	return -1
}
