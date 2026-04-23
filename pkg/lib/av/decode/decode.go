package decode

import (
	"errors"
	"fmt"

	"github.com/asticode/go-astiav"
)

var hwAccelMap = map[string]astiav.HardwareDeviceType{
	"vaapi":        astiav.HardwareDeviceTypeVAAPI,
	"qsv":          astiav.HardwareDeviceTypeQSV,
	"videotoolbox": astiav.HardwareDeviceTypeVideoToolbox,
	"cuda":         astiav.HardwareDeviceTypeCUDA,
	"nvenc":        astiav.HardwareDeviceTypeCUDA,
	"d3d11va":      astiav.HardwareDeviceTypeD3D11VA,
	"dxva2":        astiav.HardwareDeviceTypeDXVA2,
	"vulkan":       astiav.HardwareDeviceTypeVulkan,
}

type Decoder struct {
	codecCtx *astiav.CodecContext
	hwCtx    *astiav.HardwareDeviceContext
}

type DecodeOpts struct {
	HWAccel     string
	MaxBitDepth int
	DecoderName string
}

func NewVideoDecoderFromParams(cp *astiav.CodecParameters, opts DecodeOpts) (*Decoder, error) {
	codecID := cp.CodecID()
	var codec *astiav.Codec
	var hwCtx *astiav.HardwareDeviceContext

	if opts.DecoderName != "" {
		codec = astiav.FindDecoderByName(opts.DecoderName)
		if codec == nil {
			return nil, fmt.Errorf("decode: decoder %q not found", opts.DecoderName)
		}
	}

	if codec == nil && opts.HWAccel != "" && opts.HWAccel != "none" {
		hwType, ok := hwAccelMap[opts.HWAccel]
		if ok {
			var err error
			hwCtx, err = astiav.CreateHardwareDeviceContext(hwType, "", nil, 0)
			if err == nil {
				codec = astiav.FindDecoder(codecID)
			} else {
				hwCtx = nil
			}
		}
	}

	if codec == nil {
		codec = astiav.FindDecoder(codecID)
		hwCtx = nil
	}
	if codec == nil {
		return nil, fmt.Errorf("decode: no decoder found for codec ID %d", codecID)
	}

	cc := astiav.AllocCodecContext(codec)
	if cc == nil {
		if hwCtx != nil {
			hwCtx.Free()
		}
		return nil, errors.New("decode: failed to allocate codec context")
	}

	if err := cp.ToCodecContext(cc); err != nil {
		cc.Free()
		if hwCtx != nil {
			hwCtx.Free()
		}
		return nil, fmt.Errorf("decode: copy codec params: %w", err)
	}

	if hwCtx != nil {
		cc.SetHardwareDeviceContext(hwCtx)
	}

	if err := cc.Open(codec, nil); err != nil {
		cc.Free()
		if hwCtx != nil {
			hwCtx.Free()
			return newVideoDecoderFromParamsSW(cp)
		}
		return nil, fmt.Errorf("decode: open codec: %w", err)
	}

	return &Decoder{codecCtx: cc, hwCtx: hwCtx}, nil
}

func newVideoDecoderFromParamsSW(cp *astiav.CodecParameters) (*Decoder, error) {
	codec := astiav.FindDecoder(cp.CodecID())
	if codec == nil {
		return nil, fmt.Errorf("decode: no SW decoder for codec ID %d", cp.CodecID())
	}
	cc := astiav.AllocCodecContext(codec)
	if cc == nil {
		return nil, errors.New("decode: failed to allocate codec context")
	}
	if err := cp.ToCodecContext(cc); err != nil {
		cc.Free()
		return nil, fmt.Errorf("decode: copy codec params: %w", err)
	}
	if err := cc.Open(codec, nil); err != nil {
		cc.Free()
		return nil, fmt.Errorf("decode: open SW codec: %w", err)
	}
	return &Decoder{codecCtx: cc}, nil
}

func NewVideoDecoder(codecID astiav.CodecID, extradata []byte, opts DecodeOpts) (*Decoder, error) {
	var codec *astiav.Codec
	var hwCtx *astiav.HardwareDeviceContext

	if opts.DecoderName != "" {
		codec = astiav.FindDecoderByName(opts.DecoderName)
		if codec == nil {
			return nil, fmt.Errorf("decode: decoder %q not found", opts.DecoderName)
		}
	}

	if codec == nil && opts.HWAccel != "" && opts.HWAccel != "none" {
		hwType, ok := hwAccelMap[opts.HWAccel]
		if ok {
			var err error
			hwCtx, err = astiav.CreateHardwareDeviceContext(hwType, "", nil, 0)
			if err == nil {
				codec = astiav.FindDecoder(codecID)
			} else {
				hwCtx = nil
			}
		}
	}

	if codec == nil {
		codec = astiav.FindDecoder(codecID)
		hwCtx = nil
	}
	if codec == nil {
		return nil, fmt.Errorf("decode: no decoder found for codec ID %d", codecID)
	}

	cc := astiav.AllocCodecContext(codec)
	if cc == nil {
		if hwCtx != nil {
			hwCtx.Free()
		}
		return nil, errors.New("decode: failed to allocate codec context")
	}

	if hwCtx != nil {
		cc.SetHardwareDeviceContext(hwCtx)
	}

	if len(extradata) > 0 {
		if err := cc.SetExtraData(extradata); err != nil {
			cc.Free()
			if hwCtx != nil {
				hwCtx.Free()
			}
			return nil, fmt.Errorf("decode: set extradata: %w", err)
		}
	}

	if err := cc.Open(codec, nil); err != nil {
		cc.Free()
		if hwCtx != nil {
			hwCtx.Free()
			return newSWDecoder(codecID, extradata)
		}
		return nil, fmt.Errorf("decode: open codec: %w", err)
	}

	return &Decoder{codecCtx: cc, hwCtx: hwCtx}, nil
}

func isContainerExtradata(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	return data[0] == 0x01
}

func newSWDecoder(codecID astiav.CodecID, extradata []byte) (*Decoder, error) {
	codec := astiav.FindDecoder(codecID)
	if codec == nil {
		return nil, fmt.Errorf("decode: no SW decoder for codec ID %d", codecID)
	}

	cc := astiav.AllocCodecContext(codec)
	if cc == nil {
		return nil, errors.New("decode: failed to allocate codec context")
	}

	if len(extradata) > 0 {
		if err := cc.SetExtraData(extradata); err != nil {
			cc.Free()
			return nil, fmt.Errorf("decode: set extradata: %w", err)
		}
	}

	if err := cc.Open(codec, nil); err != nil {
		cc.Free()
		return nil, fmt.Errorf("decode: open SW codec: %w", err)
	}

	return &Decoder{codecCtx: cc}, nil
}

func NewAudioDecoderFromParams(cp *astiav.CodecParameters) (*Decoder, error) {
	codec := astiav.FindDecoder(cp.CodecID())
	if codec == nil {
		return nil, fmt.Errorf("decode: no audio decoder for codec ID %d", cp.CodecID())
	}
	cc := astiav.AllocCodecContext(codec)
	if cc == nil {
		return nil, errors.New("decode: failed to allocate codec context")
	}
	if err := cp.ToCodecContext(cc); err != nil {
		cc.Free()
		return nil, fmt.Errorf("decode: copy audio codec params: %w", err)
	}
	if err := cc.Open(codec, nil); err != nil {
		cc.Free()
		return nil, fmt.Errorf("decode: open audio codec: %w", err)
	}
	return &Decoder{codecCtx: cc}, nil
}

func NewAudioDecoder(codecID astiav.CodecID, extradata []byte) (*Decoder, error) {
	codec := astiav.FindDecoder(codecID)
	if codec == nil {
		return nil, fmt.Errorf("decode: no audio decoder for codec ID %d", codecID)
	}

	cc := astiav.AllocCodecContext(codec)
	if cc == nil {
		return nil, errors.New("decode: failed to allocate codec context")
	}

	if len(extradata) > 0 {
		if err := cc.SetExtraData(extradata); err != nil {
			cc.Free()
			return nil, fmt.Errorf("decode: set extradata: %w", err)
		}
	}

	if err := cc.Open(codec, nil); err != nil {
		cc.Free()
		return nil, fmt.Errorf("decode: open audio codec: %w", err)
	}

	return &Decoder{codecCtx: cc}, nil
}

func (d *Decoder) Decode(pkt *astiav.Packet) ([]*astiav.Frame, error) {
	if err := d.codecCtx.SendPacket(pkt); err != nil {
		if errors.Is(err, astiav.ErrEof) {
			// fall through to receive buffered frames
		} else if errors.Is(err, astiav.ErrInvaliddata) || errors.Is(err, astiav.ErrEagain) {
			return nil, nil
		} else {
			return nil, fmt.Errorf("decode: send packet: %w", err)
		}
	}

	var frames []*astiav.Frame
	for {
		f := astiav.AllocFrame()
		if f == nil {
			return frames, errors.New("decode: failed to allocate frame")
		}

		if err := d.codecCtx.ReceiveFrame(f); err != nil {
			f.Free()
			if errors.Is(err, astiav.ErrEagain) || errors.Is(err, astiav.ErrEof) {
				break
			}
			return frames, fmt.Errorf("decode: receive frame: %w", err)
		}

		if isHWPixelFormat(f.PixelFormat()) {
			sw := astiav.AllocFrame()
			if sw == nil {
				f.Free()
				return frames, errors.New("decode: failed to allocate SW frame for HW transfer")
			}
			if err := f.TransferHardwareData(sw); err != nil {
				sw.Free()
				f.Free()
				return frames, fmt.Errorf("decode: HW frame transfer: %w", err)
			}
			sw.SetPts(f.Pts())
			f.Free()
			f = sw
		}

		frames = append(frames, f)
	}

	return frames, nil
}

var hwPixelFormats = map[astiav.PixelFormat]bool{
	astiav.PixelFormatCuda:         true,
	astiav.PixelFormatD3D11:        true,
	astiav.PixelFormatD3D11VaVld:   true,
	astiav.PixelFormatDrmPrime:     true,
	astiav.PixelFormatMediacodec:   true,
	astiav.PixelFormatOpencl:       true,
	astiav.PixelFormatQsv:          true,
	astiav.PixelFormatVaapi:        true,
	astiav.PixelFormatVideotoolbox: true,
}

func isHWPixelFormat(pf astiav.PixelFormat) bool {
	return hwPixelFormats[pf]
}

func (d *Decoder) Flush() ([]*astiav.Frame, error) {
	if d.codecCtx == nil {
		return nil, nil
	}
	if err := d.codecCtx.SendPacket(nil); err != nil {
		return nil, err
	}
	var frames []*astiav.Frame
	for {
		f := astiav.AllocFrame()
		if err := d.codecCtx.ReceiveFrame(f); err != nil {
			f.Free()
			break
		}
		frames = append(frames, f)
	}
	return frames, nil
}

func (d *Decoder) Close() {
	if d.codecCtx != nil {
		d.codecCtx.SendPacket(nil) //nolint:errcheck
		f := astiav.AllocFrame()
		if f != nil {
			for d.codecCtx.ReceiveFrame(f) == nil {
				f.Unref()
			}
			f.Free()
		}
		d.codecCtx.Free()
		d.codecCtx = nil
	}
	if d.hwCtx != nil {
		d.hwCtx.Free()
		d.hwCtx = nil
	}
}
