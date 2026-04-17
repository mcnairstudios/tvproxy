package fmp4

import (
	"bytes"
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Eyevinn/mp4ff/aac"
	"github.com/Eyevinn/mp4ff/avc"
	"github.com/Eyevinn/mp4ff/hevc"
	"github.com/Eyevinn/mp4ff/mp4"
)

const SegmentDuration = 2 * time.Second

type segmentEntry struct {
	data        []byte
	startTimeNs int64
}

type TrackStore struct {
	mu        sync.Mutex
	cond      *sync.Cond
	closed    bool
	gen       int64
	initReady int32

	initSeg  []byte
	segments []segmentEntry

	pendingSamples []mp4.FullSample
	pendingPTSNs   []int64
	pendingStart   time.Time
	seqNum         uint32
	decodeTime     uint64
	trackID        uint32
	timescale      uint32

	vps [][]byte
	sps [][]byte
	pps [][]byte

	av1SeqHdr []byte

	audioObjType byte
	audioFreq    int

	isVideo      bool
	videoCodec   string // "h264", "h265", "av1"
	targetHeight int

	lastPTSNs    int64
	firstPTSNs   int64
	sharedBaseNs *int64
	partner      *TrackStore
	maxSegments  int

	durHistory  [10]uint32
	durIdx      int
	durCount    int
}

func NewTrackStore(isVideo bool, videoCodec string) *TrackStore {
	ts := &TrackStore{
		isVideo:     isVideo,
		videoCodec:  videoCodec,
		trackID:     1,
		timescale:   90000,
		lastPTSNs:   -1,
		firstPTSNs:  -1,
		maxSegments: 60,
	}
	if !isVideo {
		ts.timescale = 48000
	}
	ts.cond = sync.NewCond(&ts.mu)
	return ts
}

func (ts *TrackStore) Reset(gen int64, seekPosNs int64) {
	ts.mu.Lock()
	atomic.StoreInt32(&ts.initReady, 0)
	ts.gen = gen
	ts.segments = nil
	ts.pendingSamples = nil
	ts.pendingPTSNs = nil
	ts.pendingStart = time.Time{}
	ts.seqNum = 0
	ts.decodeTime = 0
	ts.lastPTSNs = -1
	ts.firstPTSNs = -1
	if ts.sharedBaseNs != nil {
		atomic.StoreInt64(ts.sharedBaseNs, -1)
	}
	ts.mu.Unlock()
	ts.cond.Broadcast()
}

func (ts *TrackStore) Close() {
	ts.mu.Lock()
	ts.closed = true
	ts.mu.Unlock()
	ts.cond.Broadcast()
}

func (ts *TrackStore) TimestampOffset() float64 {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.firstPTSNs < 0 || ts.sharedBaseNs == nil {
		return 0
	}
	base := atomic.LoadInt64(ts.sharedBaseNs)
	if base < 0 {
		return 0
	}
	off := float64(ts.firstPTSNs-base) / 1e9
	if off < -10 || off > 30 {
		return 0
	}
	return off
}

func (ts *TrackStore) IsClosed() bool {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.closed
}

func (ts *TrackStore) IsInitReady() bool {
	return atomic.LoadInt32(&ts.initReady) == 1
}

func (ts *TrackStore) Generation() int64 {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.gen
}

func (ts *TrackStore) SegmentCount() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return len(ts.segments)
}

func (ts *TrackStore) recordDuration(d uint32) {
	ts.durHistory[ts.durIdx] = d
	ts.durIdx = (ts.durIdx + 1) % len(ts.durHistory)
	if ts.durCount < len(ts.durHistory) {
		ts.durCount++
	}
}

func (ts *TrackStore) avgDuration() uint32 {
	if ts.durCount == 0 {
		return 0
	}
	var sum uint64
	for i := 0; i < ts.durCount; i++ {
		sum += uint64(ts.durHistory[i])
	}
	return uint32(sum / uint64(ts.durCount))
}

func (ts *TrackStore) trackName() string {
	if ts.isVideo {
		return "video"
	}
	return "audio"
}

func (ts *TrackStore) buildInitSegment() {
	init := mp4.CreateEmptyInit()
	if ts.isVideo {
		trak := init.AddEmptyTrack(ts.timescale, "video", "und")
		switch ts.videoCodec {
		case "h265":
			if err := trak.SetHEVCDescriptor("hvc1", ts.vps, ts.sps, ts.pps, nil, true); err != nil {
				log.Printf("[%s] SetHEVCDescriptor error: %v", ts.trackName(), err)
				return
			}
		case "h264":
			if len(ts.sps) > 0 && len(ts.pps) > 0 {
				if err := trak.SetAVCDescriptor("avc1", ts.sps, ts.pps, true); err != nil {
					log.Printf("[%s] SetAVCDescriptor error: %v", ts.trackName(), err)
					return
				}
			}
		case "av1":
			ts.initSeg = buildAV1Init(ts.trackID, ts.timescale, ts.av1SeqHdr)
			if ts.initSeg == nil {
				log.Printf("[%s] failed to build AV1 init segment", ts.trackName())
			} else {
				atomic.StoreInt32(&ts.initReady, 1)
			}
			return
		default:
			log.Printf("[%s] unsupported video codec for fMP4: %s", ts.trackName(), ts.videoCodec)
			return
		}
	} else {
		trak := init.AddEmptyTrack(ts.timescale, "audio", "und")
		if err := trak.SetAACDescriptor(ts.audioObjType, ts.audioFreq); err != nil {
			log.Printf("[%s] SetAACDescriptor error: %v", ts.trackName(), err)
			return
		}
	}
	var buf bytes.Buffer
	if err := init.Encode(&buf); err != nil {
		log.Printf("[%s] init encode error: %v", ts.trackName(), err)
		return
	}
	ts.initSeg = buf.Bytes()
	atomic.StoreInt32(&ts.initReady, 1)
}

func (ts *TrackStore) flushSegment() {
	if len(ts.pendingSamples) == 0 {
		return
	}

	if len(ts.pendingSamples) > 1 && ts.lastPTSNs >= 0 && ts.sharedBaseNs != nil {
		base := atomic.LoadInt64(ts.sharedBaseNs)
		if base >= 0 {
			targetTicks := uint64(ts.lastPTSNs-base) * uint64(ts.timescale) / 1e9
			if targetTicks > ts.decodeTime {
				var sumPrev uint64
				for i := 0; i < len(ts.pendingSamples)-1; i++ {
					sumPrev += uint64(ts.pendingSamples[i].Dur)
				}
				segStart := ts.pendingSamples[0].DecodeTime
				needed := targetTicks - segStart - sumPrev
				last := &ts.pendingSamples[len(ts.pendingSamples)-1]
				avg := ts.avgDuration()
				if avg == 0 {
					avg = last.Dur
				}
				if needed > 0 && needed < uint64(avg*3) {
					last.Dur = uint32(needed)
					ts.decodeTime = segStart + sumPrev + needed
				}
			}
		}
	}

	ts.seqNum++
	seqNum := ts.seqNum
	trackID := ts.trackID
	samples := ts.pendingSamples
	ts.pendingSamples = nil
	ts.pendingPTSNs = nil
	ts.pendingStart = time.Time{}
	name := ts.trackName()

	ts.mu.Unlock()

	seg := mp4.NewMediaSegment()
	frag, err := mp4.CreateFragment(seqNum, trackID)
	if err != nil {
		log.Printf("[%s] CreateFragment error: %v", name, err)
		ts.mu.Lock()
		return
	}
	seg.AddFragment(frag)

	for _, s := range samples {
		if err := frag.AddFullSampleToTrack(s, trackID); err != nil {
			log.Printf("[%s] AddFullSampleToTrack error: %v", name, err)
			ts.mu.Lock()
			return
		}
	}

	var buf bytes.Buffer
	if err := seg.Encode(&buf); err != nil {
		log.Printf("[%s] segment encode error: %v", name, err)
		ts.mu.Lock()
		return
	}

	startNs := int64(-1)
	if len(samples) > 0 && ts.sharedBaseNs != nil {
		base := atomic.LoadInt64(ts.sharedBaseNs)
		if base >= 0 {
			sampleTicks := samples[0].DecodeTime
			startNs = int64(sampleTicks) * 1e9 / int64(ts.timescale)
		}
	}

	ts.mu.Lock()
	ts.segments = append(ts.segments, segmentEntry{data: buf.Bytes(), startTimeNs: startNs})

	if ts.maxSegments > 0 && len(ts.segments) > ts.maxSegments {
		ts.segments = ts.segments[len(ts.segments)-ts.maxSegments:]
	}

	ts.cond.Broadcast()
}

func NewSharedBasePTS() *int64 {
	v := int64(-1)
	return &v
}

func (ts *TrackStore) SetSharedBase(base *int64) {
	ts.sharedBaseNs = base
}

func (ts *TrackStore) SetTargetHeight(h int) {
	ts.targetHeight = h
}

func (ts *TrackStore) SetPartner(p *TrackStore) {
	ts.partner = p
}

func (ts *TrackStore) FlushNow() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if len(ts.pendingSamples) > 0 {
		ts.flushSegment()
	}
}

func (ts *TrackStore) ptsToTicks(ptsNs int64) uint64 {
	if ts.sharedBaseNs != nil {
		base := atomic.LoadInt64(ts.sharedBaseNs)
		if base < 0 {
			atomic.CompareAndSwapInt64(ts.sharedBaseNs, -1, ptsNs)
			base = atomic.LoadInt64(ts.sharedBaseNs)
		}
		rel := ptsNs - base
		if rel < 0 {
			rel = 0
		}
		return uint64(rel) * uint64(ts.timescale) / 1e9
	}
	return uint64(ptsNs) * uint64(ts.timescale) / 1e9
}

func (ts *TrackStore) resolveDuration(ptsNs, bufDurNs int64) uint32 {
	if !ts.isVideo {
		if bufDurNs > 0 && bufDurNs < 50000000 {
			dur := uint32(bufDurNs * int64(ts.timescale) / 1e9)
			if dur > 0 && dur <= 2048 {
				ts.recordDuration(dur)
				return dur
			}
		}
		if avg := ts.avgDuration(); avg > 0 && avg <= 2048 {
			return avg
		}
		return 1024
	}
	if ptsNs >= 0 && ts.lastPTSNs >= 0 {
		diffNs := ptsNs - ts.lastPTSNs
		if diffNs > 0 && diffNs < 1e9 {
			dur := uint32(diffNs * int64(ts.timescale) / 1e9)
			ts.recordDuration(dur)
			return dur
		}
	}
	if bufDurNs > 0 && bufDurNs < 1e9 {
		dur := uint32(bufDurNs * int64(ts.timescale) / 1e9)
		if dur > 0 {
			ts.recordDuration(dur)
			return dur
		}
	}
	if avg := ts.avgDuration(); avg > 0 {
		return avg
	}
	return 3600
}

func (ts *TrackStore) PushVideoFrame(data []byte, ptsNs, bufDurNs int64, isKeyframe bool) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if ts.videoCodec == "av1" {
		seqHdr := extractAV1SequenceHeader(data)
		if seqHdr != nil && isKeyframe {
			if ts.initSeg == nil {
				if ts.av1SeqHdr == nil {
					ts.av1SeqHdr = seqHdr
				} else {
					ts.av1SeqHdr = seqHdr
					ts.buildInitSegment()
					ts.cond.Broadcast()
				}
			} else if !bytes.Equal(seqHdr, ts.av1SeqHdr) {
				ts.av1SeqHdr = seqHdr
				ts.initSeg = nil
				ts.buildInitSegment()
				ts.cond.Broadcast()
			}
		}
	} else if ts.initSeg == nil {
		switch ts.videoCodec {
		case "h265":
			vpsNALUs, spsNALUs, ppsNALUs := hevc.GetParameterSetsFromByteStream(data)
			if len(vpsNALUs) > 0 && len(spsNALUs) > 0 && len(ppsNALUs) > 0 {
				ts.vps = vpsNALUs
				ts.sps = spsNALUs
				ts.pps = ppsNALUs
				ts.buildInitSegment()
				ts.cond.Broadcast()
			}
		case "h264":
			spsNALUs, ppsNALUs := avc.GetParameterSetsFromByteStream(data)
			if len(spsNALUs) > 0 && len(ppsNALUs) > 0 {
				ts.sps = spsNALUs
				ts.pps = ppsNALUs
				ts.buildInitSegment()
				ts.cond.Broadcast()
			}
		}
	}
	if ts.initSeg == nil {
		return
	}

	var sampleData []byte
	if ts.videoCodec == "av1" {
		sampleData = stripAV1TemporalDelimiter(data)
	} else {
		sampleData = avc.ConvertByteStreamToNaluSample(data)
	}

	duration := ts.resolveDuration(ptsNs, bufDurNs)
	if ptsNs >= 0 {
		if ts.firstPTSNs < 0 {
			ts.firstPTSNs = ptsNs
		}
		ts.lastPTSNs = ptsNs
	}

	flags := mp4.NonSyncSampleFlags
	if isKeyframe {
		flags = mp4.SyncSampleFlags
	}

	sample := mp4.FullSample{
		Sample: mp4.Sample{
			Flags:                 flags,
			Dur:                   duration,
			Size:                  uint32(len(sampleData)),
			CompositionTimeOffset: 0,
		},
		DecodeTime: ts.decodeTime,
		Data:       sampleData,
	}
	ts.decodeTime += uint64(duration)
	ts.pendingSamples = append(ts.pendingSamples, sample)

	now := time.Now()
	if ts.pendingStart.IsZero() {
		ts.pendingStart = now
	}

	if isKeyframe && len(ts.pendingSamples) > 1 && now.Sub(ts.pendingStart) >= SegmentDuration {
		ts.flushSegment()
		if ts.partner != nil {
			ts.partner.FlushNow()
		}
	}
}

func (ts *TrackStore) PushAudioFrame(data []byte, ptsNs, bufDurNs int64) {
	if ts.partner != nil && !ts.partner.IsInitReady() {
		return
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()

	if ts.initSeg == nil {
		ts.audioObjType = aac.AAClc
		ts.audioFreq = int(ts.timescale)
		ts.buildInitSegment()
		ts.cond.Broadcast()
	}

	duration := ts.resolveDuration(ptsNs, bufDurNs)
	if ptsNs >= 0 {
		if ts.firstPTSNs < 0 {
			ts.firstPTSNs = ptsNs
		}
		ts.lastPTSNs = ptsNs
	}

	sample := mp4.FullSample{
		Sample: mp4.Sample{
			Flags:                 mp4.SyncSampleFlags,
			Dur:                   duration,
			Size:                  uint32(len(data)),
			CompositionTimeOffset: 0,
		},
		DecodeTime: ts.decodeTime,
		Data:       data,
	}
	ts.decodeTime += uint64(duration)
	ts.pendingSamples = append(ts.pendingSamples, sample)
	ts.pendingPTSNs = append(ts.pendingPTSNs, ptsNs)

	if !ts.isVideo {
		if ts.pendingStart.IsZero() {
			ts.pendingStart = time.Now()
		}
	}
}

func (ts *TrackStore) GetInit() ([]byte, int64) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	deadline := time.Now().Add(30 * time.Second)
	for ts.initSeg == nil && !ts.closed {
		if time.Now().After(deadline) {
			return nil, ts.gen
		}
		go func() {
			time.Sleep(500 * time.Millisecond)
			ts.cond.Broadcast()
		}()
		ts.cond.Wait()
	}
	return ts.initSeg, ts.gen
}

func (ts *TrackStore) GetSegment(gen int64, seq int) ([]byte, int64, bool) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if ts.closed || ts.gen != gen {
			return nil, 0, false
		}
		if seq < len(ts.segments) {
			e := ts.segments[seq]
			return e.data, e.startTimeNs, true
		}
		if time.Now().After(deadline) {
			return nil, 0, false
		}
		go func() {
			time.Sleep(500 * time.Millisecond)
			ts.cond.Broadcast()
		}()
		ts.cond.Wait()
	}
}

func IsHEVCKeyframe(data []byte) bool {
	for i := 0; i < len(data)-4; i++ {
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 0 && data[i+3] == 1 {
			naluType := (data[i+4] >> 1) & 0x3F
			if naluType == 19 || naluType == 20 {
				return true
			}
		}
	}
	return false
}

func IsH264Keyframe(data []byte) bool {
	for i := 0; i < len(data)-4; i++ {
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 0 && data[i+3] == 1 {
			naluType := data[i+4] & 0x1F
			if naluType == 5 {
				return true
			}
		}
	}
	return false
}

func RunSegmentFlusher(ctx context.Context, video, audio *TrackStore) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			video.mu.Lock()
			if len(video.pendingSamples) > 0 && !video.pendingStart.IsZero() && time.Since(video.pendingStart) >= SegmentDuration {
				video.flushSegment()
				if video.partner != nil {
					video.partner.FlushNow()
				}
			}
			video.mu.Unlock()
		}
	}
}

type SeekGen struct {
	val atomic.Int64
}

func (g *SeekGen) Add(n int64) int64 { return g.val.Add(n) }
func (g *SeekGen) Load() int64       { return g.val.Load() }
