package proto

import (
	"bytes"
	"os"
	"testing"

	"github.com/gavinmcnair/tvproxy/pkg/lib/av/probe"
)

func TestMarshalProbe_BackwardsCompatible(t *testing.T) {
	info := &probe.StreamInfo{
		Container:  "matroska,webm",
		DurationMs: 7139483,
		Video: &probe.VideoStream{
			Index: 0, Codec: "hevc", Width: 3840, Height: 2160,
			BitDepth: 10, FramerateN: 24000, FramerateD: 1001,
		},
		AudioTracks: []probe.AudioTrack{
			{Index: 1, Codec: "dts", Channels: 6, SampleRate: 48000, Language: "eng", BitrateKbps: 1536},
		},
	}

	data, err := MarshalProbe(info, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty output")
	}

	if data[0] != 0x0A {
		t.Errorf("expected first tag 0x0A (field 1 string), got 0x%02X", data[0])
	}
	if data[1] != 0x04 {
		t.Errorf("expected string length 4 (hevc), got %d", data[1])
	}
	if string(data[2:6]) != "hevc" {
		t.Errorf("expected 'hevc', got %q", string(data[2:6]))
	}
}

func TestMarshalProbe_AudioFields(t *testing.T) {
	info := &probe.StreamInfo{
		AudioTracks: []probe.AudioTrack{
			{Index: 1, Codec: "dts", Channels: 6, SampleRate: 48000},
		},
	}

	data, err := MarshalProbe(info, 1)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Contains(data, []byte("dts")) {
		t.Error("expected 'dts' in output for audio_source_codec")
	}
}

func TestMarshalProbeEmpty(t *testing.T) {
	info := &probe.StreamInfo{}
	data, err := MarshalProbe(info, -1)
	if err != nil {
		t.Fatal(err)
	}
	_ = data
}

func TestWriteProbeFile(t *testing.T) {
	info := &probe.StreamInfo{
		Video: &probe.VideoStream{Codec: "h264", Width: 1920, Height: 1080},
	}
	tmp := t.TempDir() + "/probe.pb"
	err := WriteProbeFile(tmp, info, 0)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty file")
	}
}

func TestMarshalSignal(t *testing.T) {
	data := MarshalSignal(85.5, 92.0, 15.3, 1700000000)
	if len(data) == 0 {
		t.Error("expected non-empty signal output")
	}
}

func TestWriteSignalFile(t *testing.T) {
	tmp := t.TempDir() + "/signal.pb"
	err := WriteSignalFile(tmp, 85.5, 92.0, 15.3, 1700000000)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty file")
	}
}

func TestVarintEncoding(t *testing.T) {
	tests := []struct {
		val  uint64
		want []byte
	}{
		{0, []byte{0x00}},
		{1, []byte{0x01}},
		{127, []byte{0x7F}},
		{128, []byte{0x80, 0x01}},
		{300, []byte{0xAC, 0x02}},
	}
	for _, tt := range tests {
		got := appendVarint(nil, tt.val)
		if !bytes.Equal(got, tt.want) {
			t.Errorf("varint(%d) = %x, want %x", tt.val, got, tt.want)
		}
	}
}

func TestAppendString(t *testing.T) {
	got := appendString(nil, 1, "hello")
	want := []byte{0x0A, 0x05, 'h', 'e', 'l', 'l', 'o'}
	if !bytes.Equal(got, want) {
		t.Errorf("appendString(1, \"hello\") = %x, want %x", got, want)
	}
}

func TestAppendStringEmpty(t *testing.T) {
	got := appendString(nil, 1, "")
	if len(got) != 0 {
		t.Errorf("appendString with empty string should produce no output, got %x", got)
	}
}

func TestAppendBool(t *testing.T) {
	got := appendBool(nil, 3, true)
	want := []byte{0x18, 0x01}
	if !bytes.Equal(got, want) {
		t.Errorf("appendBool(3, true) = %x, want %x", got, want)
	}

	got = appendBool(nil, 3, false)
	if len(got) != 0 {
		t.Errorf("appendBool(3, false) should produce no output, got %x", got)
	}
}
