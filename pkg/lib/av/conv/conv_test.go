package conv

import (
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/gavinmcnair/tvproxy/pkg/lib/av/demux"
)

func TestCodecIDFromString(t *testing.T) {
	tests := []struct {
		codec string
		want  astiav.CodecID
		err   bool
	}{
		{"h264", astiav.CodecIDH264, false},
		{"hevc", astiav.CodecIDHevc, false},
		{"h265", astiav.CodecIDHevc, false},
		{"aac", astiav.CodecIDAac, false},
		{"ac3", astiav.CodecIDAc3, false},
		{"dts", astiav.CodecIDDts, false},
		{"opus", astiav.CodecIDOpus, false},
		{"vp9", astiav.CodecIDVp9, false},
		{"av1", astiav.CodecIDAv1, false},
		{"unknown_codec", astiav.CodecIDNone, true},
		{"", astiav.CodecIDNone, true},
	}
	for _, tt := range tests {
		t.Run(tt.codec, func(t *testing.T) {
			got, err := CodecIDFromString(tt.codec)
			if tt.err && err == nil {
				t.Error("expected error")
			}
			if !tt.err && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("CodecIDFromString(%q) = %d, want %d", tt.codec, got, tt.want)
			}
		})
	}
}

func TestCodecIDFromString_CaseInsensitive(t *testing.T) {
	id, err := CodecIDFromString("H264")
	if err != nil {
		t.Fatal(err)
	}
	if id != astiav.CodecIDH264 {
		t.Errorf("expected H264, got %d", id)
	}
}

func TestToAVPacket(t *testing.T) {
	p := &demux.Packet{
		Type:     demux.Video,
		Data:     []byte{0x00, 0x00, 0x00, 0x01, 0x65, 0xAA},
		PTS:      1_000_000_000,
		DTS:      999_000_000,
		Duration: 40_000_000,
		Keyframe: true,
	}

	tb := astiav.NewRational(1, 90000)
	avpkt, err := ToAVPacket(p, tb)
	if err != nil {
		t.Fatal(err)
	}
	defer avpkt.Free()

	if avpkt.Size() != 6 {
		t.Errorf("expected size 6, got %d", avpkt.Size())
	}
	if avpkt.Pts() != 90000 {
		t.Errorf("expected PTS 90000, got %d", avpkt.Pts())
	}
	if !avpkt.Flags().Has(astiav.PacketFlagKey) {
		t.Error("expected keyframe flag")
	}
}

func TestToAVPacket_EmptyData(t *testing.T) {
	p := &demux.Packet{
		Data: nil,
		PTS:  0,
	}
	tb := astiav.NewRational(1, 1000)
	avpkt, err := ToAVPacket(p, tb)
	if err != nil {
		t.Fatal(err)
	}
	defer avpkt.Free()

	if avpkt.Size() != 0 {
		t.Errorf("expected size 0, got %d", avpkt.Size())
	}
}
