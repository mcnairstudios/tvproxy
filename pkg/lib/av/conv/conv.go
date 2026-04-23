package conv

import (
	"fmt"
	"strings"

	"github.com/asticode/go-astiav"
	"github.com/gavinmcnair/tvproxy/pkg/lib/av/demux"
	"github.com/gavinmcnair/tvproxy/pkg/lib/av/probe"
)

func ToAVPacket(p *demux.Packet, timeBase astiav.Rational) (*astiav.Packet, error) {
	pkt := astiav.AllocPacket()
	if pkt == nil {
		return nil, fmt.Errorf("conv: failed to allocate packet")
	}

	if len(p.Data) > 0 {
		if err := pkt.FromData(p.Data); err != nil {
			pkt.Free()
			return nil, fmt.Errorf("conv: set packet data: %w", err)
		}
	}

	tbNum := int64(timeBase.Num())
	tbDen := int64(timeBase.Den())
	if tbNum > 0 && tbDen > 0 {
		pkt.SetPts(p.PTS * tbDen / (1_000_000_000 * tbNum))
		pkt.SetDts(p.DTS * tbDen / (1_000_000_000 * tbNum))
		pkt.SetDuration(p.Duration * tbDen / (1_000_000_000 * tbNum))
	}

	if p.Keyframe {
		pkt.SetFlags(pkt.Flags().Add(astiav.PacketFlagKey))
	}

	return pkt, nil
}

var codecIDMap = map[string]astiav.CodecID{
	"h264":       astiav.CodecIDH264,
	"hevc":       astiav.CodecIDHevc,
	"h265":       astiav.CodecIDHevc,
	"mpeg2video": astiav.CodecIDMpeg2Video,
	"mpeg4":      astiav.CodecIDMpeg4,
	"vp8":        astiav.CodecIDVp8,
	"vp9":        astiav.CodecIDVp9,
	"av1":        astiav.CodecIDAv1,
	"theora":     astiav.CodecIDTheora,
	"aac":        astiav.CodecIDAac,
	"aac_latm":   astiav.CodecIDAacLatm,
	"ac3":        astiav.CodecIDAc3,
	"eac3":       astiav.CodecIDEac3,
	"dts":        astiav.CodecIDDts,
	"mp2":        astiav.CodecIDMp2,
	"mp3":        astiav.CodecIDMp3,
	"flac":       astiav.CodecIDFlac,
	"vorbis":     astiav.CodecIDVorbis,
	"opus":       astiav.CodecIDOpus,
	"truehd":     astiav.CodecIDTruehd,
	"pcm_s16le":  astiav.CodecIDPcmS16Le,
	"subrip":     astiav.CodecIDSubrip,
	"ass":        astiav.CodecIDAss,
	"webvtt":     astiav.CodecIDWebvtt,
}

func CodecIDFromString(codec string) (astiav.CodecID, error) {
	if id, ok := codecIDMap[strings.ToLower(codec)]; ok {
		return id, nil
	}
	return astiav.CodecIDNone, fmt.Errorf("conv: unknown codec %q", codec)
}

func CodecParamsFromVideoProbe(v *probe.VideoStream) (*astiav.CodecParameters, error) {
	codecID, err := CodecIDFromString(v.Codec)
	if err != nil {
		return nil, err
	}

	cp := astiav.AllocCodecParameters()
	if cp == nil {
		return nil, fmt.Errorf("conv: failed to allocate codec parameters")
	}

	cp.SetCodecID(codecID)
	cp.SetMediaType(astiav.MediaTypeVideo)
	cp.SetWidth(v.Width)
	cp.SetHeight(v.Height)
	if len(v.Extradata) > 0 {
		if err := cp.SetExtraData(v.Extradata); err != nil {
			cp.Free()
			return nil, fmt.Errorf("conv: set video extradata: %w", err)
		}
	}

	return cp, nil
}

func CodecParamsFromAudioProbe(a *probe.AudioTrack) (*astiav.CodecParameters, error) {
	codecID, err := CodecIDFromString(a.Codec)
	if err != nil {
		return nil, err
	}

	cp := astiav.AllocCodecParameters()
	if cp == nil {
		return nil, fmt.Errorf("conv: failed to allocate codec parameters")
	}

	cp.SetCodecID(codecID)
	cp.SetMediaType(astiav.MediaTypeAudio)
	cp.SetSampleRate(a.SampleRate)

	return cp, nil
}
