// Package extradata converts FFmpeg codec extradata to GStreamer codec_data format.
//
// FFmpeg stores codec extradata as raw NAL units (typically Annex B format with
// 0x00000001 start codes). GStreamer expects codec_data in ISO BMFF format:
//   - H264: avcC box format (AVCDecoderConfigurationRecord)
//   - H265: hvcC box format (HEVCDecoderConfigurationRecord)
package extradata

import (
	"encoding/hex"
	"fmt"
)

// ToGstCodecData converts ffmpeg extradata bytes to GStreamer codec_data format.
// Returns nil, nil if the codec doesn't need conversion or extradata is empty.
// The codec parameter is the CodecID string from go-astiav (e.g. "h264", "hevc").
func ToGstCodecData(codec string, extradata []byte) ([]byte, error) {
	if len(extradata) == 0 {
		return nil, nil
	}

	switch codec {
	case "h264":
		return h264ToAvcC(extradata)
	case "hevc", "h265":
		return h265ToHvcC(extradata)
	default:
		return nil, nil
	}
}

// ToHexString converts codec_data bytes to a hex string for use in GStreamer caps.
// For example: "01640028ffe1001b67640028..." for use in
// caps="...,codec_data=(buffer)01640028..."
func ToHexString(data []byte) string {
	return hex.EncodeToString(data)
}

// SplitNALUnits splits an Annex B byte stream into individual NAL units.
// Handles both 3-byte (0x000001) and 4-byte (0x00000001) start codes.
// Returns NAL units without their start codes.
func SplitNALUnits(data []byte) [][]byte {
	var nalus [][]byte
	i := 0
	n := len(data)

	// Find the first start code.
	start := -1
	for i < n {
		if i+3 < n && data[i] == 0x00 && data[i+1] == 0x00 && data[i+2] == 0x00 && data[i+3] == 0x01 {
			if start >= 0 {
				nalu := trimTrailingZeros(data[start:i])
				if len(nalu) > 0 {
					nalus = append(nalus, nalu)
				}
			}
			i += 4
			start = i
			continue
		}
		if i+2 < n && data[i] == 0x00 && data[i+1] == 0x00 && data[i+2] == 0x01 {
			if start >= 0 {
				nalu := trimTrailingZeros(data[start:i])
				if len(nalu) > 0 {
					nalus = append(nalus, nalu)
				}
			}
			i += 3
			start = i
			continue
		}
		i++
	}

	// Capture the last NAL unit.
	if start >= 0 && start < n {
		nalu := data[start:n]
		if len(nalu) > 0 {
			nalus = append(nalus, nalu)
		}
	}

	return nalus
}

// trimTrailingZeros removes trailing zero bytes that may appear between NAL units
// when a 4-byte start code follows a 3-byte boundary.
func trimTrailingZeros(data []byte) []byte {
	end := len(data)
	for end > 0 && data[end-1] == 0x00 {
		end--
	}
	return data[:end]
}

// putU16BE writes a uint16 in big-endian into a byte slice.
func putU16BE(b []byte, v uint16) {
	b[0] = byte(v >> 8)
	b[1] = byte(v)
}

// errNoSPS is returned when no SPS NAL unit is found.
var errNoSPS = fmt.Errorf("extradata: no SPS NAL unit found")

// errNoPPS is returned when no PPS NAL unit is found.
var errNoPPS = fmt.Errorf("extradata: no PPS NAL unit found")
