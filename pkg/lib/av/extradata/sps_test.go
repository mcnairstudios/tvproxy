package extradata

import "testing"

func TestBitReader_ReadBits(t *testing.T) {
	// 0b10110001 0b01010101
	r := newBitReader([]byte{0b10110001, 0b01010101})

	// read 3 bits: 101 = 5
	v, err := r.readBits(3)
	if err != nil {
		t.Fatal(err)
	}
	if v != 5 {
		t.Errorf("readBits(3) = %d, want 5", v)
	}

	// read 5 bits: 10001 = 17
	v, err = r.readBits(5)
	if err != nil {
		t.Fatal(err)
	}
	if v != 17 {
		t.Errorf("readBits(5) = %d, want 17", v)
	}

	// read 8 bits: 01010101 = 85
	v, err = r.readBits(8)
	if err != nil {
		t.Fatal(err)
	}
	if v != 85 {
		t.Errorf("readBits(8) = %d, want 85", v)
	}

	// reading past end should fail
	_, err = r.readBits(1)
	if err == nil {
		t.Error("expected error reading past end")
	}
}

func TestBitReader_ReadBit(t *testing.T) {
	r := newBitReader([]byte{0b11000000})
	b, _ := r.readBit()
	if b != 1 {
		t.Errorf("bit 0 = %d, want 1", b)
	}
	b, _ = r.readBit()
	if b != 1 {
		t.Errorf("bit 1 = %d, want 1", b)
	}
	b, _ = r.readBit()
	if b != 0 {
		t.Errorf("bit 2 = %d, want 0", b)
	}
}

func TestReadUE(t *testing.T) {
	// Exp-golomb test vectors packed into a bitstream:
	// 0 → 1                 (1 bit)
	// 1 → 010               (3 bits)
	// 2 → 011               (3 bits)
	// 3 → 00100             (5 bits)
	// 4 → 00101             (5 bits)
	// 5 → 00110             (5 bits)
	// 6 → 00111             (5 bits)
	// 7 → 0001000           (7 bits)
	// 9 → 0001010           (7 bits)
	//
	// Total: 1+3+3+5+5+5+5+7+7 = 41 bits → 6 bytes (pad with zeros)
	//
	// Bitstream: 1 010 011 00100 00101 00110 00111 0001000 0001010 000
	// Group:     1 010 011 0010000101001100011100010000001010000
	// Let me lay it out byte by byte:
	// 1_010_011_0 = 0b10100110 = 0xA6
	// 0100_0010 = 0b01000010  = 0x42
	// 1_00110_00 = 0b10011000 = 0x98
	// 111_00010 = 0b11100010  = 0xE2
	// 00_00010 1 = 0b00000101 = 0x05
	// 0_0000000 = 0b00000000  = 0x00

	data := []byte{0xA6, 0x42, 0x98, 0xE2, 0x05, 0x00}
	r := newBitReader(data)

	expected := []uint32{0, 1, 2, 3, 4, 5, 6, 7, 9}
	for _, want := range expected {
		got, err := r.readUE()
		if err != nil {
			t.Fatalf("readUE() for value %d: %v", want, err)
		}
		if got != want {
			t.Errorf("readUE() = %d, want %d", got, want)
		}
	}
}

func TestReadSE(t *testing.T) {
	// Signed exp-golomb: k = readUE(); even → -(k/2), odd → (k+1)/2
	// SE value 0 → UE 0 → 1 (1 bit)
	// SE value 1 → UE 1 → 010 (3 bits)
	// SE value -1 → UE 2 → 011 (3 bits)
	// SE value 2 → UE 3 → 00100 (5 bits)
	// SE value -2 → UE 4 → 00101 (5 bits)
	//
	// Total: 1+3+3+5+5 = 17 bits → 3 bytes
	// Bitstream: 1_010_011_0 0100_0010 1_0000000
	// = 0xA6 0x42 0x80

	data := []byte{0xA6, 0x42, 0x80}
	r := newBitReader(data)

	type tc struct {
		want int32
	}
	cases := []tc{{0}, {1}, {-1}, {2}, {-2}}
	for _, c := range cases {
		got, err := r.readSE()
		if err != nil {
			t.Fatalf("readSE() for value %d: %v", c.want, err)
		}
		if got != c.want {
			t.Errorf("readSE() = %d, want %d", got, c.want)
		}
	}
}

func TestParseH264SPS_Progressive(t *testing.T) {
	// Build an H264 SPS NAL for High profile (100), level 4.0, progressive, 1920x1080.
	//
	// Fixed bytes: 0x67 (NAL header), 0x64 (profile=100), 0x00 (constraints), 0x28 (level=40)
	//
	// Then exp-golomb encoded fields (starting at byte 4):
	//   seq_parameter_set_id = 0 → 1                        1 bit
	//   (High profile fields):
	//     chroma_format_idc = 1 → 010                       3 bits
	//     bit_depth_luma_minus8 = 0 → 1                     1 bit
	//     bit_depth_chroma_minus8 = 0 → 1                   1 bit
	//     qpprime_y_zero = 0 → u(1)                         1 bit
	//     seq_scaling_matrix = 0 → u(1)                     1 bit
	//   log2_max_frame_num_minus4 = 0 → 1                   1 bit
	//   pic_order_cnt_type = 0 → 1                          1 bit
	//     log2_max_pic_order_cnt_lsb_minus4 = 0 → 1         1 bit
	//   max_num_ref_frames = 4 → 00101                      5 bits
	//   gaps_in_frame_num = 0 → u(1)                        1 bit
	//   pic_width_in_mbs_minus1 = 119 → ue(119)             ?
	//   pic_height_in_map_units_minus1 = 67 → ue(67)        ?
	//   frame_mbs_only_flag = 1 → u(1)                      1 bit
	//
	// ue(119): 119+1=120, 120 in binary = 1111000, 7 bits, leadingZeros=6
	//   → 000000 1 1111000 = 0000001111000 (13 bits)
	// Wait: leadingZeros = floor(log2(120)) = 6, so prefix = 6 zeros + 1 = 7 bits,
	// suffix = 120 - 2^6 = 120 - 64 = 56 in 6 bits = 111000
	// → 0000001 111000 (13 bits)
	//
	// ue(67): 67+1=68, floor(log2(68))=6, prefix=0000001, suffix=68-64=4=000100
	// → 0000001 000100 (13 bits)
	//
	// Bitstream after fixed 4 bytes:
	// 1 010 1 1 0 0 1 1 1 00101 0 0000001111000 0000001000100 1
	//
	// Let me pack this:
	// Pos 0:  1           = 1
	// Pos 1:  010         = 010
	// Pos 4:  1           = 1
	// Pos 5:  1           = 1
	// Pos 6:  0           = 0
	// Pos 7:  0           = 0
	// byte 0: 1_010_1_1_0_0 = 0b10101100 = 0xAC
	//
	// Pos 8:  1           = 1
	// Pos 9:  1           = 1
	// Pos 10: 1           = 1
	// Pos 11: 00101       = 00101
	// byte 1: 1_1_1_00101 = 0b11100101 = 0xE5
	//
	// Pos 16: 0           = 0 (gaps_in_frame_num)
	// Pos 17: 0000001111000 (ue 119, 13 bits)
	// byte 2: 0_0000001 = 0b00000001 = 0x01
	//
	// Pos 24: 111000 (rest of ue 119)
	// then: 0000001000100 (ue 67, 13 bits)
	// byte 3: 111000_00 = 0b11100000 = 0xE0
	//
	// Pos 30: 00001000100 (rest of ue 67, 11 bits)
	// byte 4: 00001_000 = 0b00001000 = 0x08  (wait, let me recount)
	//
	// Let me be more careful. Starting from bit 0 after the 4 fixed bytes:
	// bit 0: 1                       (sps_id=0)
	// bit 1-3: 010                   (chroma=1)
	// bit 4: 1                       (bd_luma=0)
	// bit 5: 1                       (bd_chroma=0)
	// bit 6: 0                       (qpprime)
	// bit 7: 0                       (scaling_matrix)
	// bit 8: 1                       (log2_max_frame_num=0)
	// bit 9: 1                       (poc_type=0)
	// bit 10: 1                      (log2_max_poc_lsb=0)
	// bit 11-15: 00101               (max_ref_frames=4)
	// bit 16: 0                      (gaps)
	// bit 17-29: 0000001111000       (width_mbs=119)
	// bit 30-42: 0000001000100       (height_map=67)
	// bit 43: 1                      (frame_mbs_only=1)
	//
	// Bytes:
	// bits 0-7:   1_010_1_1_0_0       = 0xAC
	// bits 8-15:  1_1_1_00101          = 0xE5
	// bits 16-23: 0_0000001            = 0x01
	// bits 24-31: 111000_00            wait... bit 17-29 is "0000001111000"
	//   bit 17=0, 18=0, 19=0, 20=0, 21=0, 22=0, 23=1
	//   bit 24=1, 25=1, 26=1, 27=0, 28=0, 29=0
	// bits 24-31: 1_1_1_0_0_0 + bit30-31 of next
	//   bit 30-42 = 0000001000100
	//   bit 30=0, 31=0
	// bits 24-31: 1_1_1_0_0_0_0_0     = 0xE0
	// bits 32-39: 00001_000            wait: bit 32=0, 33=0, 34=0, 35=0, 36=1, 37=0, 38=0, 39=0
	// = 0b00001000 = 0x08
	// bits 40-47: 100_1_0000           bit 40=1, 41=0, 42=0, 43=1, then pad
	// = 0b10010000 = 0x90

	sps := []byte{
		0x67,                                     // NAL header
		0x64,                                     // profile_idc = 100
		0x00,                                     // constraint flags
		0x28,                                     // level_idc = 40
		0xAC, 0xE5, 0x01, 0xE0, 0x08, 0x90, // encoded SPS fields
	}

	info := ParseH264SPS(sps)
	if info == nil {
		t.Fatal("ParseH264SPS returned nil")
	}

	if info.ProfileIDC != 100 {
		t.Errorf("ProfileIDC = %d, want 100", info.ProfileIDC)
	}
	if info.LevelIDC != 40 {
		t.Errorf("LevelIDC = %d, want 40", info.LevelIDC)
	}
	if info.ChromaFormatIDC != 1 {
		t.Errorf("ChromaFormatIDC = %d, want 1", info.ChromaFormatIDC)
	}
	if info.BitDepthLuma != 8 {
		t.Errorf("BitDepthLuma = %d, want 8", info.BitDepthLuma)
	}
	if info.BitDepthChroma != 8 {
		t.Errorf("BitDepthChroma = %d, want 8", info.BitDepthChroma)
	}
	if !info.FrameMBSOnlyFlag {
		t.Error("FrameMBSOnlyFlag = false, want true (progressive)")
	}
	if info.Width != 120 {
		t.Errorf("Width = %d macroblocks, want 120 (1920px)", info.Width)
	}
	if info.Height != 68 {
		t.Errorf("Height = %d map units, want 68 (1088px)", info.Height)
	}
}

func TestParseH264SPS_Interlaced(t *testing.T) {
	// Same as progressive test but with frame_mbs_only_flag = 0.
	// Change bit 43 from 1 to 0.
	// Original last byte: 0x90 = 0b10010000
	// bit 43 is the 4th bit of the last byte (bit 43 mod 8 = 3, so bit index 3 from MSB)
	// Actually bit 43 in the stream starting from byte 4:
	// bits 40-47 are byte index 5 (0-indexed from start of exp-golomb data)
	// bit 40=1, 41=0, 42=0, 43=1 → change 43 to 0
	// 0b10000000 = 0x80

	sps := []byte{
		0x67,
		0x64,
		0x00,
		0x28,
		0xAC, 0xE5, 0x01, 0xE0, 0x08, 0x80,
	}

	info := ParseH264SPS(sps)
	if info == nil {
		t.Fatal("ParseH264SPS returned nil")
	}

	if info.FrameMBSOnlyFlag {
		t.Error("FrameMBSOnlyFlag = true, want false (interlaced)")
	}
}

func TestParseH264SPS_Baseline(t *testing.T) {
	// Baseline profile (66) does not have chroma/bitdepth fields.
	// Build: NAL=0x67, profile=66 (0x42), constraints=0xC0, level=30 (0x1E)
	//
	// Fields after fixed bytes (no high-profile chroma/bd):
	// bit 0: 1                       (sps_id=0)
	// bit 1: 1                       (log2_max_frame_num=0)
	// bit 2: 1                       (poc_type=0)
	// bit 3: 1                       (log2_max_poc_lsb=0)
	// bit 4-6: 010                   (max_ref_frames=1)
	// bit 7: 0                       (gaps)
	// bit 8-18: 00000101100          (width_mbs=19 → 320px) = ue(19): 20=10100, 5 bits, lz=4
	//   → 00001 0100 (9 bits)
	// Wait: ue(19): N=19, N+1=20, floor(log2(20))=4, lz=4
	//   prefix: 00001, suffix: 20-16=4=0100 → 00001_0100 (9 bits)
	// bit 8-16: 000010100
	// bit 17-25: ue(14): N=14, N+1=15, floor(log2(15))=3, lz=3
	//   prefix: 0001, suffix: 15-8=7=111 → 0001_111 (7 bits)
	// bit 17-23: 0001111
	// bit 24: 1                      (frame_mbs_only=1)
	//
	// Packed bits: 1 1 1 1 010 0 000010100 0001111 1
	// byte 0: 11110100 = 0xF4
	// byte 1: 00001010 = 0x0A
	// byte 2: 00001111 = 0x0F
	// byte 3: 10000000 = 0x80

	sps := []byte{
		0x67,
		0x42, // Baseline
		0xC0,
		0x1E, // level 30
		0xF4, 0x0A, 0x0F, 0x80,
	}

	info := ParseH264SPS(sps)
	if info == nil {
		t.Fatal("ParseH264SPS returned nil")
	}

	if info.ProfileIDC != 66 {
		t.Errorf("ProfileIDC = %d, want 66", info.ProfileIDC)
	}
	// Baseline defaults to chroma=1, bitdepth=8.
	if info.ChromaFormatIDC != 1 {
		t.Errorf("ChromaFormatIDC = %d, want 1", info.ChromaFormatIDC)
	}
	if info.BitDepthLuma != 8 {
		t.Errorf("BitDepthLuma = %d, want 8", info.BitDepthLuma)
	}
	if !info.FrameMBSOnlyFlag {
		t.Error("FrameMBSOnlyFlag = false, want true")
	}
	if info.Width != 20 {
		t.Errorf("Width = %d, want 20 (320px)", info.Width)
	}
	if info.Height != 15 {
		t.Errorf("Height = %d, want 15 (240px)", info.Height)
	}
}

func TestParseH264SPS_TooShort(t *testing.T) {
	result := ParseH264SPS([]byte{0x67, 0x64})
	if result != nil {
		t.Error("expected nil for too-short SPS")
	}

	result = ParseH264SPS(nil)
	if result != nil {
		t.Error("expected nil for nil SPS")
	}
}

func TestParseH265SPS(t *testing.T) {
	// Build an H265 SPS NAL with chroma_format_idc=1, bit_depth=10.
	//
	// NAL header: 2 bytes. For SPS (type 33): (33 << 1) = 0x42, second byte = 0x01
	//
	// After NAL header (byte 2 onward, read as bits):
	// sps_video_parameter_set_id: u(4) = 0 → 0000
	// sps_max_sub_layers_minus1: u(3) = 0 → 000
	// sps_temporal_id_nesting_flag: u(1) = 1 → 1
	//
	// profile_tier_level(1, 0):
	//   profilePresentFlag=1, maxNumSubLayersMinus1=0
	//   88 bits of profile data + 8 bits general_level_idc = 96 bits = 12 bytes
	//   (no sub-layer data since maxNumSubLayersMinus1=0)
	//   Let's use: all zeros for profile data (88 bits) + level=0 (8 bits)
	//   = 12 bytes of 0x00
	//
	// sps_seq_parameter_set_id: ue(0) = 1 → 1 bit
	// chroma_format_idc: ue(1) = 010 → 3 bits
	// pic_width_in_luma_samples: ue(1920-1)=ue(1919)
	//   Actually this is the raw value, not minus1. Let's use ue(1920):
	//   N=1920, N+1=1921, floor(log2(1921))=10, lz=10
	//   prefix: 10 zeros + 1 = 00000000001
	//   suffix: 1921-1024=897 in 10 bits = 1110000001
	//   → 00000000001_1110000001 (21 bits)
	// pic_height_in_luma_samples: ue(1080):
	//   N=1080, N+1=1081, floor(log2(1081))=10, lz=10
	//   prefix: 00000000001
	//   suffix: 1081-1024=57 in 10 bits = 0000111001
	//   → 00000000001_0000111001 (21 bits)
	// conformance_window_flag: u(1) = 0
	// bit_depth_luma_minus8: ue(2) = 011 → 3 bits (10-bit)
	// bit_depth_chroma_minus8: ue(2) = 011 → 3 bits
	//
	// Total after profile_tier_level:
	// 1 + 3 + 21 + 21 + 1 + 3 + 3 = 53 bits = 7 bytes (padded)
	//
	// Let me pack it:
	// Byte 0-1: NAL header = 0x42, 0x01
	// Byte 2: sps_vps_id(4)=0000, max_sub_layers(3)=000, temporal_nesting(1)=1 → 0b00000001 = 0x01
	// Bytes 3-14: profile_tier_level = 12 bytes of zeros
	// Then the remaining fields as bits:
	//
	// bit 0: 1                         (sps_seq_param_set_id = 0)
	// bit 1-3: 010                     (chroma_format_idc = 1)
	// bit 4-24: 00000000001_1110000001 (width = 1920)
	// bit 25-45: 00000000001_0000111001 (height = 1080)
	// bit 46: 0                        (conformance_window = 0)
	// bit 47-49: 011                   (bit_depth_luma_minus8 = 2)
	// bit 50-52: 011                   (bit_depth_chroma_minus8 = 2)
	//
	// Pack into bytes:
	// byte 0 (bits 0-7):  1_010_0000 = 0b10100000 = 0xA0
	// byte 1 (bits 8-15): 0000_0111 = 0b00000111 = 0x07
	// byte 2 (bits 16-23): 0000_01_00 = wait let me recount
	//
	// bit 4-24 = 00000000001_1110000001
	// bit 4: 0, 5:0, 6:0, 7:0
	// byte 0: 1_010_0000 = 0xA0
	//
	// bit 8:0, 9:0, 10:0, 11:0, 12:0, 13:0, 14:1, 15:1
	// byte 1: 00000011 = 0x03  wait
	//
	// Let me redo more carefully. bit4=0 bit5=0 bit6=0 bit7=0 bit8=0 bit9=0 bit10=0 bit11=0
	// bit12=0 bit13=0 bit14=1  ← that's the "1" in the middle of ue(1920)
	//
	// byte 0 (bits 0-7): 1 010 0000 = 0xA0
	// byte 1 (bits 8-15): 00000 0 01 = wait
	// bits 4-14: 0 0000 0000 01  → 11 bits for the prefix of ue(1920)
	//
	// Let me just be precise:
	// bit:  0  1  2  3  4  5  6  7  8  9 10 11 12 13 14 15
	// val:  1  0  1  0  0  0  0  0  0  0  0  0  0  0  1  1
	//
	// Wait. ue(1920): N+1=1921. Let me recalculate.
	// 2^10 = 1024, 2^11 = 2048. 1024 <= 1921 < 2048. So floor(log2(1921)) = 10.
	// leadingZeros = 10. Prefix = 10 zeros + 1 = "00000000001" (11 bits)
	// suffix = 1921 - 1024 = 897. In 10 bits: 897 = 0b1110000001
	// Full: 00000000001 1110000001 (21 bits)
	//
	// bit:  0  1  2  3  | 4  5  6  7  8  9 10 11 12 13 14 | 15 16 17 18 19 20 21 22 23 24
	// val:  1  0  1  0  | 0  0  0  0  0  0  0  0  0  0  1 | 1  1  1  0  0  0  0  0  0  1
	//
	// Then ue(1080): N+1=1081, floor(log2(1081))=10, lz=10
	// prefix: 00000000001, suffix: 1081-1024=57=0b0000111001
	// Full: 00000000001 0000111001 (21 bits)
	//
	// bit 25-45: 0 0000 0000 0 1 0000 1110 01
	//
	// bit:  25 26 27 28 29 30 31 | 32 33 34 35 36 37 38 39 | 40 41 42 43 44 45
	// val:   0  0  0  0  0  0  0 |  0  0  0  1  0  0  0  0 |  1  1  1  0  0  1
	//
	// bit 46: 0 (conformance_window)
	// bit 47-49: 011 (ue(2) = bit_depth_luma_minus8)
	// bit 50-52: 011 (ue(2) = bit_depth_chroma_minus8)
	//
	// Now pack into bytes from bit 0:
	// byte 0 (bits 0-7):   1 0 1 0 0 0 0 0 = 0xA0
	// byte 1 (bits 8-15):  0 0 0 0 0 0 1 1 = 0x03
	// byte 2 (bits 16-23): 1 1 0 0 0 0 0 1 = 0xC1
	// byte 3 (bits 24-31): 0 0 0 0 0 0 0 0 = wait, bit24=1
	//
	// Hmm, bit 24. From the width encoding: the last bit is at position 4+21-1 = 24. Yes bit 24 = 1 (the last bit of 897's binary: ...0001, last bit is 1).
	//
	// byte 3 (bits 24-31): 1 0 0 0 0 0 0 0 = 0x80
	// Wait bit 24 is the last bit of ue(1920), and bits 25-31 are the start of ue(1080).
	// bit 24=1, bit25=0, 26=0, 27=0, 28=0, 29=0, 30=0, 31=0
	// byte 3: 0b10000000 = 0x80
	//
	// byte 4 (bits 32-39): 0 0 1 0 0 0 0 1 = 0x21
	// Wait: bit32=0, 33=0, 34=0, 35=1. Hmm.
	// ue(1080) = 00000000001 0000111001
	// bit 25 = 0 (first of 10 leading zeros)
	// ...
	// bit 34 = 0 (10th zero)
	// bit 35 = 1 (the "1" separator)
	// bit 36-45 = 0000111001 (suffix)
	//
	// byte 4 (bits 32-39): bit32=0, 33=0, 34=0, 35=1, 36=0, 37=0, 38=0, 39=0
	// = 0b00010000 = 0x10
	//
	// byte 5 (bits 40-47): bit40=1, 41=1, 42=1, 43=0, 44=0, 45=1, 46=0, 47=0
	// = 0b11100100 = 0xE4
	// Wait bit 46 = 0 (conformance window) and bit 47 = first bit of ue(2)=011 → bit47=0
	//
	// byte 6 (bits 48-55): bit48=1, 49=1, 50=0, 51=1, 52=1, pad with 0s
	// = 0b11011000 = 0xD8
	// Wait. ue(2) = 011 (3 bits).
	// First ue(2) for luma: bit47=0, bit48=1, bit49=1
	// Second ue(2) for chroma: bit50=0, bit51=1, bit52=1
	//
	// byte 5 (bits 40-47): 1 1 1 0 0 1 0 0 = 0xE4
	// byte 6 (bits 48-55): 1 1 0 1 1 0 0 0 = 0xD8

	payload := []byte{0xA0, 0x03, 0xC0, 0x80, 0x10, 0xE4, 0xD8}

	// Full SPS: NAL header (2 bytes) + first SPS byte + 12 bytes PTL + payload
	sps := make([]byte, 0, 2+1+12+len(payload))
	sps = append(sps, 0x42, 0x01)               // NAL header
	sps = append(sps, 0x01)                       // vps_id=0, max_sub_layers=0, temporal=1
	sps = append(sps, make([]byte, 12)...)        // profile_tier_level (all zeros)
	sps = append(sps, payload...)

	info := ParseH265SPS(sps)
	if info == nil {
		t.Fatal("ParseH265SPS returned nil")
	}

	if info.ChromaFormatIDC != 1 {
		t.Errorf("ChromaFormatIDC = %d, want 1", info.ChromaFormatIDC)
	}
	if info.BitDepthLuma != 10 {
		t.Errorf("BitDepthLuma = %d, want 10", info.BitDepthLuma)
	}
	if info.BitDepthChroma != 10 {
		t.Errorf("BitDepthChroma = %d, want 10", info.BitDepthChroma)
	}
	if info.Width != 1920 {
		t.Errorf("Width = %d, want 1920", info.Width)
	}
	if info.Height != 1080 {
		t.Errorf("Height = %d, want 1080", info.Height)
	}
}

func TestParseH265SPS_TooShort(t *testing.T) {
	result := ParseH265SPS([]byte{0x42, 0x01})
	if result != nil {
		t.Error("expected nil for too-short SPS")
	}

	result = ParseH265SPS(nil)
	if result != nil {
		t.Error("expected nil for nil SPS")
	}
}
