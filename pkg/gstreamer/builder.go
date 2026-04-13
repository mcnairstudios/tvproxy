package gstreamer

import (
	"fmt"
	"strings"
	"sync"

	"github.com/go-gst/go-gst/gst"
)

func Build(opts PipelineOpts) (*gst.Pipeline, string, error) {
	container := strings.ToLower(opts.Container)
	isRTSP := strings.HasPrefix(opts.InputURL, "rtsp://") || strings.HasPrefix(opts.InputURL, "rtsps://")

	if container == "" {
		container = containerFromURL(opts.InputURL)
	}
	isMPEGTS := isRTSP || container == "mpegts" || container == "mpeg-ts" || container == "ts" || container == ""

	srcCodec := NormalizeCodec(opts.VideoCodec)
	outCodec := NormalizeCodec(opts.OutputVideoCodec)
	isCopy := outCodec == "" || outCodec == "default" || outCodec == "copy" || outCodec == srcCodec

	if isMPEGTS {
		mode := "transcode"
		if isCopy {
			mode = "copy"
		}
		path := fmt.Sprintf("mpegts-%s", mode)
		if isRTSP {
			path = "rtsp-" + mode
		}
		p, err := buildMPEGTSNative(opts, srcCodec, isRTSP)
		return p, path, err
	}
	mode := "transcode"
	if isCopy {
		mode = "copy"
	}
	p, err := buildNonMPEGTSNative(opts, srcCodec)
	return p, fmt.Sprintf("vod-%s-%s", container, mode), err
}

// buildMPEGTSPluginCopy builds a string pipeline using tvproxy plugins.
// NOTE: Currently not called — go-gst NewPipelineFromString produces 0 bytes with plugin bins.
// The pipeline works with gst-launch-1.0 CLI. When the go-gst issue is resolved, this path
// can be re-enabled for fastest MPEG-TS copy (bypasses tsdemux pad-added entirely).
func buildMPEGTSPluginCopy(opts PipelineOpts, srcCodec string) (*gst.Pipeline, error) {
	srcAudio := NormalizeCodec(opts.AudioCodec)
	audioMode := "aac"
	if srcAudio == "aac" {
		audioMode = "copy"
	}

	pipeStr := fmt.Sprintf(
		"tvproxysrc location=%s is-live=true"+
			" ! tvproxydemux name=d container-hint=mpegts video-codec-hint=%s audio-codec-hint=%s audio-codec=%s"+
			" d.video ! m.video"+
			" d.audio ! m.audio"+
			" tvproxymux name=m output-format=mp4"+
			" ! filesink location=%s",
		opts.InputURL, srcCodec, srcAudio, audioMode, opts.RecordingPath)

	return gst.NewPipelineFromString(pipeStr)
}

func buildMPEGTSNative(opts PipelineOpts, srcCodec string, isRTSP bool) (*gst.Pipeline, error) {
	pipeline, err := gst.NewPipeline("tvproxy")
	if err != nil {
		return nil, err
	}

	var sourceElements []*gst.Element
	var linkStart *gst.Element

	if isRTSP {
		src, _ := gst.NewElement("rtspsrc")
		src.SetProperty("location", opts.InputURL)
		src.SetProperty("latency", uint(0))
		src.SetProperty("protocols", uint(4))
		src.SetProperty("buffer-mode", uint(0))
		if opts.UserAgent != "" {
			src.SetProperty("user-agent", opts.UserAgent)
		}
		depay, _ := gst.NewElement("rtpmp2tdepay")
		src.Connect("pad-added", func(self *gst.Element, pad *gst.Pad) {
			sinkPad := depay.GetStaticPad("sink")
			if sinkPad != nil && !sinkPad.IsLinked() {
				pad.Link(sinkPad)
			}
		})
		sourceElements = []*gst.Element{src, depay}
		linkStart = depay
	} else {
		src, _ := gst.NewElement("souphttpsrc")
		src.SetProperty("location", opts.InputURL)
		src.SetProperty("do-timestamp", true)
		src.SetProperty("is-live", true)
		if opts.UserAgent != "" {
			src.SetProperty("user-agent", opts.UserAgent)
		}
		if len(opts.ExtraHeaders) > 0 {
			headers := gst.NewStructure("extra-headers")
			for k, v := range opts.ExtraHeaders {
				headers.SetValue(k, v)
			}
			src.SetProperty("extra-headers", headers)
		}
		sourceElements = []*gst.Element{src}
		linkStart = src
	}

	tsparse, _ := gst.NewElement("tsparse")
	tsparse.SetProperty("set-timestamps", true)
	demux, _ := gst.NewElement("tsdemux")

	vQueue, _ := gst.NewElement("queue")
	vQueue.SetProperty("max-size-time", uint64(10000000000))
	aQueue, _ := gst.NewElement("queue")
	aQueue.SetProperty("max-size-time", uint64(10000000000))

	hw := opts.HWAccel
	outCodec := NormalizeCodec(opts.OutputVideoCodec)
	isCopy := outCodec == "" || outCodec == "default" || outCodec == "copy" || outCodec == srcCodec

	var videoElements []*gst.Element
	if isCopy {
		videoElements = createOutputParser(srcCodec)
	} else {
		videoElements = append(videoElements, createHWDecoder(srcCodec, hw)...)
		videoElements = append(videoElements, createHWEncoder(outCodec, hw, bitrate(opts))...)
		videoElements = append(videoElements, createOutputParser(outCodec)...)
	}

	audioElements := buildAudioChain(NormalizeCodec(opts.AudioCodec))

	var mux *gst.Element
	if opts.OutputFormat == OutputMPEGTS || (isCopy && opts.OutputFormat == "") {
		mux, _ = gst.NewElement("mpegtsmux")
	} else {
		mux, _ = gst.NewElement("mp4mux")
		mux.SetProperty("fragment-duration", uint(2000))
		mux.SetProperty("streamable", true)
	}
	sink, _ := gst.NewElement("filesink")
	sink.SetProperty("location", opts.RecordingPath)

	var all []*gst.Element
	all = append(all, sourceElements...)
	all = append(all, tsparse, demux, vQueue, aQueue)
	all = append(all, videoElements...)
	all = append(all, audioElements...)
	all = append(all, mux, sink)

	if err := checkNilElements(all); err != nil {
		return nil, err
	}

	pipeline.AddMany(all...)
	gst.ElementLinkMany(linkStart, tsparse, demux)

	vChain := []*gst.Element{vQueue}
	vChain = append(vChain, videoElements...)
	vChain = append(vChain, mux)
	gst.ElementLinkMany(vChain...)

	aChain := []*gst.Element{aQueue}
	aChain = append(aChain, audioElements...)
	aChain = append(aChain, mux)
	gst.ElementLinkMany(aChain...)

	gst.ElementLinkMany(mux, sink)

	var videoOnce, audioOnce sync.Once
	demux.Connect("pad-added", func(self *gst.Element, pad *gst.Pad) {
		caps := pad.GetCurrentCaps()
		if caps == nil {
			return
		}
		name := caps.GetStructureAt(0).Name()
		if strings.HasPrefix(name, "video") {
			videoOnce.Do(func() { pad.Link(vQueue.GetStaticPad("sink")) })
		} else if strings.Contains(name, "audio") {
			audioOnce.Do(func() { pad.Link(aQueue.GetStaticPad("sink")) })
		}
	})

	return pipeline, nil
}

func buildNonMPEGTSNative(opts PipelineOpts, srcCodec string) (*gst.Pipeline, error) {
	pipeline, err := gst.NewPipeline("tvproxy")
	if err != nil {
		return nil, err
	}

	src, _ := gst.NewElement("souphttpsrc")
	src.SetProperty("location", opts.InputURL)
	if opts.UserAgent != "" {
		src.SetProperty("user-agent", opts.UserAgent)
	}
	if len(opts.ExtraHeaders) > 0 {
		headers := gst.NewStructure("extra-headers")
		for k, v := range opts.ExtraHeaders {
			headers.SetValue(k, v)
		}
		src.SetProperty("extra-headers", headers)
	}

	container := strings.ToLower(opts.Container)
	if container == "" {
		container = containerFromURL(opts.InputURL)
	}
	var demux *gst.Element
	switch container {
	case "matroska", "webm":
		demux, _ = gst.NewElement("matroskademux")
	case "flv":
		demux, _ = gst.NewElement("flvdemux")
	default:
		demux, _ = gst.NewElement("qtdemux")
	}

	vQueue, _ := gst.NewElement("queue")
	vQueue.SetProperty("max-size-time", uint64(10000000000))
	aQueue, _ := gst.NewElement("queue")
	aQueue.SetProperty("max-size-time", uint64(10000000000))

	hw := opts.HWAccel
	outCodec := NormalizeCodec(opts.OutputVideoCodec)
	isCopy := outCodec == "" || outCodec == "default" || outCodec == "copy" || outCodec == srcCodec

	var videoElements []*gst.Element
	if isCopy {
		videoElements = createOutputParser(srcCodec)
	} else {
		videoElements = append(videoElements, createHWDecoder(srcCodec, hw)...)
		videoElements = append(videoElements, createHWEncoder(outCodec, hw, bitrate(opts))...)
		videoElements = append(videoElements, createOutputParser(outCodec)...)
	}

	srcAudio := NormalizeCodec(opts.AudioCodec)
	var audioElements []*gst.Element
	if srcAudio == "aac" || srcAudio == "" {
		aPass, _ := gst.NewElement("aacparse")
		audioElements = []*gst.Element{aPass}
	} else {
		audioElements = buildAudioChain(srcAudio)
	}

	mux, _ := gst.NewElement("mp4mux")
	mux.SetProperty("fragment-duration", uint(2000))
	mux.SetProperty("streamable", true)
	sink, _ := gst.NewElement("filesink")
	sink.SetProperty("location", opts.RecordingPath)

	var all []*gst.Element
	all = append(all, src, demux, vQueue, aQueue)
	all = append(all, videoElements...)
	all = append(all, audioElements...)
	all = append(all, mux, sink)

	if err := checkNilElements(all); err != nil {
		return nil, err
	}

	pipeline.AddMany(all...)
	gst.ElementLinkMany(src, demux)

	vChain := []*gst.Element{vQueue}
	vChain = append(vChain, videoElements...)
	vChain = append(vChain, mux)
	gst.ElementLinkMany(vChain...)

	aChain := []*gst.Element{aQueue}
	aChain = append(aChain, audioElements...)
	aChain = append(aChain, mux)
	gst.ElementLinkMany(aChain...)
	gst.ElementLinkMany(mux, sink)

	var videoOnce, audioOnce sync.Once
	demux.Connect("pad-added", func(self *gst.Element, pad *gst.Pad) {
		padName := pad.GetName()
		capsName := ""
		if c := pad.GetCurrentCaps(); c != nil {
			capsName = c.GetStructureAt(0).Name()
		}
		isVideo := strings.HasPrefix(capsName, "video") || strings.HasPrefix(padName, "video")
		isAudio := strings.Contains(capsName, "audio") || strings.HasPrefix(padName, "audio")
		if isVideo {
			videoOnce.Do(func() { pad.Link(vQueue.GetStaticPad("sink")) })
		} else if isAudio {
			audioOnce.Do(func() { pad.Link(aQueue.GetStaticPad("sink")) })
		}
	})

	return pipeline, nil
}

func checkNilElements(elements []*gst.Element) error {
	for i, el := range elements {
		if el == nil {
			return fmt.Errorf("pipeline element %d is nil (missing GStreamer plugin — check gst-inspect-1.0)", i)
		}
	}
	return nil
}

func containerFromURL(url string) string {
	u := strings.ToLower(url)
	if idx := strings.Index(u, "?"); idx > 0 {
		u = u[:idx]
	}
	if idx := strings.Index(u, "#"); idx > 0 {
		u = u[:idx]
	}
	if strings.HasSuffix(u, ".mp4") || strings.HasSuffix(u, ".m4v") {
		return "mp4"
	}
	if strings.HasSuffix(u, ".mkv") {
		return "matroska"
	}
	if strings.HasSuffix(u, ".webm") {
		return "webm"
	}
	if strings.HasSuffix(u, ".mov") {
		return "mp4"
	}
	if strings.HasSuffix(u, ".avi") {
		return "avi"
	}
	if strings.HasSuffix(u, ".ts") {
		return "mpegts"
	}
	if strings.HasSuffix(u, ".flv") {
		return "flv"
	}
	return ""
}

func bitrate(opts PipelineOpts) int {
	if opts.OutputBitrate > 0 {
		return opts.OutputBitrate
	}
	return 6000
}
