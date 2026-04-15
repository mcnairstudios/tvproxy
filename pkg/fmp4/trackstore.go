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

type TrackStore struct {
	mu   sync.Mutex
	cond *sync.Cond
	gen  int64

	initSeg  []byte
	segments [][]byte

	pendingSamples []mp4.FullSample
	pendingStart   time.Time
	seqNum         uint32
	decodeTime     uint64
	trackID        uint32
	timescale      uint32

	vps [][]byte
	sps [][]byte
	pps [][]byte

	audioObjType byte
	audioFreq    int

	isVideo    bool
	videoCodec string // "h264", "h265", "av1"

	LastPTS    int64
	maxSegments int
}

func NewTrackStore(isVideo bool, videoCodec string) *TrackStore {
	ts := &TrackStore{
		isVideo:     isVideo,
		videoCodec:  videoCodec,
		trackID:     1,
		timescale:   90000,
		LastPTS:     -1,
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
	ts.gen = gen
	ts.segments = nil
	ts.pendingSamples = nil
	ts.pendingStart = time.Time{}
	ts.seqNum = 0
	ts.decodeTime = uint64(seekPosNs) * uint64(ts.timescale) / 1e9
	ts.LastPTS = -1
	ts.mu.Unlock()
	ts.cond.Broadcast()
}

func (ts *TrackStore) Close() {
	ts.cond.Broadcast()
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
		default:
			log.Printf("[%s] unsupported video codec for fMP4: %s (only h264/h265 supported)", ts.trackName(), ts.videoCodec)
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
}

func (ts *TrackStore) flushSegment() {
	if len(ts.pendingSamples) == 0 {
		return
	}

	ts.seqNum++
	seqNum := ts.seqNum
	trackID := ts.trackID
	samples := ts.pendingSamples
	ts.pendingSamples = nil
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

	ts.mu.Lock()
	ts.segments = append(ts.segments, buf.Bytes())

	if ts.maxSegments > 0 && len(ts.segments) > ts.maxSegments {
		ts.segments = ts.segments[len(ts.segments)-ts.maxSegments:]
	}

	ts.cond.Broadcast()
}

func (ts *TrackStore) PushVideoFrame(data []byte, duration uint32, isKeyframe bool) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if ts.initSeg == nil {
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

	sampleData := avc.ConvertByteStreamToNaluSample(data)

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
	}
}

func (ts *TrackStore) PushAudioFrame(data []byte, duration uint32) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if ts.initSeg == nil {
		ts.audioObjType = aac.AAClc
		ts.audioFreq = int(ts.timescale)
		ts.buildInitSegment()
		ts.cond.Broadcast()
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

	now := time.Now()
	if ts.pendingStart.IsZero() {
		ts.pendingStart = now
	}

	if now.Sub(ts.pendingStart) >= SegmentDuration {
		ts.flushSegment()
	}
}

func (ts *TrackStore) GetInit() ([]byte, int64) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	for ts.initSeg == nil {
		ts.cond.Wait()
	}
	return ts.initSeg, ts.gen
}

func (ts *TrackStore) GetSegment(gen int64, seq int) ([]byte, bool) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	for {
		if ts.gen != gen {
			return nil, false
		}
		if seq < len(ts.segments) {
			return ts.segments[seq], true
		}
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
			for _, store := range []*TrackStore{video, audio} {
				store.mu.Lock()
				if len(store.pendingSamples) > 0 && !store.pendingStart.IsZero() && time.Since(store.pendingStart) >= SegmentDuration {
					store.flushSegment()
				}
				store.mu.Unlock()
			}
		}
	}
}

type SeekGen struct {
	val atomic.Int64
}

func (g *SeekGen) Add(n int64) int64 { return g.val.Add(n) }
func (g *SeekGen) Load() int64       { return g.val.Load() }
