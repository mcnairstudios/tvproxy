package mux

import (
	"encoding/binary"
	"fmt"
)

// extractCodecString parses a fragmented MP4 init segment (ftyp+moov) and
// returns the codec string for the video track. For example:
//   - H.264: "avc1.640028" (profile=High, level=4.0)
//   - H.265: "hev1.1.6.L120.B0" (Main profile, level 4.0)
//
// Returns "" if the init segment doesn't contain a recognisable video codec.
func extractCodecString(initSegment []byte) string {
	// Walk the moov box tree: moov → trak → mdia → minf → stbl → stsd → (avc1|hev1|hvc1)
	moov := findBox(initSegment, "moov")
	if moov == nil {
		return ""
	}
	trak := findBox(moov, "trak")
	if trak == nil {
		return ""
	}
	mdia := findBox(trak, "mdia")
	if mdia == nil {
		return ""
	}
	minf := findBox(mdia, "minf")
	if minf == nil {
		return ""
	}
	stbl := findBox(minf, "stbl")
	if stbl == nil {
		return ""
	}
	stsd := findBox(stbl, "stsd")
	if stsd == nil {
		return ""
	}

	// stsd has an 8-byte header (version + flags + entry_count) before the sample entries.
	if len(stsd) < 8 {
		return ""
	}
	entries := stsd[8:]

	// Try avc1 (H.264)
	if avc1 := findBox(entries, "avc1"); avc1 != nil {
		return parseAVC1CodecString(avc1)
	}
	// Try hev1 (H.265)
	if hev1 := findBox(entries, "hev1"); hev1 != nil {
		return parseHEVCCodecString(hev1, "hev1")
	}
	// Try hvc1 (H.265 alternate)
	if hvc1 := findBox(entries, "hvc1"); hvc1 != nil {
		return parseHEVCCodecString(hvc1, "hvc1")
	}

	return ""
}

// parseAVC1CodecString extracts the codec string from an avc1 sample entry.
// Format: avc1.PPCCLL where PP=profile, CC=constraints, LL=level (all hex).
// visualSampleEntrySize is the fixed-size header of a VisualSampleEntry
// (6 reserved + 2 data_ref_index + 2 pre_defined + 2 reserved + 12 pre_defined
// + 2 width + 2 height + 4 horiz_res + 4 vert_res + 4 reserved + 2 frame_count
// + 32 compressorname + 2 depth + 2 pre_defined = 78 bytes).
const visualSampleEntrySize = 78

func parseAVC1CodecString(avc1 []byte) string {
	if len(avc1) < visualSampleEntrySize+8 {
		return ""
	}
	avcC := findBox(avc1[visualSampleEntrySize:], "avcC")
	if avcC == nil || len(avcC) < 4 {
		return ""
	}
	profile := avcC[1]
	compat := avcC[2]
	level := avcC[3]
	return fmt.Sprintf("avc1.%02X%02X%02X", profile, compat, level)
}

func parseHEVCCodecString(entry []byte, tag string) string {
	if len(entry) < visualSampleEntrySize+8 {
		return ""
	}
	hvcC := findBox(entry[visualSampleEntrySize:], "hvcC")
	if hvcC == nil || len(hvcC) < 13 {
		return ""
	}
	// HEVCDecoderConfigurationRecord:
	// byte 0: configurationVersion (must be 1)
	// byte 1: general_profile_space(2) | general_tier_flag(1) | general_profile_idc(5)
	// bytes 2-5: general_profile_compatibility_flags
	// bytes 6-11: general_constraint_indicator_flags
	// byte 12: general_level_idc
	if hvcC[0] != 1 {
		return ""
	}
	profileSpace := (hvcC[1] >> 6) & 0x03
	tierFlag := (hvcC[1] >> 5) & 0x01
	profileIDC := hvcC[1] & 0x1F
	profileCompat := binary.BigEndian.Uint32(hvcC[2:6])
	levelIDC := hvcC[12]

	// Build constraint bytes string (6 bytes, reversed, trimmed trailing zeros)
	var constraints [6]byte
	copy(constraints[:], hvcC[6:12])

	// Profile space prefix
	var spacePrefix string
	if profileSpace > 0 {
		spacePrefix = string(rune('A' + profileSpace - 1))
	}

	// Tier
	tier := "L"
	if tierFlag == 1 {
		tier = "H"
	}

	// Constraint bytes as hex, reversed, trimmed
	constraintStr := ""
	for i := 5; i >= 0; i-- {
		if constraints[i] != 0 || constraintStr != "" {
			if constraintStr != "" {
				constraintStr = fmt.Sprintf("%02X.", constraints[i]) + constraintStr
			} else {
				constraintStr = fmt.Sprintf("%02X", constraints[i])
			}
		}
	}
	if constraintStr == "" {
		constraintStr = "B0" // default
	}

	// Profile compatibility as hex (use only first 32 bits)
	_ = profileCompat

	return fmt.Sprintf("%s.%s%d.%s%d.%s", tag, spacePrefix, profileIDC, tier, levelIDC, constraintStr)
}

// findBox searches for a box with the given 4CC type in data.
// Returns the box content (excluding the 8-byte header), or nil if not found.
// Handles both regular (32-bit size) and extended (64-bit size) boxes.
func findBox(data []byte, boxType string) []byte {
	offset := 0
	for offset+8 <= len(data) {
		size := int(binary.BigEndian.Uint32(data[offset : offset+4]))
		fourcc := string(data[offset+4 : offset+8])

		headerSize := 8
		if size == 1 && offset+16 <= len(data) {
			extSize := binary.BigEndian.Uint64(data[offset+8 : offset+16])
			if extSize > uint64(len(data)-offset) {
				size = len(data) - offset
			} else {
				size = int(extSize)
			}
			headerSize = 16
		}
		if size < headerSize {
			break // invalid box
		}
		if size > len(data)-offset {
			size = len(data) - offset // truncated, use what we have
		}

		if fourcc == boxType {
			return data[offset+headerSize : offset+size]
		}

		offset += size
	}
	return nil
}
