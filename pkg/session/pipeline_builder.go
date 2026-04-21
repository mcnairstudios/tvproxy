package session

import (
	"path/filepath"

	"github.com/gavinmcnair/tvproxy/pkg/gstreamer"
)

func BuildMSEPipeline(opts gstreamer.SessionOpts) gstreamer.PipelineSpec {
	spec := gstreamer.PipelineSpec{}

	addSource(&spec, opts)
	addRawRecording(&spec, opts)
	addDemux(&spec, opts)

	if opts.NeedsTranscode {
		addTranscodeChain(&spec, opts)
		spec.LinkPads("enc", "src", "fmp4", "video")
	} else {
		spec.LinkPads("d", "video", "fmp4", "video")
	}
	spec.LinkPads("d", "audio", "fmp4", "audio")

	videoCodec := resolveOutputCodec(opts)
	spec.AddElement("fmp4", "tvproxyfmp4", gstreamer.Props{
		"output-dir":          opts.OutputDir,
		"video-codec":         videoCodec,
		"segment-duration-ms": segmentDuration(opts),
	})

	return spec
}

func BuildStreamPipeline(opts gstreamer.SessionOpts) gstreamer.PipelineSpec {
	spec := gstreamer.PipelineSpec{}

	addSource(&spec, opts)
	addRawRecording(&spec, opts)

	spec.AddElement("d", "tvproxydemux", demuxProps(opts, true))
	linkSourceToDemux(&spec, opts)

	spec.AddElement("mux", "tvproxymux", nil)
	addStreamSink(&spec, opts)

	spec.LinkPads("d", "video", "mux", "video")
	spec.LinkPads("d", "audio", "mux", "audio")
	spec.Link("mux", "sink")

	return spec
}

func BuildStreamTranscodePipeline(opts gstreamer.SessionOpts) gstreamer.PipelineSpec {
	spec := gstreamer.PipelineSpec{}

	addSource(&spec, opts)
	addRawRecording(&spec, opts)
	addDemux(&spec, opts)
	addTranscodeChain(&spec, opts)

	spec.AddElement("mux", "tvproxymux", nil)
	addStreamSink(&spec, opts)

	spec.LinkPads("enc", "src", "mux", "video")
	spec.LinkPads("d", "audio", "mux", "audio")
	spec.Link("mux", "sink")

	return spec
}

func addSource(spec *gstreamer.PipelineSpec, opts gstreamer.SessionOpts) {
	spec.AddElement("src", "tvproxysrc", gstreamer.Props{
		"location": opts.SourceURL,
		"is-live":  opts.IsLive,
	})
}

func addRawRecording(spec *gstreamer.PipelineSpec, opts gstreamer.SessionOpts) {
	if opts.IsFileSource {
		return
	}

	spec.AddElement("tee", "tee", nil)
	spec.AddElement("q_demux", "queue", unboundedQueue())
	spec.AddElement("q_raw", "queue", unboundedQueue())
	spec.AddElement("rawsink", "filesink", gstreamer.Props{
		"location": filepath.Join(opts.OutputDir, "source.ts"),
		"async":    false,
	})

	spec.Link("src", "tee")
	spec.LinkPads("tee", "src_0", "q_demux", "sink")
	spec.LinkPads("tee", "src_1", "q_raw", "sink")
	spec.Link("q_raw", "rawsink")
}

func addDemux(spec *gstreamer.PipelineSpec, opts gstreamer.SessionOpts) {
	spec.AddElement("d", "tvproxydemux", demuxProps(opts, false))
	linkSourceToDemux(spec, opts)
}

func linkSourceToDemux(spec *gstreamer.PipelineSpec, opts gstreamer.SessionOpts) {
	if opts.IsFileSource {
		spec.Link("src", "d")
	} else {
		spec.Link("q_demux", "d")
	}
}

func addTranscodeChain(spec *gstreamer.PipelineSpec, opts gstreamer.SessionOpts) {
	decHW := opts.DecodeHWAccel
	if decHW == "" {
		decHW = opts.HWAccel
	}

	spec.AddElement("dec", "tvproxydecode", gstreamer.Props{
		"hw-accel": decHW,
	})

	encProps := gstreamer.Props{
		"hw-accel": opts.HWAccel,
		"codec":    opts.OutputCodec,
	}
	if opts.Bitrate > 0 {
		encProps["bitrate"] = opts.Bitrate
	}
	spec.AddElement("enc", "tvproxyencode", encProps)

	spec.LinkPads("d", "video", "dec", "sink")
	spec.Link("dec", "enc")
}

func demuxProps(opts gstreamer.SessionOpts, passthrough bool) gstreamer.Props {
	props := gstreamer.Props{}

	if passthrough {
		props["audio-codec"] = "copy"
		props["audio-channels"] = 0
	} else {
		channels := opts.AudioChannels
		if channels == 0 {
			channels = 2
		}
		props["audio-channels"] = channels
	}

	if opts.AudioLanguage != "" {
		props["audio-language"] = opts.AudioLanguage
	}
	if opts.ContainerHint != "" {
		props["container-hint"] = opts.ContainerHint
	}

	return props
}

func resolveOutputCodec(opts gstreamer.SessionOpts) string {
	if opts.OutputCodec != "" {
		return opts.OutputCodec
	}
	if opts.VideoCodec != "" {
		return opts.VideoCodec
	}
	return "h264"
}

func segmentDuration(opts gstreamer.SessionOpts) uint {
	if opts.SegmentDurationMs > 0 {
		return opts.SegmentDurationMs
	}
	return 2000
}

func BuildRecordingPlaybackPipeline(sourceTSPath, outputDir string) gstreamer.PipelineSpec {
	return BuildMSEPipeline(gstreamer.SessionOpts{
		SourceURL:    sourceTSPath,
		IsLive:       false,
		IsFileSource: true,
		OutputDir:    outputDir,
	})
}

func addStreamSink(spec *gstreamer.PipelineSpec, opts gstreamer.SessionOpts) {
	spec.AddElement("sink", "filesink", gstreamer.Props{
		"location": opts.MuxOutputPath,
		"async":    false,
	})
}

func unboundedQueue() gstreamer.Props {
	return gstreamer.Props{
		"max-size-buffers": uint(0),
		"max-size-time":    uint64(0),
		"max-size-bytes":   uint(0),
	}
}
