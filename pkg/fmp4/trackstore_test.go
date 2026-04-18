package fmp4

import (
	"testing"
	"time"
)

func TestTrackStore_GetInit_Timeout(t *testing.T) {
	ts := NewTrackStore(true, "h265")
	go func() {
		time.Sleep(100 * time.Millisecond)
		ts.Close()
	}()
	data, _ := ts.GetInit()
	if data != nil {
		t.Error("expected nil from closed store")
	}
}

func TestTrackStore_GetSegment_Timeout(t *testing.T) {
	ts := NewTrackStore(true, "h265")
	go func() {
		time.Sleep(100 * time.Millisecond)
		ts.Close()
	}()
	data, _, ok := ts.GetSegment(0, 0)
	if ok || data != nil {
		t.Error("expected nil/false from closed store")
	}
}

func TestTrackStore_GetInit_Deadline(t *testing.T) {
	ts := NewTrackStore(true, "h265")
	start := time.Now()
	data, _ := ts.GetInit()
	elapsed := time.Since(start)
	if data != nil {
		t.Error("expected nil from timed out store")
	}
	if elapsed > 35*time.Second {
		t.Errorf("GetInit blocked too long: %v", elapsed)
	}
}

func TestTrackStore_SharedBasePTS(t *testing.T) {
	video := NewTrackStore(true, "h265")
	audio := NewTrackStore(false, "")
	audio.SetAudioRate(48000)
	base := NewSharedBasePTS()
	video.SetSharedBase(base)
	audio.SetSharedBase(base)

	videoTicks := video.ptsToTicks(2000000000)
	audioTicks := audio.ptsToTicks(2000000000)

	if videoTicks != 0 {
		t.Errorf("first video PTS should be base, got ticks=%d", videoTicks)
	}

	videoTicks2 := video.ptsToTicks(2040000000)
	if videoTicks2 == 0 {
		t.Error("second video PTS should be > 0")
	}

	audioTicks2 := audio.ptsToTicks(2020000000)
	if audioTicks2 == 0 {
		t.Error("audio PTS after shared base should be > 0")
	}
	_ = audioTicks
}

func TestTrackStore_GetAudioCodecString(t *testing.T) {
	ts := NewTrackStore(false, "")
	if got := ts.GetAudioCodecString(); got != "mp4a.40.2" {
		t.Errorf("default audio codec = %q, want mp4a.40.2", got)
	}

	ts.audioObjType = 2
	if got := ts.GetAudioCodecString(); got != "mp4a.40.2" {
		t.Errorf("AAC-LC = %q, want mp4a.40.2", got)
	}

	ts.audioObjType = 5
	if got := ts.GetAudioCodecString(); got != "mp4a.40.5" {
		t.Errorf("HE-AAC = %q, want mp4a.40.5", got)
	}

	ts.audioObjType = 29
	if got := ts.GetAudioCodecString(); got != "mp4a.40.29" {
		t.Errorf("HE-AACv2 = %q, want mp4a.40.29", got)
	}
}

func TestTrackStore_RollingAverage(t *testing.T) {
	ts := NewTrackStore(true, "h265")
	for i := 0; i < 5; i++ {
		ts.recordDuration(3600)
	}
	avg := ts.avgDuration()
	if avg != 3600 {
		t.Errorf("expected avg 3600, got %d", avg)
	}

	ts.recordDuration(3700)
	avg = ts.avgDuration()
	if avg < 3600 || avg > 3700 {
		t.Errorf("expected avg between 3600-3700, got %d", avg)
	}
}

func TestTrackStore_ResolveDuration(t *testing.T) {
	ts := NewTrackStore(true, "h265")

	dur := ts.resolveDuration(-1, -1)
	if dur != 3600 {
		t.Errorf("expected fallback 3600, got %d", dur)
	}

	dur = ts.resolveDuration(-1, 40000000)
	expected := uint32(40000000 * 90000 / 1e9)
	if dur != expected {
		t.Errorf("expected %d from bufDur, got %d", expected, dur)
	}

	ts.lastPTSNs = 1000000000
	dur = ts.resolveDuration(1040000000, -1)
	expected = uint32(40000000 * 90000 / 1e9)
	if dur != expected {
		t.Errorf("expected %d from PTS diff, got %d", expected, dur)
	}
}
