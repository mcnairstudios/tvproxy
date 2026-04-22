package service

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/asticode/go-astiav"
	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/lib/av/conv"
	"github.com/gavinmcnair/tvproxy/pkg/lib/av/decode"
	"github.com/gavinmcnair/tvproxy/pkg/lib/av/demux"
	"github.com/gavinmcnair/tvproxy/pkg/lib/av/encode"
	"github.com/gavinmcnair/tvproxy/pkg/lib/av/mux"
	"github.com/gavinmcnair/tvproxy/pkg/lib/av/probe"
	"github.com/gavinmcnair/tvproxy/pkg/lib/av/resample"
)

type AVPipelineOpts struct {
	URL          string
	Writer       io.Writer
	Format       string
	VideoCodec   string
	AudioCodec   string
	HWAccel      string
	DecodeHWAccel string
	Bitrate      int
	OutputHeight int
	UserAgent    string
	TimeoutSec   int
	IsLive       bool
	SeekOffsetMs int64
	Log          zerolog.Logger
}

func StartAVPipeline(ctx context.Context, opts AVPipelineOpts) (io.ReadCloser, error) {
	pr, pw := io.Pipe()
	opts.Writer = pw
	go func() {
		err := RunAVPipeline(ctx, opts)
		if err != nil {
			pw.CloseWithError(err)
		} else {
			pw.Close()
		}
	}()
	return pr, nil
}

func RunAVPipeline(ctx context.Context, opts AVPipelineOpts) error {
	info, err := probe.Probe(opts.URL, max(opts.TimeoutSec, 5))
	if err != nil {
		return fmt.Errorf("avpipeline: probe: %w", err)
	}

	demuxOpts := demux.DemuxOpts{
		TimeoutSec: opts.TimeoutSec,
		AudioTrack: -1,
	}
	if opts.UserAgent != "" {
		demuxOpts.UserAgent = opts.UserAgent
	}
	if strings.HasPrefix(opts.URL, "rtsp://") {
		demuxOpts.RTSPLatency = 200
	}

	videoCopy := opts.VideoCodec == "" || opts.VideoCodec == "copy"
	audioCopy := opts.AudioCodec == "" || opts.AudioCodec == "copy"

	if audioCopy {
		demuxOpts.AudioPassthrough = true
	}

	demuxer, err := demux.NewDemuxer(opts.URL, demuxOpts)
	if err != nil {
		return fmt.Errorf("avpipeline: demux: %w", err)
	}
	defer demuxer.Close()

	if opts.SeekOffsetMs > 0 {
		demuxer.SeekTo(opts.SeekOffsetMs)
	}

	format := opts.Format
	if format == "" {
		format = "mpegts"
	}

	muxer, err := mux.NewStreamMuxer(format, opts.Writer)
	if err != nil {
		return fmt.Errorf("avpipeline: muxer: %w", err)
	}
	defer muxer.Close()

	var videoDec *decode.Decoder
	var videoEnc *encode.Encoder
	var audioDec *decode.Decoder
	var audioResample *resample.Resampler
	var audioEnc *encode.Encoder

	videoStreamIdx := -1
	audioStreamIdx := -1
	var videoTB, audioTB astiav.Rational

	if info.Video != nil {
		if videoCopy {
			videoCP, err := conv.CodecParamsFromVideoProbe(info.Video)
			if err != nil {
				return fmt.Errorf("avpipeline: video params: %w", err)
			}
			vs, err := muxer.AddStream(videoCP)
			videoCP.Free()
			if err != nil {
				return fmt.Errorf("avpipeline: add video stream: %w", err)
			}
			videoStreamIdx = vs.Index()
			videoTB = vs.TimeBase()
		} else {
			videoCodecID, err := conv.CodecIDFromString(info.Video.Codec)
			if err != nil {
				return fmt.Errorf("avpipeline: video codec: %w", err)
			}
			decHW := opts.DecodeHWAccel
			if decHW == "" {
				decHW = opts.HWAccel
			}
			videoDec, err = decode.NewVideoDecoder(videoCodecID, info.Video.Extradata, decode.DecodeOpts{HWAccel: decHW})
			if err != nil {
				return fmt.Errorf("avpipeline: video decoder: %w", err)
			}
			defer videoDec.Close()

			outCodec := opts.VideoCodec
			outW := info.Video.Width
			outH := info.Video.Height
			if opts.OutputHeight > 0 && opts.OutputHeight < info.Video.Height {
				outH = opts.OutputHeight
				outW = info.Video.Width * opts.OutputHeight / info.Video.Height
				outW = outW &^ 1
			}

			videoEnc, err = encode.NewVideoEncoder(encode.EncodeOpts{
				Codec:   outCodec,
				HWAccel: opts.HWAccel,
				Bitrate: opts.Bitrate,
				Width:   outW,
				Height:  outH,
			})
			if err != nil {
				return fmt.Errorf("avpipeline: video encoder: %w", err)
			}
			defer videoEnc.Close()

			outCodecID, _ := conv.CodecIDFromString(outCodec)
			videoCP := astiav.AllocCodecParameters()
			videoCP.SetCodecID(outCodecID)
			videoCP.SetMediaType(astiav.MediaTypeVideo)
			videoCP.SetWidth(outW)
			videoCP.SetHeight(outH)
			vs, err := muxer.AddStream(videoCP)
			videoCP.Free()
			if err != nil {
				return fmt.Errorf("avpipeline: add video stream: %w", err)
			}
			videoStreamIdx = vs.Index()
			videoTB = vs.TimeBase()
		}
	}

	if len(info.AudioTracks) > 0 {
		at := &info.AudioTracks[0]
		if audioCopy {
			audioCP, err := conv.CodecParamsFromAudioProbe(at)
			if err != nil {
				return fmt.Errorf("avpipeline: audio params: %w", err)
			}
			as, err := muxer.AddStream(audioCP)
			audioCP.Free()
			if err != nil {
				return fmt.Errorf("avpipeline: add audio stream: %w", err)
			}
			audioStreamIdx = as.Index()
			audioTB = as.TimeBase()
		} else {
			audioCodecID, err := conv.CodecIDFromString(at.Codec)
			if err != nil {
				return fmt.Errorf("avpipeline: audio codec: %w", err)
			}
			audioDec, err = decode.NewAudioDecoder(audioCodecID, nil)
			if err != nil {
				return fmt.Errorf("avpipeline: audio decoder: %w", err)
			}
			defer audioDec.Close()

			if at.Channels > 2 || at.SampleRate != 48000 {
				audioResample, err = resample.NewResampler(
					at.Channels, at.SampleRate, astiav.SampleFormatFltp,
					2, 48000, astiav.SampleFormatFltp,
				)
				if err != nil {
					return fmt.Errorf("avpipeline: resampler: %w", err)
				}
				defer audioResample.Close()
			}

			audioEnc, err = encode.NewAACEncoder(2, 48000)
			if err != nil {
				return fmt.Errorf("avpipeline: AAC encoder: %w", err)
			}
			defer audioEnc.Close()

			aacCP := astiav.AllocCodecParameters()
			aacCP.SetCodecID(astiav.CodecIDAac)
			aacCP.SetMediaType(astiav.MediaTypeAudio)
			aacCP.SetSampleRate(48000)
			as, err := muxer.AddStream(aacCP)
			aacCP.Free()
			if err != nil {
				return fmt.Errorf("avpipeline: add audio stream: %w", err)
			}
			audioStreamIdx = as.Index()
			audioTB = as.TimeBase()
		}
	}

	if err := muxer.WriteHeader(); err != nil {
		return fmt.Errorf("avpipeline: write header: %w", err)
	}

	audioLatched := false

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		pkt, err := demuxer.ReadPacket()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("avpipeline: read: %w", err)
		}

		switch pkt.Type {
		case demux.Video:
			if videoStreamIdx < 0 {
				continue
			}
			if videoCopy {
				avPkt, err := conv.ToAVPacket(pkt, videoTB)
				if err != nil {
					return err
				}
				avPkt.SetStreamIndex(videoStreamIdx)
				err = muxer.WritePacket(avPkt)
				avPkt.Free()
				if err != nil {
					return fmt.Errorf("avpipeline: write video: %w", err)
				}
			} else {
				avPkt, err := conv.ToAVPacket(pkt, videoTB)
				if err != nil {
					return err
				}
				frames, err := videoDec.Decode(avPkt)
				avPkt.Free()
				if err != nil {
					return fmt.Errorf("avpipeline: video decode: %w", err)
				}
				for _, frame := range frames {
					encPkts, err := videoEnc.Encode(frame)
					if err != nil {
						return fmt.Errorf("avpipeline: video encode: %w", err)
					}
					for _, ep := range encPkts {
						ep.SetStreamIndex(videoStreamIdx)
						muxer.WritePacket(ep)
					}
				}
			}

		case demux.Audio:
			if audioStreamIdx < 0 || audioLatched {
				continue
			}
			if audioCopy {
				avPkt, err := conv.ToAVPacket(pkt, audioTB)
				if err != nil {
					continue
				}
				avPkt.SetStreamIndex(audioStreamIdx)
				muxer.WritePacket(avPkt)
				avPkt.Free()
			} else {
				avPkt, err := conv.ToAVPacket(pkt, audioTB)
				if err != nil {
					audioLatched = true
					opts.Log.Error().Err(err).Msg("audio conversion error latched")
					continue
				}
				frames, err := audioDec.Decode(avPkt)
				avPkt.Free()
				if err != nil {
					audioLatched = true
					opts.Log.Error().Err(err).Msg("audio decode error latched")
					continue
				}
				for _, frame := range frames {
					outFrame := frame
					if audioResample != nil {
						outFrame, err = audioResample.Convert(frame)
						if err != nil {
							audioLatched = true
							opts.Log.Error().Err(err).Msg("audio resample error latched")
							break
						}
					}
					encPkts, err := audioEnc.Encode(outFrame)
					if err != nil {
						audioLatched = true
						opts.Log.Error().Err(err).Msg("audio encode error latched")
						break
					}
					for _, ep := range encPkts {
						ep.SetStreamIndex(audioStreamIdx)
						muxer.WritePacket(ep)
					}
				}
			}
		}
	}
}
