package fmp4

import (
	"encoding/hex"
	"testing"
)

func TestExtractAV1SequenceHeader(t *testing.T) {
	// Minimal OBU_SEQUENCE_HEADER: obu_type=1, has_size_field=1
	// Header byte: 0b0000_1010 = 0x0A (type=1, no extension, has_size=1)
	// Size: 2 bytes payload
	// Payload: profile=0, still_picture=0, reduced_still_picture=0, ...
	obu := []byte{0x0A, 0x02, 0x00, 0x00}
	result := extractAV1SequenceHeader(obu)
	if result == nil {
		t.Fatal("expected sequence header, got nil")
	}
	if len(result) != 4 {
		t.Fatalf("expected 4 bytes, got %d", len(result))
	}
}

func TestExtractAV1SequenceHeader_WithTD(t *testing.T) {
	// Temporal delimiter OBU followed by sequence header
	td := []byte{0x12, 0x00} // type=2 (TD), has_size=1, size=0
	seqHdr := []byte{0x0A, 0x03, 0x01, 0x02, 0x03}
	data := append(td, seqHdr...)
	result := extractAV1SequenceHeader(data)
	if result == nil {
		t.Fatal("expected sequence header, got nil")
	}
	if len(result) != 5 {
		t.Fatalf("expected 5 bytes, got %d", len(result))
	}
}

func TestStripAV1TemporalDelimiter(t *testing.T) {
	td := []byte{0x12, 0x00} // TD OBU
	frame := []byte{0x32, 0x03, 0xAA, 0xBB, 0xCC} // Frame OBU (type=6)
	data := append(td, frame...)
	result := stripAV1TemporalDelimiter(data)
	if len(result) != len(frame) {
		t.Fatalf("expected %d bytes, got %d", len(frame), len(result))
	}
	for i := range frame {
		if result[i] != frame[i] {
			t.Fatalf("byte %d: expected %02x, got %02x", i, frame[i], result[i])
		}
	}
}

func TestIsAV1Keyframe(t *testing.T) {
	// OBU_FRAME (type=6), has_size=1, size=1
	// Payload first byte: show_existing_frame=0 (bit7), frame_type=00 (bits 6-5) = KEY_FRAME
	// Byte: 0b0_00_xxxxx = 0x00
	keyframe := []byte{0x32, 0x01, 0x00}
	if !IsAV1Keyframe(keyframe) {
		t.Error("expected keyframe detection")
	}

	// Non-keyframe: frame_type=01 (INTER_FRAME)
	// Byte: 0b0_01_xxxxx = 0x20
	interframe := []byte{0x32, 0x01, 0x20}
	if IsAV1Keyframe(interframe) {
		t.Error("expected non-keyframe")
	}
}

func TestParseAV1SequenceHeader_Profile0(t *testing.T) {
	// Build a minimal sequence header OBU
	// OBU header: type=1, has_size=1 → 0x0A
	// Payload bits:
	//   seq_profile = 000 (Main)
	//   still_picture = 0
	//   reduced_still_picture_header = 1
	//   seq_level_idx[0] = 01101 (13 = level 5.1)
	//   → byte 0: 0b000_0_1_011 = 0x0B (profile=0, still=0, reduced=1, level MSB=011)
	//   → byte 1: 0b01_xxxxxx = 0x40 (level LSB=01, rest don't matter)
	// Actually let me recalculate:
	// Bits: 000 0 1 01101 ...
	// Byte 0: 0b0000_1011 = 0x0B
	// Byte 1: 0b01xx_xxxx = 0x40
	payload := []byte{0x0B, 0x40}
	obu := append([]byte{0x0A, byte(len(payload))}, payload...)

	profile, level, tier, bitDepth, _, chromaX, chromaY, _ := parseAV1SequenceHeader(obu)
	if profile != 0 {
		t.Errorf("expected profile 0, got %d", profile)
	}
	// reduced_still_picture=1 so level is parsed from bits
	if level != 13 {
		t.Errorf("expected level 13, got %d", level)
	}
	if tier != 0 {
		t.Errorf("expected tier 0, got %d", tier)
	}
	if bitDepth != 8 {
		t.Errorf("expected bitDepth 8, got %d", bitDepth)
	}
	if chromaX != 1 || chromaY != 1 {
		t.Errorf("expected chroma 4:2:0, got %d:%d", chromaX, chromaY)
	}
}

func TestBuildAV1Init(t *testing.T) {
	// Minimal sequence header OBU
	seqHdr := []byte{0x0A, 0x02, 0x0B, 0x40}
	init := buildAV1Init(1, 90000, seqHdr)
	if init == nil {
		t.Fatal("expected non-nil init segment")
	}
	if len(init) < 100 {
		t.Fatalf("init segment too small: %d bytes", len(init))
	}

	// Verify it starts with ftyp box
	if string(init[4:8]) != "ftyp" {
		t.Errorf("expected ftyp box, got %s", string(init[4:8]))
	}

	// Find av1C box
	initHex := hex.EncodeToString(init)
	if !containsBox(init, "av1C") {
		t.Error("init segment missing av1C box")
	}
	if !containsBox(init, "av01") {
		t.Error("init segment missing av01 box")
	}
	_ = initHex
}

func containsBox(data []byte, boxType string) bool {
	target := []byte(boxType)
	for i := 0; i <= len(data)-4; i++ {
		if data[i] == target[0] && data[i+1] == target[1] && data[i+2] == target[2] && data[i+3] == target[3] {
			return true
		}
	}
	return false
}

func TestBitReader(t *testing.T) {
	br := &bitReader{data: []byte{0b10110100}, pos: 0}
	if v := br.readBits(3); v != 5 { // 101
		t.Errorf("expected 5, got %d", v)
	}
	if v := br.readBit(); v != 1 { // 1
		t.Errorf("expected 1, got %d", v)
	}
	if v := br.readBit(); v != 0 { // 0
		t.Errorf("expected 0, got %d", v)
	}
}

func TestReadLEB128(t *testing.T) {
	// Single byte: 0x05 = 5
	val, n := readLEB128([]byte{0x05})
	if val != 5 || n != 1 {
		t.Errorf("expected (5,1), got (%d,%d)", val, n)
	}

	// Two bytes: 0x80 0x01 = 128
	val, n = readLEB128([]byte{0x80, 0x01})
	if val != 128 || n != 2 {
		t.Errorf("expected (128,2), got (%d,%d)", val, n)
	}
}
