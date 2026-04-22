package probe

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/asticode/go-astiav"
	"github.com/gavinmcnair/tvproxy/pkg/lib/av/extradata"
)

type StreamInfo struct {
	Container   string
	DurationMs  int64
	IsLive      bool
	Video       *VideoStream
	AudioTracks []AudioTrack
	SubTracks   []SubtitleTrack
}

type VideoStream struct {
	Index       int
	Codec       string
	Width       int
	Height      int
	Interlaced  bool
	BitDepth    int
	FramerateN  int
	FramerateD  int
	BitrateKbps int
	Extradata   []byte // raw codec extradata for pipeline caps
}

type AudioTrack struct {
	Index       int
	Codec       string
	Channels    int
	SampleRate  int
	Language    string
	IsAD        bool
	BitrateKbps int
}

type SubtitleTrack struct {
	Index    int
	Codec    string
	Language string
}

// Probe opens a media file or URL, extracts stream information, and closes
// the connection. For playback, prefer using avdemux.NewDemuxer() and calling
// Demuxer.StreamInfo() instead — that avoids opening the URL twice.
func Probe(url string, timeoutSec int) (*StreamInfo, error) {
	fc := astiav.AllocFormatContext()
	if fc == nil {
		return nil, fmt.Errorf("avprobe: failed to allocate format context")
	}
	defer fc.Free()

	// Set timeout via dictionary if specified.
	var d *astiav.Dictionary
	if timeoutSec > 0 {
		d = astiav.NewDictionary()
		defer d.Free()
		d.Set("timeout", fmt.Sprintf("%d", timeoutSec*1_000_000), 0)
	}

	if err := fc.OpenInput(url, nil, d); err != nil {
		return nil, fmt.Errorf("avprobe: open input %q: %w", url, err)
	}
	defer fc.CloseInput()

	if err := fc.FindStreamInfo(nil); err != nil {
		return nil, fmt.Errorf("avprobe: find stream info: %w", err)
	}

	return ExtractStreamInfo(fc), nil
}

// ExtractStreamInfo reads stream information from an already-open FormatContext.
// This is used by avdemux.Demuxer.StreamInfo() to avoid opening the URL twice.
func ExtractStreamInfo(fc *astiav.FormatContext) *StreamInfo {
	info := &StreamInfo{
		Container:  fc.InputFormat().Name(),
		DurationMs: fc.Duration() / 1000,
		IsLive:     fc.Duration() <= 0,
	}

	for _, s := range fc.Streams() {
		cp := s.CodecParameters()
		// Skip streams with undetected codecs (DVB teletext, unknown streams).
		if cp.CodecID() == astiav.CodecIDNone {
			continue
		}
		switch cp.MediaType() {
		case astiav.MediaTypeVideo:
			if info.Video != nil {
				continue // keep the first video stream
			}
			fr := s.RFrameRate()
			ext := cp.ExtraData()
			info.Video = &VideoStream{
				Index:       s.Index(),
				Codec:       cp.CodecID().String(),
				Width:       cp.Width(),
				Height:      cp.Height(),
				Interlaced:  detectInterlaced(cp.CodecID().String(), ext),
				BitDepth:    bitDepthFromPixelFormat(cp.PixelFormat()),
				FramerateN:  fr.Num(),
				FramerateD:  fr.Den(),
				BitrateKbps: int(cp.BitRate() / 1000),
				Extradata:   ext,
			}
		case astiav.MediaTypeAudio:
			info.AudioTracks = append(info.AudioTracks, AudioTrack{
				Index:       s.Index(),
				Codec:       cp.CodecID().String(),
				Channels:    cp.ChannelLayout().Channels(),
				SampleRate:  cp.SampleRate(),
				Language:    metadataValue(s.Metadata(), "language"),
				IsAD:        s.DispositionFlags().Has(astiav.DispositionFlagVisualImpaired),
				BitrateKbps: int(cp.BitRate() / 1000),
			})
		case astiav.MediaTypeSubtitle:
			info.SubTracks = append(info.SubTracks, SubtitleTrack{
				Index:    s.Index(),
				Codec:    cp.CodecID().String(),
				Language: metadataValue(s.Metadata(), "language"),
			})
		}
	}

	return info
}

// detectInterlaced checks if a video stream is interlaced by parsing the SPS.
// For H264, parses frame_mbs_only_flag from the SPS NAL unit.
// Returns false if SPS parsing fails or codec is not H264.
func detectInterlaced(codec string, ext []byte) bool {
	if codec != "h264" || len(ext) == 0 {
		return false
	}
	// ExtraData may already be in avcC format (starts with 0x01).
	// In that case, we need to extract the SPS from the avcC structure.
	// If it's Annex B, splitNALUnits will find the SPS.
	var spsData []byte
	if ext[0] == 0x01 && len(ext) > 8 {
		// avcC format: skip to SPS
		numSPS := int(ext[5] & 0x1F)
		if numSPS > 0 && len(ext) > 8 {
			spsLen := int(ext[6])<<8 | int(ext[7])
			if len(ext) >= 8+spsLen {
				spsData = ext[8 : 8+spsLen]
			}
		}
	} else {
		// Annex B: use extradata package to split NALs
		nalus := extradata.SplitNALUnits(ext)
		for _, nalu := range nalus {
			if len(nalu) > 0 && (nalu[0]&0x1F) == 7 {
				spsData = nalu
				break
			}
		}
	}
	if spsData == nil {
		return false
	}
	info := extradata.ParseH264SPS(spsData)
	if info == nil {
		return false
	}
	return !info.FrameMBSOnlyFlag
}

// bitDepthRe matches a bit depth number in pixel format names like "yuv420p10le".
var bitDepthRe = regexp.MustCompile(`(\d+)(le|be)?$`)

// bitDepthFromPixelFormat derives bit depth from the pixel format descriptor name.
// Common patterns: "yuv420p" → 8, "yuv420p10le" → 10, "yuv444p12be" → 12.
func bitDepthFromPixelFormat(pf astiav.PixelFormat) int {
	desc := pf.Descriptor()
	if desc == nil {
		return 8
	}
	name := desc.Name()
	if m := bitDepthRe.FindStringSubmatch(name); m != nil {
		if bits, err := strconv.Atoi(m[1]); err == nil && bits > 8 && bits <= 16 {
			return bits
		}
	}
	return 8
}

// metadataValue safely reads a metadata key from a dictionary.
func metadataValue(d *astiav.Dictionary, key string) string {
	if d == nil {
		return ""
	}
	entry := d.Get(key, nil, 0)
	if entry == nil {
		return ""
	}
	return entry.Value()
}
