// Package avresample provides audio resampling and format conversion
// using libswresample via go-astiav.
package resample

import (
	"errors"
	"fmt"

	"github.com/asticode/go-astiav"
)

// Resampler wraps a SoftwareResampleContext to convert audio frames between
// sample rates, channel layouts, and sample formats.
type Resampler struct {
	swrCtx     *astiav.SoftwareResampleContext
	dstLayout  astiav.ChannelLayout
	dstRate    int
	dstFmt     astiav.SampleFormat
}

// channelLayoutForCount returns a standard channel layout for the given
// number of channels.
func channelLayoutForCount(channels int) (astiav.ChannelLayout, error) {
	switch channels {
	case 1:
		return astiav.ChannelLayoutMono, nil
	case 2:
		return astiav.ChannelLayoutStereo, nil
	case 6:
		return astiav.ChannelLayout5Point1, nil
	case 8:
		return astiav.ChannelLayout7Point1, nil
	default:
		return astiav.ChannelLayout{}, fmt.Errorf("avresample: unsupported channel count %d", channels)
	}
}

// NewResampler creates a Resampler that converts audio from the source
// parameters to the destination parameters.
func NewResampler(srcChannels, srcRate int, srcFmt astiav.SampleFormat,
	dstChannels, dstRate int, dstFmt astiav.SampleFormat) (*Resampler, error) {

	dstLayout, err := channelLayoutForCount(dstChannels)
	if err != nil {
		return nil, fmt.Errorf("avresample: destination: %w", err)
	}

	ctx := astiav.AllocSoftwareResampleContext()
	if ctx == nil {
		return nil, fmt.Errorf("avresample: failed to allocate SoftwareResampleContext")
	}

	return &Resampler{
		swrCtx:    ctx,
		dstLayout: dstLayout,
		dstRate:   dstRate,
		dstFmt:    dstFmt,
	}, nil
}

// Convert resamples src into a new frame with the destination parameters.
// The SoftwareResampleContext auto-negotiates from the source frame's
// properties. The caller owns the returned frame and must free it.
func (r *Resampler) Convert(src *astiav.Frame) (*astiav.Frame, error) {
	dst := astiav.AllocFrame()
	dst.SetChannelLayout(r.dstLayout)
	dst.SetSampleRate(r.dstRate)
	dst.SetSampleFormat(r.dstFmt)

	if err := r.swrCtx.ConvertFrame(src, dst); err != nil {
		if errors.Is(err, astiav.ErrInputChanged) {
			r.swrCtx.Free()
			r.swrCtx = astiav.AllocSoftwareResampleContext()
			if r.swrCtx == nil {
				dst.Free()
				return nil, fmt.Errorf("avresample: failed to reallocate after input change")
			}
			if retryErr := r.swrCtx.ConvertFrame(src, dst); retryErr != nil {
				dst.Free()
				return nil, fmt.Errorf("avresample: convert frame after input change: %w", retryErr)
			}
		} else {
			dst.Free()
			return nil, fmt.Errorf("avresample: convert frame: %w", err)
		}
	}
	dst.SetPts(src.Pts())
	return dst, nil
}

// Close frees the underlying SoftwareResampleContext. It is safe to call
// multiple times.
func (r *Resampler) Close() {
	if r.swrCtx != nil {
		r.swrCtx.Free()
		r.swrCtx = nil
	}
}
