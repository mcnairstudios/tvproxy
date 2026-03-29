package tvsatipscan

import (
	"encoding/binary"
	"strings"
)

// DVB service types (EN 300 468)
var serviceTypeName = map[uint8]string{
	0x01: "TV",
	0x02: "Radio",
	0x04: "NVOD-ref",
	0x11: "HD-TV",
	0x16: "SD-TV(AVC)",
	0x19: "HD-TV(AVC)",
	0x1f: "TV(HEVC)",
}

// Stream type names (ISO 13818-1 / DVB)
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

// Descriptor tags not yet in go-astits v1.15.0
const (
	tagSatelliteDelivery   = 0x43
	tagCableDelivery       = 0x44
	tagTerrestrialDelivery = 0x5a
	tagExtT2Delivery       = 0x04 // extension descriptor tag for T2 delivery system (EN 300 468 §6.4.6.3)
)

func serviceTypStr(t uint8) string {
	if s, ok := serviceTypeName[t]; ok {
		return s
	}
	return "0x" + byteHex(t)
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

// dvbString decodes a DVB character string, stripping any leading encoding byte.
func dvbString(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	if b[0] < 0x20 {
		b = b[1:]
	}
	return strings.TrimSpace(string(b))
}

// parseTerrestrialDelivery parses descriptor tag 0x5A. EN 300 468 §6.2.13.4
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

// parseSatelliteDelivery parses descriptor tag 0x43. EN 300 468 §6.2.13.2
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

// parseCableDelivery parses descriptor tag 0x44. EN 300 468 §6.2.13.1
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

// parseT2Delivery parses a T2 delivery system descriptor (extension tag 0x04).
// EN 300 468 §6.4.6.3. b is desc.Extension.Unknown bytes (extension tag already consumed).
// Layout: [0]=plp_id [1-2]=T2_system_id [3]=SISO_MISO(2)|bandwidth(4)|rsvd(2)
//
//	[4]=guard(3)|mode(3)|other_freq(1)|tfs(1)  [5-6]=cell_id [7-10]=centre_frequency
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
		return Transponder{}, false // TFS mode not supported
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
