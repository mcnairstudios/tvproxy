package gstreamer

import (
	"fmt"
	"log"
	"strings"

	"github.com/go-gst/go-gst/gst"

	"github.com/gavinmcnair/tvproxy/pkg/media"
)

func init() {
	gst.Init(nil)
}

func BuildNativePipeline(name string, probe *media.ProbeResult, opts PipelineOpts) (*gst.Pipeline, error) {
	applyProbe(&opts, probe)
	pstr := buildPipelineStr(opts)
	pipeline, err := gst.NewPipelineFromString(pstr)
	if err != nil {
		return nil, fmt.Errorf("failed to create pipeline from: %s: %w", pstr, err)
	}
	return pipeline, nil
}

func drainUnlinkedPadNative(pipeline *gst.Pipeline, pad *gst.Pad) {
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

func aacEncoder() *gst.Element {
	enc, _ := gst.NewElement("faac")
	if enc == nil {
		enc, _ = gst.NewElement("voaacenc")
	}
	if enc == nil {
		enc, _ = gst.NewElement("avenc_aac")
	}
	if enc == nil {
		log.Printf("[gstreamer] FATAL: no AAC encoder available (tried faac, voaacenc, avenc_aac) — install gst-plugins-bad or gst-libav")
	}
	return enc
}

func buildAudioChain(srcAudio string, channels int) []*gst.Element {
	aConv, _ := gst.NewElement("audioconvert")
	aResample, _ := gst.NewElement("audioresample")
	encChain := []*gst.Element{aConv, aResample}
	if channels > 0 {
		aCaps, _ := gst.NewElement("capsfilter")
		aCaps.SetProperty("caps", gst.NewCapsFromString(fmt.Sprintf("audio/x-raw,channels=%d", channels)))
		encChain = append(encChain, aCaps)
	}
	aEnc := aacEncoder()
	aOutParse, _ := gst.NewElement("aacparse")
	encChain = append(encChain, aEnc, aOutParse)

	switch srcAudio {
	case "":
		aInParse, _ := gst.NewElement("aacparse")
		aDec, _ := gst.NewElement("avdec_aac_latm")
		return append([]*gst.Element{aInParse, aDec}, encChain...)
	case "aac_latm":
		aInParse, _ := gst.NewElement("aacparse")
		aDec, _ := gst.NewElement("avdec_aac_latm")
		return append([]*gst.Element{aInParse, aDec}, encChain...)
	case "aac":
		aInParse, _ := gst.NewElement("aacparse")
		aAlign, _ := gst.NewElement("capsfilter")
		if aAlign != nil {
			aAlign.SetProperty("caps", gst.NewCapsFromString("audio/mpeg,mpegversion=4,alignment=frame"))
			return []*gst.Element{aInParse, aAlign}
		}
		return []*gst.Element{aInParse}
	case "mp2":
		aInParse, _ := gst.NewElement("mpegaudioparse")
		aDec, _ := gst.NewElement("mpg123audiodec")
		return append([]*gst.Element{aInParse, aDec}, encChain...)
	case "ac3", "eac3":
		decName := "avdec_ac3"
		if srcAudio == "eac3" {
			decName = "avdec_eac3"
		}
		aDec, _ := gst.NewElement(decName)
		return append([]*gst.Element{aDec}, encChain...)
	case "dts":
		aDec, _ := gst.NewElement("avdec_dca")
		return append([]*gst.Element{aDec}, encChain...)
	case "truehd":
		aDec, _ := gst.NewElement("avdec_truehd")
		return append([]*gst.Element{aDec}, encChain...)
	case "opus":
		aDec, _ := gst.NewElement("opusdec")
		return append([]*gst.Element{aDec}, encChain...)
	case "flac":
		aParse, _ := gst.NewElement("flacparse")
		aDec, _ := gst.NewElement("flacdec")
		return append([]*gst.Element{aParse, aDec}, encChain...)
	case "vorbis":
		aDec, _ := gst.NewElement("vorbisdec")
		return append([]*gst.Element{aDec}, encChain...)
	default:
		aInParse, _ := gst.NewElement("aacparse")
		aDec, _ := gst.NewElement("avdec_aac_latm")
		return append([]*gst.Element{aInParse, aDec}, encChain...)
	}
}

func createExplicitDecoder(elementName, codec string) []*gst.Element {
	parser := createParserForCodec(codec)
	decoder, _ := gst.NewElement(elementName)
	if decoder == nil {
		log.Printf("[gstreamer] explicit decoder %q not available, falling back to software", elementName)
		return createHWDecoder(codec, HWNone)
	}
	log.Printf("[gstreamer] using explicit decoder %q for %s", elementName, codec)
	return []*gst.Element{parser, decoder}
}

func createParserForCodec(codec string) *gst.Element {
	var parser *gst.Element
	switch codec {
	case "h264":
		parser, _ = gst.NewElement("h264parse")
	case "h265":
		parser, _ = gst.NewElement("h265parse")
	case "av1":
		parser, _ = gst.NewElement("av1parse")
	case "mpeg2video":
		parser, _ = gst.NewElement("mpegvideoparse")
	case "mpeg4":
		parser, _ = gst.NewElement("mpeg4videoparse")
	default:
		parser, _ = gst.NewElement("h264parse")
	}
	return parser
}

func resolveDecodeHW(opts PipelineOpts, srcCodec string) HWAccel {
	hw := opts.DecodeHWAccel
	if hw == "" {
		hw = opts.HWAccel
	}
	is10bit := opts.SourceBitDepth > 8 || (opts.SourceBitDepth == 0 && (strings.Contains(opts.SourcePixFmt, "10") || strings.Contains(opts.SourcePixFmt, "12")))
	if is10bit && !opts.Decode10Bit {
		hw = HWNone
	}
	if hw == HWVideoToolbox && (srcCodec == "h265" || srcCodec == "hevc") {
		hw = HWNone
	}
	return hw
}

func hwAccelString(hw HWAccel) string {
	switch hw {
	case HWVAAPI:
		return "vaapi"
	case HWQSV:
		return "qsv"
	case HWVideoToolbox:
		return "videotoolbox"
	case HWNVENC:
		return "nvenc"
	default:
		return "none"
	}
}

func createDecoderChain(opts PipelineOpts, srcCodec string) []*gst.Element {
	if opts.VideoDecoderElement != "" {
		return createExplicitDecoder(opts.VideoDecoderElement, srcCodec)
	}
	hw := resolveDecodeHW(opts, srcCodec)
	dec, _ := gst.NewElement("tvproxydecode")
	if dec != nil {
		dec.SetProperty("hw-accel", hwAccelString(hw))
		log.Printf("[gstreamer] using tvproxydecode hw-accel=%s for %s", hwAccelString(hw), srcCodec)
		return []*gst.Element{dec}
	}
	return createHWDecoder(srcCodec, hw)
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
	case "mpeg4":
		parser, _ = gst.NewElement("mpeg4videoparse")
	default:
		parser, _ = gst.NewElement("h264parse")
	}

	switch hw {
	case HWVideoToolbox:
		decoder, _ = gst.NewElement("vtdec")
	case HWVAAPI:
		switch codec {
		case "h264":
			decoder, _ = gst.NewElement("vah264dec")
			if decoder == nil {
				decoder, _ = gst.NewElement("vaapih264dec")
			}
		case "h265":
			decoder, _ = gst.NewElement("vah265dec")
			if decoder == nil {
				decoder, _ = gst.NewElement("vaapih265dec")
			}
		case "av1":
			decoder, _ = gst.NewElement("vaav1dec")
		case "mpeg2video":
			decoder, _ = gst.NewElement("vampeg2dec")
			if decoder == nil {
				decoder, _ = gst.NewElement("vaapimpeg2dec")
			}
		default:
			decoder, _ = gst.NewElement("vaapidecode")
		}
	case HWNVENC:
		switch codec {
		case "h264":
			decoder, _ = gst.NewElement("nvh264dec")
		case "h265":
			decoder, _ = gst.NewElement("nvh265dec")
		case "av1":
			decoder, _ = gst.NewElement("nvav1dec")
		default:
			decoder, _ = gst.NewElement("nvh264dec")
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
			decoder, _ = gst.NewElement("dav1ddec")
			if decoder == nil {
				decoder, _ = gst.NewElement("avdec_av1")
			}
		case "mpeg2video":
			decoder, _ = gst.NewElement("avdec_mpeg2video")
		default:
			decoder, _ = gst.NewElement("avdec_h264")
		}
	}

	if decoder == nil {
		switch codec {
		case "h264":
			decoder, _ = gst.NewElement("avdec_h264")
		case "h265":
			decoder, _ = gst.NewElement("avdec_h265")
		case "av1":
			decoder, _ = gst.NewElement("dav1ddec")
			if decoder == nil {
				decoder, _ = gst.NewElement("avdec_av1")
			}
		case "mpeg2video":
			decoder, _ = gst.NewElement("avdec_mpeg2video")
		case "mpeg4":
			decoder, _ = gst.NewElement("avdec_mpeg4")
		default:
			decoder, _ = gst.NewElement("avdec_h264")
		}
	}

	return []*gst.Element{parser, decoder}
}

func createEncoderChain(opts PipelineOpts, outCodec string, hw HWAccel) []*gst.Element {
	if opts.VideoEncoderElement != "" {
		elems := createExplicitEncoder(opts.VideoEncoderElement, outCodec, bitrate(opts))
		elems = append(elems, createOutputParser(outCodec)...)
		return elems
	}
	enc, _ := gst.NewElement("tvproxyencode")
	if enc != nil {
		enc.SetProperty("hw-accel", hwAccelString(hw))
		enc.SetProperty("codec", outCodec)
		enc.SetProperty("bitrate", bitrate(opts))
		log.Printf("[gstreamer] using tvproxyencode hw-accel=%s codec=%s bitrate=%d", hwAccelString(hw), outCodec, bitrate(opts))
		return []*gst.Element{enc}
	}
	elems := createHWEncoder(outCodec, hw, bitrate(opts))
	elems = append(elems, createOutputParser(outCodec)...)
	return elems
}

func createHWEncoder(codec string, hw HWAccel, bitrate int) []*gst.Element {
	return createEncoderByName("", codec, hw, bitrate)
}

func createExplicitEncoder(elementName, codec string, bitrate int) []*gst.Element {
	return createEncoderByName(elementName, codec, HWNone, bitrate)
}

func createEncoderByName(elementName, codec string, hw HWAccel, bitrate int) []*gst.Element {
	if elementName != "" {
		encoder, _ := gst.NewElement(elementName)
		if encoder != nil {
			switch {
			case strings.HasPrefix(elementName, "va") || strings.HasPrefix(elementName, "vaapi"):
				encoder.SetProperty("bitrate", uint(bitrate))
				encoder.SetProperty("key-int-max", uint(60))
				conv, _ := gst.NewElement("videoconvert")
				caps, _ := gst.NewElement("capsfilter")
				caps.SetProperty("caps", gst.NewCapsFromString("video/x-raw,format=NV12"))
				return []*gst.Element{conv, caps, encoder}
			case strings.HasPrefix(elementName, "vtenc_"):
				encoder.SetProperty("bitrate", uint(bitrate))
				encoder.SetProperty("realtime", true)
				encoder.SetProperty("allow-frame-reordering", false)
			case strings.HasPrefix(elementName, "nv"):
				encoder.SetProperty("bitrate", uint(bitrate))
			case strings.HasPrefix(elementName, "qsv"):
				encoder.SetProperty("bitrate", uint(bitrate))
			case elementName == "svtav1enc":
				encoder.SetProperty("preset", uint(12))
				encoder.SetProperty("target-bitrate", uint(bitrate))
				conv, _ := gst.NewElement("videoconvert")
				caps, _ := gst.NewElement("capsfilter")
				caps.SetProperty("caps", gst.NewCapsFromString("video/x-raw,format=I420"))
				return []*gst.Element{conv, caps, encoder}
			case elementName == "rav1enc":
				encoder.SetProperty("speed-preset", uint(10))
				encoder.SetProperty("low-latency", true)
				encoder.SetProperty("bitrate", bitrate*1000)
				conv, _ := gst.NewElement("videoconvert")
				caps, _ := gst.NewElement("capsfilter")
				caps.SetProperty("caps", gst.NewCapsFromString("video/x-raw,format=I420"))
				return []*gst.Element{conv, caps, encoder}
			case elementName == "x264enc":
				encoder.SetProperty("speed-preset", 1)
				encoder.SetProperty("tune", uint(4))
				encoder.SetProperty("bitrate", uint(bitrate))
				conv, _ := gst.NewElement("videoconvert")
				return []*gst.Element{conv, encoder}
			case elementName == "x265enc":
				encoder.SetProperty("speed-preset", 1)
				encoder.SetProperty("bitrate", uint(bitrate))
				conv, _ := gst.NewElement("videoconvert")
				return []*gst.Element{conv, encoder}
			}
			return []*gst.Element{encoder}
		}
	}

	var encoder *gst.Element

	switch hw {
	case HWVideoToolbox:
		switch codec {
		case "h265":
			encoder, _ = gst.NewElement("vtenc_h265")
			if encoder == nil {
				conv, _ := gst.NewElement("videoconvert")
				enc, _ := gst.NewElement("x265enc")
				if enc != nil {
					enc.SetProperty("speed-preset", 1)
					enc.SetProperty("bitrate", uint(bitrate))
				}
				return []*gst.Element{conv, enc}
			}
		case "av1":
			encoder, _ = gst.NewElement("vtenc_av1")
			if encoder == nil {
				return createSoftwareAV1Encoder(bitrate)
			}
		default:
			encoder, _ = gst.NewElement("vtenc_h264")
			if encoder == nil {
				conv, _ := gst.NewElement("videoconvert")
				enc, _ := gst.NewElement("x264enc")
				if enc != nil {
					enc.SetProperty("speed-preset", 1)
					enc.SetProperty("tune", uint(4))
					enc.SetProperty("bitrate", uint(bitrate))
				}
				return []*gst.Element{conv, enc}
			}
		}
		if encoder != nil {
			encoder.SetProperty("bitrate", uint(bitrate))
			encoder.SetProperty("realtime", true)
			encoder.SetProperty("allow-frame-reordering", false)
			encoder.SetProperty("max-keyframe-interval", 25)
		}
	case HWVAAPI:
		switch codec {
		case "h265":
			encoder, _ = gst.NewElement("vah265lpenc")
			if encoder == nil {
				encoder, _ = gst.NewElement("vaapih265enc")
			}
		case "av1":
			encoder, _ = gst.NewElement("vaav1lpenc")
			if encoder == nil {
				encoder, _ = gst.NewElement("vaav1enc")
			}
			if encoder == nil {
				encoder, _ = gst.NewElement("vaapiav1enc")
			}
			if encoder == nil {
				return createSoftwareAV1Encoder(bitrate)
			}
		default:
			encoder, _ = gst.NewElement("vah264lpenc")
			if encoder == nil {
				encoder, _ = gst.NewElement("vaapih264enc")
			}
		}
		if encoder != nil {
			encoder.SetProperty("bitrate", uint(bitrate))
			encoder.SetProperty("key-int-max", uint(60))
			conv, _ := gst.NewElement("videoconvert")
			caps, _ := gst.NewElement("capsfilter")
			caps.SetProperty("caps", gst.NewCapsFromString("video/x-raw,format=NV12"))
			return []*gst.Element{conv, caps, encoder}
		}
	case HWNVENC:
		switch codec {
		case "h265":
			encoder, _ = gst.NewElement("nvh265enc")
			if encoder == nil {
				conv, _ := gst.NewElement("videoconvert")
				enc, _ := gst.NewElement("x265enc")
				if enc != nil {
					enc.SetProperty("speed-preset", 1)
					enc.SetProperty("bitrate", uint(bitrate))
				}
				return []*gst.Element{conv, enc}
			}
		case "av1":
			encoder, _ = gst.NewElement("nvav1enc")
			if encoder == nil {
				return createSoftwareAV1Encoder(bitrate)
			}
		default:
			encoder, _ = gst.NewElement("nvh264enc")
			if encoder == nil {
				conv, _ := gst.NewElement("videoconvert")
				enc, _ := gst.NewElement("x264enc")
				if enc != nil {
					enc.SetProperty("speed-preset", 1)
					enc.SetProperty("bitrate", uint(bitrate))
				}
				return []*gst.Element{conv, enc}
			}
		}
		if encoder != nil {
			encoder.SetProperty("bitrate", uint(bitrate))
		}
	case HWQSV:
		switch codec {
		case "h265":
			encoder, _ = gst.NewElement("qsvh265enc")
			if encoder == nil {
				encoder, _ = gst.NewElement("vah265lpenc")
			}
		case "av1":
			encoder, _ = gst.NewElement("qsvav1enc")
			if encoder == nil {
				encoder, _ = gst.NewElement("vaav1lpenc")
			if encoder == nil {
				encoder, _ = gst.NewElement("vaav1enc")
			}
			}
			if encoder == nil {
				return createSoftwareAV1Encoder(bitrate)
			}
		default:
			encoder, _ = gst.NewElement("qsvh264enc")
			if encoder == nil {
				encoder, _ = gst.NewElement("vah264lpenc")
			}
		}
		if encoder != nil {
			encoder.SetProperty("bitrate", uint(bitrate))
		}
	default:
		conv, _ := gst.NewElement("videoconvert")
		switch codec {
		case "h265":
			encoder, _ = gst.NewElement("x265enc")
			if encoder != nil {
				encoder.SetProperty("speed-preset", 1)
			}
		case "av1":
			return createSoftwareAV1Encoder(bitrate)
		default:
			encoder, _ = gst.NewElement("x264enc")
			if encoder != nil {
				encoder.SetProperty("speed-preset", 1)
				encoder.SetProperty("tune", uint(4))
			}
		}
		if encoder != nil {
			encoder.SetProperty("bitrate", uint(bitrate))
		}
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
	case "av1":
		parser, _ = gst.NewElement("av1parse")
	case "mpeg2video":
		parser, _ = gst.NewElement("mpegvideoparse")
	case "mpeg4":
		parser, _ = gst.NewElement("mpeg4videoparse")
	default:
		parser, _ = gst.NewElement("h264parse")
	}
	if parser == nil {
		return []*gst.Element{}
	}
	switch codec {
	case "h265", "h264", "":
		parser.SetProperty("config-interval", -1)
	}
	return []*gst.Element{parser}
}
