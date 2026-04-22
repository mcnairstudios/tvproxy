package extradata

import (
	"bytes"
	"testing"
)

func TestH264AnnexBToAvcC(t *testing.T) {
	// Annex B with SPS (type 7, profile High=0x64, compat=0x00, level=0x28)
	// and PPS (type 8).
	annexB := []byte{
		0x00, 0x00, 0x00, 0x01, // 4-byte start code
		0x67, 0x64, 0x00, 0x28, 0xAC, 0xD9, 0x40, // SPS (NAL type 7)
		0x00, 0x00, 0x00, 0x01, // 4-byte start code
		0x68, 0xEE, 0x38, 0x80, // PPS (NAL type 8)
	}

	result, err := ToGstCodecData("h264", annexB)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Verify configurationVersion.
	if result[0] != 0x01 {
		t.Errorf("configurationVersion: got 0x%02x, want 0x01", result[0])
	}
	// Verify profile.
	if result[1] != 0x64 {
		t.Errorf("AVCProfileIndication: got 0x%02x, want 0x64", result[1])
	}
	// Verify profile_compatibility.
	if result[2] != 0x00 {
		t.Errorf("profile_compatibility: got 0x%02x, want 0x00", result[2])
	}
	// Verify level.
	if result[3] != 0x28 {
		t.Errorf("AVCLevelIndication: got 0x%02x, want 0x28", result[3])
	}
	// Verify lengthSizeMinusOne.
	if result[4] != 0xFF {
		t.Errorf("lengthSizeMinusOne byte: got 0x%02x, want 0xFF", result[4])
	}
	// Verify numSPS = 1.
	if result[5] != 0xE1 {
		t.Errorf("numSPS byte: got 0x%02x, want 0xE1", result[5])
	}

	// Verify SPS length (7 bytes).
	spsLen := int(result[6])<<8 | int(result[7])
	if spsLen != 7 {
		t.Errorf("SPS length: got %d, want 7", spsLen)
	}

	// Verify SPS data.
	spsData := result[8 : 8+spsLen]
	expectedSPS := []byte{0x67, 0x64, 0x00, 0x28, 0xAC, 0xD9, 0x40}
	if !bytes.Equal(spsData, expectedSPS) {
		t.Errorf("SPS data: got %x, want %x", spsData, expectedSPS)
	}

	// Verify numPPS = 1.
	ppsCountIdx := 8 + spsLen
	if result[ppsCountIdx] != 0x01 {
		t.Errorf("numPPS: got 0x%02x, want 0x01", result[ppsCountIdx])
	}

	// Verify PPS length (4 bytes).
	ppsLenIdx := ppsCountIdx + 1
	ppsLen := int(result[ppsLenIdx])<<8 | int(result[ppsLenIdx+1])
	if ppsLen != 4 {
		t.Errorf("PPS length: got %d, want 4", ppsLen)
	}

	// Verify PPS data.
	ppsData := result[ppsLenIdx+2 : ppsLenIdx+2+ppsLen]
	expectedPPS := []byte{0x68, 0xEE, 0x38, 0x80}
	if !bytes.Equal(ppsData, expectedPPS) {
		t.Errorf("PPS data: got %x, want %x", ppsData, expectedPPS)
	}
}

func TestH264AlreadyAvcC(t *testing.T) {
	// Data already in avcC format (starts with 0x01).
	avcC := []byte{
		0x01, 0x64, 0x00, 0x28, 0xFF, 0xE1,
		0x00, 0x07, 0x67, 0x64, 0x00, 0x28, 0xAC, 0xD9, 0x40,
		0x01, 0x00, 0x04, 0x68, 0xEE, 0x38, 0x80,
	}

	result, err := ToGstCodecData("h264", avcC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(result, avcC) {
		t.Errorf("avcC passthrough failed: got %x, want %x", result, avcC)
	}
}

func TestH264ThreeByteStartCode(t *testing.T) {
	// Annex B with 3-byte start codes.
	annexB := []byte{
		0x00, 0x00, 0x01, // 3-byte start code
		0x67, 0x42, 0x00, 0x1E, 0xAB, // SPS (Baseline, level 3.0)
		0x00, 0x00, 0x01, // 3-byte start code
		0x68, 0xCE, 0x38, 0x80, // PPS
	}

	result, err := ToGstCodecData("h264", annexB)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[0] != 0x01 {
		t.Errorf("configurationVersion: got 0x%02x, want 0x01", result[0])
	}
	if result[1] != 0x42 {
		t.Errorf("profile: got 0x%02x, want 0x42 (Baseline)", result[1])
	}
	if result[3] != 0x1E {
		t.Errorf("level: got 0x%02x, want 0x1E (3.0)", result[3])
	}
}

func TestH265AnnexBToHvcC(t *testing.T) {
	// Minimal HEVC Annex B: VPS + SPS + PPS.
	// NAL header for HEVC is 2 bytes: (type<<1) in first byte.
	// VPS type=32: (32<<1)=0x40, so first byte = 0x40, second byte = 0x01
	// SPS type=33: (33<<1)=0x42, so first byte = 0x42, second byte = 0x01
	// PPS type=34: (34<<1)=0x44, so first byte = 0x44, second byte = 0x01

	// Build a fake SPS with enough bytes for profile/tier/level extraction.
	// Bytes: [NAL0, NAL1, sps_header, ptl_byte, profile_compat(4), constraints(6), level]
	fakeSPS := []byte{
		0x42, 0x01, // NAL header (SPS)
		0x01,                                           // sps_video_parameter_set_id etc.
		0x01,                                           // general_profile_space=0, tier=0, profile_idc=1 (Main)
		0x60, 0x00, 0x00, 0x00,                         // profile_compatibility_flags
		0x90, 0x00, 0x00, 0x00, 0x00, 0x00,             // constraint_indicator_flags
		0x5D,                                           // general_level_idc = 93 (level 3.1)
	}

	fakeVPS := []byte{0x40, 0x01, 0x0C, 0x01, 0xFF, 0xFF}
	fakePPS := []byte{0x44, 0x01, 0xC1, 0x72, 0xB4}

	annexB := []byte{}
	annexB = append(annexB, 0x00, 0x00, 0x00, 0x01)
	annexB = append(annexB, fakeVPS...)
	annexB = append(annexB, 0x00, 0x00, 0x00, 0x01)
	annexB = append(annexB, fakeSPS...)
	annexB = append(annexB, 0x00, 0x00, 0x00, 0x01)
	annexB = append(annexB, fakePPS...)

	result, err := ToGstCodecData("hevc", annexB)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Verify configurationVersion.
	if result[0] != 0x01 {
		t.Errorf("configurationVersion: got 0x%02x, want 0x01", result[0])
	}

	// Verify profile_idc = 1 (Main).
	profileIDC := result[1] & 0x1F
	if profileIDC != 0x01 {
		t.Errorf("general_profile_idc: got %d, want 1", profileIDC)
	}

	// Verify general_level_idc.
	if result[12] != 0x5D {
		t.Errorf("general_level_idc: got 0x%02x, want 0x5D", result[12])
	}

	// Verify numOfArrays = 3 (VPS + SPS + PPS).
	if result[22] != 3 {
		t.Errorf("numOfArrays: got %d, want 3", result[22])
	}

	// Verify the output length is correct: 23 header + 3 arrays.
	expectedLen := 23 +
		3 + 2 + len(fakeVPS) + // VPS array
		3 + 2 + len(fakeSPS) + // SPS array
		3 + 2 + len(fakePPS) // PPS array
	if len(result) != expectedLen {
		t.Errorf("output length: got %d, want %d", len(result), expectedLen)
	}
}

func TestH265AlreadyHvcC(t *testing.T) {
	hvcC := []byte{0x01, 0x01, 0x60, 0x00, 0x00, 0x00, 0x90, 0x00}

	result, err := ToGstCodecData("hevc", hvcC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(result, hvcC) {
		t.Errorf("hvcC passthrough failed: got %x, want %x", result, hvcC)
	}
}

func TestEmptyExtradata(t *testing.T) {
	result, err := ToGstCodecData("h264", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil for empty extradata, got %x", result)
	}

	result, err = ToGstCodecData("h264", []byte{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil for empty extradata, got %x", result)
	}
}

func TestUnknownCodec(t *testing.T) {
	result, err := ToGstCodecData("vp8", []byte{0x00, 0x01, 0x02})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil for unknown codec, got %x", result)
	}
}

func TestToHexString(t *testing.T) {
	data := []byte{0x01, 0x64, 0x00, 0x28, 0xFF, 0xE1}
	expected := "01640028ffe1"
	got := ToHexString(data)
	if got != expected {
		t.Errorf("ToHexString: got %q, want %q", got, expected)
	}
}

func TestToHexStringEmpty(t *testing.T) {
	got := ToHexString(nil)
	if got != "" {
		t.Errorf("ToHexString(nil): got %q, want empty", got)
	}
}

func TestSplitNALUnits(t *testing.T) {
	// Mixed 3 and 4 byte start codes.
	data := []byte{
		0x00, 0x00, 0x00, 0x01, 0xAA, 0xBB, // 4-byte start code, NAL = [AA BB]
		0x00, 0x00, 0x01, 0xCC, 0xDD, 0xEE, // 3-byte start code, NAL = [CC DD EE]
	}

	nalus := SplitNALUnits(data)
	if len(nalus) != 2 {
		t.Fatalf("expected 2 NALUs, got %d", len(nalus))
	}
	if !bytes.Equal(nalus[0], []byte{0xAA, 0xBB}) {
		t.Errorf("NALU 0: got %x, want aabb", nalus[0])
	}
	if !bytes.Equal(nalus[1], []byte{0xCC, 0xDD, 0xEE}) {
		t.Errorf("NALU 1: got %x, want ccddee", nalus[1])
	}
}

func TestH264NoSPS(t *testing.T) {
	// Annex B with only PPS, no SPS.
	annexB := []byte{
		0x00, 0x00, 0x00, 0x01,
		0x68, 0xEE, 0x38, 0x80, // PPS only
	}
	_, err := ToGstCodecData("h264", annexB)
	if err == nil {
		t.Fatal("expected error for missing SPS")
	}
}

func TestH264NoPPS(t *testing.T) {
	// Annex B with only SPS, no PPS.
	annexB := []byte{
		0x00, 0x00, 0x00, 0x01,
		0x67, 0x64, 0x00, 0x28, 0xAC, 0xD9, 0x40, // SPS only
	}
	_, err := ToGstCodecData("h264", annexB)
	if err == nil {
		t.Fatal("expected error for missing PPS")
	}
}

func TestH265AlsoAcceptsH265String(t *testing.T) {
	// Ensure "h265" codec string also works (not just "hevc").
	fakeSPS := []byte{
		0x42, 0x01, 0x01, 0x01,
		0x60, 0x00, 0x00, 0x00,
		0x90, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x5D,
	}
	fakePPS := []byte{0x44, 0x01, 0xC1}

	annexB := []byte{}
	annexB = append(annexB, 0x00, 0x00, 0x00, 0x01)
	annexB = append(annexB, fakeSPS...)
	annexB = append(annexB, 0x00, 0x00, 0x00, 0x01)
	annexB = append(annexB, fakePPS...)

	result, err := ToGstCodecData("h265", annexB)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result[0] != 0x01 {
		t.Errorf("configurationVersion: got 0x%02x, want 0x01", result[0])
	}
}
