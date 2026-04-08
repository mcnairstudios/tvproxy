package tvsatipscan

import (
	"encoding/binary"
	"strings"
)

var serviceTypeName = map[uint8]string{
	0x01: "TV",
	0x02: "Radio",
	0x04: "NVOD-ref",
	0x11: "HD-TV",
	0x16: "SD-TV(AVC)",
	0x19: "HD-TV(AVC)",
	0x1f: "TV(HEVC)",
}

var streamTypeName = map[uint8]string{
	0x01: "MPEG-1 Video",
	0x02: "MPEG-2 Video",
	0x03: "MPEG-1 Audio",
	0x04: "MPEG-2 Audio",
	0x06: "Private",
	0x0f: "AAC Audio",
	0x11: "AAC Audio (LATM)",
	0x1b: "H.264 Video",
	0x24: "H.265/HEVC Video",
	0x42: "AVS Video",
	0x81: "AC-3 Audio",
	0x87: "E-AC-3 Audio",
}

const (
	tagSatelliteDelivery   = 0x43
	tagCableDelivery       = 0x44
	tagTerrestrialDelivery = 0x5a
	tagExtT2Delivery       = 0x04
)

func serviceTypStr(t uint8) string {
	if s, ok := serviceTypeName[t]; ok {
		return s
	}
	return "0x" + byteHex(t)
}

func streamCategory(t uint8) string {
	switch t {
	case 0x01, 0x02, 0x1b, 0x24, 0x42:
		return "video"
	case 0x03, 0x04, 0x0f, 0x11, 0x81, 0x87:
		return "audio"
	}
	return ""
}

func streamTypStr(t uint8) string {
	if s, ok := streamTypeName[t]; ok {
		return s
	}
	return "0x" + byteHex(t)
}

func byteHex(b uint8) string {
	const hex = "0123456789abcdef"
	return string([]byte{hex[b>>4], hex[b&0xf]})
}

func dvbString(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	if b[0] < 0x20 {
		b = b[1:]
	}
	return strings.TrimSpace(string(b))
}

func parseTerrestrialDelivery(b []byte) (Transponder, bool) {
	if len(b) < 11 {
		return Transponder{}, false
	}
	freqHz := uint64(binary.BigEndian.Uint32(b[0:4])) * 10
	freqMHz := float64(freqHz) / 1e6
	bwCode := (b[4] >> 5) & 0x07
	bwMHz := [8]int{8, 7, 6, 5, 0, 0, 0, 0}[bwCode]
	constCode := (b[5] >> 6) & 0x03
	modulation := [4]string{"qpsk", "16qam", "64qam", ""}[constCode]
	return Transponder{
		FreqMHz:      freqMHz,
		System:       "dvbt",
		Modulation:   modulation,
		BandwidthMHz: bwMHz,
	}, true
}

func parseSatelliteDelivery(b []byte) (Transponder, bool) {
	if len(b) < 11 {
		return Transponder{}, false
	}
	freqBCD := binary.BigEndian.Uint32(b[0:4])
	freqKHz := bcdToUint32(freqBCD) * 10
	freqMHz := float64(freqKHz) / 1000.0
	polCode := (b[8] >> 5) & 0x03
	pol := [4]string{"h", "v", "l", "r"}[polCode]
	modSys := (b[8] >> 2) & 0x01
	sys := "dvbs"
	if modSys == 1 {
		sys = "dvbs2"
	}
	modCode := b[8] & 0x03
	modulation := [4]string{"auto", "qpsk", "8psk", "16qam"}[modCode]
	srBCD := binary.BigEndian.Uint32(b[7:11]) >> 4
	srKS := int(bcdToUint32(srBCD) / 10)
	return Transponder{
		FreqMHz:      freqMHz,
		System:       sys,
		Modulation:   modulation,
		SymbolRateKS: srKS,
		Polarization: pol,
	}, true
}

func parseCableDelivery(b []byte) (Transponder, bool) {
	if len(b) < 11 {
		return Transponder{}, false
	}
	freqBCD := binary.BigEndian.Uint32(b[0:4])
	freqHz := bcdToUint32(freqBCD) * 100
	freqMHz := float64(freqHz) / 1e6
	modCode := b[6]
	modulation := map[uint8]string{1: "16qam", 2: "32qam", 3: "64qam", 4: "128qam", 5: "256qam"}[modCode]
	if modulation == "" {
		modulation = "64qam"
	}
	srBCD := binary.BigEndian.Uint32(b[7:11]) >> 4
	srKS := int(bcdToUint32(srBCD) / 10)
	return Transponder{
		FreqMHz:      freqMHz,
		System:       "dvbc",
		Modulation:   modulation,
		SymbolRateKS: srKS,
	}, true
}

func parseT2Delivery(b []byte) (Transponder, bool) {
	if len(b) < 11 {
		return Transponder{}, false
	}
	bwCode := (b[3] >> 2) & 0x0F
	bwMHz := [16]int{8, 7, 6, 5, 10, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}[bwCode]
	if bwMHz == 0 {
		bwMHz = 8
	}
	tfs := b[4] & 0x01
	if tfs != 0 {
		return Transponder{}, false
	}
	freqHz := uint64(binary.BigEndian.Uint32(b[7:11])) * 10
	freqMHz := float64(freqHz) / 1e6
	return Transponder{
		FreqMHz:      freqMHz,
		System:       "dvbt2",
		Modulation:   "256qam",
		BandwidthMHz: bwMHz,
	}, true
}

func bcdToUint32(bcd uint32) uint32 {
	return (bcd>>28)*10000000 +
		((bcd>>24)&0xF)*1000000 +
		((bcd>>20)&0xF)*100000 +
		((bcd>>16)&0xF)*10000 +
		((bcd>>12)&0xF)*1000 +
		((bcd>>8)&0xF)*100 +
		((bcd>>4)&0xF)*10 +
		(bcd&0xF)
}
