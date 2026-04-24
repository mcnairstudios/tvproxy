package demux

import (
	"os"
	"testing"
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

	if err := dm.SeekTo(120000); err != nil {
		t.Fatalf("SeekTo(120s): %v", err)
	}

	dm.basePTS = -1
	dm.audioPTSInited = false
	dm.audioFrameCount = 0

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
