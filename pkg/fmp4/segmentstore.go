package fmp4

import (
	"sync"
	"time"
)

type SegmentStore struct {
	mu       sync.Mutex
	cond     *sync.Cond
	closed   bool
	gen      int64
	initSeg  []byte
	segments []segmentEntry
	seqNum   int
	maxSegs  int
	offset   float64
}

func NewSegmentStore() *SegmentStore {
	ss := &SegmentStore{
		maxSegs: 60,
	}
	ss.cond = sync.NewCond(&ss.mu)
	return ss
}

func (ss *SegmentStore) SetInit(data []byte) {
	ss.mu.Lock()
	ss.initSeg = make([]byte, len(data))
	copy(ss.initSeg, data)
	ss.mu.Unlock()
	ss.cond.Broadcast()
}

func (ss *SegmentStore) AddSegment(data []byte, startTimeNs int64) {
	ss.mu.Lock()
	buf := make([]byte, len(data))
	copy(buf, data)
	ss.segments = append(ss.segments, segmentEntry{data: buf, startTimeNs: startTimeNs})
	if ss.maxSegs > 0 && len(ss.segments) > ss.maxSegs {
		ss.segments = ss.segments[len(ss.segments)-ss.maxSegs:]
	}
	ss.seqNum++
	ss.mu.Unlock()
	ss.cond.Broadcast()
}

func (ss *SegmentStore) GetInit() ([]byte, int64) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	deadline := time.Now().Add(30 * time.Second)
	for ss.initSeg == nil && !ss.closed {
		if time.Now().After(deadline) {
			return nil, ss.gen
		}
		go func() {
			time.Sleep(500 * time.Millisecond)
			ss.cond.Broadcast()
		}()
		ss.cond.Wait()
	}
	return ss.initSeg, ss.gen
}

func (ss *SegmentStore) GetSegment(gen int64, seq int) ([]byte, int64, bool) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if ss.closed || ss.gen != gen {
			return nil, 0, false
		}
		if seq < len(ss.segments) {
			e := ss.segments[seq]
			return e.data, e.startTimeNs, true
		}
		if time.Now().After(deadline) {
			return nil, 0, false
		}
		go func() {
			time.Sleep(500 * time.Millisecond)
			ss.cond.Broadcast()
		}()
		ss.cond.Wait()
	}
}

func (ss *SegmentStore) Reset(gen int64) {
	ss.mu.Lock()
	ss.gen = gen
	ss.segments = nil
	ss.initSeg = nil
	ss.seqNum = 0
	ss.mu.Unlock()
	ss.cond.Broadcast()
}

func (ss *SegmentStore) Close() {
	ss.mu.Lock()
	ss.closed = true
	ss.mu.Unlock()
	ss.cond.Broadcast()
}

func (ss *SegmentStore) Generation() int64 {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.gen
}

func (ss *SegmentStore) SegmentCount() int {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return len(ss.segments)
}

func (ss *SegmentStore) SetTimestampOffset(offset float64) {
	ss.mu.Lock()
	ss.offset = offset
	ss.mu.Unlock()
}

func (ss *SegmentStore) TimestampOffset() float64 {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.offset
}

func (ss *SegmentStore) IsAudioRejected() bool {
	return false
}

func (ss *SegmentStore) GetAudioCodecString() string {
	return "mp4a.40.2"
}

func (ss *SegmentStore) GetTimingDebug() TimingDebug {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	lastPTS := float64(0)
	if len(ss.segments) > 0 {
		lastPTS = float64(ss.segments[len(ss.segments)-1].startTimeNs) / 1e9
	}
	return TimingDebug{
		SegmentCount:  len(ss.segments),
		DecodeTimeSec: lastPTS,
	}
}

