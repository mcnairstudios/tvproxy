package demux

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/gavinmcnair/tvproxy/pkg/lib/av/probe"
)

type StreamType int

const (
	Video    StreamType = iota
	Audio
	Subtitle
)

type DemuxOpts struct {
	TimeoutSec       int
	AudioTrack       int
	AudioLanguage    string
	Follow           bool
	FormatHint       string
	SATIPHTTPMode    bool
	UserAgent        string
	RTSPLatency      int
	AudioPassthrough bool
	ProbeSize        int
	AnalyzeDuration  int
	CachedStreamInfo *probe.StreamInfo
}

const followRetryInterval = 100 * time.Millisecond

func DefaultDemuxOpts() DemuxOpts {
	return DemuxOpts{AudioTrack: -1}
}

type Packet struct {
	Type     StreamType
	Data     []byte
	PTS      int64
	DTS      int64
	Duration int64
	Keyframe bool
}

type Demuxer struct {
	fc         *astiav.FormatContext
	pkt        *astiav.Packet
	streams    []*astiav.Stream
	videoIdx   int
	audioIdx   int
	subIdx     int
	basePTS    int64
	mu         sync.Mutex
	closed     bool
	url        string
	streamInfo *probe.StreamInfo
	opts       DemuxOpts

	seekCh          chan seekRequest
	onSeek          func()
	audioBasePTS    int64
	audioFrameCount int64
	audioSampleRate int
	audioFrameSize  int
	audioPTSInited  bool
}

type seekRequest struct {
	posMs  int64
	result chan error
}

type TeeSource interface {
	SetupFormatContext(fc *astiav.FormatContext)
}

func NewDemuxerWithTee(tee TeeSource, opts DemuxOpts) (*Demuxer, error) {
	fc := astiav.AllocFormatContext()
	if fc == nil {
		return nil, fmt.Errorf("demux: failed to allocate format context")
	}

	tee.SetupFormatContext(fc)

	var inputFmt *astiav.InputFormat
	if opts.FormatHint != "" {
		inputFmt = astiav.FindInputFormat(opts.FormatHint)
	}

	if err := fc.OpenInput("", inputFmt, nil); err != nil {
		fc.Free()
		return nil, fmt.Errorf("demux: open input via tee: %w", err)
	}

	return finishDemuxerSetup(fc, "", opts)
}

func NewDemuxer(url string, opts DemuxOpts) (*Demuxer, error) {
	if opts.SATIPHTTPMode && strings.HasPrefix(url, "rtsp://") {
		url = convertRTSPtoHTTP(url)
	}

	fc := astiav.AllocFormatContext()
	if fc == nil {
		return nil, fmt.Errorf("demux: failed to allocate format context")
	}

	d := astiav.NewDictionary()
	defer d.Free()
	if opts.TimeoutSec > 0 {
		d.Set("timeout", fmt.Sprintf("%d", opts.TimeoutSec*1_000_000), 0)
	}
	if opts.UserAgent != "" {
		d.Set("user_agent", opts.UserAgent, 0)
	}
	if opts.RTSPLatency > 0 {
		d.Set("stimeout", fmt.Sprintf("%d", opts.RTSPLatency*1000), 0)
	}
	isLive := strings.HasPrefix(url, "rtsp://")
	probeSize := opts.ProbeSize
	analyzeDur := opts.AnalyzeDuration
	if probeSize <= 0 {
		if isLive {
			probeSize = 1000000
		} else {
			probeSize = 5000000
		}
	}
	if analyzeDur <= 0 {
		if isLive {
			analyzeDur = 0
		} else {
			analyzeDur = 5000000
		}
	}
	d.Set("probesize", fmt.Sprintf("%d", probeSize), 0)
	d.Set("analyzeduration", fmt.Sprintf("%d", analyzeDur), 0)
	if isLive {
		d.Set("fflags", "nobuffer+igndts+flush_packets", 0)
		d.Set("flags", "low_delay", 0)
	}

	var inputFmt *astiav.InputFormat
	if opts.FormatHint != "" {
		inputFmt = astiav.FindInputFormat(opts.FormatHint)
	}

	if err := fc.OpenInput(url, inputFmt, d); err != nil {
		fc.Free()
		return nil, fmt.Errorf("demux: open input %q: %w", url, err)
	}

	return finishDemuxerSetup(fc, url, opts)
}

func finishDemuxerSetup(fc *astiav.FormatContext, url string, opts DemuxOpts) (*Demuxer, error) {
	var si *probe.StreamInfo
	if opts.CachedStreamInfo != nil {
		si = opts.CachedStreamInfo
	} else {
		if err := fc.FindStreamInfo(nil); err != nil {
			fc.CloseInput()
			fc.Free()
			return nil, fmt.Errorf("demux: find stream info: %w", err)
		}
		si = probe.ExtractStreamInfo(fc)
	}

	dm := &Demuxer{
		fc:         fc,
		streams:    fc.Streams(),
		streamInfo: si,
		videoIdx:   -1,
		audioIdx:   -1,
		subIdx:     -1,
		basePTS:    -1,
		url:        url,
		opts:       opts,
		seekCh:     make(chan seekRequest, 1),
	}

	if len(dm.streams) > 0 {
		dm.setIndicesFromStreams(opts)
	} else if si != nil {
		dm.setIndicesFromCachedInfo(si, opts)
	}

	if dm.audioIdx >= 0 {
		if as := dm.streamByIndex(dm.audioIdx); as != nil {
			cp := as.CodecParameters()
			dm.audioSampleRate = cp.SampleRate()
			dm.audioFrameSize = cp.FrameSize()
			if dm.audioFrameSize <= 0 {
				dm.audioFrameSize = 1024
			}
		}
	}

	p := astiav.AllocPacket()
	if p == nil {
		fc.CloseInput()
		fc.Free()
		return nil, fmt.Errorf("demux: failed to allocate packet")
	}
	dm.pkt = p

	return dm, nil
}

type audioCandidate struct {
	index int
	lang  string
}

func (d *Demuxer) setIndicesFromStreams(opts DemuxOpts) {
	var audioCandidates []audioCandidate

	for _, s := range d.streams {
		cp := s.CodecParameters()
		if cp.CodecID() == astiav.CodecIDNone {
			continue
		}
		switch cp.MediaType() {
		case astiav.MediaTypeVideo:
			if d.videoIdx < 0 {
				d.videoIdx = s.Index()
			}
		case astiav.MediaTypeAudio:
			lang := metadataValue(s.Metadata(), "language")
			audioCandidates = append(audioCandidates, audioCandidate{index: s.Index(), lang: lang})
		case astiav.MediaTypeSubtitle:
			if d.subIdx < 0 {
				d.subIdx = s.Index()
			}
		}
	}

	d.selectAudio(audioCandidates, opts)
}

func (d *Demuxer) setIndicesFromCachedInfo(si *probe.StreamInfo, opts DemuxOpts) {
	if si.Video != nil {
		d.videoIdx = si.Video.Index
	}
	if len(si.SubTracks) > 0 {
		d.subIdx = si.SubTracks[0].Index
	}

	var audioCandidates []audioCandidate
	for _, at := range si.AudioTracks {
		audioCandidates = append(audioCandidates, audioCandidate{index: at.Index, lang: at.Language})
	}

	d.selectAudio(audioCandidates, opts)
}

func (d *Demuxer) selectAudio(candidates []audioCandidate, opts DemuxOpts) {
	switch {
	case opts.AudioLanguage != "" && len(candidates) > 0:
		d.audioIdx = candidates[0].index
		for _, c := range candidates {
			if c.lang == opts.AudioLanguage {
				d.audioIdx = c.index
				break
			}
		}
	case opts.AudioTrack >= 0:
		valid := false
		for _, c := range candidates {
			if c.index == opts.AudioTrack {
				valid = true
				break
			}
		}
		if valid {
			d.audioIdx = opts.AudioTrack
		} else if len(candidates) > 0 {
			d.audioIdx = candidates[0].index
		}
	default:
		if len(candidates) > 0 {
			d.audioIdx = candidates[0].index
		}
	}
}

var retryDelays = []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}

func (d *Demuxer) ReadPacket() (*Packet, error) {
	pkt, err := d.readPacketOnce()
	if err == nil || errors.Is(err, io.EOF) || !isTransient(err) {
		return pkt, err
	}

	for _, delay := range retryDelays {
		time.Sleep(delay)
		if reconnErr := d.Reconnect(); reconnErr != nil {
			continue
		}
		pkt, err = d.readPacketOnce()
		if err == nil {
			return pkt, nil
		}
		if errors.Is(err, io.EOF) {
			return nil, io.EOF
		}
	}
	return nil, fmt.Errorf("demux: read failed after retries: %w", err)
}

func (d *Demuxer) readPacketOnce() (*Packet, error) {
	for {
		select {
		case req := <-d.seekCh:
			req.result <- d.SeekTo(req.posMs)
			d.basePTS = -1
			d.audioPTSInited = false
			d.audioFrameCount = 0
			if d.onSeek != nil {
				d.onSeek()
			}
			continue
		default:
		}

		if err := d.fc.ReadFrame(d.pkt); err != nil {
			if errors.Is(err, astiav.ErrEof) {
				if d.opts.Follow {
					time.Sleep(followRetryInterval)
					continue
				}
				return nil, io.EOF
			}
			return nil, fmt.Errorf("demux: read frame: %w", err)
		}

		idx := d.pkt.StreamIndex()

		d.mu.Lock()
		curAudio := d.audioIdx
		d.mu.Unlock()

		var stype StreamType
		switch idx {
		case d.videoIdx:
			stype = Video
		case curAudio:
			stype = Audio
		case d.subIdx:
			stype = Subtitle
		default:
			d.pkt.Unref()
			continue
		}

		stream := d.streamByIndex(idx)
		if stream == nil {
			d.pkt.Unref()
			continue
		}

		tb := stream.TimeBase()

		rawDts := d.pkt.Dts()
		if rawDts == astiav.NoPtsValue {
			rawDts = d.pkt.Pts()
		}

		ptsNs := toNanoseconds(d.pkt.Pts(), tb)
		dtsNs := toNanoseconds(rawDts, tb)
		durNs := toNanoseconds(d.pkt.Duration(), tb)

		if d.basePTS < 0 {
			d.basePTS = ptsNs
		}
		ptsNs -= d.basePTS
		dtsNs -= d.basePTS

		if stype == Audio && d.audioSampleRate > 0 && !d.opts.AudioPassthrough {
			if !d.audioPTSInited {
				d.audioBasePTS = ptsNs
				d.audioFrameCount = 0
				d.audioPTSInited = true
			}
			ptsNs = d.audioBasePTS + d.audioFrameCount*int64(d.audioFrameSize)*1_000_000_000/int64(d.audioSampleRate)
			dtsNs = ptsNs
			d.audioFrameCount++
		}

		src := d.pkt.Data()
		data := make([]byte, len(src))
		copy(data, src)

		keyframe := d.pkt.Flags().Has(astiav.PacketFlagKey)

		d.pkt.Unref()

		return &Packet{
			Type:     stype,
			Data:     data,
			PTS:      ptsNs,
			DTS:      dtsNs,
			Duration: durNs,
			Keyframe: keyframe,
		}, nil
	}
}

func (d *Demuxer) Reconnect() error {
	d.mu.Lock()
	savedVideo := d.videoIdx
	savedAudio := d.audioIdx
	savedSub := d.subIdx
	d.mu.Unlock()

	if d.pkt != nil {
		d.pkt.Free()
		d.pkt = nil
	}
	if d.fc != nil {
		d.fc.CloseInput()
		d.fc.Free()
		d.fc = nil
	}

	fc := astiav.AllocFormatContext()
	if fc == nil {
		return fmt.Errorf("demux: reconnect: failed to allocate format context")
	}

	dict := astiav.NewDictionary()
	defer dict.Free()
	if d.opts.TimeoutSec > 0 {
		dict.Set("timeout", fmt.Sprintf("%d", d.opts.TimeoutSec*1_000_000), 0)
	}
	if d.opts.UserAgent != "" {
		dict.Set("user_agent", d.opts.UserAgent, 0)
	}
	if d.opts.RTSPLatency > 0 {
		dict.Set("stimeout", fmt.Sprintf("%d", d.opts.RTSPLatency*1000), 0)
	}
	probeSize := d.opts.ProbeSize
	if probeSize <= 0 {
		probeSize = 1000000
	}
	analyzeDur := d.opts.AnalyzeDuration
	if analyzeDur <= 0 {
		analyzeDur = 1000000
	}
	dict.Set("probesize", fmt.Sprintf("%d", probeSize), 0)
	dict.Set("analyzeduration", fmt.Sprintf("%d", analyzeDur), 0)

	var inputFmt *astiav.InputFormat
	if d.opts.FormatHint != "" {
		inputFmt = astiav.FindInputFormat(d.opts.FormatHint)
	}

	if err := fc.OpenInput(d.url, inputFmt, dict); err != nil {
		fc.Free()
		return fmt.Errorf("demux: reconnect: open input %q: %w", d.url, err)
	}

	if err := fc.FindStreamInfo(nil); err != nil {
		fc.CloseInput()
		fc.Free()
		return fmt.Errorf("demux: reconnect: find stream info: %w", err)
	}

	pkt := astiav.AllocPacket()
	if pkt == nil {
		fc.CloseInput()
		fc.Free()
		return fmt.Errorf("demux: reconnect: failed to allocate packet")
	}

	d.fc = fc
	d.pkt = pkt
	d.streams = fc.Streams()

	d.mu.Lock()
	d.videoIdx = savedVideo
	d.audioIdx = savedAudio
	d.subIdx = savedSub
	d.mu.Unlock()

	d.basePTS = -1
	d.audioPTSInited = false
	d.audioFrameCount = 0
	d.closed = false

	return nil
}

func isTransient(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, astiav.ErrEagain) {
		return true
	}
	if errors.Is(err, astiav.ErrEio) {
		return true
	}
	if errors.Is(err, astiav.ErrEtimedout) {
		return true
	}
	if errors.Is(err, astiav.ErrEpipe) {
		return true
	}
	s := err.Error()
	return strings.Contains(s, "Connection reset") ||
		strings.Contains(s, "Connection refused") ||
		strings.Contains(s, "Network is unreachable") ||
		strings.Contains(s, "timeout")
}

func (d *Demuxer) SetAudioTrack(idx int) error {
	s := d.streamByIndex(idx)
	if s == nil {
		return fmt.Errorf("demux: stream index %d not found", idx)
	}
	if s.CodecParameters().MediaType() != astiav.MediaTypeAudio {
		return fmt.Errorf("demux: stream index %d is not an audio stream", idx)
	}

	d.mu.Lock()
	d.audioIdx = idx
	d.mu.Unlock()
	return nil
}

func (d *Demuxer) SetOnSeek(fn func()) {
	d.onSeek = fn
}

func (d *Demuxer) RequestSeek(posMs int64) error {
	req := seekRequest{posMs: posMs, result: make(chan error, 1)}
	select {
	case d.seekCh <- req:
		return <-req.result
	default:
		return fmt.Errorf("demux: seek channel full")
	}
}

func (d *Demuxer) SeekTo(posMs int64) error {
	streamIdx := d.videoIdx
	if streamIdx < 0 {
		streamIdx = -1
	}

	var ts int64
	if streamIdx >= 0 {
		s := d.streamByIndex(streamIdx)
		if s != nil {
			tb := s.TimeBase()
			ts = posMs * int64(tb.Den()) / (1000 * int64(tb.Num()))
		}
	} else {
		ts = posMs * 1000
	}

	flags := astiav.NewSeekFlags(astiav.SeekFlagBackward)
	if err := d.fc.SeekFrame(streamIdx, ts, flags); err != nil {
		return fmt.Errorf("demux: seek to %d ms: %w", posMs, err)
	}
	return nil
}

func (d *Demuxer) Close() {
	if d.closed {
		return
	}
	d.closed = true
	if d.pkt != nil {
		d.pkt.Free()
		d.pkt = nil
	}
	if d.fc != nil {
		d.fc.CloseInput()
		d.fc.Free()
		d.fc = nil
	}
}

func (d *Demuxer) StreamInfo() *probe.StreamInfo {
	return d.streamInfo
}

func (d *Demuxer) VideoCodecParameters() *astiav.CodecParameters {
	if d.videoIdx < 0 {
		return nil
	}
	s := d.streamByIndex(d.videoIdx)
	if s == nil {
		return nil
	}
	return s.CodecParameters()
}

func (d *Demuxer) AudioCodecParameters() *astiav.CodecParameters {
	d.mu.Lock()
	idx := d.audioIdx
	d.mu.Unlock()
	if idx < 0 {
		return nil
	}
	s := d.streamByIndex(idx)
	if s == nil {
		return nil
	}
	return s.CodecParameters()
}

func (d *Demuxer) streamByIndex(idx int) *astiav.Stream {
	for _, s := range d.streams {
		if s.Index() == idx {
			return s
		}
	}
	return nil
}

func toNanoseconds(ts int64, tb astiav.Rational) int64 {
	return ts * int64(tb.Num()) * 1_000_000_000 / int64(tb.Den())
}

func metadataValue(d *astiav.Dictionary, key string) string {
	if d == nil {
		return ""
	}
	entry := d.Get(key, nil, 0)
	if entry == nil {
		return ""
	}
	return entry.Value()
}

func convertRTSPtoHTTP(url string) string {
	url = strings.Replace(url, "rtsp://", "http://", 1)
	if strings.Contains(url, ":554/") {
		url = strings.Replace(url, ":554/", ":8875/", 1)
	} else if strings.Contains(url, ":554") {
		url = strings.Replace(url, ":554", ":8875", 1)
	} else {
		afterScheme := strings.TrimPrefix(url, "http://")
		slashIdx := strings.Index(afterScheme, "/")
		if slashIdx >= 0 {
			url = "http://" + afterScheme[:slashIdx] + ":8875" + afterScheme[slashIdx:]
		} else {
			url = "http://" + afterScheme + ":8875"
		}
	}
	return url
}
