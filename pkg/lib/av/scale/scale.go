// Package avscale provides video frame scaling and pixel-format conversion
// using libswscale via go-astiav.
package scale

import (
	"github.com/asticode/go-astiav"
)

// Scaler wraps a SoftwareScaleContext to convert frames between resolutions
// and/or pixel formats.
type Scaler struct {
	swsCtx *astiav.SoftwareScaleContext
	dstW   int
	dstH   int
	dstFmt astiav.PixelFormat
}

// NewScaler creates a Scaler that converts frames from src dimensions/format
// to dst dimensions/format using bilinear interpolation.
func NewScaler(srcW, srcH int, srcFmt astiav.PixelFormat,
	dstW, dstH int, dstFmt astiav.PixelFormat) (*Scaler, error) {

	flags := astiav.NewSoftwareScaleContextFlags(astiav.SoftwareScaleContextFlagBilinear)
	ctx, err := astiav.CreateSoftwareScaleContext(srcW, srcH, srcFmt, dstW, dstH, dstFmt, flags)
	if err != nil {
		return nil, err
	}
	return &Scaler{
		swsCtx: ctx,
		dstW:   dstW,
		dstH:   dstH,
		dstFmt: dstFmt,
	}, nil
}

// Scale converts src into a new frame with the destination dimensions and
// pixel format. The caller owns the returned frame and must free it.
func (s *Scaler) Scale(src *astiav.Frame) (*astiav.Frame, error) {
	dst := astiav.AllocFrame()
	dst.SetWidth(s.dstW)
	dst.SetHeight(s.dstH)
	dst.SetPixelFormat(s.dstFmt)
	if err := dst.AllocBuffer(0); err != nil {
		dst.Free()
		return nil, err
	}
	if err := s.swsCtx.ScaleFrame(src, dst); err != nil {
		dst.Free()
		return nil, err
	}
	return dst, nil
}

// Close releases the underlying SoftwareScaleContext. It is safe to call
// multiple times.
func (s *Scaler) Close() {
	if s.swsCtx != nil {
		s.swsCtx.Free()
		s.swsCtx = nil
	}
}
