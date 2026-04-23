package session

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/asticode/go-astiav"
	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/lib/av/conv"
	"github.com/gavinmcnair/tvproxy/pkg/lib/av/decode"
	"github.com/gavinmcnair/tvproxy/pkg/lib/av/demux"
	"github.com/gavinmcnair/tvproxy/pkg/lib/av/encode"
	"github.com/gavinmcnair/tvproxy/pkg/lib/av/filter"
	"github.com/gavinmcnair/tvproxy/pkg/lib/av/keyframe"
	"github.com/gavinmcnair/tvproxy/pkg/lib/av/mux"
	"github.com/gavinmcnair/tvproxy/pkg/lib/av/probe"
	"github.com/gavinmcnair/tvproxy/pkg/lib/av/resample"
	"github.com/gavinmcnair/tvproxy/pkg/lib/av/scale"
	"github.com/gavinmcnair/tvproxy/pkg/proto"
)

type StreamCopyPipeline struct {
	muxer    *mux.StreamMuxer
	file     *os.File
	videoTB  astiav.Rational
	audioTB  astiav.Rational
	videoIdx int
	audioIdx int
	stopped  bool
	mu       sync.Mutex
	log      zerolog.Logger
}

type StreamCopyOpts struct {
	Info             *probe.StreamInfo
	AudioIndex       int
	FilePath         string
	Format           string
	OutputAudioCodec string
	Log              zerolog.Logger
}

func NewStreamCopyPipeline(opts StreamCopyOpts) (*StreamCopyPipeline, error) {
	format := opts.Format
	if format == "" {
		format = "mpegts"
	}

	f, err := os.Create(opts.FilePath)
	if err != nil {
		return nil, fmt.Errorf("gopipeline: create output file: %w", err)
	}

	muxer, err := mux.NewStreamMuxer(format, f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("gopipeline: create muxer: %w", err)
	}

	p := &StreamCopyPipeline{
		muxer:    muxer,
		file:     f,
		videoIdx: -1,
		audioIdx: -1,
		log:      opts.Log,
	}

	if opts.Info.Video != nil {
		videoCP, err := conv.CodecParamsFromVideoProbe(opts.Info.Video)
		if err != nil {
			muxer.Close()
			f.Close()
			return nil, fmt.Errorf("gopipeline: video codec params: %w", err)
		}
		vs, err := muxer.AddStream(videoCP)
		if err != nil {
			videoCP.Free()
			muxer.Close()
			f.Close()
			return nil, fmt.Errorf("gopipeline: add video stream: %w", err)
		}
		videoCP.Free()
		p.videoIdx = vs.Index()
		p.videoTB = vs.TimeBase()
	}

	if opts.AudioIndex >= 0 {
		var audioTrack *probe.AudioTrack
		for i := range opts.Info.AudioTracks {
			if opts.Info.AudioTracks[i].Index == opts.AudioIndex {
				audioTrack = &opts.Info.AudioTracks[i]
				break
			}
		}
		if audioTrack != nil {
			audioCP, err := conv.CodecParamsFromAudioProbe(audioTrack)
			if err != nil {
				muxer.Close()
				f.Close()
				return nil, fmt.Errorf("gopipeline: audio codec params: %w", err)
			}
			as, err := muxer.AddStream(audioCP)
			if err != nil {
				audioCP.Free()
				muxer.Close()
				f.Close()
				return nil, fmt.Errorf("gopipeline: add audio stream: %w", err)
			}
			audioCP.Free()
			p.audioIdx = as.Index()
			p.audioTB = as.TimeBase()
		}
	}

	if err := muxer.WriteHeader(); err != nil {
		muxer.Close()
		f.Close()
		return nil, fmt.Errorf("gopipeline: write header: %w", err)
	}

	return p, nil
}

func (p *StreamCopyPipeline) PushVideo(data []byte, pts, dts int64, keyframe bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped || p.videoIdx < 0 {
		return nil
	}
	pkt := &demux.Packet{Type: demux.Video, Data: data, PTS: pts, DTS: dts, Keyframe: keyframe}
	return p.writePacket(pkt, p.videoTB, p.videoIdx)
}

func (p *StreamCopyPipeline) PushAudio(data []byte, pts, dts int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped || p.audioIdx < 0 {
		return nil
	}
	pkt := &demux.Packet{Type: demux.Audio, Data: data, PTS: pts, DTS: dts}
	return p.writePacket(pkt, p.audioTB, p.audioIdx)
}

func (p *StreamCopyPipeline) PushSubtitle(data []byte, pts int64, duration int64) error {
	return nil
}

func (p *StreamCopyPipeline) EndOfStream() {
	p.Stop()
}

func (p *StreamCopyPipeline) writePacket(pkt *demux.Packet, tb astiav.Rational, streamIdx int) error {
	avPkt, err := conv.ToAVPacket(pkt, tb)
	if err != nil {
		return err
	}
	avPkt.SetStreamIndex(streamIdx)
	err = p.muxer.WritePacket(avPkt)
	avPkt.Free() // free packet struct; WriteInterleavedFrame already unrefs data
	return err
}

func (p *StreamCopyPipeline) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return
	}
	p.stopped = true
	p.muxer.Close()
	p.file.Close()
}

type AudioTranscodePipeline struct {
	muxer         *mux.StreamMuxer
	file          *os.File
	audioDec      *decode.Decoder
	audioResample *resample.Resampler
	audioEnc      *encode.Encoder
	audioFifo     *encode.AudioFIFO
	videoTB       astiav.Rational
	audioOutTB    astiav.Rational
	videoIdx      int
	audioIdx      int
	audioLatched  bool
	stopped       bool
	mu            sync.Mutex
	log           zerolog.Logger
}

type AudioTranscodeOpts struct {
	Info              *probe.StreamInfo
	AudioIndex        int
	FilePath          string
	OutputDir         string
	Format            string
	OutputAudioCodec  string
	AudioCodecParams  *astiav.CodecParameters
	Log               zerolog.Logger
}

func NewAudioTranscodePipeline(opts AudioTranscodeOpts) (*AudioTranscodePipeline, error) {
	format := opts.Format
	if format == "" {
		format = "mpegts"
	}

	var audioTrack *probe.AudioTrack
	for i := range opts.Info.AudioTracks {
		if opts.Info.AudioTracks[i].Index == opts.AudioIndex {
			audioTrack = &opts.Info.AudioTracks[i]
			break
		}
	}

	f, err := os.Create(opts.FilePath)
	if err != nil {
		return nil, fmt.Errorf("gopipeline: create output file: %w", err)
	}

	muxer, err := mux.NewStreamMuxer(format, f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("gopipeline: create muxer: %w", err)
	}

	p := &AudioTranscodePipeline{
		muxer:    muxer,
		file:     f,
		videoIdx: -1,
		audioIdx: -1,
		log:      opts.Log,
	}

	if opts.Info.Video != nil {
		videoCP, err := conv.CodecParamsFromVideoProbe(opts.Info.Video)
		if err != nil {
			muxer.Close()
			f.Close()
			return nil, fmt.Errorf("gopipeline: video codec params: %w", err)
		}
		vs, err := muxer.AddStream(videoCP)
		if err != nil {
			videoCP.Free()
			muxer.Close()
			f.Close()
			return nil, fmt.Errorf("gopipeline: add video stream: %w", err)
		}
		videoCP.Free()
		p.videoIdx = vs.Index()
		p.videoTB = vs.TimeBase()
	}

	if audioTrack != nil {
		codecID, err := conv.CodecIDFromString(audioTrack.Codec)
		if err != nil {
			muxer.Close()
			f.Close()
			return nil, fmt.Errorf("gopipeline: audio codec ID: %w", err)
		}

		if opts.AudioCodecParams != nil {
			p.audioDec, err = decode.NewAudioDecoderFromParams(opts.AudioCodecParams)
		} else {
			p.audioDec, err = decode.NewAudioDecoder(codecID, nil)
		}
		if err != nil {
			muxer.Close()
			f.Close()
			return nil, fmt.Errorf("gopipeline: audio decoder: %w", err)
		}

		if audioTrack.Channels > 2 || audioTrack.SampleRate != 48000 {
			p.audioResample, err = resample.NewResampler(
				audioTrack.Channels, audioTrack.SampleRate, astiav.SampleFormatFltp,
				2, 48000, astiav.SampleFormatFltp,
			)
			if err != nil {
				p.audioDec.Close()
				muxer.Close()
				f.Close()
				return nil, fmt.Errorf("gopipeline: audio resampler: %w", err)
			}
		}

		outAudioCodec := opts.OutputAudioCodec
		if outAudioCodec == "" {
			outAudioCodec = "aac"
		}
		encoderName := encode.ResolveAudioEncoderName(outAudioCodec)
		p.audioEnc, err = encode.NewAudioEncoder(encode.AudioEncodeOpts{
			Codec: encoderName, Channels: 2, SampleRate: 48000,
		})
		if err != nil {
			if p.audioResample != nil {
				p.audioResample.Close()
			}
			p.audioDec.Close()
			muxer.Close()
			f.Close()
			return nil, fmt.Errorf("gopipeline: audio encoder (%s): %w", outAudioCodec, err)
		}
		p.audioFifo = encode.NewAudioFIFOFromEncoder(p.audioEnc, 2, astiav.ChannelLayoutStereo, 48000)

		outAudioCodecID, _ := conv.CodecIDFromString(outAudioCodec)
		aacCP := astiav.AllocCodecParameters()
		aacCP.SetCodecID(outAudioCodecID)
		aacCP.SetMediaType(astiav.MediaTypeAudio)
		aacCP.SetSampleRate(48000)
		as, err := muxer.AddStream(aacCP)
		if err != nil {
			aacCP.Free()
			p.close()
			return nil, fmt.Errorf("gopipeline: add audio stream: %w", err)
		}
		aacCP.Free()
		p.audioIdx = as.Index()
		p.audioOutTB = as.TimeBase()
	}

	if err := muxer.WriteHeader(); err != nil {
		p.close()
		return nil, fmt.Errorf("gopipeline: write header: %w", err)
	}

	if opts.OutputDir != "" {
		proto.EnrichProbeFile(
			filepath.Join(opts.OutputDir, "probe.pb"),
			"", "aac", 2, 48000,
		)
	}

	return p, nil
}

func (p *AudioTranscodePipeline) PushVideo(data []byte, pts, dts int64, keyframe bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped || p.videoIdx < 0 {
		return nil
	}
	pkt := &demux.Packet{Type: demux.Video, Data: data, PTS: pts, DTS: dts, Keyframe: keyframe}
	avPkt, err := conv.ToAVPacket(pkt, p.videoTB)
	if err != nil {
		return err
	}
	avPkt.SetStreamIndex(p.videoIdx)
	err = p.muxer.WritePacket(avPkt)
	avPkt.Free()
	return err
}

func (p *AudioTranscodePipeline) PushAudio(data []byte, pts, dts int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped || p.audioIdx < 0 || p.audioLatched {
		return nil
	}

	pkt := &demux.Packet{Type: demux.Audio, Data: data, PTS: pts, DTS: dts}
	avPkt, err := conv.ToAVPacket(pkt, p.audioOutTB)
	if err != nil {
		p.latchAudioError(err)
		return nil
	}

	frames, err := p.audioDec.Decode(avPkt)
	avPkt.Free()
	if err != nil {
		p.latchAudioError(err)
		return nil
	}

	for _, frame := range frames {
		outFrame := frame
		if p.audioResample != nil {
			outFrame, err = p.audioResample.Convert(frame)
			frame.Free()
			if err != nil {
				p.latchAudioError(err)
				return nil
			}
		}
		encPkts, err := p.audioFifo.Write(outFrame)
		outFrame.Free()
		if err != nil {
			p.latchAudioError(err)
			return nil
		}
		for _, encPkt := range encPkts {
			encPkt.SetStreamIndex(p.audioIdx)
			if wErr := p.muxer.WritePacket(encPkt); wErr != nil {
				encPkt.Free()
				p.latchAudioError(wErr)
				return nil
			}
			encPkt.Free()
		}
	}
	return nil
}

func (p *AudioTranscodePipeline) PushSubtitle(data []byte, pts int64, duration int64) error {
	return nil
}

func (p *AudioTranscodePipeline) EndOfStream() {
	p.Stop()
}

func (p *AudioTranscodePipeline) latchAudioError(err error) {
	if !p.audioLatched {
		p.audioLatched = true
		p.log.Error().Err(err).Msg("audio transcode error latched — video continues")
	}
}

func (p *AudioTranscodePipeline) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return
	}
	p.stopped = true
	p.close()
}

func (p *AudioTranscodePipeline) close() {
	if p.audioFifo != nil {
		p.audioFifo.Close()
	}
	if p.audioEnc != nil {
		p.audioEnc.Close()
	}
	if p.audioResample != nil {
		p.audioResample.Close()
	}
	if p.audioDec != nil {
		p.audioDec.Close()
	}
	if p.muxer != nil {
		p.muxer.Close()
	}
	if p.file != nil {
		p.file.Close()
	}
}

type FullTranscodePipeline struct {
	muxer         *mux.StreamMuxer
	file          *os.File
	videoDec      *decode.Decoder
	videoEnc      *encode.Encoder
	deint         *filter.Deinterlacer
	scaler        *scale.Scaler
	audioDec      *decode.Decoder
	audioResample *resample.Resampler
	audioEnc      *encode.Encoder
	audioFifo     *encode.AudioFIFO
	kfTracker     *keyframe.KeyframeTracker
	videoCodec    string
	videoTB       astiav.Rational
	audioTB       astiav.Rational
	videoStreamIdx int
	audioStreamIdx int
	audioLatched  bool
	stopped       bool
	mu            sync.Mutex
	log           zerolog.Logger
}

type FullTranscodeOpts struct {
	Info              *probe.StreamInfo
	AudioIndex        int
	FilePath          string
	OutputDir         string
	Format            string
	IsLive            bool
	HWAccel           string
	DecodeHWAccel     string
	OutputCodec       string
	OutputAudioCodec  string
	Bitrate           int
	OutputHeight      int
	MaxBitDepth       int
	Deinterlace       bool
	VideoCodecParams  *astiav.CodecParameters
	AudioCodecParams  *astiav.CodecParameters
	Log               zerolog.Logger
}

func NewFullTranscodePipeline(opts FullTranscodeOpts) (*FullTranscodePipeline, error) {
	format := opts.Format
	if format == "" {
		format = "mpegts"
	}
	info := opts.Info

	f, err := os.Create(opts.FilePath)
	if err != nil {
		return nil, fmt.Errorf("gopipeline: create output file: %w", err)
	}

	muxer, err := mux.NewStreamMuxer(format, f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("gopipeline: create muxer: %w", err)
	}

	p := &FullTranscodePipeline{
		muxer:          muxer,
		file:           f,
		kfTracker:      keyframe.NewKeyframeTracker(!opts.IsLive),
		videoCodec:     info.Video.Codec,
		videoStreamIdx: -1,
		audioStreamIdx: -1,
		log:            opts.Log,
	}

	videoCodecID, err := conv.CodecIDFromString(info.Video.Codec)
	if err != nil {
		muxer.Close()
		f.Close()
		return nil, fmt.Errorf("gopipeline: video codec ID: %w", err)
	}

	decHW := opts.DecodeHWAccel
	if decHW == "" {
		decHW = opts.HWAccel
	}
	if opts.VideoCodecParams != nil {
		p.videoDec, err = decode.NewVideoDecoderFromParams(opts.VideoCodecParams, decode.DecodeOpts{
			HWAccel:     decHW,
			MaxBitDepth: opts.MaxBitDepth,
		})
	} else {
		p.videoDec, err = decode.NewVideoDecoder(videoCodecID, info.Video.Extradata, decode.DecodeOpts{
			HWAccel:     decHW,
			MaxBitDepth: opts.MaxBitDepth,
		})
	}
	if err != nil {
		p.fullClose()
		return nil, fmt.Errorf("gopipeline: video decoder: %w", err)
	}

	if opts.Deinterlace || info.Video.Interlaced {
		p.deint, err = filter.NewDeinterlacer(
			info.Video.Width, info.Video.Height,
			astiav.PixelFormatYuv420P,
			astiav.NewRational(info.Video.FramerateD, info.Video.FramerateN),
		)
		if err != nil {
			p.fullClose()
			return nil, fmt.Errorf("gopipeline: deinterlacer: %w", err)
		}
	}

	outW := info.Video.Width
	outH := info.Video.Height
	if opts.OutputHeight > 0 && opts.OutputHeight < info.Video.Height {
		outH = opts.OutputHeight
		outW = info.Video.Width * opts.OutputHeight / info.Video.Height
		outW = outW &^ 1
		p.scaler, err = scale.NewScaler(
			info.Video.Width, info.Video.Height, astiav.PixelFormatYuv420P,
			outW, outH, astiav.PixelFormatYuv420P,
		)
		if err != nil {
			p.fullClose()
			return nil, fmt.Errorf("gopipeline: scaler: %w", err)
		}
	}

	outCodec := opts.OutputCodec
	if outCodec == "" {
		outCodec = "h264"
	}
	p.videoEnc, err = encode.NewVideoEncoder(encode.EncodeOpts{
		Codec:   outCodec,
		HWAccel: opts.HWAccel,
		Bitrate: opts.Bitrate,
		Width:   outW,
		Height:  outH,
	})
	if err != nil {
		p.fullClose()
		return nil, fmt.Errorf("gopipeline: video encoder: %w", err)
	}

	outVideoCodecID, err := conv.CodecIDFromString(outCodec)
	if err != nil {
		p.fullClose()
		return nil, fmt.Errorf("gopipeline: output video codec ID: %w", err)
	}
	videoCP := astiav.AllocCodecParameters()
	videoCP.SetCodecID(outVideoCodecID)
	videoCP.SetMediaType(astiav.MediaTypeVideo)
	videoCP.SetWidth(outW)
	videoCP.SetHeight(outH)
	vs, err := muxer.AddStream(videoCP)
	if err != nil {
		videoCP.Free()
		p.fullClose()
		return nil, fmt.Errorf("gopipeline: add video stream: %w", err)
	}
	videoCP.Free()
	p.videoStreamIdx = vs.Index()
	p.videoTB = vs.TimeBase()

	var audioTrack *probe.AudioTrack
	for i := range info.AudioTracks {
		if info.AudioTracks[i].Index == opts.AudioIndex {
			audioTrack = &info.AudioTracks[i]
			break
		}
	}

	if audioTrack != nil {
		audioCodecID, err := conv.CodecIDFromString(audioTrack.Codec)
		if err != nil {
			p.fullClose()
			return nil, fmt.Errorf("gopipeline: audio codec ID: %w", err)
		}
		if opts.AudioCodecParams != nil {
			p.audioDec, err = decode.NewAudioDecoderFromParams(opts.AudioCodecParams)
		} else {
			p.audioDec, err = decode.NewAudioDecoder(audioCodecID, nil)
		}
		if err != nil {
			p.fullClose()
			return nil, fmt.Errorf("gopipeline: audio decoder: %w", err)
		}

		if audioTrack.Channels > 2 || audioTrack.SampleRate != 48000 {
			p.audioResample, err = resample.NewResampler(
				audioTrack.Channels, audioTrack.SampleRate, astiav.SampleFormatFltp,
				2, 48000, astiav.SampleFormatFltp,
			)
			if err != nil {
				p.fullClose()
				return nil, fmt.Errorf("gopipeline: audio resampler: %w", err)
			}
		}

		fullOutAudio := opts.OutputAudioCodec
		if fullOutAudio == "" {
			fullOutAudio = "aac"
		}
		fullEncName := encode.ResolveAudioEncoderName(fullOutAudio)
		p.audioEnc, err = encode.NewAudioEncoder(encode.AudioEncodeOpts{
			Codec: fullEncName, Channels: 2, SampleRate: 48000,
		})
		if err != nil {
			p.fullClose()
			return nil, fmt.Errorf("gopipeline: audio encoder (%s): %w", fullOutAudio, err)
		}
		p.audioFifo = encode.NewAudioFIFOFromEncoder(p.audioEnc, 2, astiav.ChannelLayoutStereo, 48000)

		fullOutAudioID, _ := conv.CodecIDFromString(fullOutAudio)
		aacCP := astiav.AllocCodecParameters()
		aacCP.SetCodecID(fullOutAudioID)
		aacCP.SetMediaType(astiav.MediaTypeAudio)
		aacCP.SetSampleRate(48000)
		as, err := muxer.AddStream(aacCP)
		if err != nil {
			aacCP.Free()
			p.fullClose()
			return nil, fmt.Errorf("gopipeline: add audio stream: %w", err)
		}
		aacCP.Free()
		p.audioStreamIdx = as.Index()
		p.audioTB = as.TimeBase()
	}

	if err := muxer.WriteHeader(); err != nil {
		p.fullClose()
		return nil, fmt.Errorf("gopipeline: write header: %w", err)
	}

	if opts.OutputDir != "" {
		proto.EnrichProbeFile(
			filepath.Join(opts.OutputDir, "probe.pb"),
			"", "aac", 2, 48000,
		)
	}

	return p, nil
}

func (p *FullTranscodePipeline) PushVideo(data []byte, pts, dts int64, keyframe bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return nil
	}

	if p.kfTracker.ShouldDrop(data, p.videoCodec) {
		return nil
	}

	pkt := &demux.Packet{Type: demux.Video, Data: data, PTS: pts, DTS: dts, Keyframe: keyframe}
	avPkt, err := conv.ToAVPacket(pkt, p.videoTB)
	if err != nil {
		return err
	}

	frames, err := p.videoDec.Decode(avPkt)
	avPkt.Free()
	if err != nil {
		for _, f := range frames {
			f.Free()
		}
		return fmt.Errorf("video decode: %w", err)
	}

	for i, frame := range frames {
		decFrame := frame
		if p.deint != nil {
			frame, err = p.deint.Process(frame)
			decFrame.Free()
			if err != nil {
				for _, f := range frames[i+1:] {
					f.Free()
				}
				return fmt.Errorf("deinterlace: %w", err)
			}
			if frame == nil {
				continue
			}
			decFrame = frame
		}
		if p.scaler != nil {
			frame, err = p.scaler.Scale(frame)
			decFrame.Free()
			if err != nil {
				for _, f := range frames[i+1:] {
					f.Free()
				}
				return fmt.Errorf("scale: %w", err)
			}
		}
		encPkts, err := p.videoEnc.Encode(frame)
		frame.Free()
		if err != nil {
			for _, f := range frames[i+1:] {
				f.Free()
			}
			return fmt.Errorf("video encode: %w", err)
		}
		for _, encPkt := range encPkts {
			encPkt.SetStreamIndex(p.videoStreamIdx)
			if err := p.muxer.WritePacket(encPkt); err != nil {
				encPkt.Free()
				for _, f := range frames[i+1:] {
					f.Free()
				}
				return fmt.Errorf("mux video: %w", err)
			}
			encPkt.Free()
		}
	}
	return nil
}

func (p *FullTranscodePipeline) PushAudio(data []byte, pts, dts int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped || p.audioStreamIdx < 0 || p.audioLatched {
		return nil
	}

	pkt := &demux.Packet{Type: demux.Audio, Data: data, PTS: pts, DTS: dts}
	avPkt, err := conv.ToAVPacket(pkt, p.audioTB)
	if err != nil {
		p.latchFullAudioError(err)
		return nil
	}

	frames, err := p.audioDec.Decode(avPkt)
	avPkt.Free()
	if err != nil {
		p.latchFullAudioError(err)
		return nil
	}

	for _, frame := range frames {
		outFrame := frame
		if p.audioResample != nil {
			outFrame, err = p.audioResample.Convert(frame)
			frame.Free()
			if err != nil {
				p.latchFullAudioError(err)
				return nil
			}
		}
		encPkts, err := p.audioFifo.Write(outFrame)
		outFrame.Free()
		if err != nil {
			p.latchFullAudioError(err)
			return nil
		}
		for _, encPkt := range encPkts {
			encPkt.SetStreamIndex(p.audioStreamIdx)
			if err := p.muxer.WritePacket(encPkt); err != nil {
				encPkt.Free()
				p.latchFullAudioError(err)
				return nil
			}
			encPkt.Free()
		}
	}
	return nil
}

func (p *FullTranscodePipeline) PushSubtitle(data []byte, pts int64, duration int64) error {
	return nil
}

func (p *FullTranscodePipeline) EndOfStream() {
	p.Stop()
}

func (p *FullTranscodePipeline) latchFullAudioError(err error) {
	if !p.audioLatched {
		p.audioLatched = true
		p.log.Error().Err(err).Msg("full transcode audio error latched — video continues")
	}
}

func (p *FullTranscodePipeline) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return
	}
	p.stopped = true
	p.fullClose()
}

func (p *FullTranscodePipeline) fullClose() {
	if p.audioFifo != nil {
		p.audioFifo.Close()
	}
	if p.videoEnc != nil {
		p.videoEnc.Close()
	}
	if p.scaler != nil {
		p.scaler.Close()
	}
	if p.deint != nil {
		p.deint.Close()
	}
	if p.videoDec != nil {
		p.videoDec.Close()
	}
	if p.audioEnc != nil {
		p.audioEnc.Close()
	}
	if p.audioResample != nil {
		p.audioResample.Close()
	}
	if p.audioDec != nil {
		p.audioDec.Close()
	}
	if p.muxer != nil {
		p.muxer.Close()
	}
	if p.file != nil {
		p.file.Close()
	}
}

type MSETranscodePipeline struct {
	muxer         *mux.FragmentedMuxer
	videoDec      *decode.Decoder
	videoEnc      *encode.Encoder
	deint         *filter.Deinterlacer
	scaler        *scale.Scaler
	audioDec      *decode.Decoder
	audioResample *resample.Resampler
	audioEnc      *encode.Encoder
	audioFifo     *encode.AudioFIFO
	kfTracker     *keyframe.KeyframeTracker
	videoCodec    string
	videoTB       astiav.Rational
	audioTB       astiav.Rational
	audioLatched  bool
	stopped       bool
	mu            sync.Mutex
	log           zerolog.Logger
}

type MSETranscodeOpts struct {
	Info              *probe.StreamInfo
	AudioIndex        int
	OutputDir         string
	IsLive            bool
	HWAccel           string
	DecodeHWAccel     string
	OutputCodec       string
	OutputAudioCodec  string
	Bitrate           int
	OutputHeight      int
	MaxBitDepth       int
	Deinterlace       bool
	VideoCodecParams  *astiav.CodecParameters
	AudioCodecParams  *astiav.CodecParameters
	Log               zerolog.Logger
}

func NewMSETranscodePipeline(opts MSETranscodeOpts) (*MSETranscodePipeline, error) {
	segDir := filepath.Join(opts.OutputDir, "segments")
	if err := os.MkdirAll(segDir, 0755); err != nil {
		return nil, fmt.Errorf("gopipeline: create segments dir: %w", err)
	}

	info := opts.Info
	p := &MSETranscodePipeline{
		kfTracker:  keyframe.NewKeyframeTracker(!opts.IsLive),
		videoCodec: info.Video.Codec,
		log:        opts.Log,
	}

	decHW := opts.DecodeHWAccel
	if decHW == "" {
		decHW = opts.HWAccel
	}
	var err error
	if opts.VideoCodecParams != nil {
		p.videoDec, err = decode.NewVideoDecoderFromParams(opts.VideoCodecParams, decode.DecodeOpts{
			HWAccel:     decHW,
			MaxBitDepth: opts.MaxBitDepth,
		})
	} else {
		videoCodecID, cerr := conv.CodecIDFromString(info.Video.Codec)
		if cerr != nil {
			return nil, fmt.Errorf("gopipeline: video codec ID: %w", cerr)
		}
		p.videoDec, err = decode.NewVideoDecoder(videoCodecID, info.Video.Extradata, decode.DecodeOpts{
			HWAccel:     decHW,
			MaxBitDepth: opts.MaxBitDepth,
		})
	}
	if err != nil {
		return nil, fmt.Errorf("gopipeline: video decoder: %w", err)
	}

	if opts.Deinterlace || info.Video.Interlaced {
		p.deint, err = filter.NewDeinterlacer(
			info.Video.Width, info.Video.Height,
			astiav.PixelFormatYuv420P,
			astiav.NewRational(info.Video.FramerateD, info.Video.FramerateN),
		)
		if err != nil {
			p.closeAll()
			return nil, fmt.Errorf("gopipeline: deinterlacer: %w", err)
		}
	}

	outW := info.Video.Width
	outH := info.Video.Height
	if opts.OutputHeight > 0 && opts.OutputHeight < info.Video.Height {
		outH = opts.OutputHeight
		outW = info.Video.Width * opts.OutputHeight / info.Video.Height
		outW = outW &^ 1
		p.scaler, err = scale.NewScaler(
			info.Video.Width, info.Video.Height, astiav.PixelFormatYuv420P,
			outW, outH, astiav.PixelFormatYuv420P,
		)
		if err != nil {
			p.closeAll()
			return nil, fmt.Errorf("gopipeline: scaler: %w", err)
		}
	}

	outCodec := opts.OutputCodec
	if outCodec == "" {
		outCodec = "h264"
	}
	p.videoEnc, err = encode.NewVideoEncoder(encode.EncodeOpts{
		Codec:   outCodec,
		HWAccel: opts.HWAccel,
		Bitrate: opts.Bitrate,
		Width:   outW,
		Height:  outH,
	})
	if err != nil {
		p.closeAll()
		return nil, fmt.Errorf("gopipeline: video encoder: %w", err)
	}

	outVideoCodecID, err := conv.CodecIDFromString(outCodec)
	if err != nil {
		p.closeAll()
		return nil, fmt.Errorf("gopipeline: output video codec ID: %w", err)
	}
	p.videoTB = astiav.NewRational(1, 90000)

	var audioTrack *probe.AudioTrack
	for i := range info.AudioTracks {
		if info.AudioTracks[i].Index == opts.AudioIndex {
			audioTrack = &info.AudioTracks[i]
			break
		}
	}

	muxOpts := mux.MuxOpts{
		OutputDir:    segDir,
		VideoCodecID: outVideoCodecID,
		VideoExtradata: p.videoEnc.Extradata(),
		VideoWidth:   outW,
		VideoHeight:  outH,
		VideoTimeBase: p.videoTB,
	}

	if audioTrack != nil {
		p.audioTB = astiav.NewRational(1, 48000)

		if opts.AudioCodecParams != nil {
			p.audioDec, err = decode.NewAudioDecoderFromParams(opts.AudioCodecParams)
		} else {
			audioCodecID, cerr := conv.CodecIDFromString(audioTrack.Codec)
			if cerr != nil {
				p.closeAll()
				return nil, fmt.Errorf("gopipeline: audio codec ID: %w", cerr)
			}
			p.audioDec, err = decode.NewAudioDecoder(audioCodecID, nil)
		}
		if err != nil {
			p.closeAll()
			return nil, fmt.Errorf("gopipeline: audio decoder: %w", err)
		}

		p.audioResample, err = resample.NewResampler(
			audioTrack.Channels, audioTrack.SampleRate, astiav.SampleFormatFltp,
			2, 48000, astiav.SampleFormatFltp,
		)
		if err != nil {
			p.closeAll()
			return nil, fmt.Errorf("gopipeline: audio resampler: %w", err)
		}

		mseAudio := opts.OutputAudioCodec
		if mseAudio == "" {
			mseAudio = "aac"
		}
		mseEncName := encode.ResolveAudioEncoderName(mseAudio)
		p.audioEnc, err = encode.NewAudioEncoder(encode.AudioEncodeOpts{
			Codec: mseEncName, Channels: 2, SampleRate: 48000,
		})
		if err != nil {
			p.closeAll()
			return nil, fmt.Errorf("gopipeline: audio encoder (%s): %w", mseAudio, err)
		}
		p.audioFifo = encode.NewAudioFIFOFromEncoder(p.audioEnc, 2, astiav.ChannelLayoutStereo, 48000)

		mseAudioID, _ := conv.CodecIDFromString(mseAudio)
		muxOpts.AudioCodecID = mseAudioID
		muxOpts.AudioChannels = 2
		muxOpts.AudioSampleRate = 48000
		muxOpts.AudioExtradata = p.audioEnc.Extradata()
	}

	p.muxer, err = mux.NewFragmentedMuxer(muxOpts)
	if err != nil {
		p.closeAll()
		return nil, fmt.Errorf("gopipeline: fragmented muxer: %w", err)
	}

	codecString := p.muxer.VideoCodecString()
	mseOutAudio := opts.OutputAudioCodec
	if mseOutAudio == "" {
		mseOutAudio = "aac"
	}
	proto.EnrichProbeFile(
		filepath.Join(opts.OutputDir, "probe.pb"),
		codecString, mseOutAudio, 2, 48000,
	)

	return p, nil
}

func (p *MSETranscodePipeline) PushVideo(data []byte, pts, dts int64, keyframe bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return nil
	}

	if p.kfTracker.ShouldDrop(data, p.videoCodec) {
		return nil
	}

	pkt := &demux.Packet{Type: demux.Video, Data: data, PTS: pts, DTS: dts, Keyframe: keyframe}
	avPkt, err := conv.ToAVPacket(pkt, p.videoTB)
	if err != nil {
		return err
	}

	frames, err := p.videoDec.Decode(avPkt)
	avPkt.Free()
	if err != nil {
		for _, f := range frames {
			f.Free()
		}
		return fmt.Errorf("video decode: %w", err)
	}

	for i, frame := range frames {
		decFrame := frame
		if p.deint != nil {
			frame, err = p.deint.Process(frame)
			decFrame.Free()
			if err != nil {
				for _, f := range frames[i+1:] {
					f.Free()
				}
				return fmt.Errorf("deinterlace: %w", err)
			}
			if frame == nil {
				continue
			}
			decFrame = frame
		}
		if p.scaler != nil {
			frame, err = p.scaler.Scale(frame)
			decFrame.Free()
			if err != nil {
				for _, f := range frames[i+1:] {
					f.Free()
				}
				return fmt.Errorf("scale: %w", err)
			}
		}
		encPkts, err := p.videoEnc.Encode(frame)
		frame.Free()
		if err != nil {
			for _, f := range frames[i+1:] {
				f.Free()
			}
			return fmt.Errorf("video encode: %w", err)
		}
		for _, encPkt := range encPkts {
			if err := p.muxer.WriteVideoPacket(encPkt); err != nil {
				encPkt.Free()
				for _, f := range frames[i+1:] {
					f.Free()
				}
				return fmt.Errorf("mux video: %w", err)
			}
			encPkt.Free()
		}
	}
	return nil
}

func (p *MSETranscodePipeline) PushAudio(data []byte, pts, dts int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped || p.audioLatched {
		return nil
	}

	if p.audioDec == nil {
		return nil
	}

	pkt := &demux.Packet{Type: demux.Audio, Data: data, PTS: pts, DTS: dts}
	avPkt, err := conv.ToAVPacket(pkt, p.audioTB)
	if err != nil {
		p.latchMSEAudioError(err)
		return nil
	}

	frames, err := p.audioDec.Decode(avPkt)
	avPkt.Free()
	if err != nil {
		p.latchMSEAudioError(err)
		return nil
	}

	for _, frame := range frames {
		outFrame := frame
		if p.audioResample != nil {
			outFrame, err = p.audioResample.Convert(frame)
			frame.Free()
			if err != nil {
				p.latchMSEAudioError(err)
				return nil
			}
		}
		encPkts, err := p.audioFifo.Write(outFrame)
		outFrame.Free()
		if err != nil {
			p.latchMSEAudioError(err)
			return nil
		}
		for _, encPkt := range encPkts {
			if err := p.muxer.WriteAudioPacket(encPkt); err != nil {
				encPkt.Free()
				p.latchMSEAudioError(err)
				return nil
			}
			encPkt.Free()
		}
	}
	return nil
}

func (p *MSETranscodePipeline) PushSubtitle(data []byte, pts int64, duration int64) error {
	return nil
}

func (p *MSETranscodePipeline) EndOfStream() {
	p.Stop()
}

func (p *MSETranscodePipeline) latchMSEAudioError(err error) {
	if !p.audioLatched {
		p.audioLatched = true
		p.log.Error().Err(err).Msg("MSE audio error latched — video continues")
	}
}

func (p *MSETranscodePipeline) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return
	}
	p.stopped = true
	p.closeAll()
}

func (p *MSETranscodePipeline) closeAll() {
	if p.audioFifo != nil {
		p.audioFifo.Close()
	}
	if p.videoEnc != nil {
		p.videoEnc.Close()
	}
	if p.scaler != nil {
		p.scaler.Close()
	}
	if p.deint != nil {
		p.deint.Close()
	}
	if p.videoDec != nil {
		p.videoDec.Close()
	}
	if p.audioEnc != nil {
		p.audioEnc.Close()
	}
	if p.audioResample != nil {
		p.audioResample.Close()
	}
	if p.audioDec != nil {
		p.audioDec.Close()
	}
	if p.muxer != nil {
		p.muxer.Close()
	}
}

type MSECopyPipeline struct {
	muxer            *mux.FragmentedMuxer
	audioDec         *decode.Decoder
	audioResample    *resample.Resampler
	audioEnc         *encode.Encoder
	audioFifo        *encode.AudioFIFO
	videoTB          astiav.Rational
	audioTB          astiav.Rational
	audioLatched     bool
	stopped          bool
	mu               sync.Mutex
	log              zerolog.Logger
}

type MSECopyOpts struct {
	Info              *probe.StreamInfo
	AudioIndex        int
	OutputDir         string
	IsLive            bool
	OutputAudioCodec  string
	VideoCodecParams  *astiav.CodecParameters
	AudioCodecParams  *astiav.CodecParameters
	Log               zerolog.Logger
}

func NewMSECopyPipeline(opts MSECopyOpts) (*MSECopyPipeline, error) {
	segDir := filepath.Join(opts.OutputDir, "segments")
	if err := os.MkdirAll(segDir, 0755); err != nil {
		return nil, fmt.Errorf("gopipeline: create segments dir: %w", err)
	}

	info := opts.Info
	p := &MSECopyPipeline{
		log: opts.Log,
	}

	if info.Video == nil {
		return nil, fmt.Errorf("gopipeline: no video track")
	}

	videoCodecID, err := conv.CodecIDFromString(info.Video.Codec)
	if err != nil {
		return nil, fmt.Errorf("gopipeline: video codec ID: %w", err)
	}

	var videoExtradata []byte
	if opts.VideoCodecParams != nil {
		videoExtradata = opts.VideoCodecParams.ExtraData()
	}
	if len(videoExtradata) == 0 {
		videoExtradata = info.Video.Extradata
	}

	p.videoTB = astiav.NewRational(1, 90000)

	var audioTrack *probe.AudioTrack
	for i := range info.AudioTracks {
		if info.AudioTracks[i].Index == opts.AudioIndex {
			audioTrack = &info.AudioTracks[i]
			break
		}
	}

	muxOpts := mux.MuxOpts{
		OutputDir:      segDir,
		VideoCodecID:   videoCodecID,
		VideoExtradata: videoExtradata,
		VideoWidth:     info.Video.Width,
		VideoHeight:    info.Video.Height,
		VideoTimeBase:  p.videoTB,
	}

	if audioTrack != nil {
		p.audioTB = astiav.NewRational(1, 48000)

		if opts.AudioCodecParams != nil {
			p.audioDec, err = decode.NewAudioDecoderFromParams(opts.AudioCodecParams)
			if err != nil {
				p.log.Warn().Err(err).Msg("audio decoder init failed, video-only")
				p.audioDec = nil
			}
		} else {
			audioCodecID, cerr := conv.CodecIDFromString(audioTrack.Codec)
			if cerr != nil {
				p.log.Warn().Err(cerr).Msg("unknown audio codec, video-only")
			} else {
				p.audioDec, err = decode.NewAudioDecoder(audioCodecID, nil)
				if err != nil {
					p.log.Warn().Err(err).Msg("audio decoder init failed, video-only")
					p.audioDec = nil
				}
			}
		}

		if p.audioDec != nil {
			p.audioResample, err = resample.NewResampler(
				audioTrack.Channels, audioTrack.SampleRate, astiav.SampleFormatFltp,
				2, 48000, astiav.SampleFormatFltp,
			)
			if err != nil {
				p.audioDec.Close()
				p.audioDec = nil
				p.log.Warn().Err(err).Msg("resampler init failed, video-only")
			}
		}

		if p.audioDec != nil {
			copyMseAudio := opts.OutputAudioCodec
			if copyMseAudio == "" {
				copyMseAudio = "aac"
			}
			copyMseEncName := encode.ResolveAudioEncoderName(copyMseAudio)
			p.audioEnc, err = encode.NewAudioEncoder(encode.AudioEncodeOpts{
				Codec: copyMseEncName, Channels: 2, SampleRate: 48000,
			})
			if err != nil {
				if p.audioResample != nil {
					p.audioResample.Close()
				}
				p.audioDec.Close()
				p.audioDec = nil
				p.log.Warn().Err(err).Str("codec", copyMseAudio).Msg("audio encoder init failed, video-only")
			} else {
				p.audioFifo = encode.NewAudioFIFOFromEncoder(p.audioEnc, 2, astiav.ChannelLayoutStereo, 48000)
			}
		}

		if p.audioEnc != nil {
			copyMseAudioID, _ := conv.CodecIDFromString(opts.OutputAudioCodec)
			if copyMseAudioID == 0 {
				copyMseAudioID = astiav.CodecIDAac
			}
			muxOpts.AudioCodecID = copyMseAudioID
			muxOpts.AudioChannels = 2
			muxOpts.AudioSampleRate = 48000
			muxOpts.AudioExtradata = p.audioEnc.Extradata()
		}
	}

	p.muxer, err = mux.NewFragmentedMuxer(muxOpts)
	if err != nil {
		p.closeMSECopy()
		return nil, fmt.Errorf("gopipeline: muxer: %w", err)
	}

	codecString := p.muxer.VideoCodecString()
	copyOutAudio := opts.OutputAudioCodec
	if copyOutAudio == "" {
		copyOutAudio = "aac"
	}
	probeAudio := opts.OutputAudioCodec
	if probeAudio == "" {
		probeAudio = "aac"
	}
	proto.EnrichProbeFile(
		filepath.Join(opts.OutputDir, "probe.pb"),
		codecString, probeAudio, 2, 48000,
	)

	return p, nil
}

func (p *MSECopyPipeline) PushVideo(data []byte, pts, dts int64, keyframe bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return nil
	}

	pkt := &demux.Packet{Type: demux.Video, Data: data, PTS: pts, DTS: dts, Keyframe: keyframe}
	avPkt, err := conv.ToAVPacket(pkt, p.videoTB)
	if err != nil {
		return err
	}
	err = p.muxer.WriteVideoPacket(avPkt)
	avPkt.Free()
	return err
}

func (p *MSECopyPipeline) PushAudio(data []byte, pts, dts int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped || p.audioLatched {
		return nil
	}

	if p.audioDec == nil {
		return nil
	}

	pkt := &demux.Packet{Type: demux.Audio, Data: data, PTS: pts, DTS: dts}
	avPkt, err := conv.ToAVPacket(pkt, p.audioTB)
	if err != nil {
		p.audioLatched = true
		return nil
	}
	frames, err := p.audioDec.Decode(avPkt)
	avPkt.Free()
	if err != nil {
		for _, f := range frames {
			f.Free()
		}
		p.audioLatched = true
		p.log.Warn().Err(err).Msg("MSE copy audio decode error latched")
		return nil
	}
	for _, frame := range frames {
		outFrame := frame
		if p.audioResample != nil {
			outFrame, err = p.audioResample.Convert(frame)
			frame.Free()
			if err != nil {
				p.audioLatched = true
				return nil
			}
		}
		encPkts, err := p.audioFifo.Write(outFrame)
		outFrame.Free()
		if err != nil {
			p.audioLatched = true
			p.log.Warn().Err(err).Msg("MSE copy audio encode error latched")
			return nil
		}
		for _, ep := range encPkts {
			if err := p.muxer.WriteAudioPacket(ep); err != nil {
				ep.Free()
				p.audioLatched = true
				return nil
			}
			ep.Free()
		}
	}
	return nil
}

func (p *MSECopyPipeline) PushSubtitle(data []byte, pts int64, duration int64) error {
	return nil
}

func (p *MSECopyPipeline) EndOfStream() {
	p.Stop()
}

func (p *MSECopyPipeline) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return
	}
	p.stopped = true
	p.closeMSECopy()
}

func (p *MSECopyPipeline) closeMSECopy() {
	if p.audioFifo != nil {
		p.audioFifo.Close()
	}
	if p.audioEnc != nil {
		p.audioEnc.Close()
	}
	if p.audioResample != nil {
		p.audioResample.Close()
	}
	if p.audioDec != nil {
		p.audioDec.Close()
	}
	if p.muxer != nil {
		p.muxer.Close()
	}
}
