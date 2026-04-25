package mux

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	AudioCodecID       astiav.CodecID
	AudioExtradata     []byte
	AudioChannels      int
	AudioSampleRate    int
	AudioTimeBase      astiav.Rational
}

type HLSMuxer struct {
	opts           HLSMuxOpts
	seg            *hlsSegment
	segCount       int
	segDurations   []float64
	targetDuration int
	closed         bool
	mu             sync.Mutex
}

type hlsSegment struct {
	muxer        *StreamMuxer
	file         *os.File
	path         string
	videoIdx     int
	audioIdx     int
	startPTS     int64
	startPTSSet  bool
	pktCount     int
	videoPktCount int
	videoDTS      int64
	videoDTSInit  bool
	audioDTS      int64
	audioDTSInit  bool
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
		opts:           opts,
		targetDuration: opts.SegmentDurationSec + 1,
	}

	if err := m.openSegment(); err != nil {
		return nil, fmt.Errorf("avmux: open initial segment: %w", err)
	}

	return m, nil
}

func (m *HLSMuxer) openSegment() error {
	segName := fmt.Sprintf("seg%d.ts", m.segCount)
	segPath := filepath.Join(m.opts.OutputDir, segName)

	f, err := os.Create(segPath)
	if err != nil {
		return fmt.Errorf("create segment file: %w", err)
	}

	sm, err := NewStreamMuxer("mpegts", f)
	if err != nil {
		f.Close()
		return fmt.Errorf("create stream muxer: %w", err)
	}

	seg := &hlsSegment{
		muxer:    sm,
		file:     f,
		path:     segPath,
		videoIdx: -1,
		audioIdx: -1,
	}

	if m.opts.VideoCodecID != astiav.CodecIDNone {
		videoCP := astiav.AllocCodecParameters()
		videoCP.SetCodecID(m.opts.VideoCodecID)
		videoCP.SetMediaType(astiav.MediaTypeVideo)
		videoCP.SetWidth(m.opts.VideoWidth)
		videoCP.SetHeight(m.opts.VideoHeight)
		if len(m.opts.VideoExtradata) > 0 {
			if err := videoCP.SetExtraData(m.opts.VideoExtradata); err != nil {
				videoCP.Free()
				sm.Close()
				f.Close()
				return fmt.Errorf("set video extradata: %w", err)
			}
		}
		vs, err := sm.AddStream(videoCP)
		videoCP.Free()
		if err != nil {
			sm.Close()
			f.Close()
			return fmt.Errorf("add video stream: %w", err)
		}
		seg.videoIdx = vs.Index()
	}

	if m.opts.AudioCodecID != astiav.CodecIDNone {
		audioCP := astiav.AllocCodecParameters()
		audioCP.SetCodecID(m.opts.AudioCodecID)
		audioCP.SetMediaType(astiav.MediaTypeAudio)
		if m.opts.AudioSampleRate > 0 {
			audioCP.SetSampleRate(m.opts.AudioSampleRate)
		}
		switch m.opts.AudioChannels {
		case 1:
			audioCP.SetChannelLayout(astiav.ChannelLayoutMono)
		case 2:
			audioCP.SetChannelLayout(astiav.ChannelLayoutStereo)
		case 6:
			audioCP.SetChannelLayout(astiav.ChannelLayout5Point1)
		case 8:
			audioCP.SetChannelLayout(astiav.ChannelLayout7Point1)
		}
		if len(m.opts.AudioExtradata) > 0 {
			if err := audioCP.SetExtraData(m.opts.AudioExtradata); err != nil {
				audioCP.Free()
				sm.Close()
				f.Close()
				return fmt.Errorf("set audio extradata: %w", err)
			}
		}
		as, err := sm.AddStream(audioCP)
		audioCP.Free()
		if err != nil {
			sm.Close()
			f.Close()
			return fmt.Errorf("add audio stream: %w", err)
		}
		seg.audioIdx = as.Index()
	}

	if err := sm.WriteHeader(); err != nil {
		sm.Close()
		f.Close()
		return fmt.Errorf("write header: %w", err)
	}

	m.seg = seg
	return nil
}

func (m *HLSMuxer) closeSegmentWithDTS(nextDTS int64) (float64, error) {
	if m.seg == nil {
		return 0, nil
	}

	var dur float64
	if m.seg.startPTSSet {
		endDTS := nextDTS
		if endDTS == 0 && m.seg.videoDTSInit {
			endDTS = m.seg.videoDTS
		}
		tb := m.opts.VideoTimeBase
		if tb.Den() > 0 && endDTS > m.seg.startPTS {
			dur = float64(endDTS-m.seg.startPTS) * float64(tb.Num()) / float64(tb.Den())
		}
	}
	if dur <= 0 {
		dur = float64(m.opts.SegmentDurationSec)
	}

	if err := m.seg.muxer.Close(); err != nil {
		m.seg.file.Close()
		return dur, fmt.Errorf("close segment muxer: %w", err)
	}
	m.seg.file.Close()

	m.segDurations = append(m.segDurations, dur)
	m.segCount++
	m.seg = nil

	m.writePlaylist(false)

	return dur, nil
}

func (m *HLSMuxer) segmentDurationAtDTS(dts int64) float64 {
	if m.seg == nil || !m.seg.startPTSSet {
		return 0
	}
	tb := m.opts.VideoTimeBase
	if tb.Den() == 0 {
		return 0
	}
	return float64(dts-m.seg.startPTS) * float64(tb.Num()) / float64(tb.Den())
}

func (m *HLSMuxer) WriteVideoPacket(pkt *astiav.Packet) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("avmux: muxer is closed")
	}
	if m.seg == nil || m.seg.videoIdx < 0 {
		return errors.New("avmux: no video track configured")
	}

	isKeyframe := pkt.Flags().Has(astiav.PacketFlagKey)

	if isKeyframe && m.seg.videoPktCount > 0 {
		dur := m.segmentDurationAtDTS(pkt.Dts())
		if dur >= float64(m.opts.SegmentDurationSec) {
			if _, err := m.closeSegmentWithDTS(pkt.Dts()); err != nil {
				return err
			}
			if err := m.openSegment(); err != nil {
				return err
			}
		}
	}

	if !m.seg.startPTSSet {
		m.seg.startPTS = pkt.Dts()
		if m.seg.startPTS == astiav.NoPtsValue {
			m.seg.startPTS = pkt.Pts()
		}
		m.seg.startPTSSet = true
	}

	pkt.SetStreamIndex(m.seg.videoIdx)

	dts := pkt.Dts()
	if m.seg.videoDTSInit && dts <= m.seg.videoDTS {
		dts = m.seg.videoDTS + 1
		pts := pkt.Pts()
		if pts < dts {
			pts = dts
		}
		pkt.SetDts(dts)
		pkt.SetPts(pts)
	}
	m.seg.videoDTS = dts
	m.seg.videoDTSInit = true

	if err := m.seg.muxer.WritePacket(pkt); err != nil {
		return fmt.Errorf("avmux: write video packet: %w", err)
	}
	m.seg.pktCount++
	m.seg.videoPktCount++
	return nil
}

func (m *HLSMuxer) WriteAudioPacket(pkt *astiav.Packet) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("avmux: muxer is closed")
	}
	if m.seg == nil || m.seg.audioIdx < 0 {
		return errors.New("avmux: no audio track configured")
	}

	pkt.SetStreamIndex(m.seg.audioIdx)

	dts := pkt.Dts()
	if m.seg.audioDTSInit && dts <= m.seg.audioDTS {
		dts = m.seg.audioDTS + 1
		pts := pkt.Pts()
		if pts < dts {
			pts = dts
		}
		pkt.SetDts(dts)
		pkt.SetPts(pts)
	}
	m.seg.audioDTS = dts
	m.seg.audioDTSInit = true

	if err := m.seg.muxer.WritePacket(pkt); err != nil {
		return fmt.Errorf("avmux: write audio packet: %w", err)
	}
	m.seg.pktCount++
	return nil
}

func (m *HLSMuxer) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	m.closed = true

	var firstErr error
	if m.seg != nil && m.seg.pktCount > 0 {
		if _, err := m.closeSegmentWithDTS(0); err != nil && firstErr == nil {
			firstErr = err
		}
	} else if m.seg != nil {
		m.seg.muxer.Close()
		m.seg.file.Close()
		os.Remove(m.seg.path)
		m.seg = nil
	}

	m.writePlaylist(true)

	return firstErr
}

func (m *HLSMuxer) Reset() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}

	if m.seg != nil {
		m.seg.muxer.Close()
		m.seg.file.Close()
		m.seg = nil
	}

	for i := 0; i < m.segCount; i++ {
		os.Remove(filepath.Join(m.opts.OutputDir, fmt.Sprintf("seg%d.ts", i)))
	}
	os.Remove(filepath.Join(m.opts.OutputDir, "playlist.m3u8"))

	m.segCount = 0
	m.segDurations = nil

	return m.openSegment()
}

func (m *HLSMuxer) SegmentCount() int {
	m.mu.Lock()
	n := m.segCount
	m.mu.Unlock()
	return n
}

func (m *HLSMuxer) PlaylistContent() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.buildPlaylist(false)
}

func (m *HLSMuxer) buildPlaylist(endlist bool) string {
	maxDur := m.opts.SegmentDurationSec + 1
	for _, d := range m.segDurations {
		if int(d)+1 > maxDur {
			maxDur = int(d) + 1
		}
	}

	pl := "#EXTM3U\n"
	pl += "#EXT-X-VERSION:3\n"
	pl += fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", maxDur)
	pl += "#EXT-X-MEDIA-SEQUENCE:0\n"

	for i, dur := range m.segDurations {
		pl += fmt.Sprintf("#EXTINF:%.3f,\n", dur)
		pl += fmt.Sprintf("seg%d.ts\n", i)
	}

	if endlist {
		pl += "#EXT-X-ENDLIST\n"
	}

	return pl
}

func (m *HLSMuxer) writePlaylist(endlist bool) {
	content := m.buildPlaylist(endlist)
	path := filepath.Join(m.opts.OutputDir, "playlist.m3u8")
	atomicWrite(path, []byte(content)) //nolint:errcheck
}
