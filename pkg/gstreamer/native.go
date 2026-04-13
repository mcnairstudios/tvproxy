package gstreamer

import (
	"fmt"
	"strings"
	"sync"

	"github.com/go-gst/go-gst/gst"

	"github.com/gavinmcnair/tvproxy/pkg/media"
)

func init() {
	gst.Init(nil)
}

func BuildNativePipeline(name string, probe *media.ProbeResult, opts PipelineOpts) (*gst.Pipeline, error) {
	if probe != nil {
		if probe.Video != nil {
			opts.VideoCodec = probe.Video.Codec
		}
		if len(probe.AudioTracks) > 0 {
			opts.AudioCodec = probe.AudioTracks[0].Codec
		}
		opts.Container = probe.FormatName
	}
	pstr := buildPipelineStr(opts)
	pipeline, err := gst.NewPipelineFromString(pstr)
	if err != nil {
		return nil, fmt.Errorf("failed to create pipeline from: %s: %w", pstr, err)
	}
	return pipeline, nil
}

func mustElement(name string) (*gst.Element, error) {
	el, err := gst.NewElement(name)
	if err != nil || el == nil {
		return nil, fmt.Errorf("GStreamer element %q not available", name)
	}
	return el, nil
}

func BuildNativeFromOpts(outputVideoCodec, audioCodec, hwAccel, inputURL, outputPath string, sourceVideoCodec ...string) (*gst.Pipeline, error) {
	srcCodec := "h264"
	if len(sourceVideoCodec) > 0 && sourceVideoCodec[0] != "" {
		srcCodec = NormalizeCodec(sourceVideoCodec[0])
	}
	pipeline, err := gst.NewPipeline("tvproxy")
	if err != nil {
		return nil, fmt.Errorf("failed to create pipeline: %w", err)
	}
	isRTSP := strings.HasPrefix(inputURL, "rtsp://") || strings.HasPrefix(inputURL, "rtsps://")
	useTSDemux := isRTSP || IsMPEGTS("", inputURL)

	var sourceElements []*gst.Element
	if isRTSP {
		src, _ := gst.NewElement("rtspsrc")
		src.SetProperty("location", inputURL)
		src.SetProperty("latency", uint(0))
		src.SetProperty("protocols", uint(4))
		depay, _ := gst.NewElement("rtpmp2tdepay")
		src.Connect("pad-added", func(self *gst.Element, pad *gst.Pad) {
			sinkPad := depay.GetStaticPad("sink")
			if sinkPad != nil && !sinkPad.IsLinked() {
				pad.Link(sinkPad)
			}
		})
		sourceElements = []*gst.Element{src, depay}
	} else {
		src, _ := gst.NewElement("souphttpsrc")
		src.SetProperty("location", inputURL)
		if useTSDemux {
			src.SetProperty("do-timestamp", true)
			src.SetProperty("is-live", true)
		}
		sourceElements = []*gst.Element{src}
	}

	var demuxElements []*gst.Element
	var demux *gst.Element

	if useTSDemux {
		tsparse, _ := gst.NewElement("tsparse")
		tsparse.SetProperty("set-timestamps", true)
		td, _ := gst.NewElement("tsdemux")
		demuxElements = []*gst.Element{tsparse, td}
		demux = td
	} else {
		qd, _ := gst.NewElement("qtdemux")
		demuxElements = []*gst.Element{qd}
		demux = qd
	}

	vQueue, _ := gst.NewElement("queue")
	vQueue.SetProperty("max-size-time", uint64(10000000000))
	vQueue.SetProperty("max-size-buffers", uint(0))
	aQueue, _ := gst.NewElement("queue")
	aQueue.SetProperty("max-size-time", uint64(10000000000))
	aQueue.SetProperty("max-size-buffers", uint(0))

	outVideo := NormalizeCodec(outputVideoCodec)
	if outVideo == "" || outVideo == "default" {
		outVideo = "copy"
	}
	hw := HWAccel(hwAccel)

	var videoElements []*gst.Element
	var audioElements []*gst.Element

	if outVideo == "copy" {
		parser := createOutputParser(srcCodec)
		videoElements = parser
	} else {
		vDec := createHWDecoder(srcCodec, hw)
		vEnc := createHWEncoder(outVideo, hw, 6000)
		vOutParse := createOutputParser(outVideo)
		videoElements = append(videoElements, vDec...)
		videoElements = append(videoElements, vEnc...)
		videoElements = append(videoElements, vOutParse...)
	}

	srcAudio := NormalizeCodec(audioCodec)
	if useTSDemux {
		audioElements = buildAudioChain(srcAudio)
	} else {
		aPass, _ := gst.NewElement("aacparse")
		audioElements = []*gst.Element{aPass}
	}

	mux, _ := gst.NewElement("mp4mux")
	mux.SetProperty("fragment-duration", uint(500))
	mux.SetProperty("streamable", true)
	sink, _ := gst.NewElement("filesink")
	sink.SetProperty("location", outputPath)

	var all []*gst.Element
	all = append(all, sourceElements...)
	all = append(all, demuxElements...)
	all = append(all, vQueue, aQueue)
	all = append(all, videoElements...)
	all = append(all, audioElements...)
	all = append(all, mux, sink)

	for i, el := range all {
		if el == nil {
			return nil, fmt.Errorf("pipeline element at position %d is nil (missing GStreamer plugin)", i)
		}
	}

	pipeline.AddMany(all...)

	if isRTSP {
		srcLink := []*gst.Element{sourceElements[1]}
		srcLink = append(srcLink, demuxElements...)
		gst.ElementLinkMany(srcLink...)
	} else {
		srcLink := []*gst.Element{sourceElements[0]}
		srcLink = append(srcLink, demuxElements...)
		gst.ElementLinkMany(srcLink...)
	}

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
		caps := pad.GetCurrentCaps()
		if caps != nil {
			capsName = caps.GetStructureAt(0).Name()
		}

		isVideo := strings.HasPrefix(capsName, "video") || strings.HasPrefix(padName, "video")
		isAudio := strings.Contains(capsName, "audio") || strings.HasPrefix(padName, "audio")

		if isVideo {
			videoOnce.Do(func() {
				pad.Link(vQueue.GetStaticPad("sink"))
			})
		} else if isAudio {
			audioOnce.Do(func() {
				pad.Link(aQueue.GetStaticPad("sink"))
			})
		}
	})

	return pipeline, nil
}

func aacEncoder() *gst.Element {
	enc, _ := gst.NewElement("faac")
	if enc == nil {
		enc, _ = gst.NewElement("voaacenc")
	}
	if enc == nil {
		enc, _ = gst.NewElement("avenc_aac")
	}
	return enc
}

func buildAudioChain(srcAudio string) []*gst.Element {
	aConv, _ := gst.NewElement("audioconvert")
	aResample, _ := gst.NewElement("audioresample")
	aCaps, _ := gst.NewElement("capsfilter")
	aCaps.SetProperty("caps", gst.NewCapsFromString("audio/x-raw,channels=2"))
	aEnc := aacEncoder()
	aOutParse, _ := gst.NewElement("aacparse")

	switch srcAudio {
	case "aac_latm":
		aInParse, _ := gst.NewElement("aacparse")
		aDec, _ := gst.NewElement("avdec_aac_latm")
		return []*gst.Element{aInParse, aDec, aConv, aResample, aCaps, aEnc, aOutParse}
	case "aac":
		aInParse, _ := gst.NewElement("aacparse")
		return []*gst.Element{aInParse}
	case "mp2":
		aInParse, _ := gst.NewElement("mpegaudioparse")
		aDec, _ := gst.NewElement("mpg123audiodec")
		return []*gst.Element{aInParse, aDec, aConv, aResample, aCaps, aEnc, aOutParse}
	case "ac3", "eac3":
		decName := "avdec_ac3"
		if srcAudio == "eac3" {
			decName = "avdec_eac3"
		}
		aDec, _ := gst.NewElement(decName)
		return []*gst.Element{aDec, aConv, aResample, aCaps, aEnc, aOutParse}
	default:
		aInParse, _ := gst.NewElement("aacparse")
		aDec, _ := gst.NewElement("avdec_aac_latm")
		return []*gst.Element{aInParse, aDec, aConv, aResample, aCaps, aEnc, aOutParse}
	}
}

func createHWDecoder(codec string, hw HWAccel) []*gst.Element {
	var parser *gst.Element
	var decoder *gst.Element

	switch codec {
	case "h264":
		parser, _ = gst.NewElement("h264parse")
	case "h265":
		parser, _ = gst.NewElement("h265parse")
	case "av1":
		parser, _ = gst.NewElement("av1parse")
	case "mpeg2video":
		parser, _ = gst.NewElement("mpegvideoparse")
	default:
		parser, _ = gst.NewElement("h264parse")
	}

	switch hw {
	case HWVideoToolbox:
		decoder, _ = gst.NewElement("vtdec")
	case HWVAAPI:
		switch codec {
		case "h264":
			decoder, _ = gst.NewElement("vaapih264dec")
		case "h265":
			decoder, _ = gst.NewElement("vaapih265dec")
		default:
			decoder, _ = gst.NewElement("vaapidecode")
		}
	case HWQSV:
		switch codec {
		case "h264":
			decoder, _ = gst.NewElement("qsvh264dec")
		case "h265":
			decoder, _ = gst.NewElement("qsvh265dec")
		case "av1":
			decoder, _ = gst.NewElement("qsvav1dec")
		default:
			decoder, _ = gst.NewElement("avdec_h264")
		}
	default:
		switch codec {
		case "h264":
			decoder, _ = gst.NewElement("avdec_h264")
		case "h265":
			decoder, _ = gst.NewElement("avdec_h265")
		case "av1":
			decoder, _ = gst.NewElement("avdec_av1")
		case "mpeg2video":
			decoder, _ = gst.NewElement("avdec_mpeg2video")
		default:
			decoder, _ = gst.NewElement("avdec_h264")
		}
	}

	return []*gst.Element{parser, decoder}
}

func createHWEncoder(codec string, hw HWAccel, bitrate int) []*gst.Element {
	var encoder *gst.Element

	switch hw {
	case HWVideoToolbox:
		switch codec {
		case "h265":
			encoder, _ = gst.NewElement("vtenc_h265")
		case "av1":
			encoder, _ = gst.NewElement("vtenc_av1")
			if encoder == nil {
				return createSoftwareAV1Encoder(bitrate)
			}
		default:
			encoder, _ = gst.NewElement("vtenc_h264")
		}
		if encoder != nil {
			encoder.SetProperty("bitrate", uint(bitrate))
			encoder.SetProperty("realtime", true)
			encoder.SetProperty("allow-frame-reordering", false)
		}
	case HWVAAPI:
		switch codec {
		case "h265":
			encoder, _ = gst.NewElement("vaapih265enc")
		case "av1":
			encoder, _ = gst.NewElement("vaapiav1enc")
		default:
			encoder, _ = gst.NewElement("vaapih264enc")
			encoder.SetProperty("tune", uint(3))
		}
		encoder.SetProperty("bitrate", uint(bitrate))
	case HWQSV:
		switch codec {
		case "h265":
			encoder, _ = gst.NewElement("qsvh265enc")
		case "av1":
			encoder, _ = gst.NewElement("qsvav1enc")
		default:
			encoder, _ = gst.NewElement("qsvh264enc")
			encoder.SetProperty("target-usage", uint(1))
		}
		encoder.SetProperty("bitrate", uint(bitrate))
	default:
		conv, _ := gst.NewElement("videoconvert")
		switch codec {
		case "h265":
			encoder, _ = gst.NewElement("x265enc")
			encoder.SetProperty("speed-preset", 1)
		case "av1":
			return createSoftwareAV1Encoder(bitrate)
		default:
			encoder, _ = gst.NewElement("x264enc")
			encoder.SetProperty("speed-preset", 1)
			encoder.SetProperty("tune", uint(4))
		}
		encoder.SetProperty("bitrate", uint(bitrate))
		return []*gst.Element{conv, encoder}
	}

	return []*gst.Element{encoder}
}

func createSoftwareAV1Encoder(bitrate int) []*gst.Element {
	conv, _ := gst.NewElement("videoconvert")
	caps, _ := gst.NewElement("capsfilter")
	caps.SetProperty("caps", gst.NewCapsFromString("video/x-raw,format=I420"))

	encoder, _ := gst.NewElement("svtav1enc")
	if encoder != nil {
		encoder.SetProperty("preset", uint(12))
		encoder.SetProperty("target-bitrate", uint(bitrate))
		return []*gst.Element{conv, caps, encoder}
	}

	encoder, _ = gst.NewElement("rav1enc")
	if encoder != nil {
		encoder.SetProperty("speed-preset", uint(10))
		encoder.SetProperty("low-latency", true)
		encoder.SetProperty("bitrate", bitrate*1000)
		return []*gst.Element{conv, caps, encoder}
	}

	encoder, _ = gst.NewElement("av1enc")
	if encoder != nil {
		encoder.SetProperty("target-bitrate", uint(bitrate))
		encoder.SetProperty("cpu-used", uint(8))
		encoder.SetProperty("usage-profile", uint(1))
		return []*gst.Element{conv, caps, encoder}
	}

	return []*gst.Element{conv, caps}
}

func createOutputParser(codec string) []*gst.Element {
	var parser *gst.Element
	switch codec {
	case "h265":
		parser, _ = gst.NewElement("h265parse")
		parser.SetProperty("config-interval", -1)
	case "av1":
		parser, _ = gst.NewElement("av1parse")
	default:
		parser, _ = gst.NewElement("h264parse")
		parser.SetProperty("config-interval", -1)
	}
	return []*gst.Element{parser}
}
