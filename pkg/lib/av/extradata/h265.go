package extradata

import "fmt"

const (
	h265NALTypeVPS = 32
	h265NALTypeSPS = 33
	h265NALTypePPS = 34
)

// h265ToHvcC converts H265 extradata to HEVCDecoderConfigurationRecord (hvcC) format.
// If the data already starts with configurationVersion=1, it is returned as-is.
func h265ToHvcC(extradata []byte) ([]byte, error) {
	if len(extradata) == 0 {
		return nil, nil
	}

	// Already in hvcC format.
	if extradata[0] == 0x01 {
		out := make([]byte, len(extradata))
		copy(out, extradata)
		return out, nil
	}

	nalus := SplitNALUnits(extradata)
	if len(nalus) == 0 {
		return nil, fmt.Errorf("extradata: h265: no NAL units found in Annex B data")
	}

	var vpsList, spsList, ppsList [][]byte
	for _, nalu := range nalus {
		if len(nalu) < 2 {
			continue
		}
		nalType := (nalu[0] >> 1) & 0x3F
		switch nalType {
		case h265NALTypeVPS:
			vpsList = append(vpsList, nalu)
		case h265NALTypeSPS:
			spsList = append(spsList, nalu)
		case h265NALTypePPS:
			ppsList = append(ppsList, nalu)
		}
	}

	if len(spsList) == 0 {
		return nil, errNoSPS
	}
	if len(ppsList) == 0 {
		return nil, errNoPPS
	}

	sps := spsList[0]

	// Extract profile/tier/level from SPS.
	// HEVC SPS NAL structure after the 2-byte NAL header:
	//   4 bits: sps_video_parameter_set_id
	//   3 bits: sps_max_sub_layers_minus1
	//   1 bit:  sps_temporal_id_nesting_flag
	//   then profile_tier_level(1, sps_max_sub_layers_minus1):
	//     2 bits: general_profile_space
	//     1 bit:  general_tier_flag
	//     5 bits: general_profile_idc
	//     32 bits: general_profile_compatibility_flags
	//     48 bits: general_constraint_indicator_flags
	//     8 bits: general_level_idc
	//
	// The profile_tier_level starts at byte offset 2 (after NAL header) at bit 8.
	// So byte 2 has the sps header byte, and bytes 3..15 have profile_tier_level.

	var profileSpace, tierFlag, profileIDC byte
	var profileCompat [4]byte
	var constraintFlags [6]byte
	var levelIDC byte

	if len(sps) >= 15 {
		// Byte 2: sps_video_parameter_set_id(4) | sps_max_sub_layers_minus1(3) | temporal_id_nesting(1)
		// Byte 3: general_profile_space(2) | general_tier_flag(1) | general_profile_idc(5)
		ptlByte := sps[3]
		profileSpace = (ptlByte >> 6) & 0x03
		tierFlag = (ptlByte >> 5) & 0x01
		profileIDC = ptlByte & 0x1F

		copy(profileCompat[:], sps[4:8])
		copy(constraintFlags[:], sps[8:14])
		levelIDC = sps[14]
	}

	chromaFormat := byte(1)
	bitDepthLumaMinus8 := byte(0)
	bitDepthChromaMinus8 := byte(0)
	if parsed := ParseH265SPS(sps); parsed != nil {
		chromaFormat = byte(parsed.ChromaFormatIDC)
		bitDepthLumaMinus8 = byte(parsed.BitDepthLuma - 8)
		bitDepthChromaMinus8 = byte(parsed.BitDepthChroma - 8)
	}

	// Count how many NAL type arrays we have.
	var numArrays byte
	if len(vpsList) > 0 {
		numArrays++
	}
	if len(spsList) > 0 {
		numArrays++
	}
	if len(ppsList) > 0 {
		numArrays++
	}

	// Calculate output size: 23-byte header + arrays.
	size := 23
	type naluArray struct {
		nalType byte
		nalus   [][]byte
	}
	arrays := []naluArray{}
	if len(vpsList) > 0 {
		arrays = append(arrays, naluArray{h265NALTypeVPS, vpsList})
	}
	arrays = append(arrays, naluArray{h265NALTypeSPS, spsList})
	arrays = append(arrays, naluArray{h265NALTypePPS, ppsList})

	for _, arr := range arrays {
		size += 3 // type byte + numNalus (2 bytes)
		for _, nalu := range arr.nalus {
			size += 2 + len(nalu)
		}
	}

	out := make([]byte, size)
	i := 0

	out[i] = 0x01 // configurationVersion
	i++
	out[i] = (profileSpace << 6) | (tierFlag << 5) | profileIDC
	i++
	copy(out[i:i+4], profileCompat[:])
	i += 4
	copy(out[i:i+6], constraintFlags[:])
	i += 6
	out[i] = levelIDC
	i++
	// min_spatial_segmentation_idc = 0, with reserved bits
	out[i] = 0xF0
	out[i+1] = 0x00
	i += 2
	out[i] = 0xFC // parallelismType = 0, reserved
	i++
	out[i] = 0xFC | (chromaFormat & 0x03)
	i++
	out[i] = 0xF8 | (bitDepthLumaMinus8 & 0x07)
	i++
	out[i] = 0xF8 | (bitDepthChromaMinus8 & 0x07)
	i++
	// avgFrameRate = 0
	out[i] = 0x00
	out[i+1] = 0x00
	i += 2
	// constantFrameRate(2)=0 | numTemporalLayers(3)=0 | temporalIdNested(1)=1 | lengthSizeMinusOne(2)=3
	// = 0b00_000_1_11 = 0x07
	// Actually let's use the value from the spec suggestion: 0x0F would be
	// numTemporalLayers=1, temporalIdNested=1, lengthSizeMinusOne=3
	// 0b00_001_1_11 = 0x0F
	out[i] = 0x0F
	i++
	out[i] = byte(len(arrays))
	i++

	for _, arr := range arrays {
		// array_completeness=1, reserved=0, NAL_unit_type
		out[i] = 0x80 | (arr.nalType & 0x3F)
		i++
		putU16BE(out[i:], uint16(len(arr.nalus)))
		i += 2
		for _, nalu := range arr.nalus {
			putU16BE(out[i:], uint16(len(nalu)))
			i += 2
			copy(out[i:], nalu)
			i += len(nalu)
		}
	}

	if i != size {
		return nil, fmt.Errorf("extradata: h265: size mismatch: wrote %d, expected %d", i, size)
	}

	return out, nil
}
