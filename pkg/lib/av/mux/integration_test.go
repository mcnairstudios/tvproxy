package mux_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/gavinmcnair/tvproxy/pkg/lib/av/conv"
	"github.com/gavinmcnair/tvproxy/pkg/lib/av/decode"
	"github.com/gavinmcnair/tvproxy/pkg/lib/av/demux"
	"github.com/gavinmcnair/tvproxy/pkg/lib/av/encode"
	"github.com/gavinmcnair/tvproxy/pkg/lib/av/mux"
	"github.com/gavinmcnair/tvproxy/pkg/lib/av/resample"
)

func TestMSEPipeline_RealStream(t *testing.T) {
	streamURL := os.Getenv("AVMUX_TEST_STREAM")
	if streamURL == "" {
		streamURL = "http://192.168.1.149:8090/stream/37969f9dbb17d735"
	}

	dm, err := demux.NewDemuxer(streamURL, demux.DemuxOpts{
		TimeoutSec: 10,
		AudioTrack: -1,
	})
	if err != nil {
		t.Skipf("cannot open stream %s: %v", streamURL, err)
	}
	defer dm.Close()

	info := dm.StreamInfo()
	if info == nil {
		t.Fatal("no stream info")
	}

	t.Logf("stream info:")
	if info.Video != nil {
		t.Logf("  video: %s %dx%d", info.Video.Codec, info.Video.Width, info.Video.Height)
		t.Logf("  video extradata: %d bytes", len(info.Video.Extradata))
	}
	for i, a := range info.AudioTracks {
		t.Logf("  audio[%d]: %s %dch %dHz (index=%d)", i, a.Codec, a.Channels, a.SampleRate, a.Index)
	}

	if info.Video == nil {
		t.Fatal("no video track")
	}

	dir := t.TempDir()
	segDir := filepath.Join(dir, "segments")
	os.MkdirAll(segDir, 0755)

	videoCopy := true

	var videoDec *decode.Decoder
	var videoEnc *encode.Encoder
	var videoCodecID astiav.CodecID
	var videoTB astiav.Rational

	muxOpts := mux.MuxOpts{
		OutputDir: segDir,
	}

	if videoCopy {
		videoCodecID, err = conv.CodecIDFromString(info.Video.Codec)
		if err != nil {
			t.Fatalf("video codec ID: %v", err)
		}
		videoCP, err := conv.CodecParamsFromVideoProbe(info.Video)
		if err != nil {
			t.Fatalf("video codec params: %v", err)
		}
		defer videoCP.Free()

		muxOpts.VideoCodecID = videoCodecID
		muxOpts.VideoExtradata = info.Video.Extradata
		muxOpts.VideoWidth = info.Video.Width
		muxOpts.VideoHeight = info.Video.Height
		muxOpts.VideoTimeBase = astiav.NewRational(1, 90000)
		videoTB = astiav.NewRational(1, 90000)
	} else {
		videoCodecID, err = conv.CodecIDFromString(info.Video.Codec)
		if err != nil {
			t.Fatalf("video codec ID: %v", err)
		}
		videoDec, err = decode.NewVideoDecoder(videoCodecID, info.Video.Extradata, decode.DecodeOpts{})
		if err != nil {
			t.Fatalf("video decoder: %v", err)
		}
		defer videoDec.Close()

		videoEnc, err = encode.NewVideoEncoder(encode.EncodeOpts{
			Codec:  "h264",
			Width:  info.Video.Width,
			Height: info.Video.Height,
		})
		if err != nil {
			t.Fatalf("video encoder: %v", err)
		}
		defer videoEnc.Close()

		outCodecID, _ := conv.CodecIDFromString("h264")
		muxOpts.VideoCodecID = outCodecID
		muxOpts.VideoExtradata = videoEnc.Extradata()
		muxOpts.VideoWidth = info.Video.Width
		muxOpts.VideoHeight = info.Video.Height
		muxOpts.VideoTimeBase = astiav.NewRational(1, 90000)
		videoTB = astiav.NewRational(1, 90000)
	}

	var audioDec *decode.Decoder
	var audioResamp *resample.Resampler
	var audioEnc *encode.Encoder
	var audioTB astiav.Rational
	audioLatched := false

	if len(info.AudioTracks) > 0 {
		at := &info.AudioTracks[0]
		t.Logf("setting up audio: %s %dch %dHz", at.Codec, at.Channels, at.SampleRate)

		audioCodecID, err := conv.CodecIDFromString(at.Codec)
		if err != nil {
			t.Logf("audio codec ID error (will skip audio): %v", err)
		} else {
			audioDec, err = decode.NewAudioDecoder(audioCodecID, nil)
			if err != nil {
				t.Logf("audio decoder error (will skip audio): %v", err)
				audioDec = nil
			}
		}

		if audioDec != nil {
			if at.Channels > 2 || at.SampleRate != 48000 {
				audioResamp, err = resample.NewResampler(
					at.Channels, at.SampleRate, astiav.SampleFormatFltp,
					2, 48000, astiav.SampleFormatFltp,
				)
				if err != nil {
					t.Logf("resampler error (will skip audio): %v", err)
					audioDec.Close()
					audioDec = nil
				}
			}
		}

		if audioDec != nil {
			audioEnc, err = encode.NewAACEncoder(2, 48000)
			if err != nil {
				t.Logf("AAC encoder error (will skip audio): %v", err)
				if audioResamp != nil {
					audioResamp.Close()
				}
				audioDec.Close()
				audioDec = nil
			}
		}

		if audioEnc != nil {
			defer audioEnc.Close()
			defer audioDec.Close()
			if audioResamp != nil {
				defer audioResamp.Close()
			}

			muxOpts.AudioCodecID = astiav.CodecIDAac
			muxOpts.AudioExtradata = audioEnc.Extradata()
			muxOpts.AudioChannels = 2
			muxOpts.AudioSampleRate = 48000
			audioTB = astiav.NewRational(1, 48000)
			t.Logf("audio extradata: %d bytes", len(muxOpts.AudioExtradata))
		}
	}

	fmuxer, err := mux.NewFragmentedMuxer(muxOpts)
	if err != nil {
		t.Fatalf("create muxer: %v", err)
	}
	defer fmuxer.Close()

	codecStr := fmuxer.VideoCodecString()
	t.Logf("video codec string from init: %q", codecStr)

	initVideo := filepath.Join(segDir, "init_video.mp4")
	if st, err := os.Stat(initVideo); err != nil {
		t.Errorf("init_video.mp4 missing: %v", err)
	} else {
		t.Logf("init_video.mp4: %d bytes", st.Size())
	}

	if audioEnc != nil {
		initAudio := filepath.Join(segDir, "init_audio.mp4")
		if st, err := os.Stat(initAudio); err != nil {
			t.Errorf("init_audio.mp4 missing: %v", err)
		} else {
			t.Logf("init_audio.mp4: %d bytes", st.Size())
		}
	}

	var videoPackets, audioPackets, videoSegments, audioSegments int
	keyframeCount := 0
	maxPackets := 500

	for i := 0; i < maxPackets; i++ {
		pkt, err := dm.ReadPacket()
		if err != nil {
			t.Logf("read packet %d: %v (stopping)", i, err)
			break
		}

		switch pkt.Type {
		case demux.Video:
			if videoCopy {
				avPkt, err := conv.ToAVPacket(pkt, videoTB)
				if err != nil {
					t.Fatalf("conv video pkt: %v", err)
				}
				if pkt.Keyframe {
					keyframeCount++
				}
				if err := fmuxer.WriteVideoPacket(avPkt); err != nil {
					avPkt.Free()
					if videoPackets == 0 {
						t.Logf("first video write error: %v", err)
					}
					videoPackets++
					continue
				}
				avPkt.Free()
				videoPackets++
			}

		case demux.Audio:
			if audioDec == nil || audioLatched {
				continue
			}
			avPkt, err := conv.ToAVPacket(pkt, audioTB)
			if err != nil {
				audioLatched = true
				t.Logf("audio conv error (latched): %v", err)
				continue
			}
			frames, err := audioDec.Decode(avPkt)
			avPkt.Free()
			if err != nil {
				audioLatched = true
				t.Logf("audio decode error (latched): %v", err)
				continue
			}
			for _, frame := range frames {
				outFrame := frame
				if audioResamp != nil {
					outFrame, err = audioResamp.Convert(frame)
					frame.Free()
					if err != nil {
						audioLatched = true
						t.Logf("audio resample error (latched): %v", err)
						break
					}
				}
				encPkts, err := audioEnc.Encode(outFrame)
				outFrame.Free()
				if err != nil {
					audioLatched = true
					t.Logf("audio encode error (latched): %v", err)
					break
				}
				for _, ep := range encPkts {
					if err := fmuxer.WriteAudioPacket(ep); err != nil {
						ep.Free()
						t.Logf("write audio pkt error: %v", err)
						audioLatched = true
						break
					}
					ep.Free()
					audioPackets++
				}
			}
		}
	}

	t.Logf("processed: %d video packets (%d keyframes), %d audio packets, audio_latched=%v",
		videoPackets, keyframeCount, audioPackets, audioLatched)

	if err := fmuxer.Close(); err != nil {
		t.Errorf("close muxer: %v", err)
	}

	videoSegs, _ := filepath.Glob(filepath.Join(segDir, "video_*.m4s"))
	audioSegs, _ := filepath.Glob(filepath.Join(segDir, "audio_*.m4s"))
	videoSegments = len(videoSegs)
	audioSegments = len(audioSegs)

	entries, _ := os.ReadDir(segDir)
	t.Log("segment directory:")
	for _, e := range entries {
		info, _ := e.Info()
		t.Logf("  %s (%d bytes)", e.Name(), info.Size())
	}

	if videoPackets > 0 && keyframeCount >= 2 && videoSegments == 0 {
		t.Errorf("BUG: %d video packets with %d keyframes but 0 video segments — fragment flush not triggering", videoPackets, keyframeCount)
	}

	if videoSegments > 0 {
		t.Logf("PASS: %d video segments produced", videoSegments)
	}

	if audioPackets > 0 && audioSegments == 0 {
		t.Errorf("BUG: %d audio packets but 0 audio segments — audio fragment flush not triggering", audioPackets)
	}

	if audioSegments > 0 {
		t.Logf("PASS: %d audio segments produced", audioSegments)
	}

	if audioLatched {
		t.Log("NOTE: audio decode latched (expected for DTS mid-stream join)")
	}
}
