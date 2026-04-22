package keyframe

import "testing"

func TestIsKeyframe_H264_IDR(t *testing.T) {
	data := []byte{0x00, 0x00, 0x00, 0x01, 0x65, 0xAA, 0xBB}
	if !IsKeyframe(data, "h264") {
		t.Error("expected H264 IDR to be detected as keyframe")
	}
}

func TestIsKeyframe_H264_NonIDR(t *testing.T) {
	data := []byte{0x00, 0x00, 0x00, 0x01, 0x41, 0xAA}
	if IsKeyframe(data, "h264") {
		t.Error("expected H264 non-IDR to not be detected as keyframe")
	}
}

func TestIsKeyframe_H265_IDR(t *testing.T) {
	data := []byte{0x00, 0x00, 0x00, 0x01, 0x26, 0x01, 0xAA}
	if !IsKeyframe(data, "hevc") {
		t.Error("expected H265 IDR_W_RADL to be detected as keyframe")
	}
}

func TestIsKeyframe_H265_NonIDR(t *testing.T) {
	data := []byte{0x00, 0x00, 0x00, 0x01, 0x02, 0x01, 0xAA}
	if IsKeyframe(data, "hevc") {
		t.Error("expected H265 non-IDR to not be detected as keyframe")
	}
}

func TestIsKeyframe_UnknownCodec(t *testing.T) {
	data := []byte{0x00, 0x00, 0x00, 0x01, 0x65, 0xAA}
	if IsKeyframe(data, "vp9") {
		t.Error("expected unknown codec to return false")
	}
}

func TestIsKeyframe_EmptyData(t *testing.T) {
	if IsKeyframe(nil, "h264") {
		t.Error("expected nil data to return false")
	}
	if IsKeyframe([]byte{}, "h264") {
		t.Error("expected empty data to return false")
	}
}

func TestFixDeltaUnit_IDR(t *testing.T) {
	data := []byte{0x00, 0x00, 0x00, 0x01, 0x65, 0xAA}
	if FixDeltaUnit(data, "h264") {
		t.Error("expected IDR frame to not be a delta unit")
	}
}

func TestFixDeltaUnit_NonIDR(t *testing.T) {
	data := []byte{0x00, 0x00, 0x00, 0x01, 0x41, 0xAA}
	if !FixDeltaUnit(data, "h264") {
		t.Error("expected non-IDR frame to be a delta unit")
	}
}

func TestKeyframeTracker_VOD_DropsPreIDR(t *testing.T) {
	tracker := NewKeyframeTracker(false)
	nonIDR := []byte{0x00, 0x00, 0x00, 0x01, 0x41, 0xAA}
	idr := []byte{0x00, 0x00, 0x00, 0x01, 0x65, 0xBB}

	if !tracker.ShouldDrop(nonIDR, "h264") {
		t.Error("expected pre-IDR packet to be dropped in VOD mode")
	}
	if tracker.ShouldDrop(idr, "h264") {
		t.Error("expected IDR packet to not be dropped")
	}
	if tracker.ShouldDrop(nonIDR, "h264") {
		t.Error("expected post-IDR packet to not be dropped")
	}
}

func TestKeyframeTracker_Live_NeverDrops(t *testing.T) {
	tracker := NewKeyframeTracker(true)
	nonIDR := []byte{0x00, 0x00, 0x00, 0x01, 0x41, 0xAA}

	if tracker.ShouldDrop(nonIDR, "h264") {
		t.Error("expected live mode to never drop packets")
	}
}

func TestKeyframeTracker_VOD_MultipleNonIDR(t *testing.T) {
	tracker := NewKeyframeTracker(false)
	nonIDR := []byte{0x00, 0x00, 0x00, 0x01, 0x41, 0xAA}

	if !tracker.ShouldDrop(nonIDR, "h264") {
		t.Error("expected first non-IDR to be dropped")
	}
	if !tracker.ShouldDrop(nonIDR, "h264") {
		t.Error("expected second non-IDR to be dropped")
	}
}

func TestIsKeyframe_H264_ThreeByteStartCode(t *testing.T) {
	data := []byte{0x00, 0x00, 0x01, 0x65, 0xAA}
	if !IsKeyframe(data, "h264") {
		t.Error("expected H264 IDR with 3-byte start code to be detected")
	}
}
