package mux

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/asticode/go-astiav"
)

type HLSMuxOpts struct {
	OutputDir          string
	SegmentDurationSec int
	VideoCodecID       astiav.CodecID
	VideoExtradata     []byte
	VideoWidth         int
	VideoHeight        int
	VideoTimeBase      astiav.Rational
	VideoFrameRate     int
	AudioCodecID       astiav.CodecID
	AudioExtradata     []byte
	AudioChannels      int
	AudioSampleRate    int
	AudioTimeBase      astiav.Rational
	AudioFrameSize     int
}

type HLSMuxer struct {
	opts         HLSMuxOpts
	fc           *astiav.FormatContext
	videoIdx     int
	audioIdx     int
	videoOutTB   astiav.Rational
	audioOutTB   astiav.Rational
	closed       bool
	mu           sync.Mutex
}

func NewHLSMuxer(opts HLSMuxOpts) (*HLSMuxer, error) {
	if opts.OutputDir == "" {
		return nil, errors.New("avmux: OutputDir is required")
	}
	if err := os.MkdirAll(opts.OutputDir, 0755); err != nil {
		return nil, fmt.Errorf("avmux: create output dir: %w", err)
	}
	if opts.SegmentDurationSec <= 0 {
		opts.SegmentDurationSec = 6
	}

	m := &HLSMuxer{
		opts:     opts,
		videoIdx: -1,
		audioIdx: -1,
	}

	if err := m.openFormatContext(); err != nil {
		return nil, fmt.Errorf("avmux: open hls muxer: %w", err)
	}

	return m, nil
}

func (m *HLSMuxer) openFormatContext() error {
	playlistPath := filepath.Join(m.opts.OutputDir, "playlist.m3u8")

	fc, err := astiav.AllocOutputFormatContext(nil, "hls", playlistPath)
	if err != nil {
		return fmt.Errorf("alloc output format context: %w", err)
	}
	m.fc = fc

	if m.opts.VideoCodecID != astiav.CodecIDNone {
		vs := fc.NewStream(nil)
		if vs == nil {
			fc.Free()
			m.fc = nil
			return errors.New("failed to allocate video stream")
		}
		cp := vs.CodecParameters()
		cp.SetCodecID(m.opts.VideoCodecID)
		cp.SetMediaType(astiav.MediaTypeVideo)
		cp.SetWidth(m.opts.VideoWidth)
		cp.SetHeight(m.opts.VideoHeight)
		if len(m.opts.VideoExtradata) > 0 {
			if err := cp.SetExtraData(m.opts.VideoExtradata); err != nil {
				fc.Free()
				m.fc = nil
				return fmt.Errorf("set video extradata: %w", err)
			}
		}
		vs.SetTimeBase(m.opts.VideoTimeBase)
		m.videoIdx = vs.Index()
	}

	if m.opts.AudioCodecID != astiav.CodecIDNone {
		as := fc.NewStream(nil)
		if as == nil {
			fc.Free()
			m.fc = nil
			return errors.New("failed to allocate audio stream")
		}
		cp := as.CodecParameters()
		cp.SetCodecID(m.opts.AudioCodecID)
		cp.SetMediaType(astiav.MediaTypeAudio)
		if m.opts.AudioSampleRate > 0 {
			cp.SetSampleRate(m.opts.AudioSampleRate)
		}
		switch m.opts.AudioChannels {
		case 1:
			cp.SetChannelLayout(astiav.ChannelLayoutMono)
		case 2:
			cp.SetChannelLayout(astiav.ChannelLayoutStereo)
		case 6:
			cp.SetChannelLayout(astiav.ChannelLayout5Point1)
		case 8:
			cp.SetChannelLayout(astiav.ChannelLayout7Point1)
		}
		if len(m.opts.AudioExtradata) > 0 {
			if err := cp.SetExtraData(m.opts.AudioExtradata); err != nil {
				fc.Free()
				m.fc = nil
				return fmt.Errorf("set audio extradata: %w", err)
			}
		}
		as.SetTimeBase(m.opts.AudioTimeBase)
		m.audioIdx = as.Index()
	}

	dict := astiav.NewDictionary()
	defer dict.Free()
	dict.Set("hls_time", strconv.Itoa(m.opts.SegmentDurationSec), 0)
	dict.Set("hls_segment_filename", filepath.Join(m.opts.OutputDir, "seg%d.ts"), 0)
	dict.Set("hls_list_size", "0", 0)
	dict.Set("hls_flags", "append_list", 0)

	if err := fc.WriteHeader(dict); err != nil {
		fc.Free()
		m.fc = nil
		return fmt.Errorf("write header: %w", err)
	}

	if m.videoIdx >= 0 {
		m.videoOutTB = fc.Streams()[m.videoIdx].TimeBase()
	}
	if m.audioIdx >= 0 {
		m.audioOutTB = fc.Streams()[m.audioIdx].TimeBase()
	}

	return nil
}

func (m *HLSMuxer) fixVideoDuration(pkt *astiav.Packet) {
	if pkt.Duration() > 0 {
		return
	}
	fps := m.opts.VideoFrameRate
	if fps <= 0 {
		fps = 25
	}
	outTB := m.videoOutTB
	if outTB.Den() > 0 && outTB.Num() > 0 {
		pkt.SetDuration(int64(outTB.Den()) / (int64(fps) * int64(outTB.Num())))
	}
}

func (m *HLSMuxer) fixAudioDuration(pkt *astiav.Packet) {
	if pkt.Duration() > 0 {
		return
	}
	frameSize := m.opts.AudioFrameSize
	if frameSize <= 0 {
		frameSize = 1024
	}
	sampleRate := m.opts.AudioSampleRate
	if sampleRate <= 0 {
		sampleRate = 48000
	}
	outTB := m.audioOutTB
	if outTB.Den() > 0 && outTB.Num() > 0 {
		pkt.SetDuration(int64(frameSize) * int64(outTB.Den()) / (int64(sampleRate) * int64(outTB.Num())))
	}
}

func (m *HLSMuxer) WriteVideoPacket(pkt *astiav.Packet) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("avmux: muxer is closed")
	}
	if m.fc == nil || m.videoIdx < 0 {
		return errors.New("avmux: no video track configured")
	}

	pkt.RescaleTs(m.opts.VideoTimeBase, m.videoOutTB)
	pkt.SetStreamIndex(m.videoIdx)
	m.fixVideoDuration(pkt)

	if err := m.fc.WriteInterleavedFrame(pkt); err != nil {
		return fmt.Errorf("avmux: write video packet: %w", err)
	}
	return nil
}

func (m *HLSMuxer) WriteAudioPacket(pkt *astiav.Packet) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("avmux: muxer is closed")
	}
	if m.fc == nil || m.audioIdx < 0 {
		return errors.New("avmux: no audio track configured")
	}

	pkt.RescaleTs(m.opts.AudioTimeBase, m.audioOutTB)
	pkt.SetStreamIndex(m.audioIdx)
	m.fixAudioDuration(pkt)

	if err := m.fc.WriteInterleavedFrame(pkt); err != nil {
		return fmt.Errorf("avmux: write audio packet: %w", err)
	}
	return nil
}

func (m *HLSMuxer) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	m.closed = true

	if m.fc == nil {
		return nil
	}

	var firstErr error
	if err := m.fc.WriteTrailer(); err != nil {
		firstErr = err
	}
	m.fc.Free()
	m.fc = nil

	return firstErr
}

func (m *HLSMuxer) Reset() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}

	if m.fc != nil {
		m.fc.WriteTrailer() //nolint:errcheck
		m.fc.Free()
		m.fc = nil
	}

	matches, _ := filepath.Glob(filepath.Join(m.opts.OutputDir, "seg*.ts"))
	for _, f := range matches {
		os.Remove(f)
	}
	os.Remove(filepath.Join(m.opts.OutputDir, "playlist.m3u8"))

	m.videoIdx = -1
	m.audioIdx = -1

	return m.openFormatContext()
}

func (m *HLSMuxer) SegmentCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	matches, _ := filepath.Glob(filepath.Join(m.opts.OutputDir, "seg*.ts"))
	return len(matches)
}

func (m *HLSMuxer) PlaylistContent() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, err := os.ReadFile(filepath.Join(m.opts.OutputDir, "playlist.m3u8"))
	if err != nil {
		return ""
	}
	return string(data)
}
