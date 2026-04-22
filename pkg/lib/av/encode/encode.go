package encode

import (
	"errors"
	"fmt"

	"github.com/asticode/go-astiav"
)

type Encoder struct {
	codecCtx *astiav.CodecContext
	hwCtx    *astiav.HardwareDeviceContext
	closed   bool
}

type EncodeOpts struct {
	Codec            string
	HWAccel          string
	Bitrate          int
	KeyframeInterval int
	Preset           string
	Width            int
	Height           int
	EncoderName      string
}

var encoderTable = map[string]map[string]string{
	"h264": {
		"videotoolbox": "h264_videotoolbox",
		"vaapi":        "h264_vaapi",
		"qsv":          "h264_qsv",
		"nvenc":        "h264_nvenc",
		"none":         "libx264",
	},
	"h265": {
		"videotoolbox": "hevc_videotoolbox",
		"vaapi":        "hevc_vaapi",
		"qsv":          "hevc_qsv",
		"nvenc":        "hevc_nvenc",
		"none":         "libx265",
	},
	"av1": {
		"vaapi": "av1_vaapi",
		"qsv":   "av1_qsv",
		"nvenc": "av1_nvenc",
		"none":  "libsvtav1",
	},
}

var softwareFallback = map[string]string{
	"h264": "libx264",
	"h265": "libx265",
	"av1":  "libsvtav1",
}

func ResolveEncoderName(opts EncodeOpts) (string, error) {
	if opts.EncoderName != "" {
		return opts.EncoderName, nil
	}

	hwAccel := opts.HWAccel
	if hwAccel == "" {
		hwAccel = "none"
	}

	codecMap, ok := encoderTable[opts.Codec]
	if !ok {
		return "", fmt.Errorf("encode: unsupported codec %q", opts.Codec)
	}

	name, ok := codecMap[hwAccel]
	if !ok {
		return "", fmt.Errorf("encode: unsupported hwaccel %q for codec %q", hwAccel, opts.Codec)
	}

	return name, nil
}

var hwDeviceType = map[string]string{
	"vaapi":        "vaapi",
	"qsv":          "qsv",
	"videotoolbox": "videotoolbox",
	"nvenc":        "cuda",
}

func NewVideoEncoder(opts EncodeOpts) (*Encoder, error) {
	if opts.Width <= 0 || opts.Height <= 0 {
		return nil, fmt.Errorf("encode: width and height must be positive (got %dx%d)", opts.Width, opts.Height)
	}

	encoderName, err := ResolveEncoderName(opts)
	if err != nil {
		return nil, err
	}

	enc := &Encoder{}

	codec := astiav.FindEncoderByName(encoderName)
	if codec == nil {
		hwAccel := opts.HWAccel
		if hwAccel == "" {
			hwAccel = "none"
		}
		swName, hasSW := softwareFallback[opts.Codec]
		if hwAccel != "none" && hasSW {
			codec = astiav.FindEncoderByName(swName)
		}
		if codec == nil {
			return nil, fmt.Errorf("encode: encoder %q not found", encoderName)
		}
	} else {
		hwAccel := opts.HWAccel
		if hwAccel == "" {
			hwAccel = "none"
		}
		if devType, ok := hwDeviceType[hwAccel]; ok {
			hwType := astiav.FindHardwareDeviceTypeByName(devType)
			hwCtx, hwErr := astiav.CreateHardwareDeviceContext(hwType, "", nil, 0)
			if hwErr == nil {
				enc.hwCtx = hwCtx
			}
		}
	}

	cc := astiav.AllocCodecContext(codec)
	if cc == nil {
		if enc.hwCtx != nil {
			enc.hwCtx.Free()
		}
		return nil, fmt.Errorf("encode: failed to allocate codec context")
	}
	enc.codecCtx = cc

	cc.SetWidth(opts.Width)
	cc.SetHeight(opts.Height)

	if opts.Bitrate > 0 {
		cc.SetBitRate(int64(opts.Bitrate) * 1000)
	}

	if opts.KeyframeInterval > 0 {
		cc.SetGopSize(opts.KeyframeInterval)
	}

	cc.SetPixelFormat(astiav.PixelFormatYuv420P)

	cc.SetTimeBase(astiav.NewRational(1, 25))
	cc.SetFramerate(astiav.NewRational(25, 1))

	cc.SetFlags(astiav.NewCodecContextFlags(astiav.CodecContextFlagGlobalHeader))

	if enc.hwCtx != nil {
		cc.SetHardwareDeviceContext(enc.hwCtx)
	}

	var dict *astiav.Dictionary
	if opts.Preset != "" {
		dict = astiav.NewDictionary()
		defer dict.Free()
		if err := dict.Set("preset", opts.Preset, 0); err != nil {
			enc.freeResources()
			return nil, fmt.Errorf("encode: failed to set preset: %w", err)
		}
	}

	if err := cc.Open(codec, dict); err != nil {
		hwAccel := opts.HWAccel
		if hwAccel == "" {
			hwAccel = "none"
		}
		swName, hasSW := softwareFallback[opts.Codec]
		if hwAccel != "none" && hasSW {
			enc.freeResources()
			swOpts := opts
			swOpts.HWAccel = "none"
			swOpts.EncoderName = swName
			return NewVideoEncoder(swOpts)
		}
		enc.freeResources()
		return nil, fmt.Errorf("encode: open encoder %q: %w", encoderName, err)
	}

	return enc, nil
}

func NewAACEncoder(channels, sampleRate int) (*Encoder, error) {
	codec := astiav.FindEncoderByName("aac")
	if codec == nil {
		return nil, fmt.Errorf("encode: AAC encoder not found")
	}

	cc := astiav.AllocCodecContext(codec)
	if cc == nil {
		return nil, fmt.Errorf("encode: failed to allocate codec context for AAC")
	}

	cc.SetSampleRate(sampleRate)
	cc.SetSampleFormat(astiav.SampleFormatFltp)

	switch channels {
	case 1:
		cc.SetChannelLayout(astiav.ChannelLayoutMono)
	case 2:
		cc.SetChannelLayout(astiav.ChannelLayoutStereo)
	case 6:
		cc.SetChannelLayout(astiav.ChannelLayout5Point1)
	default:
		cc.SetChannelLayout(astiav.ChannelLayoutStereo)
	}

	cc.SetFlags(astiav.NewCodecContextFlags(astiav.CodecContextFlagGlobalHeader))

	cc.SetTimeBase(astiav.NewRational(1, sampleRate))

	if err := cc.Open(codec, nil); err != nil {
		cc.Free()
		return nil, fmt.Errorf("encode: open AAC encoder: %w", err)
	}

	return &Encoder{codecCtx: cc}, nil
}

func (e *Encoder) Encode(frame *astiav.Frame) ([]*astiav.Packet, error) {
	if e.codecCtx == nil {
		return nil, fmt.Errorf("encode: encoder not initialized")
	}

	if err := e.codecCtx.SendFrame(frame); err != nil {
		return nil, fmt.Errorf("encode: send frame: %w", err)
	}

	var packets []*astiav.Packet
	for {
		pkt := astiav.AllocPacket()
		if pkt == nil {
			return packets, fmt.Errorf("encode: failed to allocate packet")
		}

		err := e.codecCtx.ReceivePacket(pkt)
		if err != nil {
			pkt.Free()
			if errors.Is(err, astiav.ErrEagain) || errors.Is(err, astiav.ErrEof) {
				break
			}
			return packets, fmt.Errorf("encode: receive packet: %w", err)
		}
		packets = append(packets, pkt)
	}

	return packets, nil
}

func (e *Encoder) Extradata() []byte {
	if e.codecCtx == nil {
		return nil
	}
	return e.codecCtx.ExtraData()
}

func (e *Encoder) Flush() ([]*astiav.Packet, error) {
	if e.codecCtx == nil {
		return nil, nil
	}
	if err := e.codecCtx.SendFrame(nil); err != nil {
		return nil, err
	}
	var pkts []*astiav.Packet
	for {
		pkt := astiav.AllocPacket()
		if err := e.codecCtx.ReceivePacket(pkt); err != nil {
			pkt.Free()
			break
		}
		pkts = append(pkts, pkt)
	}
	return pkts, nil
}

func (e *Encoder) Close() {
	if e.closed {
		return
	}
	e.closed = true
	e.freeResources()
}

func (e *Encoder) freeResources() {
	if e.codecCtx != nil {
		e.codecCtx.Free()
		e.codecCtx = nil
	}
	if e.hwCtx != nil {
		e.hwCtx.Free()
		e.hwCtx = nil
	}
}
