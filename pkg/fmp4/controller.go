package fmp4

import (
	"bytes"
	"context"
	"log"
	"sync"
	"time"

	"github.com/Eyevinn/mp4ff/aac"
	"github.com/Eyevinn/mp4ff/avc"
	"github.com/Eyevinn/mp4ff/hevc"
	"github.com/Eyevinn/mp4ff/mp4"
)

const SegDuration = 2 * time.Second

type AlignedSegment struct {
	Video []byte
	Audio []byte
}

type SegmentController struct {
	mu   sync.Mutex
	cond *sync.Cond

	segments    []AlignedSegment
	seqNum      uint32
	maxSegments int
	gen         int64
	closed      bool

	pendingVideo []mp4.FullSample
	pendingAudio []mp4.FullSample
	pendingStart time.Time

	videoInit []byte
	audioInit []byte

	videoCodec    string
	vps, sps, pps [][]byte
	av1SeqHdr     []byte

	audioObjType byte
	audioFreq    int

	videoDecodeTime uint64
	audioDecodeTime uint64
	videoTimescale  uint32
	audioTimescale  uint32
	videoTrackID    uint32
	audioTrackID    uint32

	videoLastPTSNs  int64
	audioLastPTSNs  int64
	videoFirstPTSNs int64
	audioFirstPTSNs int64
	sharedBaseNs    int64

	videoDurHist  [10]uint32
	audioDurHist  [10]uint32
	videoDurIdx   int
	videoDurCount int
	audioDurIdx   int
	audioDurCount int
}

func NewSegmentController(videoCodec string) *SegmentController {
	sc := &SegmentController{
		videoCodec:      videoCodec,
		videoTimescale:  90000,
		audioTimescale:  48000,
		videoTrackID:    1,
		audioTrackID:    1,
		maxSegments:     60,
		videoLastPTSNs:  -1,
		audioLastPTSNs:  -1,
		videoFirstPTSNs: -1,
		audioFirstPTSNs: -1,
		sharedBaseNs:    -1,
	}
	sc.cond = sync.NewCond(&sc.mu)
	return sc
}

func (sc *SegmentController) recordVideoDur(d uint32) {
	sc.videoDurHist[sc.videoDurIdx] = d
	sc.videoDurIdx = (sc.videoDurIdx + 1) % len(sc.videoDurHist)
	if sc.videoDurCount < len(sc.videoDurHist) {
		sc.videoDurCount++
	}
}

func (sc *SegmentController) avgVideoDur() uint32 {
	if sc.videoDurCount == 0 {
		return 0
	}
	var sum uint64
	for i := 0; i < sc.videoDurCount; i++ {
		sum += uint64(sc.videoDurHist[i])
	}
	return uint32(sum / uint64(sc.videoDurCount))
}

func (sc *SegmentController) recordAudioDur(d uint32) {
	sc.audioDurHist[sc.audioDurIdx] = d
	sc.audioDurIdx = (sc.audioDurIdx + 1) % len(sc.audioDurHist)
	if sc.audioDurCount < len(sc.audioDurHist) {
		sc.audioDurCount++
	}
}

func (sc *SegmentController) avgAudioDur() uint32 {
	if sc.audioDurCount == 0 {
		return 0
	}
	var sum uint64
	for i := 0; i < sc.audioDurCount; i++ {
		sum += uint64(sc.audioDurHist[i])
	}
	return uint32(sum / uint64(sc.audioDurCount))
}

func (sc *SegmentController) resolveVideoDur(ptsNs, bufDurNs int64) uint32 {
	if ptsNs >= 0 && sc.videoLastPTSNs >= 0 {
		diffNs := ptsNs - sc.videoLastPTSNs
		if diffNs > 0 && diffNs < 1e9 {
			dur := uint32(diffNs * int64(sc.videoTimescale) / 1e9)
			sc.recordVideoDur(dur)
			return dur
		}
	}
	if bufDurNs > 0 && bufDurNs < 1e9 {
		dur := uint32(bufDurNs * int64(sc.videoTimescale) / 1e9)
		if dur > 0 {
			sc.recordVideoDur(dur)
			return dur
		}
	}
	if avg := sc.avgVideoDur(); avg > 0 {
		return avg
	}
	return 3600
}

func (sc *SegmentController) resolveAudioDur(ptsNs, bufDurNs int64) uint32 {
	if bufDurNs > 0 && bufDurNs < 1e9 {
		dur := uint32(bufDurNs * int64(sc.audioTimescale) / 1e9)
		if dur > 0 {
			return dur
		}
	}
	return 1024
}

func (sc *SegmentController) buildVideoInit() {
	init := mp4.CreateEmptyInit()
	trak := init.AddEmptyTrack(sc.videoTimescale, "video", "und")
	stsd := trak.Mdia.Minf.Stbl.Stsd

	switch sc.videoCodec {
	case "h265":
		if err := trak.SetHEVCDescriptor("hvc1", sc.vps, sc.sps, sc.pps, nil, true); err != nil {
			log.Printf("[video] SetHEVCDescriptor error: %v", err)
			return
		}
	case "h264":
		if len(sc.sps) > 0 && len(sc.pps) > 0 {
			if err := trak.SetAVCDescriptor("avc1", sc.sps, sc.pps, true); err != nil {
				log.Printf("[video] SetAVCDescriptor error: %v", err)
				return
			}
		}
	case "av1":
		initBytes := buildAV1Init(sc.videoTrackID, sc.videoTimescale, sc.av1SeqHdr)
		if initBytes != nil {
			sc.videoInit = initBytes
		}
		return
	default:
		log.Printf("[video] unsupported codec: %s", sc.videoCodec)
		return
	}
	_ = stsd

	var buf bytes.Buffer
	if err := init.Encode(&buf); err != nil {
		log.Printf("[video] init encode error: %v", err)
		return
	}
	sc.videoInit = buf.Bytes()
}

func (sc *SegmentController) buildAudioInit() {
	init := mp4.CreateEmptyInit()
	trak := init.AddEmptyTrack(sc.audioTimescale, "audio", "und")
	if err := trak.SetAACDescriptor(sc.audioObjType, int(sc.audioTimescale)); err != nil {
		log.Printf("[audio] SetAACDescriptor error: %v", err)
		return
	}
	var buf bytes.Buffer
	if err := init.Encode(&buf); err != nil {
		log.Printf("[audio] init encode error: %v", err)
		return
	}
	sc.audioInit = buf.Bytes()
}

func (sc *SegmentController) PushVideoFrame(data []byte, ptsNs, bufDurNs int64, isKeyframe bool) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if sc.videoInit == nil {
		switch sc.videoCodec {
		case "h265":
			vpsNALUs, spsNALUs, ppsNALUs := hevc.GetParameterSetsFromByteStream(data)
			if len(vpsNALUs) > 0 && len(spsNALUs) > 0 && len(ppsNALUs) > 0 {
				sc.vps = vpsNALUs
				sc.sps = spsNALUs
				sc.pps = ppsNALUs
				sc.buildVideoInit()
				sc.cond.Broadcast()
			}
		case "h264":
			spsNALUs, ppsNALUs := avc.GetParameterSetsFromByteStream(data)
			if len(spsNALUs) > 0 && len(ppsNALUs) > 0 {
				sc.sps = spsNALUs
				sc.pps = ppsNALUs
				sc.buildVideoInit()
				sc.cond.Broadcast()
			}
		case "av1":
			seqHdr := extractAV1SequenceHeader(data)
			if seqHdr != nil {
				sc.av1SeqHdr = seqHdr
				sc.buildVideoInit()
				sc.cond.Broadcast()
			}
		}
	}
	if sc.videoInit == nil {
		return
	}

	var sampleData []byte
	if sc.videoCodec == "av1" {
		sampleData = stripAV1TemporalDelimiter(data)
	} else {
		sampleData = avc.ConvertByteStreamToNaluSample(data)
	}

	duration := sc.resolveVideoDur(ptsNs, bufDurNs)

	if ptsNs >= 0 {
		if sc.videoFirstPTSNs < 0 {
			sc.videoFirstPTSNs = ptsNs
		}
		if sc.sharedBaseNs < 0 {
			sc.sharedBaseNs = ptsNs
		}
		sc.videoLastPTSNs = ptsNs
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
		DecodeTime: sc.videoDecodeTime,
		Data:       sampleData,
	}
	sc.videoDecodeTime += uint64(duration)
	sc.pendingVideo = append(sc.pendingVideo, sample)

	now := time.Now()
	if sc.pendingStart.IsZero() {
		sc.pendingStart = now
	}

	if isKeyframe && len(sc.pendingVideo) > 1 && now.Sub(sc.pendingStart) >= SegDuration {
		sc.flushAligned()
	}
}

func (sc *SegmentController) PushAudioFrame(data []byte, ptsNs, bufDurNs int64) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if sc.audioInit == nil {
		sc.audioObjType = aac.AAClc
		sc.audioFreq = int(sc.audioTimescale)
		sc.buildAudioInit()
		sc.cond.Broadcast()
	}

	duration := sc.resolveAudioDur(ptsNs, bufDurNs)

	if ptsNs >= 0 {
		if sc.audioFirstPTSNs < 0 {
			sc.audioFirstPTSNs = ptsNs
		}
		if sc.sharedBaseNs < 0 {
			sc.sharedBaseNs = ptsNs
		}
		sc.audioLastPTSNs = ptsNs
	}

	sample := mp4.FullSample{
		Sample: mp4.Sample{
			Flags:                 mp4.SyncSampleFlags,
			Dur:                   duration,
			Size:                  uint32(len(data)),
			CompositionTimeOffset: 0,
		},
		DecodeTime: sc.audioDecodeTime,
		Data:       data,
	}
	sc.audioDecodeTime += uint64(duration)
	sc.pendingAudio = append(sc.pendingAudio, sample)
}

func (sc *SegmentController) flushAligned() {
	if len(sc.pendingVideo) == 0 {
		return
	}

	sc.seqNum++
	seqNum := sc.seqNum
	videoSamples := sc.pendingVideo
	audioSamples := sc.pendingAudio
	sc.pendingVideo = nil
	sc.pendingAudio = nil
	sc.pendingStart = time.Time{}

	sc.mu.Unlock()

	var videoBytes, audioBytes []byte

	if len(videoSamples) > 0 {
		seg := mp4.NewMediaSegment()
		frag, err := mp4.CreateFragment(seqNum, sc.videoTrackID)
		if err == nil {
			seg.AddFragment(frag)
			for _, s := range videoSamples {
				frag.AddFullSampleToTrack(s, sc.videoTrackID)
			}
			var buf bytes.Buffer
			if err := seg.Encode(&buf); err == nil {
				videoBytes = buf.Bytes()
			}
		}
	}

	if len(audioSamples) > 0 {
		seg := mp4.NewMediaSegment()
		frag, err := mp4.CreateFragment(seqNum, sc.audioTrackID)
		if err == nil {
			seg.AddFragment(frag)
			for _, s := range audioSamples {
				frag.AddFullSampleToTrack(s, sc.audioTrackID)
			}
			var buf bytes.Buffer
			if err := seg.Encode(&buf); err == nil {
				audioBytes = buf.Bytes()
			}
		}
	}

	sc.mu.Lock()
	sc.segments = append(sc.segments, AlignedSegment{Video: videoBytes, Audio: audioBytes})
	if sc.maxSegments > 0 && len(sc.segments) > sc.maxSegments {
		sc.segments = sc.segments[len(sc.segments)-sc.maxSegments:]
	}
	sc.cond.Broadcast()
}

func (sc *SegmentController) GetVideoInit() ([]byte, int64) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	deadline := time.Now().Add(30 * time.Second)
	for sc.videoInit == nil && !sc.closed {
		if time.Now().After(deadline) {
			return nil, sc.gen
		}
		go func() {
			time.Sleep(500 * time.Millisecond)
			sc.cond.Broadcast()
		}()
		sc.cond.Wait()
	}
	return sc.videoInit, sc.gen
}

func (sc *SegmentController) GetAudioInit() ([]byte, int64) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	deadline := time.Now().Add(30 * time.Second)
	for sc.audioInit == nil && !sc.closed {
		if time.Now().After(deadline) {
			return nil, sc.gen
		}
		go func() {
			time.Sleep(500 * time.Millisecond)
			sc.cond.Broadcast()
		}()
		sc.cond.Wait()
	}
	return sc.audioInit, sc.gen
}

func (sc *SegmentController) GetSegment(gen int64, seq int) (*AlignedSegment, bool) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if sc.closed || sc.gen != gen {
			return nil, false
		}
		if seq < len(sc.segments) {
			seg := sc.segments[seq]
			return &seg, true
		}
		if time.Now().After(deadline) {
			return nil, false
		}
		go func() {
			time.Sleep(500 * time.Millisecond)
			sc.cond.Broadcast()
		}()
		sc.cond.Wait()
	}
}

func (sc *SegmentController) VideoTimestampOffset() float64 {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.videoFirstPTSNs < 0 || sc.sharedBaseNs < 0 {
		return 0
	}
	off := float64(sc.videoFirstPTSNs-sc.sharedBaseNs) / 1e9
	if off < -5 || off > 30 {
		return 0
	}
	return off
}

func (sc *SegmentController) AudioTimestampOffset() float64 {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.audioFirstPTSNs < 0 || sc.sharedBaseNs < 0 {
		return 0
	}
	off := float64(sc.audioFirstPTSNs-sc.sharedBaseNs) / 1e9
	if off < -5 || off > 30 {
		return 0
	}
	return off
}

func (sc *SegmentController) SegmentCount() int {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return len(sc.segments)
}

func (sc *SegmentController) Generation() int64 {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.gen
}

func (sc *SegmentController) IsClosed() bool {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.closed
}

func (sc *SegmentController) Reset(gen int64) {
	sc.mu.Lock()
	sc.gen = gen
	sc.segments = nil
	sc.pendingVideo = nil
	sc.pendingAudio = nil
	sc.pendingStart = time.Time{}
	sc.seqNum = 0
	sc.videoDecodeTime = 0
	sc.audioDecodeTime = 0
	sc.videoLastPTSNs = -1
	sc.audioLastPTSNs = -1
	sc.videoFirstPTSNs = -1
	sc.audioFirstPTSNs = -1
	sc.sharedBaseNs = -1
	sc.mu.Unlock()
	sc.cond.Broadcast()
}

func (sc *SegmentController) Close() {
	sc.mu.Lock()
	sc.closed = true
	sc.mu.Unlock()
	sc.cond.Broadcast()
}

func (sc *SegmentController) RunFlusher(ctx context.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sc.mu.Lock()
			if len(sc.pendingVideo) > 0 && !sc.pendingStart.IsZero() && time.Since(sc.pendingStart) >= SegDuration {
				sc.flushAligned()
			}
			sc.mu.Unlock()
		}
	}
}
