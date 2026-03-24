package worker

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/repository"
)

const (
	hdhrDiscoverPort = 65001

	// Packet types
	hdhrTypeDiscoverReq = 0x0002
	hdhrTypeDiscoverRpy = 0x0003

	// Tags
	hdhrTagDeviceType = 0x01
	hdhrTagDeviceID   = 0x02
	hdhrTagTunerCount = 0x10
	hdhrTagBaseURL    = 0x2A
	hdhrTagLineupURL  = 0x27
	hdhrTagDeviceAuth = 0x2B

	// Device types
	hdhrDeviceTypeTuner    = 0x00000001
	hdhrDeviceTypeWildcard = 0xFFFFFFFF

	// Wildcard device ID
	hdhrDeviceIDWildcard = 0xFFFFFFFF
)

// HDHRDiscoverWorker listens for HDHomeRun UDP discovery requests on port 65001
// and responds so that Plex/Jellyfin/Emby can find the virtual tuner.
type HDHRDiscoverWorker struct {
	hdhrDeviceRepo *repository.HDHRDeviceRepository
	baseURL        string
	log            zerolog.Logger
	retryDelay     time.Duration
}

// NewHDHRDiscoverWorker creates a new HDHomeRun discovery worker.
func NewHDHRDiscoverWorker(hdhrDeviceRepo *repository.HDHRDeviceRepository, baseURL string, retryDelay time.Duration, log zerolog.Logger) *HDHRDiscoverWorker {
	if retryDelay <= 0 {
		retryDelay = 2 * time.Second
	}
	return &HDHRDiscoverWorker{
		hdhrDeviceRepo: hdhrDeviceRepo,
		baseURL:        baseURL,
		log:            log.With().Str("worker", "hdhr_discover").Logger(),
		retryDelay:     retryDelay,
	}
}

// Run starts listening for HDHomeRun discovery packets.
func (w *HDHRDiscoverWorker) Run(ctx context.Context) {
	select {
	case <-time.After(w.retryDelay):
	case <-ctx.Done():
		return
	}

	addr := &net.UDPAddr{Port: hdhrDiscoverPort}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		w.log.Error().Err(err).Int("port", hdhrDiscoverPort).Msg("failed to listen for HDHomeRun discovery")
		return
	}
	defer conn.Close()

	w.log.Info().Int("port", hdhrDiscoverPort).Msg("HDHomeRun discovery listener started")

	// Close the connection when context is done
	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	buf := make([]byte, 2048)
	for {
		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				w.log.Info().Msg("HDHomeRun discovery listener stopped")
				return
			}
			w.log.Warn().Err(err).Msg("error reading UDP packet")
			continue
		}

		if n < 8 { // minimum packet size: 2 type + 2 length + 0 payload + 4 crc
			continue
		}

		pktType, tags, ok := w.parsePacket(buf[:n])
		if !ok {
			continue
		}

		if pktType != hdhrTypeDiscoverReq {
			continue
		}

		w.handleDiscoverRequest(ctx, conn, remoteAddr, tags)
	}
}

// parsePacket parses an HDHomeRun protocol packet.
// Format: uint16 type | uint16 payload_length | payload | uint32 crc
func (w *HDHRDiscoverWorker) parsePacket(data []byte) (uint16, map[byte][]byte, bool) {
	if len(data) < 4 {
		return 0, nil, false
	}

	pktType := binary.BigEndian.Uint16(data[0:2])
	payloadLen := binary.BigEndian.Uint16(data[2:4])

	if len(data) < int(4+payloadLen+4) {
		return 0, nil, false
	}

	// Verify CRC
	crcData := data[:4+payloadLen]
	expectedCRC := binary.LittleEndian.Uint32(data[4+payloadLen : 4+payloadLen+4])
	actualCRC := crc32.ChecksumIEEE(crcData)
	if expectedCRC != actualCRC {
		return 0, nil, false
	}

	// Parse TLV tags from payload
	tags := make(map[byte][]byte)
	payload := data[4 : 4+payloadLen]
	for len(payload) > 0 {
		if len(payload) < 2 {
			break
		}
		tag := payload[0]
		tagLen, consumed := w.readVarLen(payload[1:])
		payload = payload[1+consumed:]
		if len(payload) < tagLen {
			break
		}
		tags[tag] = payload[:tagLen]
		payload = payload[tagLen:]
	}

	return pktType, tags, true
}

// readVarLen reads a variable-length field.
// If high bit of first byte is clear, length is that byte.
// If high bit is set, length is (first & 0x7f) | (second << 7).
func (w *HDHRDiscoverWorker) readVarLen(data []byte) (int, int) {
	if len(data) == 0 {
		return 0, 0
	}
	if data[0]&0x80 == 0 {
		return int(data[0]), 1
	}
	if len(data) < 2 {
		return 0, 1
	}
	return int(data[0]&0x7f) | (int(data[1]) << 7), 2
}

func (w *HDHRDiscoverWorker) handleDiscoverRequest(ctx context.Context, conn *net.UDPConn, remoteAddr *net.UDPAddr, tags map[byte][]byte) {
	// Check device type filter
	if dt, ok := tags[hdhrTagDeviceType]; ok && len(dt) == 4 {
		reqType := binary.BigEndian.Uint32(dt)
		if reqType != hdhrDeviceTypeTuner && reqType != hdhrDeviceTypeWildcard {
			return // not looking for a tuner
		}
	}

	// Check device ID filter
	var requestedID uint32 = hdhrDeviceIDWildcard
	if di, ok := tags[hdhrTagDeviceID]; ok && len(di) == 4 {
		requestedID = binary.BigEndian.Uint32(di)
	}

	// Find enabled devices
	devices, err := w.hdhrDeviceRepo.List(ctx)
	if err != nil {
		return
	}

	host := w.extractHost()

	for _, device := range devices {
		if !device.IsEnabled || device.Port <= 0 {
			continue
		}

		deviceID := w.parseDeviceID(device.DeviceID)
		if requestedID != hdhrDeviceIDWildcard && requestedID != deviceID {
			continue
		}

		deviceBaseURL := fmt.Sprintf("http://%s:%d", host, device.Port)
		reply := w.buildDiscoverReply(deviceID, device.TunerCount, device.DeviceAuth, deviceBaseURL)
		if _, err := conn.WriteToUDP(reply, remoteAddr); err != nil {
			w.log.Warn().Err(err).Str("remote", remoteAddr.String()).Msg("failed to send discover reply")
		}
	}
}

func (w *HDHRDiscoverWorker) extractHost() string {
	u, err := url.Parse(w.baseURL)
	if err != nil {
		return "localhost"
	}
	host := u.Hostname()
	if host == "" {
		return "localhost"
	}
	return host
}

// parseDeviceID converts a hex device ID string to uint32.
func (w *HDHRDiscoverWorker) parseDeviceID(id string) uint32 {
	val, err := strconv.ParseUint(id, 16, 32)
	if err != nil {
		// If not valid hex, generate a deterministic ID from the string
		return crc32.ChecksumIEEE([]byte(id))
	}
	return uint32(val)
}

// buildDiscoverReply builds a complete HDHomeRun discover reply packet.
func (w *HDHRDiscoverWorker) buildDiscoverReply(deviceID uint32, tunerCount int, deviceAuth string, baseURL string) []byte {
	// Build payload with TLV tags
	var payload []byte

	// Device type
	payload = append(payload, w.encodeTLV(hdhrTagDeviceType, w.encodeUint32(hdhrDeviceTypeTuner))...)

	// Device ID
	payload = append(payload, w.encodeTLV(hdhrTagDeviceID, w.encodeUint32(deviceID))...)

	// Tuner count
	if tunerCount > 0 {
		payload = append(payload, w.encodeTLV(hdhrTagTunerCount, []byte{byte(tunerCount)})...)
	}

	// Base URL
	payload = append(payload, w.encodeTLV(hdhrTagBaseURL, []byte(baseURL))...)

	// Lineup URL
	lineupURL := fmt.Sprintf("%s/lineup.json", baseURL)
	payload = append(payload, w.encodeTLV(hdhrTagLineupURL, []byte(lineupURL))...)

	// Device auth
	if deviceAuth != "" {
		payload = append(payload, w.encodeTLV(hdhrTagDeviceAuth, []byte(deviceAuth))...)
	}

	// Build packet: type(2) + length(2) + payload + crc(4)
	pkt := make([]byte, 4+len(payload)+4)
	binary.BigEndian.PutUint16(pkt[0:2], hdhrTypeDiscoverRpy)
	binary.BigEndian.PutUint16(pkt[2:4], uint16(len(payload)))
	copy(pkt[4:], payload)

	// CRC over type + length + payload
	crc := crc32.ChecksumIEEE(pkt[:4+len(payload)])
	binary.LittleEndian.PutUint32(pkt[4+len(payload):], crc)

	return pkt
}

// encodeTLV encodes a tag-length-value entry.
func (w *HDHRDiscoverWorker) encodeTLV(tag byte, value []byte) []byte {
	result := []byte{tag}
	result = append(result, w.encodeVarLen(len(value))...)
	result = append(result, value...)
	return result
}

// encodeVarLen encodes a variable-length integer.
func (w *HDHRDiscoverWorker) encodeVarLen(length int) []byte {
	if length < 128 {
		return []byte{byte(length)}
	}
	return []byte{byte(length&0x7f) | 0x80, byte(length >> 7)}
}

// encodeUint32 encodes a uint32 in big-endian.
func (w *HDHRDiscoverWorker) encodeUint32(val uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, val)
	return b
}
