package keyframe

import (
	"github.com/gavinmcnair/tvproxy/pkg/lib/av/extradata"
)

func IsKeyframe(data []byte, codec string) bool {
	nalus := extradata.SplitNALUnits(data)
	switch codec {
	case "h264":
		return isKeyframeH264(nalus)
	case "hevc", "h265":
		return isKeyframeH265(nalus)
	default:
		return false
	}
}

func FixDeltaUnit(data []byte, codec string) bool {
	return !IsKeyframe(data, codec)
}

type KeyframeTracker struct {
	isLive       bool
	seenKeyframe bool
}

func NewKeyframeTracker(isLive bool) *KeyframeTracker {
	return &KeyframeTracker{isLive: isLive}
}

func (t *KeyframeTracker) Reset() {
	t.seenKeyframe = false
}

func (t *KeyframeTracker) ShouldDrop(data []byte, codec string) bool {
	if t.isLive {
		FixDeltaUnit(data, codec)
		return false
	}

	if !t.seenKeyframe {
		if IsKeyframe(data, codec) {
			t.seenKeyframe = true
			return false
		}
		return true
	}

	FixDeltaUnit(data, codec)
	return false
}

func isKeyframeH264(nalus [][]byte) bool {
	for _, nalu := range nalus {
		if len(nalu) == 0 {
			continue
		}
		nalType := nalu[0] & 0x1F
		if nalType == 5 {
			return true
		}
	}
	return false
}

func isKeyframeH265(nalus [][]byte) bool {
	for _, nalu := range nalus {
		if len(nalu) == 0 {
			continue
		}
		nalType := (nalu[0] >> 1) & 0x3F
		if nalType >= 16 && nalType <= 21 {
			return true
		}
	}
	return false
}
