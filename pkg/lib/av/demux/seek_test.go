package demux

import (
	"math"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestSeekTo_PreservesPTS(t *testing.T) {
	url := os.Getenv("AVMUX_TEST_STREAM")
	if url == "" {
		url = "http://192.168.1.149:8090/stream/37969f9dbb17d735"
	}

	dm, err := NewDemuxer(url, DemuxOpts{
		TimeoutSec:      10,
		AudioTrack:      -1,
		ProbeSize:       2000000,
		AnalyzeDuration: 2000000,
	})
	if err != nil {
		t.Skipf("cannot open: %v", err)
	}
	defer dm.Close()

	// Read a few packets to establish basePTS
	for i := 0; i < 5; i++ {
		dm.ReadPacket()
	}

	// Seek to 60s
	if err := dm.SeekTo(60000); err != nil {
		t.Fatalf("SeekTo(60s): %v", err)
	}

	pkt, err := dm.ReadPacket()
	if err != nil {
		t.Fatalf("read after seek: %v", err)
	}

	t.Logf("post-seek PTS: %d ns (%.2fs)", pkt.PTS, float64(pkt.PTS)/1e9)

	// PTS should be near 60s (movie time), not near 0
	if pkt.PTS < 50_000_000_000 {
		t.Errorf("expected PTS near 60s, got %.2fs — PTS was rebased to 0", float64(pkt.PTS)/1e9)
	}
}

func TestSeekTo_SegmentProduction(t *testing.T) {
	url := os.Getenv("AVMUX_TEST_STREAM")
	if url == "" {
		url = "http://192.168.1.149:8090/stream/37969f9dbb17d735"
	}

	dm, err := NewDemuxer(url, DemuxOpts{
		TimeoutSec:      10,
		AudioTrack:      -1,
		ProbeSize:       2000000,
		AnalyzeDuration: 2000000,
	})
	if err != nil {
		t.Skipf("cannot open: %v", err)
	}
	defer dm.Close()

	for i := 0; i < 5; i++ {
		dm.ReadPacket()
	}

	if err := dm.SeekTo(120000); err != nil {
		t.Fatalf("SeekTo(120s): %v", err)
	}

	vpkts := 0
	keyframes := 0
	for i := 0; i < 200; i++ {
		pkt, err := dm.ReadPacket()
		if err != nil {
			t.Logf("read %d: %v", i, err)
			break
		}
		if pkt.Type == Video {
			vpkts++
			if pkt.Keyframe {
				keyframes++
			}
			if vpkts <= 3 {
				t.Logf("vpkt %d: pts=%d dts=%d kf=%v len=%d", vpkts, pkt.PTS, pkt.DTS, pkt.Keyframe, len(pkt.Data))
			}
		}
	}

	t.Logf("after seek to 120s: %d video packets, %d keyframes", vpkts, keyframes)

	if vpkts == 0 {
		t.Error("no video packets after seek")
	}
	if keyframes == 0 {
		t.Error("no keyframes after seek")
	}
}

func TestSeekTo_FreshDemuxerSimulatesRestart(t *testing.T) {
	url := os.Getenv("AVMUX_TEST_STREAM")
	if url == "" {
		url = "http://192.168.1.149:8090/stream/37969f9dbb17d735"
	}

	dm, err := NewDemuxer(url, DemuxOpts{
		TimeoutSec:      10,
		AudioTrack:      -1,
		ProbeSize:       2000000,
		AnalyzeDuration: 2000000,
	})
	if err != nil {
		t.Skipf("cannot open: %v", err)
	}
	defer dm.Close()

	for i := 0; i < 20; i++ {
		if _, err := dm.ReadPacket(); err != nil {
			t.Fatalf("pre-seek read %d: %v", i, err)
		}
	}

	dm.Close()

	dm2, err := NewDemuxer(url, DemuxOpts{
		TimeoutSec:      10,
		AudioTrack:      -1,
		ProbeSize:       2000000,
		AnalyzeDuration: 2000000,
	})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer dm2.Close()

	if err := dm2.SeekTo(60000); err != nil {
		t.Fatalf("SeekTo: %v", err)
	}

	pkt, err := dm2.ReadPacket()
	if err != nil {
		t.Fatalf("post-seek read: %v", err)
	}

	t.Logf("fresh demuxer post-seek PTS: %d ns (%.2fs)", pkt.PTS, float64(pkt.PTS)/1e9)

	if pkt.PTS < 50_000_000_000 {
		t.Errorf("expected PTS near 60s (movie time), got %.2fs", float64(pkt.PTS)/1e9)
	}
}

func TestSeekTo_AVAlignment(t *testing.T) {
	url := os.Getenv("AVMUX_TEST_STREAM")
	if url == "" {
		url = "http://192.168.1.149:8090/stream/37969f9dbb17d735"
	}

	dm, err := NewDemuxer(url, DemuxOpts{
		TimeoutSec:      10,
		AudioTrack:      -1,
		ProbeSize:       2000000,
		AnalyzeDuration: 2000000,
	})
	if err != nil {
		t.Skipf("cannot open: %v", err)
	}
	defer dm.Close()

	for i := 0; i < 10; i++ {
		dm.ReadPacket()
	}

	if err := dm.SeekTo(60000); err != nil {
		t.Fatalf("SeekTo(60s): %v", err)
	}

	var firstVideoPTS, firstAudioPTS int64
	foundVideo, foundAudio := false, false

	for i := 0; i < 100; i++ {
		pkt, err := dm.ReadPacket()
		if err != nil {
			break
		}
		if pkt.Type == Video && !foundVideo {
			firstVideoPTS = pkt.PTS
			foundVideo = true
			t.Logf("first video PTS after seek: %.3fs", float64(pkt.PTS)/1e9)
		}
		if pkt.Type == Audio && !foundAudio {
			firstAudioPTS = pkt.PTS
			foundAudio = true
			t.Logf("first audio PTS after seek: %.3fs", float64(pkt.PTS)/1e9)
		}
		if foundVideo && foundAudio {
			break
		}
	}

	if !foundVideo || !foundAudio {
		t.Skipf("could not find both tracks after seek (video=%v audio=%v)", foundVideo, foundAudio)
	}

	gapSec := math.Abs(float64(firstVideoPTS-firstAudioPTS)) / 1e9
	t.Logf("A/V gap after seek: %.3fs", gapSec)

	if gapSec > 5.0 {
		t.Errorf("A/V gap too large after seek: %.3fs (video=%.3fs audio=%.3fs)",
			gapSec, float64(firstVideoPTS)/1e9, float64(firstAudioPTS)/1e9)
	}
}

func TestSeekTo_AudioPTSResetOnSeek(t *testing.T) {
	url := os.Getenv("AVMUX_TEST_STREAM")
	if url == "" {
		url = "http://192.168.1.149:8090/stream/37969f9dbb17d735"
	}

	dm, err := NewDemuxer(url, DemuxOpts{
		TimeoutSec:      10,
		AudioTrack:      -1,
		ProbeSize:       2000000,
		AnalyzeDuration: 2000000,
	})
	if err != nil {
		t.Skipf("cannot open: %v", err)
	}
	defer dm.Close()

	for i := 0; i < 100; i++ {
		dm.ReadPacket()
	}

	if err := dm.SeekTo(60000); err != nil {
		t.Fatalf("SeekTo(60s): %v", err)
	}

	var firstVideoPTS, firstAudioPTS int64
	foundV, foundA := false, false
	for i := 0; i < 100; i++ {
		pkt, err := dm.ReadPacket()
		if err != nil {
			break
		}
		if pkt.Type == Video && !foundV {
			firstVideoPTS = pkt.PTS
			foundV = true
		}
		if pkt.Type == Audio && !foundA {
			firstAudioPTS = pkt.PTS
			foundA = true
		}
		if foundV && foundA {
			break
		}
	}

	if !foundV || !foundA {
		t.Skipf("missing tracks after seek (video=%v audio=%v)", foundV, foundA)
	}

	t.Logf("post-seek video PTS: %.3fs, audio PTS: %.3fs", float64(firstVideoPTS)/1e9, float64(firstAudioPTS)/1e9)

	seekTargetNs := int64(60_000_000_000)
	if firstAudioPTS < seekTargetNs-15_000_000_000 {
		t.Errorf("audio PTS (%.3fs) too far from seek target (60s) — audioFrameCount may not have reset",
			float64(firstAudioPTS)/1e9)
	}

	gap := math.Abs(float64(firstVideoPTS-firstAudioPTS)) / 1e9
	if gap > 5.0 {
		t.Errorf("A/V gap %.3fs too large after seek", gap)
	}
}

func TestRequestSeek_OnSeekBeforeReturn(t *testing.T) {
	url := os.Getenv("AVMUX_TEST_STREAM")
	if url == "" {
		url = "http://192.168.1.149:8090/stream/37969f9dbb17d735"
	}

	dm, err := NewDemuxer(url, DemuxOpts{
		TimeoutSec:      10,
		AudioTrack:      -1,
		ProbeSize:       2000000,
		AnalyzeDuration: 2000000,
	})
	if err != nil {
		t.Skipf("cannot open: %v", err)
	}

	var onSeekDone atomic.Int32
	dm.SetOnSeek(func() {
		onSeekDone.Add(1)
	})

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := dm.ReadPacket(); err != nil {
				return
			}
		}
	}()

	time.Sleep(50 * time.Millisecond)

	if err := dm.RequestSeek(60000); err != nil {
		close(stop)
		<-done
		dm.Close()
		t.Fatalf("RequestSeek: %v", err)
	}

	if onSeekDone.Load() != 1 {
		t.Errorf("onSeek not called before RequestSeek returned (count=%d)", onSeekDone.Load())
	}

	close(stop)
	<-done
	dm.Close()
}

func TestSeekTo_MultipleSeeksAVAlignment(t *testing.T) {
	url := os.Getenv("AVMUX_TEST_STREAM")
	if url == "" {
		url = "http://192.168.1.149:8090/stream/37969f9dbb17d735"
	}

	dm, err := NewDemuxer(url, DemuxOpts{
		TimeoutSec:      10,
		AudioTrack:      -1,
		ProbeSize:       2000000,
		AnalyzeDuration: 2000000,
	})
	if err != nil {
		t.Skipf("cannot open: %v", err)
	}
	defer dm.Close()

	for i := 0; i < 20; i++ {
		dm.ReadPacket()
	}

	positions := []int64{300000, 60000, 600000, 10000, 120000}
	for _, posMs := range positions {
		if err := dm.SeekTo(posMs); err != nil {
			t.Fatalf("SeekTo(%dms): %v", posMs, err)
		}

		var firstV, firstA int64
		fv, fa := false, false
		for i := 0; i < 100; i++ {
			pkt, err := dm.ReadPacket()
			if err != nil {
				break
			}
			if pkt.Type == Video && !fv {
				firstV = pkt.PTS
				fv = true
			}
			if pkt.Type == Audio && !fa {
				firstA = pkt.PTS
				fa = true
			}
			if fv && fa {
				break
			}
		}

		if !fv || !fa {
			t.Skipf("seek to %dms: missing tracks", posMs)
		}

		gap := math.Abs(float64(firstV-firstA)) / 1e9
		t.Logf("seek to %ds: video=%.3fs audio=%.3fs gap=%.3fs",
			posMs/1000, float64(firstV)/1e9, float64(firstA)/1e9, gap)

		if gap > 5.0 {
			t.Errorf("seek to %dms: A/V gap %.3fs too large", posMs, gap)
		}
	}
}
