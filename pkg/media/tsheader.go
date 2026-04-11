package media

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

const tsPacketSize = 188

func CaptureTPSHeader(ctx context.Context, streamURL string, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", streamURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connecting to stream: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stream returned %d", resp.StatusCode)
	}

	return extractTSHeader(resp.Body)
}

func extractTSHeader(r io.Reader) ([]byte, error) {
	buf := make([]byte, 0, 2*1024*1024)
	tmp := make([]byte, 65536)

	var pat []byte
	var pmt []byte
	var videoPID uint16
	var spsStart int
	var collecting bool
	var videoPkts [][]byte

	for {
		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			if len(videoPkts) > 0 {
				break
			}
			return nil, err
		}

		for len(buf) >= tsPacketSize {
			if buf[0] != 0x47 {
				buf = buf[1:]
				continue
			}
			pkt := make([]byte, tsPacketSize)
			copy(pkt, buf[:tsPacketSize])
			buf = buf[tsPacketSize:]

			pid := uint16(pkt[1]&0x1f)<<8 | uint16(pkt[2])
			pusi := pkt[1]&0x40 != 0

			if pid == 0 && pusi && pat == nil {
				pat = pkt
				videoPID = parsePATForPMTPID(pkt)
			}

			if pmt == nil && pusi && pid != 0 && pid != 0x1fff {
				if isPMT(pkt) {
					pmt = pkt
					vp := parsePMTForVideoPID(pkt)
					if vp > 0 {
						videoPID = vp
					}
				}
			}

			if pat != nil && pmt != nil && videoPID > 0 && pid == videoPID {
				if pusi {
					payload := tsPayload(pkt)
					pesData := skipPESHeader(payload)
					hasSPS := hasSPSNAL(payload) || hasSPSNAL(pesData)
					if hasSPS {
						spsStart = len(videoPkts)
						collecting = true
					}
					if hasSPS {
						videoPkts = append(videoPkts, pkt)
						goto done
					}
				}
				if collecting {
					videoPkts = append(videoPkts, pkt)
				}
			}
		}
	}

done:
	if pat == nil || pmt == nil || len(videoPkts) == 0 {
		return nil, fmt.Errorf("could not capture TS header (pat=%v pmt=%v video_pkts=%d)", pat != nil, pmt != nil, len(videoPkts))
	}

	var result bytes.Buffer
	result.Write(pat)
	result.Write(pmt)
	for _, p := range videoPkts[spsStart:] {
		result.Write(p)
	}
	return result.Bytes(), nil
}

func tsPayload(pkt []byte) []byte {
	start := 4
	if pkt[3]&0x20 != 0 {
		afLen := int(pkt[4])
		start = 5 + afLen
	}
	if start >= tsPacketSize {
		return nil
	}
	return pkt[start:]
}

func hasSPSNAL(payload []byte) bool {
	return hasNALType(payload, 7)
}

func hasIDRNAL(payload []byte) bool {
	return hasNALType(payload, 5)
}

func hasNALType(payload []byte, nalType byte) bool {
	if payload == nil || len(payload) < 4 {
		return false
	}
	for i := 0; i < len(payload)-3; i++ {
		if payload[i] == 0 && payload[i+1] == 0 {
			if payload[i+2] == 1 {
				if payload[i+3]&0x1f == nalType {
					return true
				}
			} else if i+4 < len(payload) && payload[i+2] == 0 && payload[i+3] == 1 {
				if payload[i+4]&0x1f == nalType {
					return true
				}
			}
		}
	}
	return false
}

func parsePATForPMTPID(pkt []byte) uint16 {
	start := 4
	if pkt[3]&0x20 != 0 {
		if int(pkt[4])+5 > tsPacketSize {
			return 0
		}
		start = 5 + int(pkt[4])
	}
	if start >= tsPacketSize-1 {
		return 0
	}
	pointer := int(pkt[start])
	offset := start + 1 + pointer
	if offset >= len(pkt) {
		return 0
	}
	data := pkt[offset:]
	if len(data) < 12 {
		return 0
	}
	sectionLen := int(data[1]&0x0f)<<8 | int(data[2])
	pos := 8
	for pos+4 <= min(3+sectionLen-4, len(data)) {
		progNum := uint16(data[pos])<<8 | uint16(data[pos+1])
		pmtPID := uint16(data[pos+2]&0x1f)<<8 | uint16(data[pos+3])
		if progNum != 0 {
			return pmtPID
		}
		pos += 4
	}
	return 0
}

func isPMT(pkt []byte) bool {
	start := 4
	if pkt[3]&0x20 != 0 {
		if int(pkt[4])+5 > tsPacketSize {
			return false
		}
		start = 5 + int(pkt[4])
	}
	if start >= tsPacketSize-1 {
		return false
	}
	pointer := int(pkt[start])
	offset := start + 1 + pointer
	if offset >= len(pkt) {
		return false
	}
	return pkt[offset] == 0x02
}

func parsePMTForVideoPID(pkt []byte) uint16 {
	start := 4
	if pkt[3]&0x20 != 0 {
		if int(pkt[4])+5 > tsPacketSize {
			return 0
		}
		start = 5 + int(pkt[4])
	}
	if start >= tsPacketSize-1 {
		return 0
	}
	pointer := int(pkt[start])
	offset := start + 1 + pointer
	if offset >= len(pkt) {
		return 0
	}
	data := pkt[offset:]
	if len(data) < 12 {
		return 0
	}
	sectionLen := int(data[1]&0x0f)<<8 | int(data[2])
	progInfoLen := int(data[10]&0x0f)<<8 | int(data[11])
	pos := 12 + progInfoLen
	for pos+5 <= min(3+sectionLen-4, len(data)) {
		streamType := data[pos]
		esPID := uint16(data[pos+1]&0x1f)<<8 | uint16(data[pos+2])
		esInfoLen := int(data[pos+3]&0x0f)<<8 | int(data[pos+4])
		if streamType == 0x1b || streamType == 0x24 || streamType == 0x02 {
			return esPID
		}
		pos += 5 + esInfoLen
	}
	return 0
}

func skipPESHeader(payload []byte) []byte {
	if len(payload) < 9 {
		return payload
	}
	if payload[0] != 0 || payload[1] != 0 || payload[2] != 1 {
		return payload
	}
	headerLen := int(payload[8])
	start := 9 + headerLen
	if start >= len(payload) {
		return nil
	}
	return payload[start:]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
