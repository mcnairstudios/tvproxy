package fmp4

import (
	"bytes"
	"log"

	"github.com/Eyevinn/mp4ff/av1"
	"github.com/Eyevinn/mp4ff/mp4"
)

const (
	obuSequenceHeader    = 1
	obuTemporalDelimiter = 2
	obuFrameHeader       = 3
	obuFrame             = 6
)

func extractAV1SequenceHeader(data []byte) []byte {
	for i := 0; i < len(data); {
		if i >= len(data) {
			break
		}
		header := data[i]
		obuType := (header >> 3) & 0x0F
		hasExtension := (header >> 2) & 1
		hasSizeField := (header >> 1) & 1

		hdrSize := 1
		if hasExtension == 1 {
			hdrSize = 2
		}
		if i+hdrSize > len(data) {
			break
		}

		if hasSizeField == 0 {
			break
		}

		size, sizeLen := readLEB128(data[i+hdrSize:])
		if sizeLen == 0 {
			break
		}

		obuStart := i
		obuEnd := i + hdrSize + sizeLen + int(size)
		if obuEnd > len(data) {
			break
		}

		if obuType == obuSequenceHeader {
			obu := make([]byte, obuEnd-obuStart)
			copy(obu, data[obuStart:obuEnd])
			return obu
		}

		i = obuEnd
	}
	return nil
}

func stripAV1TemporalDelimiter(data []byte) []byte {
	result := make([]byte, 0, len(data))
	for i := 0; i < len(data); {
		if i >= len(data) {
			break
		}
		header := data[i]
		obuType := (header >> 3) & 0x0F
		hasSizeField := (header >> 1) & 1
		hasExtension := (header >> 2) & 1

		hdrSize := 1
		if hasExtension == 1 {
			hdrSize = 2
		}

		if hasSizeField == 0 {
			result = append(result, data[i:]...)
			break
		}

		size, sizeLen := readLEB128(data[i+hdrSize:])
		if sizeLen == 0 {
			break
		}

		obuEnd := i + hdrSize + sizeLen + int(size)
		if obuEnd > len(data) {
			break
		}

		if obuType != obuTemporalDelimiter {
			result = append(result, data[i:obuEnd]...)
		}

		i = obuEnd
	}
	if len(result) == 0 {
		return data
	}
	return result
}

func IsAV1Keyframe(data []byte) bool {
	for i := 0; i < len(data); {
		if i >= len(data) {
			break
		}
		header := data[i]
		obuType := (header >> 3) & 0x0F
		hasExtension := (header >> 2) & 1
		hasSizeField := (header >> 1) & 1

		hdrSize := 1
		if hasExtension == 1 {
			hdrSize = 2
		}

		if hasSizeField == 0 {
			break
		}

		size, sizeLen := readLEB128(data[i+hdrSize:])
		if sizeLen == 0 {
			break
		}

		payloadStart := i + hdrSize + sizeLen
		obuEnd := payloadStart + int(size)
		if obuEnd > len(data) {
			break
		}

		if (obuType == obuFrame || obuType == obuFrameHeader) && size > 0 {
			fbr := &bitReader{data: data[payloadStart:], pos: 0}
			showExistingFrame := fbr.readBit()
			if showExistingFrame == 0 {
				frameType := fbr.readBits(2)
				if frameType == 0 {
					return true
				}
			}
		}

		i = obuEnd
	}
	return false
}

func readLEB128(data []byte) (uint64, int) {
	var val uint64
	for i := 0; i < len(data) && i < 8; i++ {
		val |= uint64(data[i]&0x7F) << (7 * uint(i))
		if data[i]&0x80 == 0 {
			return val, i + 1
		}
	}
	return 0, 0
}

type bitReader struct {
	data []byte
	pos  int
}

func (b *bitReader) readBits(n int) uint32 {
	var val uint32
	for i := 0; i < n; i++ {
		byteIdx := b.pos / 8
		bitIdx := 7 - (b.pos % 8)
		if byteIdx >= len(b.data) {
			return val
		}
		val = (val << 1) | uint32((b.data[byteIdx]>>uint(bitIdx))&1)
		b.pos++
	}
	return val
}

func (b *bitReader) readBit() uint32 {
	return b.readBits(1)
}

func parseAV1SequenceHeader(seqHdr []byte) (profile, level, tier, bitDepth, mono, chromaX, chromaY, chromaPos byte) {
	header := seqHdr[0]
	hasExtension := (header >> 2) & 1
	hdrSize := 1
	if hasExtension == 1 {
		hdrSize = 2
	}
	_, sizeLen := readLEB128(seqHdr[hdrSize:])
	payload := seqHdr[hdrSize+sizeLen:]

	if len(payload) < 2 {
		return 0, 5, 0, 8, 0, 1, 1, 0
	}

	br := &bitReader{data: payload, pos: 0}

	profile = byte(br.readBits(3))
	br.readBit() // still_picture
	reducedStillPicture := br.readBit()

	// Level/tier parsing requires traversing many conditional fields (timing_info,
	// decoder_model_info, operating_points). Rather than risk bit drift, use the
	// configOBUs in av1C — the decoder reads the real values from there.
	// Set level based on profile for a safe default.
	level = 13 // 5.1 — covers 1080p60 and 4K30
	tier = 0   // Main tier

	if reducedStillPicture == 1 {
		level = byte(br.readBits(5))
	}

	bitDepth = 8
	mono = 0
	chromaX = 1
	chromaY = 1
	chromaPos = 0
	if profile >= 2 {
		bitDepth = 10
	}

	return
}

func buildAV1Init(trackID uint32, timescale uint32, seqHdr []byte) []byte {
	if len(seqHdr) == 0 {
		return nil
	}

	profile, level, tier, bitDepth, mono, chromaX, chromaY, chromaPos := parseAV1SequenceHeader(seqHdr)

	highBitdepth := byte(0)
	twelveBit := byte(0)
	if bitDepth > 8 {
		highBitdepth = 1
	}
	if bitDepth == 12 {
		twelveBit = 1
	}

	confRec := av1.CodecConfRec{
		Version:                 1,
		SeqProfile:              profile,
		SeqLevelIdx0:            level,
		SeqTier0:                tier,
		HighBitdepth:            highBitdepth,
		TwelveBit:               twelveBit,
		MonoChrome:              mono,
		ChromaSubsamplingX:      chromaX,
		ChromaSubsamplingY:      chromaY,
		ChromaSamplePosition:    chromaPos,
		ConfigOBUs:              seqHdr,
	}

	av1CBox := &mp4.Av1CBox{CodecConfRec: confRec}
	av01Box := mp4.CreateVisualSampleEntryBox("av01", 1920, 1080, av1CBox)

	init := mp4.CreateEmptyInit()
	trak := init.AddEmptyTrack(timescale, "video", "und")
	stsd := trak.Mdia.Minf.Stbl.Stsd
	stsd.AddChild(av01Box)

	log.Printf("[video] AV1 init: profile=%d level=%d tier=%d bitDepth=%d", profile, level, tier, bitDepth)

	var buf bytes.Buffer
	if err := init.Encode(&buf); err != nil {
		log.Printf("[video] AV1 init encode error: %v", err)
		return nil
	}
	return buf.Bytes()
}

