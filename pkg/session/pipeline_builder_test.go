package session

import (
	"testing"

	"github.com/gavinmcnair/tvproxy/pkg/gstreamer"
)

func TestBuildMSEPipeline_Copy_Live(t *testing.T) {
	opts := gstreamer.SessionOpts{
		SourceURL:    "http://example.com/stream.ts",
		IsLive:       true,
		VideoCodec:   "h264",
		OutputDir:    "/record/ch1/uuid1",
	}

	spec := BuildMSEPipeline(opts)

	assertHasElement(t, spec, "src", "tvproxysrc")
	assertHasElement(t, spec, "tee", "tee")
	assertHasElement(t, spec, "q_demux", "queue")
	assertHasElement(t, spec, "q_raw", "queue")
	assertHasElement(t, spec, "rawsink", "filesink")
	assertHasElement(t, spec, "d", "tvproxydemux")
	assertHasElement(t, spec, "fmp4", "tvproxyfmp4")

	if spec.HasElement("dec") {
		t.Error("copy pipeline should not have decoder")
	}
	if spec.HasElement("enc") {
		t.Error("copy pipeline should not have encoder")
	}

	fmp4 := spec.ElementByName("fmp4")
	if fmp4.Properties["video-codec"] != "h264" {
		t.Errorf("fmp4 video-codec = %v, want h264", fmp4.Properties["video-codec"])
	}
	if fmp4.Properties["output-dir"] != "/record/ch1/uuid1" {
		t.Errorf("fmp4 output-dir = %v, want /record/ch1/uuid1", fmp4.Properties["output-dir"])
	}

	rawsink := spec.ElementByName("rawsink")
	if rawsink.Properties["location"] != "/record/ch1/uuid1/source.ts" {
		t.Errorf("rawsink location = %v, want source.ts path", rawsink.Properties["location"])
	}
	if rawsink.Properties["async"] != false {
		t.Errorf("rawsink async must be false to prevent preroll deadlock")
	}

	assertHasLink(t, spec, "d", "video", "fmp4", "video")
	assertHasLink(t, spec, "d", "audio", "fmp4", "audio")
}

func TestBuildMSEPipeline_Transcode(t *testing.T) {
	opts := gstreamer.SessionOpts{
		SourceURL:      "http://example.com/stream.ts",
		IsLive:         true,
		VideoCodec:     "mpeg2video",
		NeedsTranscode: true,
		HWAccel:        "vaapi",
		DecodeHWAccel:  "vaapi",
		OutputCodec:    "h264",
		Bitrate:        5000,
		OutputDir:      "/record/ch1/uuid2",
	}

	spec := BuildMSEPipeline(opts)

	assertHasElement(t, spec, "dec", "tvproxydecode")
	assertHasElement(t, spec, "enc", "tvproxyencode")

	dec := spec.ElementByName("dec")
	if dec.Properties["hw-accel"] != "vaapi" {
		t.Errorf("dec hw-accel = %v, want vaapi", dec.Properties["hw-accel"])
	}

	enc := spec.ElementByName("enc")
	if enc.Properties["codec"] != "h264" {
		t.Errorf("enc codec = %v, want h264", enc.Properties["codec"])
	}
	if enc.Properties["bitrate"] != 5000 {
		t.Errorf("enc bitrate = %v, want 5000", enc.Properties["bitrate"])
	}

	assertHasLink(t, spec, "d", "video", "dec", "sink")
	assertHasLink(t, spec, "enc", "src", "fmp4", "video")
	assertHasLink(t, spec, "d", "audio", "fmp4", "audio")
}

func TestBuildMSEPipeline_FileSource_NoTee(t *testing.T) {
	opts := gstreamer.SessionOpts{
		SourceURL:    "/recordings/source.ts",
		IsLive:       false,
		IsFileSource: true,
		VideoCodec:   "h264",
		OutputDir:    "/record/ch1/uuid3",
	}

	spec := BuildMSEPipeline(opts)

	if spec.HasElement("tee") {
		t.Error("file source should not have tee")
	}
	if spec.HasElement("q_raw") {
		t.Error("file source should not have raw queue")
	}
	if spec.HasElement("rawsink") {
		t.Error("file source should not have rawsink")
	}

	assertHasLink(t, spec, "src", "", "d", "")
}

func TestBuildStreamPipeline_Passthrough(t *testing.T) {
	opts := gstreamer.SessionOpts{
		SourceURL:     "http://example.com/stream.ts",
		IsLive:        true,
		OutputDir:     "/record/ch1/uuid4",
		MuxOutputPath: "/record/ch1/uuid4/output.ts",
	}

	spec := BuildStreamPipeline(opts)

	assertHasElement(t, spec, "src", "tvproxysrc")
	assertHasElement(t, spec, "d", "tvproxydemux")
	assertHasElement(t, spec, "mux", "tvproxymux")
	assertHasElement(t, spec, "sink", "filesink")

	sink := spec.ElementByName("sink")
	if sink.Properties["location"] != "/record/ch1/uuid4/output.ts" {
		t.Errorf("sink location = %v, want output.ts path", sink.Properties["location"])
	}
	if sink.Properties["async"] != false {
		t.Error("stream sink async must be false")
	}

	demux := spec.ElementByName("d")
	if demux.Properties["audio-codec"] != "copy" {
		t.Errorf("stream passthrough demux audio-codec = %v, want copy", demux.Properties["audio-codec"])
	}
	if demux.Properties["audio-channels"] != 0 {
		t.Errorf("stream passthrough demux audio-channels = %v, want 0", demux.Properties["audio-channels"])
	}

	if spec.HasElement("dec") || spec.HasElement("enc") {
		t.Error("passthrough should not have decode/encode")
	}

	assertHasLink(t, spec, "d", "video", "mux", "video")
	assertHasLink(t, spec, "d", "audio", "mux", "audio")
	assertHasLink(t, spec, "mux", "", "sink", "")
}

func TestBuildStreamTranscodePipeline(t *testing.T) {
	opts := gstreamer.SessionOpts{
		SourceURL:      "http://example.com/stream.ts",
		IsLive:         true,
		NeedsTranscode: true,
		HWAccel:        "none",
		OutputCodec:    "h264",
		Bitrate:        3000,
		OutputDir:      "/record/ch1/uuid5",
		MuxOutputPath:  "/record/ch1/uuid5/output.ts",
	}

	spec := BuildStreamTranscodePipeline(opts)

	assertHasElement(t, spec, "dec", "tvproxydecode")
	assertHasElement(t, spec, "enc", "tvproxyencode")
	assertHasElement(t, spec, "mux", "tvproxymux")
	assertHasElement(t, spec, "sink", "filesink")

	assertHasLink(t, spec, "enc", "src", "mux", "video")
	assertHasLink(t, spec, "d", "audio", "mux", "audio")
	assertHasLink(t, spec, "mux", "", "sink", "")
}

func TestBuildMSEPipeline_DecodeHWAccelFallback(t *testing.T) {
	opts := gstreamer.SessionOpts{
		SourceURL:      "http://example.com/stream.ts",
		IsLive:         true,
		NeedsTranscode: true,
		HWAccel:        "qsv",
		OutputCodec:    "h265",
		OutputDir:      "/record/ch1/uuid6",
	}

	spec := BuildMSEPipeline(opts)

	dec := spec.ElementByName("dec")
	if dec.Properties["hw-accel"] != "qsv" {
		t.Errorf("when DecodeHWAccel empty, should fall back to HWAccel: got %v, want qsv", dec.Properties["hw-accel"])
	}
}

func TestBuildMSEPipeline_AudioLanguage(t *testing.T) {
	opts := gstreamer.SessionOpts{
		SourceURL:     "http://example.com/stream.ts",
		IsLive:        true,
		VideoCodec:    "h264",
		AudioLanguage: "eng",
		OutputDir:     "/record/ch1/uuid7",
	}

	spec := BuildMSEPipeline(opts)

	demux := spec.ElementByName("d")
	if demux.Properties["audio-language"] != "eng" {
		t.Errorf("demux audio-language = %v, want eng", demux.Properties["audio-language"])
	}
}

func TestBuildMSEPipeline_ContainerHint(t *testing.T) {
	opts := gstreamer.SessionOpts{
		SourceURL:     "http://example.com/stream.ts",
		IsLive:        true,
		VideoCodec:    "h264",
		ContainerHint: "mpegts",
		OutputDir:     "/record/ch1/uuid8",
	}

	spec := BuildMSEPipeline(opts)

	demux := spec.ElementByName("d")
	if demux.Properties["container-hint"] != "mpegts" {
		t.Errorf("demux container-hint = %v, want mpegts", demux.Properties["container-hint"])
	}
}

func TestBuildMSEPipeline_DefaultSegmentDuration(t *testing.T) {
	opts := gstreamer.SessionOpts{
		SourceURL:  "http://example.com/stream.ts",
		IsLive:     true,
		VideoCodec: "h264",
		OutputDir:  "/record/ch1/uuid9",
	}

	spec := BuildMSEPipeline(opts)

	fmp4 := spec.ElementByName("fmp4")
	if fmp4.Properties["segment-duration-ms"] != uint(2000) {
		t.Errorf("default segment-duration-ms = %v, want 2000", fmp4.Properties["segment-duration-ms"])
	}
}

func TestBuildMSEPipeline_CustomSegmentDuration(t *testing.T) {
	opts := gstreamer.SessionOpts{
		SourceURL:         "http://example.com/stream.ts",
		IsLive:            true,
		VideoCodec:        "h264",
		SegmentDurationMs: 4000,
		OutputDir:         "/record/ch1/uuid10",
	}

	spec := BuildMSEPipeline(opts)

	fmp4 := spec.ElementByName("fmp4")
	if fmp4.Properties["segment-duration-ms"] != uint(4000) {
		t.Errorf("custom segment-duration-ms = %v, want 4000", fmp4.Properties["segment-duration-ms"])
	}
}

func TestBuildRecordingPlaybackPipeline(t *testing.T) {
	spec := BuildRecordingPlaybackPipeline("/recordings/stream1/uuid1/source.ts", "/record/playback/uuid2")

	assertHasElement(t, spec, "src", "tvproxysrc")
	assertHasElement(t, spec, "d", "tvproxydemux")
	assertHasElement(t, spec, "fmp4", "tvproxyfmp4")

	src := spec.ElementByName("src")
	if src.Properties["location"] != "/recordings/stream1/uuid1/source.ts" {
		t.Errorf("src location = %v, want source.ts path", src.Properties["location"])
	}
	if src.Properties["is-live"] != false {
		t.Error("recording playback should not be live")
	}

	if spec.HasElement("tee") {
		t.Error("recording playback should not have tee (file source)")
	}
	if spec.HasElement("q_raw") {
		t.Error("recording playback should not have raw queue")
	}
	if spec.HasElement("rawsink") {
		t.Error("recording playback should not have rawsink")
	}

	fmp4 := spec.ElementByName("fmp4")
	if fmp4.Properties["output-dir"] != "/record/playback/uuid2" {
		t.Errorf("fmp4 output-dir = %v, want playback output dir", fmp4.Properties["output-dir"])
	}

	assertHasLink(t, spec, "src", "", "d", "")
}

func assertHasElement(t *testing.T, spec gstreamer.PipelineSpec, name, factory string) {
	t.Helper()
	el := spec.ElementByName(name)
	if el == nil {
		t.Errorf("missing element %q", name)
		return
	}
	if el.Factory != factory {
		t.Errorf("element %q factory = %q, want %q", name, el.Factory, factory)
	}
}

func assertHasLink(t *testing.T, spec gstreamer.PipelineSpec, from, fromPad, to, toPad string) {
	t.Helper()
	for _, link := range spec.Links {
		if link.FromElement == from && link.ToElement == to {
			if fromPad == "" && toPad == "" {
				return
			}
			if link.FromPad == fromPad && link.ToPad == toPad {
				return
			}
		}
	}
	if fromPad != "" || toPad != "" {
		t.Errorf("missing pad link %s.%s → %s.%s", from, fromPad, to, toPad)
	} else {
		t.Errorf("missing link %s → %s", from, to)
	}
}
