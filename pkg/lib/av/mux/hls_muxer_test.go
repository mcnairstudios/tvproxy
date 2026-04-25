package mux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/asticode/go-astiav"
)

func TestHLSMuxer_SegmentProduction(t *testing.T) {
	dir := t.TempDir()

	codec := astiav.FindEncoderByName("libx264")
	if codec == nil {
		t.Skip("libx264 not available")
	}
	cc := astiav.AllocCodecContext(codec)
	if cc == nil {
		t.Fatal("failed to alloc codec context")
	}
	cc.SetWidth(640)
	cc.SetHeight(480)
	cc.SetPixelFormat(astiav.PixelFormatYuv420P)
	cc.SetTimeBase(astiav.NewRational(1, 25))
	cc.SetFramerate(astiav.NewRational(25, 1))
	cc.SetGopSize(25)
	cc.SetFlags(astiav.NewCodecContextFlags(astiav.CodecContextFlagGlobalHeader))

	if err := cc.Open(codec, nil); err != nil {
		cc.Free()
		t.Fatalf("open encoder: %v", err)
	}
	defer cc.Free()

	m, err := NewHLSMuxer(HLSMuxOpts{
		OutputDir:          dir,
		SegmentDurationSec: 2,
		VideoCodecID:       astiav.CodecIDH264,
		VideoExtradata:     cc.ExtraData(),
		VideoWidth:         640,
		VideoHeight:        480,
		VideoTimeBase:      astiav.NewRational(1, 90000),
	})
	if err != nil {
		t.Fatal(err)
	}

	frame := astiav.AllocFrame()
	if frame == nil {
		t.Fatal("alloc frame")
	}
	defer frame.Free()
	frame.SetWidth(640)
	frame.SetHeight(480)
	frame.SetPixelFormat(astiav.PixelFormatYuv420P)
	if err := frame.AllocBuffer(0); err != nil {
		t.Fatalf("alloc buffer: %v", err)
	}

	outTB := astiav.NewRational(1, 90000)
	var totalPackets int
	for i := 0; i < 150; i++ {
		frame.SetPts(int64(i))
		if err := cc.SendFrame(frame); err != nil {
			continue
		}

		for {
			pkt := astiav.AllocPacket()
			if err := cc.ReceivePacket(pkt); err != nil {
				pkt.Free()
				break
			}

			pkt.RescaleTs(cc.TimeBase(), outTB)
			if pkt.Duration() == 0 {
				pkt.SetDuration(int64(outTB.Den()) / int64(cc.Framerate().Num()))
			}

			if err := m.WriteVideoPacket(pkt); err != nil {
				pkt.Free()
				t.Fatalf("write video pkt %d: %v", totalPackets, err)
			}
			pkt.Free()
			totalPackets++
		}
	}

	cc.SendFrame(nil) //nolint:errcheck
	for {
		pkt := astiav.AllocPacket()
		if err := cc.ReceivePacket(pkt); err != nil {
			pkt.Free()
			break
		}
		pkt.RescaleTs(cc.TimeBase(), outTB)
		if pkt.Duration() == 0 {
			pkt.SetDuration(int64(outTB.Den()) / int64(cc.Framerate().Num()))
		}
		m.WriteVideoPacket(pkt) //nolint:errcheck
		pkt.Free()
		totalPackets++
	}

	if totalPackets == 0 {
		t.Fatal("encoder produced no packets")
	}

	if err := m.Close(); err != nil {
		t.Fatalf("close muxer: %v", err)
	}

	segments, err := filepath.Glob(filepath.Join(dir, "seg*.ts"))
	if err != nil {
		t.Fatal(err)
	}

	if len(segments) == 0 {
		t.Errorf("no HLS segments produced after %d packets", totalPackets)
		entries, _ := os.ReadDir(dir)
		t.Log("directory contents:")
		for _, e := range entries {
			info, _ := e.Info()
			t.Logf("  %s (%d bytes)", e.Name(), info.Size())
		}
	} else {
		t.Logf("produced %d HLS segments from %d packets", len(segments), totalPackets)
		for _, seg := range segments {
			info, _ := os.Stat(seg)
			t.Logf("  %s (%d bytes)", filepath.Base(seg), info.Size())
		}
	}

	playlistPath := filepath.Join(dir, "playlist.m3u8")
	data, err := os.ReadFile(playlistPath)
	if err != nil {
		t.Fatalf("playlist.m3u8 missing: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "#EXTM3U") {
		t.Error("playlist missing #EXTM3U header")
	}
	if !strings.Contains(content, "#EXT-X-TARGETDURATION:") {
		t.Error("playlist missing #EXT-X-TARGETDURATION")
	}
	if !strings.Contains(content, "#EXT-X-ENDLIST") {
		t.Error("playlist missing #EXT-X-ENDLIST (Close should append it)")
	}
	if !strings.Contains(content, "seg0.ts") {
		t.Error("playlist missing seg0.ts reference")
	}

	t.Logf("playlist content:\n%s", content)
}

func TestHLSMuxer_VideoAndAudio(t *testing.T) {
	dir := t.TempDir()

	videoCodec := astiav.FindEncoderByName("libx264")
	if videoCodec == nil {
		t.Skip("libx264 not available")
	}
	audioCodec := astiav.FindEncoderByName("aac")
	if audioCodec == nil {
		t.Skip("aac encoder not available")
	}

	vcc := astiav.AllocCodecContext(videoCodec)
	vcc.SetWidth(320)
	vcc.SetHeight(240)
	vcc.SetPixelFormat(astiav.PixelFormatYuv420P)
	vcc.SetTimeBase(astiav.NewRational(1, 25))
	vcc.SetFramerate(astiav.NewRational(25, 1))
	vcc.SetGopSize(25)
	vcc.SetFlags(astiav.NewCodecContextFlags(astiav.CodecContextFlagGlobalHeader))
	if err := vcc.Open(videoCodec, nil); err != nil {
		vcc.Free()
		t.Fatalf("open video encoder: %v", err)
	}
	defer vcc.Free()

	acc := astiav.AllocCodecContext(audioCodec)
	acc.SetSampleRate(48000)
	acc.SetSampleFormat(astiav.SampleFormatFltp)
	acc.SetChannelLayout(astiav.ChannelLayoutStereo)
	acc.SetFlags(astiav.NewCodecContextFlags(astiav.CodecContextFlagGlobalHeader))
	acc.SetTimeBase(astiav.NewRational(1, 48000))
	if err := acc.Open(audioCodec, nil); err != nil {
		acc.Free()
		t.Fatalf("open audio encoder: %v", err)
	}
	defer acc.Free()

	m, err := NewHLSMuxer(HLSMuxOpts{
		OutputDir:          dir,
		SegmentDurationSec: 2,
		VideoCodecID:       astiav.CodecIDH264,
		VideoExtradata:     vcc.ExtraData(),
		VideoWidth:         320,
		VideoHeight:        240,
		VideoTimeBase:      astiav.NewRational(1, 90000),
		AudioCodecID:       astiav.CodecIDAac,
		AudioExtradata:     acc.ExtraData(),
		AudioChannels:      2,
		AudioSampleRate:    48000,
		AudioTimeBase:      astiav.NewRational(1, 48000),
	})
	if err != nil {
		t.Fatal(err)
	}

	videoFrame := astiav.AllocFrame()
	defer videoFrame.Free()
	videoFrame.SetWidth(320)
	videoFrame.SetHeight(240)
	videoFrame.SetPixelFormat(astiav.PixelFormatYuv420P)
	videoFrame.AllocBuffer(0)

	audioFrame := astiav.AllocFrame()
	defer audioFrame.Free()
	audioFrame.SetSampleRate(48000)
	audioFrame.SetSampleFormat(astiav.SampleFormatFltp)
	audioFrame.SetChannelLayout(astiav.ChannelLayoutStereo)
	audioFrame.SetNbSamples(acc.FrameSize())
	audioFrame.AllocBuffer(0)

	outVideoTB := astiav.NewRational(1, 90000)
	outAudioTB := astiav.NewRational(1, 48000)

	var videoPkts, audioPkts int
	for i := 0; i < 100; i++ {
		videoFrame.SetPts(int64(i))
		if err := vcc.SendFrame(videoFrame); err == nil {
			for {
				pkt := astiav.AllocPacket()
				if err := vcc.ReceivePacket(pkt); err != nil {
					pkt.Free()
					break
				}
				pkt.RescaleTs(vcc.TimeBase(), outVideoTB)
				if pkt.Duration() == 0 {
					pkt.SetDuration(int64(outVideoTB.Den()) / int64(vcc.Framerate().Num()))
				}
				m.WriteVideoPacket(pkt) //nolint:errcheck
				pkt.Free()
				videoPkts++
			}
		}

		audioFrame.SetPts(int64(i) * int64(acc.FrameSize()))
		if err := acc.SendFrame(audioFrame); err == nil {
			for {
				pkt := astiav.AllocPacket()
				if err := acc.ReceivePacket(pkt); err != nil {
					pkt.Free()
					break
				}
				pkt.RescaleTs(acc.TimeBase(), outAudioTB)
				if pkt.Duration() == 0 {
					pkt.SetDuration(int64(acc.FrameSize()))
				}
				m.WriteAudioPacket(pkt) //nolint:errcheck
				pkt.Free()
				audioPkts++
			}
		}
	}

	if err := m.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	segments, _ := filepath.Glob(filepath.Join(dir, "seg*.ts"))
	t.Logf("produced %d segments with %d video + %d audio packets", len(segments), videoPkts, audioPkts)

	if len(segments) == 0 {
		t.Error("no HLS segments produced with video+audio")
	}

	for _, seg := range segments {
		info, _ := os.Stat(seg)
		if info.Size() == 0 {
			t.Errorf("segment %s is empty", filepath.Base(seg))
		}
	}
}

func TestHLSMuxer_Close_AppendsEndlist(t *testing.T) {
	dir := t.TempDir()

	codec := astiav.FindEncoderByName("libx264")
	if codec == nil {
		t.Skip("libx264 not available")
	}
	cc := astiav.AllocCodecContext(codec)
	cc.SetWidth(320)
	cc.SetHeight(240)
	cc.SetPixelFormat(astiav.PixelFormatYuv420P)
	cc.SetTimeBase(astiav.NewRational(1, 25))
	cc.SetFramerate(astiav.NewRational(25, 1))
	cc.SetGopSize(25)
	cc.SetFlags(astiav.NewCodecContextFlags(astiav.CodecContextFlagGlobalHeader))
	if err := cc.Open(codec, nil); err != nil {
		cc.Free()
		t.Fatalf("open encoder: %v", err)
	}
	defer cc.Free()

	m, err := NewHLSMuxer(HLSMuxOpts{
		OutputDir:          dir,
		SegmentDurationSec: 2,
		VideoCodecID:       astiav.CodecIDH264,
		VideoExtradata:     cc.ExtraData(),
		VideoWidth:         320,
		VideoHeight:        240,
		VideoTimeBase:      astiav.NewRational(1, 90000),
	})
	if err != nil {
		t.Fatal(err)
	}

	frame := astiav.AllocFrame()
	defer frame.Free()
	frame.SetWidth(320)
	frame.SetHeight(240)
	frame.SetPixelFormat(astiav.PixelFormatYuv420P)
	frame.AllocBuffer(0)

	outTB := astiav.NewRational(1, 90000)
	for i := 0; i < 50; i++ {
		frame.SetPts(int64(i))
		cc.SendFrame(frame) //nolint:errcheck
		for {
			pkt := astiav.AllocPacket()
			if err := cc.ReceivePacket(pkt); err != nil {
				pkt.Free()
				break
			}
			pkt.RescaleTs(cc.TimeBase(), outTB)
			if pkt.Duration() == 0 {
				pkt.SetDuration(int64(outTB.Den()) / int64(cc.Framerate().Num()))
			}
			m.WriteVideoPacket(pkt) //nolint:errcheck
			pkt.Free()
		}
	}

	if err := m.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	playlistData, err := os.ReadFile(filepath.Join(dir, "playlist.m3u8"))
	if err != nil {
		t.Fatalf("read playlist: %v", err)
	}

	content := string(playlistData)
	if !strings.Contains(content, "#EXT-X-ENDLIST") {
		t.Error("Close() did not append #EXT-X-ENDLIST")
	}
	t.Logf("playlist:\n%s", content)
}

func TestHLSMuxer_Reset_ClearsSegments(t *testing.T) {
	dir := t.TempDir()

	codec := astiav.FindEncoderByName("libx264")
	if codec == nil {
		t.Skip("libx264 not available")
	}
	cc := astiav.AllocCodecContext(codec)
	cc.SetWidth(320)
	cc.SetHeight(240)
	cc.SetPixelFormat(astiav.PixelFormatYuv420P)
	cc.SetTimeBase(astiav.NewRational(1, 25))
	cc.SetFramerate(astiav.NewRational(25, 1))
	cc.SetGopSize(25)
	cc.SetFlags(astiav.NewCodecContextFlags(astiav.CodecContextFlagGlobalHeader))
	if err := cc.Open(codec, nil); err != nil {
		cc.Free()
		t.Fatalf("open encoder: %v", err)
	}
	defer cc.Free()

	m, err := NewHLSMuxer(HLSMuxOpts{
		OutputDir:          dir,
		SegmentDurationSec: 1,
		VideoCodecID:       astiav.CodecIDH264,
		VideoExtradata:     cc.ExtraData(),
		VideoWidth:         320,
		VideoHeight:        240,
		VideoTimeBase:      astiav.NewRational(1, 90000),
	})
	if err != nil {
		t.Fatal(err)
	}

	frame := astiav.AllocFrame()
	defer frame.Free()
	frame.SetWidth(320)
	frame.SetHeight(240)
	frame.SetPixelFormat(astiav.PixelFormatYuv420P)
	frame.AllocBuffer(0)

	outTB := astiav.NewRational(1, 90000)
	encodeFrames := func(count int) int {
		var pkts int
		for i := 0; i < count; i++ {
			frame.SetPts(int64(i))
			cc.SendFrame(frame) //nolint:errcheck
			for {
				pkt := astiav.AllocPacket()
				if err := cc.ReceivePacket(pkt); err != nil {
					pkt.Free()
					break
				}
				pkt.RescaleTs(cc.TimeBase(), outTB)
				if pkt.Duration() == 0 {
					pkt.SetDuration(int64(outTB.Den()) / int64(cc.Framerate().Num()))
				}
				m.WriteVideoPacket(pkt) //nolint:errcheck
				pkt.Free()
				pkts++
			}
		}
		return pkts
	}

	pkts := encodeFrames(75)
	t.Logf("before reset: %d packets, count=%d", pkts, m.SegmentCount())

	segsBefore, _ := filepath.Glob(filepath.Join(dir, "seg*.ts"))
	t.Logf("before reset: %d segment files", len(segsBefore))

	if err := m.Reset(); err != nil {
		t.Fatalf("reset: %v", err)
	}

	if m.SegmentCount() != 0 {
		t.Errorf("segment count after reset = %d, want 0", m.SegmentCount())
	}

	pkts2 := encodeFrames(75)
	t.Logf("after reset: %d packets, count=%d", pkts2, m.SegmentCount())

	m.Close()

	segsAfter, _ := filepath.Glob(filepath.Join(dir, "seg*.ts"))
	if len(segsAfter) == 0 {
		t.Error("no segments after reset + new writes")
	} else {
		t.Logf("after reset + new writes: %d segments", len(segsAfter))
	}
}

func TestHLSMuxer_SegmentCount(t *testing.T) {
	dir := t.TempDir()

	codec := astiav.FindEncoderByName("libx264")
	if codec == nil {
		t.Skip("libx264 not available")
	}
	cc := astiav.AllocCodecContext(codec)
	cc.SetWidth(320)
	cc.SetHeight(240)
	cc.SetPixelFormat(astiav.PixelFormatYuv420P)
	cc.SetTimeBase(astiav.NewRational(1, 25))
	cc.SetFramerate(astiav.NewRational(25, 1))
	cc.SetGopSize(25)
	cc.SetFlags(astiav.NewCodecContextFlags(astiav.CodecContextFlagGlobalHeader))
	if err := cc.Open(codec, nil); err != nil {
		cc.Free()
		t.Fatalf("open encoder: %v", err)
	}
	defer cc.Free()

	m, err := NewHLSMuxer(HLSMuxOpts{
		OutputDir:          dir,
		SegmentDurationSec: 1,
		VideoCodecID:       astiav.CodecIDH264,
		VideoExtradata:     cc.ExtraData(),
		VideoWidth:         320,
		VideoHeight:        240,
		VideoTimeBase:      astiav.NewRational(1, 90000),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	if m.SegmentCount() != 0 {
		t.Errorf("initial segment count = %d, want 0", m.SegmentCount())
	}

	frame := astiav.AllocFrame()
	defer frame.Free()
	frame.SetWidth(320)
	frame.SetHeight(240)
	frame.SetPixelFormat(astiav.PixelFormatYuv420P)
	frame.AllocBuffer(0)

	outTB := astiav.NewRational(1, 90000)
	for i := 0; i < 100; i++ {
		frame.SetPts(int64(i))
		cc.SendFrame(frame) //nolint:errcheck
		for {
			pkt := astiav.AllocPacket()
			if err := cc.ReceivePacket(pkt); err != nil {
				pkt.Free()
				break
			}
			pkt.RescaleTs(cc.TimeBase(), outTB)
			if pkt.Duration() == 0 {
				pkt.SetDuration(int64(outTB.Den()) / int64(cc.Framerate().Num()))
			}
			m.WriteVideoPacket(pkt) //nolint:errcheck
			pkt.Free()
		}
	}

	count := m.SegmentCount()
	t.Logf("segment count after 100 frames (GOP=25, 1s target): %d", count)
	if count == 0 {
		t.Error("expected at least one segment to be completed")
	}
}

func TestHLSMuxer_PlaylistContent(t *testing.T) {
	dir := t.TempDir()

	codec := astiav.FindEncoderByName("libx264")
	if codec == nil {
		t.Skip("libx264 not available")
	}
	cc := astiav.AllocCodecContext(codec)
	cc.SetWidth(320)
	cc.SetHeight(240)
	cc.SetPixelFormat(astiav.PixelFormatYuv420P)
	cc.SetTimeBase(astiav.NewRational(1, 25))
	cc.SetFramerate(astiav.NewRational(25, 1))
	cc.SetGopSize(25)
	cc.SetFlags(astiav.NewCodecContextFlags(astiav.CodecContextFlagGlobalHeader))
	if err := cc.Open(codec, nil); err != nil {
		cc.Free()
		t.Fatalf("open encoder: %v", err)
	}
	defer cc.Free()

	m, err := NewHLSMuxer(HLSMuxOpts{
		OutputDir:          dir,
		SegmentDurationSec: 1,
		VideoCodecID:       astiav.CodecIDH264,
		VideoExtradata:     cc.ExtraData(),
		VideoWidth:         320,
		VideoHeight:        240,
		VideoTimeBase:      astiav.NewRational(1, 90000),
	})
	if err != nil {
		t.Fatal(err)
	}

	frame := astiav.AllocFrame()
	defer frame.Free()
	frame.SetWidth(320)
	frame.SetHeight(240)
	frame.SetPixelFormat(astiav.PixelFormatYuv420P)
	frame.AllocBuffer(0)

	outTB := astiav.NewRational(1, 90000)
	for i := 0; i < 100; i++ {
		frame.SetPts(int64(i))
		cc.SendFrame(frame) //nolint:errcheck
		for {
			pkt := astiav.AllocPacket()
			if err := cc.ReceivePacket(pkt); err != nil {
				pkt.Free()
				break
			}
			pkt.RescaleTs(cc.TimeBase(), outTB)
			if pkt.Duration() == 0 {
				pkt.SetDuration(int64(outTB.Den()) / int64(cc.Framerate().Num()))
			}
			m.WriteVideoPacket(pkt) //nolint:errcheck
			pkt.Free()
		}
	}

	content := m.PlaylistContent()
	if !strings.Contains(content, "#EXTM3U") {
		t.Error("PlaylistContent missing #EXTM3U")
	}
	if strings.Contains(content, "#EXT-X-ENDLIST") {
		t.Error("PlaylistContent should not have #EXT-X-ENDLIST before Close")
	}

	m.Close()

	data, _ := os.ReadFile(filepath.Join(dir, "playlist.m3u8"))
	if !strings.Contains(string(data), "#EXT-X-ENDLIST") {
		t.Error("final playlist missing #EXT-X-ENDLIST")
	}
}
