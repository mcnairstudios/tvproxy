package encode

import (
	"errors"
	"fmt"

	"github.com/asticode/go-astiav"
)

type Encoder struct {
	codecCtx    *astiav.CodecContext
	hwCtx       *astiav.HardwareDeviceContext
	hwFramesCtx *astiav.HardwareFramesContext
	closed      bool
	hasEncoded  bool
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
	Framerate        int
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
	if hwAccel == "" || hwAccel == "default" {
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

var hwPixelFormat = map[string]astiav.PixelFormat{
	"vaapi":        astiav.PixelFormatVaapi,
	"qsv":          astiav.PixelFormatQsv,
	"videotoolbox": astiav.PixelFormatVideotoolbox,
	"nvenc":        astiav.PixelFormatCuda,
	"cuda":         astiav.PixelFormatCuda,
}

var preferredSWFormats = []astiav.PixelFormat{
	astiav.PixelFormatNv12,
	astiav.PixelFormatYuv420P,
}

func isHWPixelFormat(pf astiav.PixelFormat) bool {
	switch pf {
	case astiav.PixelFormatVaapi, astiav.PixelFormatCuda,
		astiav.PixelFormatQsv, astiav.PixelFormatVideotoolbox,
		astiav.PixelFormatD3D11, astiav.PixelFormatD3D11VaVld,
		astiav.PixelFormatDrmPrime, astiav.PixelFormatMediacodec,
		astiav.PixelFormatOpencl:
		return true
	}
	return false
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

	fps := opts.Framerate
	if fps <= 0 {
		fps = 25
	}
	cc.SetTimeBase(astiav.NewRational(1, fps))
	cc.SetFramerate(astiav.NewRational(fps, 1))

	cc.SetFlags(astiav.NewCodecContextFlags(astiav.CodecContextFlagGlobalHeader))

	if enc.hwCtx != nil {
		hwAccelKey := opts.HWAccel
		if hwAccelKey == "" {
			hwAccelKey = "none"
		}
		hwPF, hasHWPF := hwPixelFormat[hwAccelKey]
		if hasHWPF {
			cc.SetPixelFormat(hwPF)

			hwFramesCtx := astiav.AllocHardwareFramesContext(enc.hwCtx)
			if hwFramesCtx != nil {
				swPF := astiav.PixelFormatNv12
				constraints := enc.hwCtx.HardwareFramesConstraints()
				if constraints != nil {
					validSW := constraints.ValidSoftwarePixelFormats()
					if len(validSW) > 0 {
						swPF = validSW[0]
						for _, pref := range preferredSWFormats {
							for _, valid := range validSW {
								if pref == valid {
									swPF = pref
									goto foundSW
								}
							}
						}
					foundSW:
					}
					constraints.Free()
				}

				hwFramesCtx.SetHardwarePixelFormat(hwPF)
				hwFramesCtx.SetSoftwarePixelFormat(swPF)
				hwFramesCtx.SetWidth(opts.Width)
				hwFramesCtx.SetHeight(opts.Height)
				hwFramesCtx.SetInitialPoolSize(20)

				if initErr := hwFramesCtx.Initialize(); initErr != nil {
					hwFramesCtx.Free()
				} else {
					enc.hwFramesCtx = hwFramesCtx
					cc.SetHardwareFramesContext(hwFramesCtx)
				}
			}
		} else {
			cc.SetPixelFormat(astiav.PixelFormatYuv420P)
		}
		cc.SetHardwareDeviceContext(enc.hwCtx)
	} else {
		cc.SetPixelFormat(astiav.PixelFormatYuv420P)
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

var audioEncoderMap = map[string]string{
	"aac":    "aac",
	"ac3":    "ac3",
	"eac3":   "eac3",
	"mp2":    "mp2",
	"flac":   "flac",
	"opus":   "libopus",
	"mp3":    "libmp3lame",
	"vorbis": "libvorbis",
}

func ResolveAudioEncoderName(codec string) string {
	if name, ok := audioEncoderMap[codec]; ok {
		return name
	}
	return codec
}

type AudioEncodeOpts struct {
	Codec      string // codec name: "aac", "opus", "mp3", etc. (resolved to encoder name internally)
	Channels   int
	SampleRate int
}

func NewAudioEncoder(opts AudioEncodeOpts) (*Encoder, error) {
	if opts.Codec == "" {
		return nil, fmt.Errorf("encode: audio codec not specified")
	}
	codecName := ResolveAudioEncoderName(opts.Codec)

	codec := astiav.FindEncoderByName(codecName)
	if codec == nil {
		return nil, fmt.Errorf("encode: audio encoder %q not found", codecName)
	}

	cc := astiav.AllocCodecContext(codec)
	if cc == nil {
		return nil, fmt.Errorf("encode: failed to allocate codec context for %q", codecName)
	}

	cc.SetSampleRate(opts.SampleRate)
	cc.SetSampleFormat(astiav.SampleFormatFltp)

	switch opts.Channels {
	case 1:
		cc.SetChannelLayout(astiav.ChannelLayoutMono)
	case 2:
		cc.SetChannelLayout(astiav.ChannelLayoutStereo)
	case 6:
		cc.SetChannelLayout(astiav.ChannelLayout5Point1)
	case 8:
		cc.SetChannelLayout(astiav.ChannelLayout7Point1)
	default:
		cc.SetChannelLayout(astiav.ChannelLayoutStereo)
	}

	cc.SetFlags(astiav.NewCodecContextFlags(astiav.CodecContextFlagGlobalHeader))
	cc.SetTimeBase(astiav.NewRational(1, opts.SampleRate))

	if err := cc.Open(codec, nil); err != nil {
		cc.Free()
		return nil, fmt.Errorf("encode: open audio encoder %q: %w", codecName, err)
	}

	return &Encoder{codecCtx: cc}, nil
}

func (e *Encoder) FrameSize() int {
	if e.codecCtx == nil {
		return 0
	}
	return e.codecCtx.FrameSize()
}

func NewAACEncoder(channels, sampleRate int) (*Encoder, error) {
	return NewAudioEncoder(AudioEncodeOpts{Codec: "aac", Channels: channels, SampleRate: sampleRate})
}

func (e *Encoder) Encode(frame *astiav.Frame) ([]*astiav.Packet, error) {
	if e.codecCtx == nil {
		return nil, fmt.Errorf("encode: encoder not initialized")
	}

	encFrame := frame
	if frame != nil && e.hwFramesCtx != nil && !isHWPixelFormat(frame.PixelFormat()) {
		hwFrame := astiav.AllocFrame()
		if hwFrame == nil {
			return nil, fmt.Errorf("encode: failed to allocate hardware frame")
		}
		if err := hwFrame.AllocHardwareBuffer(e.hwFramesCtx); err != nil {
			hwFrame.Free()
			return nil, fmt.Errorf("encode: alloc hardware buffer: %w", err)
		}
		if err := frame.TransferHardwareData(hwFrame); err != nil {
			hwFrame.Free()
			return nil, fmt.Errorf("encode: upload frame to hardware: %w", err)
		}
		hwFrame.SetPts(frame.Pts())
		encFrame = hwFrame
		defer hwFrame.Free()
	}

	if err := e.codecCtx.SendFrame(encFrame); err != nil {
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
		e.hasEncoded = true
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
		if e.hasEncoded {
			e.codecCtx.SendFrame(nil) //nolint:errcheck
			pkt := astiav.AllocPacket()
			if pkt != nil {
				for e.codecCtx.ReceivePacket(pkt) == nil {
					pkt.Unref()
				}
				pkt.Free()
			}
		}
		e.codecCtx.Free()
		e.codecCtx = nil
	}
	if e.hwFramesCtx != nil {
		e.hwFramesCtx.Free()
		e.hwFramesCtx = nil
	}
	if e.hwCtx != nil {
		e.hwCtx.Free()
		e.hwCtx = nil
	}
}
