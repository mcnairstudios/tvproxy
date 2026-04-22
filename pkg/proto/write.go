package proto

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"

	"github.com/gavinmcnair/tvproxy/pkg/lib/av/probe"
)

const (
	wireVarint = 0
	wire32     = 5
	wireLenDel = 2
)

func appendVarint(b []byte, v uint64) []byte {
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}

func appendTag(b []byte, fieldNum int, wireType int) []byte {
	return appendVarint(b, uint64(fieldNum<<3|wireType))
}

func appendString(b []byte, fieldNum int, s string) []byte {
	if s == "" {
		return b
	}
	b = appendTag(b, fieldNum, wireLenDel)
	b = appendVarint(b, uint64(len(s)))
	return append(b, s...)
}

func appendInt32(b []byte, fieldNum int, v int32) []byte {
	if v == 0 {
		return b
	}
	b = appendTag(b, fieldNum, wireVarint)
	return appendVarint(b, uint64(v))
}

func appendInt64(b []byte, fieldNum int, v int64) []byte {
	if v == 0 {
		return b
	}
	b = appendTag(b, fieldNum, wireVarint)
	return appendVarint(b, uint64(v))
}

func appendBool(b []byte, fieldNum int, v bool) []byte {
	if !v {
		return b
	}
	b = appendTag(b, fieldNum, wireVarint)
	return append(b, 1)
}

func appendBytes(b []byte, fieldNum int, data []byte) []byte {
	if len(data) == 0 {
		return b
	}
	b = appendTag(b, fieldNum, wireLenDel)
	b = appendVarint(b, uint64(len(data)))
	return append(b, data...)
}

func appendFloat32(b []byte, fieldNum int, v float32) []byte {
	if v == 0 {
		return b
	}
	b = appendTag(b, fieldNum, wire32)
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], math.Float32bits(v))
	return append(b, buf[:]...)
}

func MarshalProbe(info *probe.StreamInfo, selectedAudioIndex int) ([]byte, error) {
	var b []byte

	var selAudio *probe.AudioTrack
	for i := range info.AudioTracks {
		if info.AudioTracks[i].Index == selectedAudioIndex {
			selAudio = &info.AudioTracks[i]
			break
		}
	}
	if selAudio == nil && len(info.AudioTracks) > 0 {
		selAudio = &info.AudioTracks[0]
	}

	if info.Video != nil {
		b = appendString(b, 1, info.Video.Codec)
	}
	if info.Video != nil {
		b = appendInt32(b, 3, int32(info.Video.Width))
	}
	if info.Video != nil {
		b = appendInt32(b, 4, int32(info.Video.Height))
	}
	if info.Video != nil {
		b = appendBool(b, 5, info.Video.Interlaced)
	}
	if selAudio != nil {
		b = appendString(b, 6, selAudio.Codec)
	}
	if selAudio != nil {
		b = appendInt32(b, 7, int32(selAudio.Channels))
	}
	if selAudio != nil {
		b = appendInt32(b, 8, int32(selAudio.SampleRate))
	}
	if info.Video != nil {
		b = appendInt32(b, 12, int32(info.Video.BitDepth))
	}
	if info.Video != nil {
		b = appendInt32(b, 13, int32(info.Video.FramerateN))
	}
	if info.Video != nil {
		b = appendInt32(b, 14, int32(info.Video.FramerateD))
	}
	if info.Video != nil {
		b = appendInt32(b, 15, int32(info.Video.BitrateKbps))
	}

	b = appendString(b, 16, info.Container)
	b = appendInt64(b, 17, info.DurationMs)
	b = appendBool(b, 18, info.IsLive)
	b = appendInt32(b, 19, int32(selectedAudioIndex))
	for i := range info.AudioTracks {
		b = appendBytes(b, 20, marshalAudioTrack(&info.AudioTracks[i]))
	}
	for i := range info.SubTracks {
		b = appendBytes(b, 21, marshalSubtitleTrack(&info.SubTracks[i]))
	}
	if selAudio != nil {
		b = appendInt32(b, 22, int32(selAudio.BitrateKbps))
	}

	return b, nil
}

func marshalAudioTrack(a *probe.AudioTrack) []byte {
	var b []byte
	b = appendInt32(b, 1, int32(a.Index))
	b = appendString(b, 2, a.Codec)
	b = appendInt32(b, 3, int32(a.Channels))
	b = appendInt32(b, 4, int32(a.SampleRate))
	b = appendString(b, 5, a.Language)
	b = appendBool(b, 6, a.IsAD)
	b = appendInt32(b, 7, int32(a.BitrateKbps))
	return b
}

func marshalSubtitleTrack(s *probe.SubtitleTrack) []byte {
	var b []byte
	b = appendInt32(b, 1, int32(s.Index))
	b = appendString(b, 2, s.Codec)
	b = appendString(b, 3, s.Language)
	return b
}

func MarshalSignal(strength, quality, snr float32, timestampUnix int64) []byte {
	var b []byte
	b = appendFloat32(b, 1, strength)
	b = appendFloat32(b, 2, quality)
	b = appendFloat32(b, 3, snr)
	b = appendInt64(b, 4, timestampUnix)
	return b
}

func WriteProbeFile(path string, info *probe.StreamInfo, selectedAudioIndex int) error {
	data, err := MarshalProbe(info, selectedAudioIndex)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("proto: write %s: %w", tmp, err)
	}
	return os.Rename(tmp, path)
}

func EnrichProbeFile(path string, codecString string, audioOutputCodec string, audioOutputChannels, audioOutputSampleRate int) error {
	existing, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("proto: read %s for enrichment: %w", path, err)
	}

	b := existing
	b = appendString(b, 2, codecString)
	b = appendString(b, 9, audioOutputCodec)
	b = appendInt32(b, 10, int32(audioOutputChannels))
	b = appendInt32(b, 11, int32(audioOutputSampleRate))

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return fmt.Errorf("proto: write %s: %w", tmp, err)
	}
	return os.Rename(tmp, path)
}

func WriteSignalFile(path string, strength, quality, snr float32, timestampUnix int64) error {
	data := MarshalSignal(strength, quality, snr, timestampUnix)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("proto: write %s: %w", tmp, err)
	}
	return os.Rename(tmp, path)
}
