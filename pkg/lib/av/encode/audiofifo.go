package encode

import (
	"fmt"

	"github.com/asticode/go-astiav"
)

type ptsEntry struct {
	pts          int64
	sampleOffset int64
}

type AudioFIFO struct {
	encoder            *Encoder
	fifo               *astiav.AudioFifo
	frameSize          int
	channels           int
	sampleFmt          astiav.SampleFormat
	layout             astiav.ChannelLayout
	rate               int
	totalInputSamples  int64
	totalOutputSamples int64
	ptsQueue           []ptsEntry
}

func NewAudioFIFOFromEncoder(encoder *Encoder, channels int, layout astiav.ChannelLayout, rate int) *AudioFIFO {
	fs := encoder.FrameSize()
	if fs <= 0 {
		fs = 1024
	}
	return &AudioFIFO{
		encoder:   encoder,
		frameSize: fs,
		channels:  channels,
		sampleFmt: astiav.SampleFormatFltp,
		layout:    layout,
		rate:      rate,
	}
}

func NewAudioFIFO(encoder *Encoder, frameSize, channels int, sampleFmt astiav.SampleFormat, layout astiav.ChannelLayout, rate int) *AudioFIFO {
	return &AudioFIFO{
		encoder:   encoder,
		frameSize: frameSize,
		channels:  channels,
		sampleFmt: sampleFmt,
		layout:    layout,
		rate:      rate,
	}
}

func (f *AudioFIFO) Write(frame *astiav.Frame) ([]*astiav.Packet, error) {
	if f.fifo == nil {
		fifo := astiav.AllocAudioFifo(f.sampleFmt, f.channels, f.frameSize*4)
		if fifo == nil {
			return nil, fmt.Errorf("audiofifo: failed to allocate")
		}
		f.fifo = fifo
	}

	f.ptsQueue = append(f.ptsQueue, ptsEntry{pts: frame.Pts(), sampleOffset: f.totalInputSamples})
	f.totalInputSamples += int64(frame.NbSamples())

	if _, err := f.fifo.Write(frame); err != nil {
		return nil, fmt.Errorf("audiofifo: write: %w", err)
	}

	var allPkts []*astiav.Packet
	for f.fifo.Size() >= f.frameSize {
		outFrame := astiav.AllocFrame()
		if outFrame == nil {
			return allPkts, fmt.Errorf("audiofifo: alloc frame")
		}
		outFrame.SetNbSamples(f.frameSize)
		outFrame.SetSampleFormat(f.sampleFmt)
		outFrame.SetChannelLayout(f.layout)
		outFrame.SetSampleRate(f.rate)
		if err := outFrame.AllocBuffer(0); err != nil {
			outFrame.Free()
			return allPkts, fmt.Errorf("audiofifo: alloc buffer: %w", err)
		}
		if _, err := f.fifo.Read(outFrame); err != nil {
			outFrame.Free()
			return allPkts, fmt.Errorf("audiofifo: read: %w", err)
		}
		useIdx := 0
		for i := 1; i < len(f.ptsQueue); i++ {
			if f.ptsQueue[i].sampleOffset <= f.totalOutputSamples {
				useIdx = i
			} else {
				break
			}
		}
		entry := f.ptsQueue[useIdx]
		outFrame.SetPts(entry.pts + (f.totalOutputSamples - entry.sampleOffset))
		f.totalOutputSamples += int64(f.frameSize)

		if useIdx > 0 {
			f.ptsQueue = f.ptsQueue[useIdx:]
		}

		pkts, err := f.encoder.Encode(outFrame)
		outFrame.Free()
		if err != nil {
			return allPkts, err
		}
		allPkts = append(allPkts, pkts...)
	}

	return allPkts, nil
}

func (f *AudioFIFO) Reset() {
	if f.fifo != nil {
		f.fifo.Free()
		f.fifo = nil
	}
	f.totalInputSamples = 0
	f.totalOutputSamples = 0
	f.ptsQueue = nil
}

func (f *AudioFIFO) Close() {
	if f.fifo != nil {
		f.fifo.Free()
		f.fifo = nil
	}
}
