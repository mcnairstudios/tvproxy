package extradata

// H264SPSInfo contains parsed H264 SPS fields relevant to probing.
type H264SPSInfo struct {
	ProfileIDC       uint8
	LevelIDC         uint8
	ChromaFormatIDC  uint32 // 0=mono, 1=4:2:0, 2=4:2:2, 3=4:4:4
	BitDepthLuma     uint32 // 8, 10, 12, etc.
	BitDepthChroma   uint32
	FrameMBSOnlyFlag bool   // false = may be interlaced
	Width            uint32 // in macroblocks
	Height           uint32 // in map units
}

// h264HighProfiles lists profiles that have chroma/bitdepth fields in the SPS.
var h264HighProfiles = map[uint8]bool{
	100: true, 110: true, 122: true, 244: true,
	44: true, 83: true, 86: true, 118: true,
	128: true, 138: true, 139: true, 134: true,
}

// ParseH264SPS parses an H264 SPS NAL unit (without start code, with NAL header byte).
// Returns nil if parsing fails (non-fatal — caller should use defaults).
func ParseH264SPS(sps []byte) *H264SPSInfo {
	// Minimum: NAL header (1) + profile (1) + constraint (1) + level (1) = 4 bytes
	if len(sps) < 4 {
		return nil
	}

	info := &H264SPSInfo{}
	info.ProfileIDC = sps[1]
	// sps[2] = constraint flags
	info.LevelIDC = sps[3]

	// Default chroma and bit depth for non-high profiles.
	info.ChromaFormatIDC = 1
	info.BitDepthLuma = 8
	info.BitDepthChroma = 8

	// Start reading exp-golomb fields after the fixed 4 bytes.
	br := newBitReader(sps[4:])

	// seq_parameter_set_id
	if _, err := br.readUE(); err != nil {
		return nil
	}

	if h264HighProfiles[info.ProfileIDC] {
		// chroma_format_idc
		chromaFmt, err := br.readUE()
		if err != nil {
			return nil
		}
		info.ChromaFormatIDC = chromaFmt

		if chromaFmt == 3 {
			// separate_colour_plane_flag
			if _, err := br.readBit(); err != nil {
				return nil
			}
		}

		// bit_depth_luma_minus8
		bdl, err := br.readUE()
		if err != nil {
			return nil
		}
		info.BitDepthLuma = bdl + 8

		// bit_depth_chroma_minus8
		bdc, err := br.readUE()
		if err != nil {
			return nil
		}
		info.BitDepthChroma = bdc + 8

		// qpprime_y_zero_transform_bypass_flag
		if _, err := br.readBit(); err != nil {
			return nil
		}

		// seq_scaling_matrix_present_flag
		scalingFlag, err := br.readBit()
		if err != nil {
			return nil
		}
		if scalingFlag == 1 {
			// Scaling matrix parsing is complex; return nil to let caller use defaults.
			return nil
		}
	}

	// log2_max_frame_num_minus4
	if _, err := br.readUE(); err != nil {
		return nil
	}

	// pic_order_cnt_type
	pocType, err := br.readUE()
	if err != nil {
		return nil
	}

	switch pocType {
	case 0:
		// log2_max_pic_order_cnt_lsb_minus4
		if _, err := br.readUE(); err != nil {
			return nil
		}
	case 1:
		// delta_pic_order_always_zero_flag
		if _, err := br.readBit(); err != nil {
			return nil
		}
		// offset_for_non_ref_pic
		if _, err := br.readSE(); err != nil {
			return nil
		}
		// offset_for_top_to_bottom_field
		if _, err := br.readSE(); err != nil {
			return nil
		}
		// num_ref_frames_in_pic_order_cnt_cycle
		numRefFrames, err := br.readUE()
		if err != nil {
			return nil
		}
		for i := uint32(0); i < numRefFrames; i++ {
			if _, err := br.readSE(); err != nil {
				return nil
			}
		}
	}

	// max_num_ref_frames
	if _, err := br.readUE(); err != nil {
		return nil
	}

	// gaps_in_frame_num_value_allowed_flag
	if _, err := br.readBit(); err != nil {
		return nil
	}

	// pic_width_in_mbs_minus1
	widthMbs, err := br.readUE()
	if err != nil {
		return nil
	}
	info.Width = widthMbs + 1

	// pic_height_in_map_units_minus1
	heightMap, err := br.readUE()
	if err != nil {
		return nil
	}
	info.Height = heightMap + 1

	// frame_mbs_only_flag
	fmo, err := br.readBit()
	if err != nil {
		return nil
	}
	info.FrameMBSOnlyFlag = fmo == 1

	return info
}

// H265SPSInfo contains parsed H265 SPS fields relevant to probing and hvcC.
type H265SPSInfo struct {
	ChromaFormatIDC uint32 // 0=mono, 1=4:2:0, 2=4:2:2, 3=4:4:4
	BitDepthLuma    uint32 // 8, 10, 12
	BitDepthChroma  uint32
	Width           uint32
	Height          uint32
}

// skipH265ProfileTierLevel skips the profile_tier_level structure in the bitstream.
// profilePresentFlag indicates whether profile info is present.
// maxNumSubLayersMinus1 controls variable-length sub-layer info.
func skipH265ProfileTierLevel(br *bitReader, profilePresentFlag bool, maxNumSubLayersMinus1 uint32) error {
	if profilePresentFlag {
		// general_profile_space(2) + general_tier_flag(1) + general_profile_idc(5)
		// + general_profile_compatibility_flags(32)
		// + general_progressive_source_flag(1) + general_interlaced_source_flag(1)
		// + general_non_packed_constraint_flag(1) + general_frame_only_constraint_flag(1)
		// + 44 bits of constraint flags
		// = 2+1+5+32+1+1+1+1+44 = 88 bits
		br.skip(88)
	}
	// general_level_idc u(8)
	br.skip(8)

	if maxNumSubLayersMinus1 > 0 {
		subLayerProfilePresent := make([]uint8, maxNumSubLayersMinus1)
		subLayerLevelPresent := make([]uint8, maxNumSubLayersMinus1)

		for i := uint32(0); i < maxNumSubLayersMinus1; i++ {
			pp, err := br.readBit()
			if err != nil {
				return err
			}
			subLayerProfilePresent[i] = pp

			lp, err := br.readBit()
			if err != nil {
				return err
			}
			subLayerLevelPresent[i] = lp
		}

		// reserved_zero_2bits for remaining slots up to 8
		if maxNumSubLayersMinus1 < 8 {
			br.skip(int(8-maxNumSubLayersMinus1) * 2)
		}

		for i := uint32(0); i < maxNumSubLayersMinus1; i++ {
			if subLayerProfilePresent[i] == 1 {
				// 88 bits of sub-layer profile info
				br.skip(88)
			}
			if subLayerLevelPresent[i] == 1 {
				// sub_layer_level_idc u(8)
				br.skip(8)
			}
		}
	}

	return nil
}

// ParseH265SPS parses an H265 SPS NAL unit (without start code, with NAL header).
// Returns nil if parsing fails.
func ParseH265SPS(sps []byte) *H265SPSInfo {
	// Minimum: NAL header (2) + at least a few bytes of SPS data
	if len(sps) < 4 {
		return nil
	}

	br := newBitReader(sps[2:])

	// sps_video_parameter_set_id u(4)
	if _, err := br.readBits(4); err != nil {
		return nil
	}

	// sps_max_sub_layers_minus1 u(3)
	maxSubLayers, err := br.readBits(3)
	if err != nil {
		return nil
	}

	// sps_temporal_id_nesting_flag u(1)
	if _, err := br.readBit(); err != nil {
		return nil
	}

	// profile_tier_level(1, sps_max_sub_layers_minus1)
	if err := skipH265ProfileTierLevel(br, true, maxSubLayers); err != nil {
		return nil
	}

	// sps_seq_parameter_set_id
	if _, err := br.readUE(); err != nil {
		return nil
	}

	// chroma_format_idc
	chromaFmt, err := br.readUE()
	if err != nil {
		return nil
	}

	if chromaFmt == 3 {
		// separate_colour_plane_flag
		if _, err := br.readBit(); err != nil {
			return nil
		}
	}

	// pic_width_in_luma_samples
	width, err := br.readUE()
	if err != nil {
		return nil
	}

	// pic_height_in_luma_samples
	height, err := br.readUE()
	if err != nil {
		return nil
	}

	// conformance_window_flag
	confWin, err := br.readBit()
	if err != nil {
		return nil
	}
	if confWin == 1 {
		// conf_win_left_offset, right, top, bottom — 4x ue(v)
		for i := 0; i < 4; i++ {
			if _, err := br.readUE(); err != nil {
				return nil
			}
		}
	}

	// bit_depth_luma_minus8
	bdl, err := br.readUE()
	if err != nil {
		return nil
	}

	// bit_depth_chroma_minus8
	bdc, err := br.readUE()
	if err != nil {
		return nil
	}

	return &H265SPSInfo{
		ChromaFormatIDC: chromaFmt,
		BitDepthLuma:    bdl + 8,
		BitDepthChroma:  bdc + 8,
		Width:           width,
		Height:          height,
	}
}
