package mux_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/gavinmcnair/tvproxy/pkg/lib/av/conv"
	"github.com/gavinmcnair/tvproxy/pkg/lib/av/demux"
	"github.com/gavinmcnair/tvproxy/pkg/lib/av/mux"
)

func TestMSECopyPipeline_RealStream(t *testing.T) {
	streamURL := os.Getenv("AVMUX_TEST_STREAM")
	if streamURL == "" {
		streamURL = "http://192.168.1.149:8090/stream/37969f9dbb17d735"
	}

	dm, err := demux.NewDemuxer(streamURL, demux.DemuxOpts{
		TimeoutSec:      10,
		AudioTrack:      -1,
		ProbeSize:       2000000,
		AnalyzeDuration: 2000000,
	})
	if err != nil {
		t.Skipf("cannot open stream: %v", err)
	}
	defer dm.Close()

	info := dm.StreamInfo()
	if info == nil || info.Video == nil {
		t.Fatal("no video")
	}
	t.Logf("video: %s %dx%d extradata=%d", info.Video.Codec, info.Video.Width, info.Video.Height, len(info.Video.Extradata))

	vcp := dm.VideoCodecParameters()
	if vcp == nil {
		t.Fatal("no video codec params")
	}

	videoCodecID, err := conv.CodecIDFromString(info.Video.Codec)
	if err != nil {
		t.Fatalf("codec ID: %v", err)
	}

	var videoExtradata []byte
	if ed := vcp.ExtraData(); len(ed) > 0 {
		videoExtradata = ed
	} else {
		videoExtradata = info.Video.Extradata
	}
	t.Logf("extradata from CodecParameters: %d bytes", len(videoExtradata))

	dir := t.TempDir()
	segDir := filepath.Join(dir, "segments")
	os.MkdirAll(segDir, 0755)

	videoTB := astiav.NewRational(1, 90000)

	fmuxer, err := mux.NewFragmentedMuxer(mux.MuxOpts{
		OutputDir:      segDir,
		VideoCodecID:   videoCodecID,
		VideoExtradata: videoExtradata,
		VideoWidth:     info.Video.Width,
		VideoHeight:    info.Video.Height,
		VideoTimeBase:  videoTB,
	})
	if err != nil {
		t.Fatalf("create muxer: %v", err)
	}
	defer fmuxer.Close()

	var videoPkts, keyframes, writeErrors int
	for i := 0; i < 500; i++ {
		pkt, err := dm.ReadPacket()
		if err != nil {
			t.Logf("read %d: %v", i, err)
			break
		}
		if pkt.Type != demux.Video {
			continue
		}
		videoPkts++

		if pkt.Keyframe {
			keyframes++
		}

		avPkt, err := conv.ToAVPacket(pkt, videoTB)
		if err != nil {
			t.Logf("conv vpkt %d: %v", videoPkts, err)
			continue
		}

		if videoPkts <= 3 {
			t.Logf("vpkt %d: %d bytes pts=%d dts=%d dur=%d kf=%v flags=%v",
				videoPkts, len(pkt.Data), avPkt.Pts(), avPkt.Dts(), avPkt.Duration(), pkt.Keyframe, avPkt.Flags())
		}

		err = fmuxer.WriteVideoPacket(avPkt)
		avPkt.Free()
		if err != nil {
			writeErrors++
			if writeErrors <= 3 {
				t.Logf("write vpkt %d: %v", videoPkts, err)
			}
		}
	}

	t.Logf("sent %d video packets (%d keyframes, %d write errors)", videoPkts, keyframes, writeErrors)

	if err := fmuxer.Close(); err != nil {
		t.Logf("close muxer: %v", err)
	}

	segments, _ := filepath.Glob(filepath.Join(segDir, "video_*.m4s"))
	entries, _ := os.ReadDir(segDir)
	t.Log("segment directory:")
	for _, e := range entries {
		info, _ := e.Info()
		t.Logf("  %s (%d bytes)", e.Name(), info.Size())
	}

	if videoPkts > 0 && keyframes >= 2 && len(segments) == 0 {
		t.Errorf("BUG REQ-015: %d video packets with %d keyframes but 0 segments — fragment flush broken in copy mode", videoPkts, keyframes)
	} else if len(segments) > 0 {
		t.Logf("PASS: %d video segments produced", len(segments))
	} else if keyframes < 2 {
		t.Logf("only %d keyframes in %d packets — need >= 2 for segment production", keyframes, videoPkts)
	}
}

func TestMSECopyPipeline_ZeroDuration(t *testing.T) {
	dir := t.TempDir()

	extradata := []byte{
		0x01, 0x42, 0xC0, 0x1E, 0xFF, 0xE1,
		0x00, 0x04, 0x67, 0x42, 0xC0, 0x1E,
		0x01,
		0x00, 0x02, 0x68, 0xCE,
	}

	m, err := mux.NewFragmentedMuxer(mux.MuxOpts{
		OutputDir:      dir,
		VideoCodecID:   astiav.CodecIDH264,
		VideoExtradata: extradata,
		VideoWidth:     1920,
		VideoHeight:    1080,
		VideoTimeBase:  astiav.NewRational(1, 90000),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	for i := 0; i < 20; i++ {
		pkt := astiav.AllocPacket()
		data := make([]byte, 500)
		data[0] = 0x65
		pkt.FromData(data)
		pkt.SetPts(int64(i) * 3600)
		pkt.SetDts(int64(i) * 3600)
		pkt.SetDuration(0) // zero duration — this is the suspected issue
		if i%5 == 0 {
			pkt.SetFlags(pkt.Flags().Add(astiav.PacketFlagKey))
		}

		err := m.WriteVideoPacket(pkt)
		pkt.Free()
		if err != nil {
			t.Errorf("pkt %d: %v", i, err)
		}
	}

	m.Close()

	segments, _ := filepath.Glob(filepath.Join(dir, "video_*.m4s"))
	if len(segments) == 0 {
		t.Errorf("BUG: 20 packets with 4 keyframes and Duration=0 — no segments produced (accumDurationUs never > 0)")
	} else {
		t.Logf("produced %d segments with zero-duration packets", len(segments))
	}
}
