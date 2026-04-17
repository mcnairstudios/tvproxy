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
	isCopy := outCodec == "" || outCodec == "default" || outCodec == "copy"

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

	audioLang := ""
	if opts.AudioLanguage != "" {
		audioLang = " audio-language=" + opts.AudioLanguage
	}

	pipeStr := fmt.Sprintf(
		"tvproxysrc location=%s is-live=true"+
			" ! tvproxydemux name=d container-hint=mpegts video-codec-hint=%s audio-codec-hint=%s audio-codec=%s%s"+
			" d.video ! m.video"+
			" d.audio ! m.audio"+
			" tvproxymux name=m output-format=mp4"+
			" ! filesink location=%s",
		opts.InputURL, srcCodec, srcAudio, audioMode, audioLang, opts.RecordingPath)

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
		rtspLatency := uint(0)
		if opts.RTSPLatency > 0 {
			rtspLatency = uint(opts.RTSPLatency)
		}
		src.SetProperty("latency", rtspLatency)
		rtspProto := uint(4)
		if opts.RTSPProtocols == "udp" {
			rtspProto = 1
		}
		src.SetProperty("protocols", rtspProto)
		src.SetProperty("buffer-mode", uint(opts.RTSPBufferMode))
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
		if opts.HTTPTimeoutSec > 0 {
			src.SetProperty("timeout", uint(opts.HTTPTimeoutSec))
		}
		if opts.HTTPRetries > 0 {
			src.SetProperty("retries", uint(opts.HTTPRetries))
		}
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
	tsparse.SetProperty("set-timestamps", opts.TSSetTimestamps)
	demux, _ := gst.NewElement("tsdemux")

	defaultQueueNs := uint64(10000000000)
	if opts.UseAppSink {
		defaultQueueNs = 3000000000
	}
	vQueueNs := defaultQueueNs
	if opts.VideoQueueMs > 0 {
		vQueueNs = uint64(opts.VideoQueueMs) * 1000000
	}
	aQueueNs := defaultQueueNs
	if opts.AudioQueueMs > 0 {
		aQueueNs = uint64(opts.AudioQueueMs) * 1000000
	}
	vQueue, _ := gst.NewElement("queue")
	vQueue.SetProperty("max-size-time", vQueueNs)
	aQueue, _ := gst.NewElement("queue")
	aQueue.SetProperty("max-size-time", aQueueNs)

	hw := opts.HWAccel
	outCodec := NormalizeCodec(opts.OutputVideoCodec)
	isCopy := outCodec == "" || outCodec == "default" || outCodec == "copy"

	var videoElements []*gst.Element
	if isCopy {
		videoElements = createOutputParser(srcCodec)
	} else {
		decHW := hw
		if strings.Contains(opts.SourcePixFmt, "10") || strings.Contains(opts.SourcePixFmt, "12") {
			decHW = HWNone
		}
		if hw == HWVideoToolbox && (srcCodec == "h265" || srcCodec == "hevc") {
			decHW = HWNone
		}
		videoElements = append(videoElements, createHWDecoder(srcCodec, decHW)...)
		if opts.Deinterlace {
			di, _ := gst.NewElement("deinterlace")
			if di != nil {
				videoElements = append(videoElements, di)
			}
		}
		scaleHeight := opts.OutputHeight
		if scaleHeight == 0 && opts.UseAppSink && opts.SourceHeight > 0 && opts.SourceHeight < 1080 {
			scaleHeight = 1080
		}
		vconv, _ := gst.NewElement("videoconvert")
		if vconv != nil {
			videoElements = append(videoElements, vconv)
		}
		if scaleHeight > 0 {
			vscale, _ := gst.NewElement("videoscale")
			scaleCaps, _ := gst.NewElement("capsfilter")
			if vscale != nil && scaleCaps != nil {
				h := (scaleHeight + 1) &^ 1
				scaleCaps.SetProperty("caps", gst.NewCapsFromString(fmt.Sprintf("video/x-raw,height=%d,pixel-aspect-ratio=1/1", h)))
				videoElements = append(videoElements, vscale, scaleCaps)
			}
		}
		if opts.VideoEncoderElement != "" {
			videoElements = append(videoElements, createExplicitEncoder(opts.VideoEncoderElement, outCodec, bitrate(opts))...)
		} else {
			videoElements = append(videoElements, createHWEncoder(outCodec, hw, bitrate(opts))...)
		}
		videoElements = append(videoElements, createOutputParser(outCodec)...)
	}

	srcAudio := NormalizeCodec(opts.AudioCodec)
	var audioElements []*gst.Element
	if isCopy && (srcAudio == "aac" || srcAudio == "") {
		aPass, _ := gst.NewElement("aacparse")
		audioElements = []*gst.Element{aPass}
	} else {
		audioElements = buildAudioChain(srcAudio)
	}
	if opts.AudioDelayMs > 0 {
		delayQueue, _ := gst.NewElement("queue")
		if delayQueue != nil {
			delayQueue.SetProperty("min-threshold-time", uint64(opts.AudioDelayMs)*1000000)
			audioElements = append([]*gst.Element{delayQueue}, audioElements...)
		}
	}

	var all []*gst.Element
	all = append(all, sourceElements...)
	all = append(all, tsparse, demux, vQueue, aQueue)
	all = append(all, videoElements...)
	all = append(all, audioElements...)

	if opts.UseAppSink {
		vCaps, _ := gst.NewElement("capsfilter")
		vCaps.SetProperty("caps", gst.NewCapsFromString("video/x-h265,stream-format=byte-stream,alignment=au;video/x-h264,stream-format=byte-stream,alignment=au;video/x-av1,alignment=frame"))
		vSink, _ := gst.NewElement("appsink")
		vSink.Set("name", "videosink")
		vSink.SetProperty("emit-signals", true)
		vSink.SetProperty("sync", false)

		aCaps, _ := gst.NewElement("capsfilter")
		aCaps.SetProperty("caps", gst.NewCapsFromString("audio/mpeg,mpegversion=4"))
		aSink, _ := gst.NewElement("appsink")
		aSink.Set("name", "audiosink")
		aSink.SetProperty("emit-signals", true)
		aSink.SetProperty("sync", false)

		all = append(all, vCaps, vSink, aCaps, aSink)
		if err := checkNilElements(all); err != nil {
			return nil, err
		}
		pipeline.AddMany(all...)
		gst.ElementLinkMany(linkStart, tsparse, demux)

		vChain := []*gst.Element{vQueue}
		vChain = append(vChain, videoElements...)
		vChain = append(vChain, vCaps, vSink)
		gst.ElementLinkMany(vChain...)

		aChain := []*gst.Element{aQueue}
		aChain = append(aChain, audioElements...)
		aChain = append(aChain, aCaps, aSink)
		gst.ElementLinkMany(aChain...)
	} else {
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
		muxSink := []*gst.Element{mux, sink}
		all = append(all, muxSink...)
		if err := checkNilElements(all); err != nil {
			return nil, err
		}
		pipeline.AddMany(all...)
		gst.ElementLinkMany(linkStart, tsparse, demux)

		muxEl := muxSink[0]
		vChain := []*gst.Element{vQueue}
		vChain = append(vChain, videoElements...)
		vChain = append(vChain, muxEl)
		gst.ElementLinkMany(vChain...)

		aChain := []*gst.Element{aQueue}
		aChain = append(aChain, audioElements...)
		aChain = append(aChain, muxEl)
		gst.ElementLinkMany(aChain...)

		if len(muxSink) == 2 {
			gst.ElementLinkMany(muxSink[0], muxSink[1])
		}
	}

	var videoOnce, audioOnce sync.Once
	demux.Connect("pad-added", func(self *gst.Element, pad *gst.Pad) {
		caps := pad.GetCurrentCaps()
		if caps == nil {
			return
		}
		name := caps.GetStructureAt(0).Name()
		linked := false
		if strings.HasPrefix(name, "video") {
			videoOnce.Do(func() {
				pad.Link(vQueue.GetStaticPad("sink"))
				linked = true
			})
		} else if strings.Contains(name, "audio") {
			audioOnce.Do(func() {
				pad.Link(aQueue.GetStaticPad("sink"))
				linked = true
			})
		}
		if !linked {
			drainUnlinkedPad(pipeline, pad)
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
	if opts.HTTPTimeoutSec > 0 {
		src.SetProperty("timeout", uint(opts.HTTPTimeoutSec))
	}
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
	case "avi":
		demux, _ = gst.NewElement("avidemux")
	default:
		demux, _ = gst.NewElement("qtdemux")
	}

	vodVQueueMs := uint64(10000000000)
	if opts.VideoQueueMs > 0 {
		vodVQueueMs = uint64(opts.VideoQueueMs) * 1000000
	}
	vodAQueueMs := uint64(10000000000)
	if opts.AudioQueueMs > 0 {
		vodAQueueMs = uint64(opts.AudioQueueMs) * 1000000
	}
	vQueue, _ := gst.NewElement("queue")
	vQueue.SetProperty("max-size-time", vodVQueueMs)
	aQueue, _ := gst.NewElement("queue")
	aQueue.SetProperty("max-size-time", vodAQueueMs)

	hw := opts.HWAccel
	outCodec := NormalizeCodec(opts.OutputVideoCodec)
	isCopy := outCodec == "" || outCodec == "default" || outCodec == "copy"

	var videoElements []*gst.Element
	if isCopy {
		videoElements = createOutputParser(srcCodec)
	} else {
		decHW := hw
		if strings.Contains(opts.SourcePixFmt, "10") || strings.Contains(opts.SourcePixFmt, "12") {
			decHW = HWNone
		}
		if hw == HWVideoToolbox && (srcCodec == "h265" || srcCodec == "hevc") {
			decHW = HWNone
		}
		videoElements = append(videoElements, createHWDecoder(srcCodec, decHW)...)
		if opts.Deinterlace {
			di, _ := gst.NewElement("deinterlace")
			if di != nil {
				videoElements = append(videoElements, di)
			}
		}
		scaleHeight := opts.OutputHeight
		if scaleHeight == 0 && opts.UseAppSink && opts.SourceHeight > 0 && opts.SourceHeight < 1080 {
			scaleHeight = 1080
		}
		vconv, _ := gst.NewElement("videoconvert")
		if vconv != nil {
			videoElements = append(videoElements, vconv)
		}
		if scaleHeight > 0 {
			vscale, _ := gst.NewElement("videoscale")
			scaleCaps, _ := gst.NewElement("capsfilter")
			if vscale != nil && scaleCaps != nil {
				h := (scaleHeight + 1) &^ 1
				scaleCaps.SetProperty("caps", gst.NewCapsFromString(fmt.Sprintf("video/x-raw,height=%d,pixel-aspect-ratio=1/1", h)))
				videoElements = append(videoElements, vscale, scaleCaps)
			}
		}
		if opts.VideoEncoderElement != "" {
			videoElements = append(videoElements, createExplicitEncoder(opts.VideoEncoderElement, outCodec, bitrate(opts))...)
		} else {
			videoElements = append(videoElements, createHWEncoder(outCodec, hw, bitrate(opts))...)
		}
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
	if opts.AudioDelayMs > 0 {
		delayQueue, _ := gst.NewElement("queue")
		if delayQueue != nil {
			delayQueue.SetProperty("min-threshold-time", uint64(opts.AudioDelayMs)*1000000)
			audioElements = append([]*gst.Element{delayQueue}, audioElements...)
		}
	}

	var all []*gst.Element
	all = append(all, src, demux, vQueue, aQueue)
	all = append(all, videoElements...)
	all = append(all, audioElements...)

	if opts.UseAppSink {
		vCaps, _ := gst.NewElement("capsfilter")
		vCaps.SetProperty("caps", gst.NewCapsFromString("video/x-h265,stream-format=byte-stream,alignment=au;video/x-h264,stream-format=byte-stream,alignment=au;video/x-av1,alignment=frame"))
		vSink, _ := gst.NewElement("appsink")
		vSink.Set("name", "videosink")
		vSink.SetProperty("emit-signals", true)
		vSink.SetProperty("sync", false)

		aCaps, _ := gst.NewElement("capsfilter")
		aCaps.SetProperty("caps", gst.NewCapsFromString("audio/mpeg,mpegversion=4"))
		aSink, _ := gst.NewElement("appsink")
		aSink.Set("name", "audiosink")
		aSink.SetProperty("emit-signals", true)
		aSink.SetProperty("sync", false)

		all = append(all, vCaps, vSink, aCaps, aSink)
		if err := checkNilElements(all); err != nil {
			return nil, err
		}
		pipeline.AddMany(all...)
		gst.ElementLinkMany(src, demux)

		vChain := []*gst.Element{vQueue}
		vChain = append(vChain, videoElements...)
		vChain = append(vChain, vCaps, vSink)
		gst.ElementLinkMany(vChain...)

		aChain := []*gst.Element{aQueue}
		aChain = append(aChain, audioElements...)
		aChain = append(aChain, aCaps, aSink)
		gst.ElementLinkMany(aChain...)
	} else {
		mux, _ := gst.NewElement("mp4mux")
		mux.SetProperty("fragment-duration", uint(2000))
		mux.SetProperty("streamable", true)
		sink, _ := gst.NewElement("filesink")
		sink.SetProperty("location", opts.RecordingPath)
		muxSink := []*gst.Element{mux, sink}
		all = append(all, muxSink...)
		if err := checkNilElements(all); err != nil {
			return nil, err
		}
		pipeline.AddMany(all...)
		gst.ElementLinkMany(src, demux)

		muxEl := muxSink[0]
		vChain := []*gst.Element{vQueue}
		vChain = append(vChain, videoElements...)
		vChain = append(vChain, muxEl)
		gst.ElementLinkMany(vChain...)

		aChain := []*gst.Element{aQueue}
		aChain = append(aChain, audioElements...)
		aChain = append(aChain, muxEl)
		gst.ElementLinkMany(aChain...)

		if len(muxSink) == 2 {
			gst.ElementLinkMany(muxSink[0], muxSink[1])
		}
	}

	var videoOnce, audioOnce sync.Once
	demux.Connect("pad-added", func(self *gst.Element, pad *gst.Pad) {
		padName := pad.GetName()
		capsName := ""
		if c := pad.GetCurrentCaps(); c != nil {
			capsName = c.GetStructureAt(0).Name()
		}
		isVideo := strings.HasPrefix(capsName, "video") || strings.HasPrefix(padName, "video")
		isAudio := strings.Contains(capsName, "audio") || strings.HasPrefix(padName, "audio")
		linked := false
		if isVideo {
			videoOnce.Do(func() {
				pad.Link(vQueue.GetStaticPad("sink"))
				linked = true
			})
		} else if isAudio {
			audioOnce.Do(func() {
				pad.Link(aQueue.GetStaticPad("sink"))
				linked = true
			})
		}
		if !linked {
			drainUnlinkedPad(pipeline, pad)
		}
	})

	return pipeline, nil
}

func buildVODTvproxyvod(opts PipelineOpts, srcCodec string) (*gst.Pipeline, error) {
	vod := gst.Find("tvproxyvod")
	if vod == nil {
		return nil, fmt.Errorf("tvproxyvod element not available")
	}

	outCodec := NormalizeCodec(opts.OutputVideoCodec)
	isCopy := outCodec == "" || outCodec == "default" || outCodec == "copy"

	videoParser := "h264parse"
	if srcCodec == "h265" {
		videoParser = "h265parse"
	} else if srcCodec == "av1" {
		videoParser = "av1parse"
	}

	var pipeStr string
	if isCopy {
		pipeStr = fmt.Sprintf(
			`tvproxyvod uri=%s name=tvproxyvod0 `+
				`tvproxyvod0.video ! %s ! mp4mux name=mux fragment-duration=2000 streamable=true ! filesink location=%s `+
				`tvproxyvod0.audio ! aacparse ! mux.`,
			opts.InputURL, videoParser, opts.RecordingPath)
	} else {
		hw := opts.HWAccel
		enc := hwEncoder(outCodec, hw, bitrate(opts))
		outParser := "h264parse"
		if outCodec == "h265" {
			outParser = "h265parse"
		} else if outCodec == "av1" {
			outParser = "av1parse"
		}

		encStr := enc
		if opts.VideoEncoderElement != "" {
			encStr = opts.VideoEncoderElement
		}
		br := bitrate(opts)

		pipeStr = fmt.Sprintf(
			`tvproxyvod uri=%s name=tvproxyvod0 `+
				`tvproxyvod0.video ! %s ! %s bitrate=%d ! %s ! mp4mux name=mux fragment-duration=2000 streamable=true ! filesink location=%s `+
				`tvproxyvod0.audio ! aacparse ! mux.`,
			opts.InputURL, hwDecoder(srcCodec, hw), encStr, br, outParser, opts.RecordingPath)
	}

	//fmt.Printf("tvproxyvod pipeline: %s\n", pipeStr)
	pipeline, err := gst.NewPipelineFromString(pipeStr)
	if err != nil {
		return nil, fmt.Errorf("tvproxyvod pipeline: %w", err)
	}

	if vodEl, err := pipeline.Bin.GetElementByName("tvproxyvod0"); err == nil && vodEl != nil {
		if opts.SeekOffset > 0 {
			vodEl.SetProperty("seek-position", int64(opts.SeekOffset*1e9))
		}
	}

	return pipeline, nil
}

func buildVODDecodebin3(opts PipelineOpts) (*gst.Pipeline, error) {
	pipeline, err := gst.NewPipeline("tvproxy")
	if err != nil {
		return nil, err
	}

	uridecodebin, _ := gst.NewElement("uridecodebin3")
	if uridecodebin == nil {
		return nil, fmt.Errorf("uridecodebin3 element not available")
	}
	uridecodebin.SetProperty("uri", opts.InputURL)

	defaultQueueNs := uint64(10000000000)
	if opts.UseAppSink {
		defaultQueueNs = 3000000000
	}
	vQueueNs := defaultQueueNs
	if opts.VideoQueueMs > 0 {
		vQueueNs = uint64(opts.VideoQueueMs) * 1000000
	}
	aQueueNs := defaultQueueNs
	if opts.AudioQueueMs > 0 {
		aQueueNs = uint64(opts.AudioQueueMs) * 1000000
	}
	vQueue, _ := gst.NewElement("queue")
	vQueue.SetProperty("max-size-time", vQueueNs)
	aQueue, _ := gst.NewElement("queue")
	aQueue.SetProperty("max-size-time", aQueueNs)

	hw := opts.HWAccel
	outCodec := NormalizeCodec(opts.OutputVideoCodec)
	if outCodec == "" || outCodec == "default" || outCodec == "copy" {
		outCodec = "h264"
	}

	var videoElements []*gst.Element
	vconv, _ := gst.NewElement("videoconvert")
	if vconv != nil {
		videoElements = append(videoElements, vconv)
	}
	if opts.Deinterlace {
		di, _ := gst.NewElement("deinterlace")
		if di != nil {
			videoElements = append(videoElements, di)
		}
	}
	if opts.VideoEncoderElement != "" {
		videoElements = append(videoElements, createExplicitEncoder(opts.VideoEncoderElement, outCodec, bitrate(opts))...)
	} else {
		videoElements = append(videoElements, createHWEncoder(outCodec, hw, bitrate(opts))...)
	}
	videoElements = append(videoElements, createOutputParser(outCodec)...)

	audioElements := buildAudioChainDecoded()
	if opts.AudioDelayMs > 0 {
		delayQueue, _ := gst.NewElement("queue")
		if delayQueue != nil {
			delayQueue.SetProperty("min-threshold-time", uint64(opts.AudioDelayMs)*1000000)
			audioElements = append([]*gst.Element{delayQueue}, audioElements...)
		}
	}

	mux, _ := gst.NewElement("mp4mux")
	mux.SetProperty("fragment-duration", uint(2000))
	mux.SetProperty("streamable", true)
	sink, _ := gst.NewElement("filesink")
	sink.SetProperty("location", opts.RecordingPath)
	muxSink := []*gst.Element{mux, sink}

	var all []*gst.Element
	all = append(all, uridecodebin, vQueue, aQueue)
	all = append(all, videoElements...)
	all = append(all, audioElements...)
	all = append(all, muxSink...)

	if err := checkNilElements(all); err != nil {
		return nil, err
	}

	pipeline.AddMany(all...)

	muxEl := muxSink[0]

	vChain := []*gst.Element{vQueue}
	vChain = append(vChain, videoElements...)
	vChain = append(vChain, muxEl)
	gst.ElementLinkMany(vChain...)

	aChain := []*gst.Element{aQueue}
	aChain = append(aChain, audioElements...)
	aChain = append(aChain, muxEl)
	gst.ElementLinkMany(aChain...)

	if len(muxSink) == 2 {
		gst.ElementLinkMany(muxSink[0], muxSink[1])
	}

	var videoOnce, audioOnce sync.Once
	uridecodebin.Connect("pad-added", func(self *gst.Element, pad *gst.Pad) {
		caps := pad.GetCurrentCaps()
		if caps == nil {
			return
		}
		name := caps.GetStructureAt(0).Name()
		linked := false
		if name == "video/x-raw" {
			videoOnce.Do(func() {
				pad.Link(vQueue.GetStaticPad("sink"))
				linked = true
			})
		} else if name == "audio/x-raw" {
			audioOnce.Do(func() {
				pad.Link(aQueue.GetStaticPad("sink"))
				linked = true
			})
		}
		if !linked {
			drainUnlinkedPad(pipeline, pad)
		}
	})

	return pipeline, nil
}

func buildAudioChainDecoded() []*gst.Element {
	aConv, _ := gst.NewElement("audioconvert")
	aResample, _ := gst.NewElement("audioresample")
	aCaps, _ := gst.NewElement("capsfilter")
	aCaps.SetProperty("caps", gst.NewCapsFromString("audio/x-raw,channels=2"))
	aEnc := aacEncoder()
	aOutParse, _ := gst.NewElement("aacparse")
	return []*gst.Element{aConv, aResample, aCaps, aEnc, aOutParse}
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
	if opts.EncoderBitrateKbps > 0 {
		return opts.EncoderBitrateKbps
	}
	if opts.OutputHeight > 0 {
		return scaledBitrate(opts.OutputHeight * 16 / 9)
	}
	if opts.SourceWidth > 0 {
		return scaledBitrate(opts.SourceWidth)
	}
	return 6000
}

func drainUnlinkedPad(pipeline *gst.Pipeline, pad *gst.Pad) {
	fake, _ := gst.NewElement("fakesink")
	if fake == nil {
		return
	}
	fake.SetProperty("sync", false)
	fake.SetProperty("async", false)
	pipeline.Add(fake)
	fake.SetState(gst.StatePlaying)
	pad.Link(fake.GetStaticPad("sink"))
}

func scaledBitrate(width int) int {
	switch {
	case width >= 3840:
		return 10000
	case width >= 2560:
		return 8000
	case width >= 1920:
		return 5000
	case width >= 1280:
		return 3000
	default:
		return 1500
	}
}
