package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	astits "github.com/asticode/go-astits"
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

// Descriptor tags not in go-astits v1.15.0
const (
	tagSatelliteDelivery   = 0x43
	tagCableDelivery       = 0x44
	tagTerrestrialDelivery = 0x5a
)

// transponder holds the full tuning parameters for one multiplex.
type transponder struct {
	FreqMHz      float64
	System       string // dvbt, dvbt2, dvbs, dvbs2, dvbc
	Modulation   string
	BandwidthMHz int    // DVB-T/T2
	SymbolRateKS int    // DVB-S/C (kSym/s)
	Polarization string // h or v (DVB-S)
}

func (t transponder) String() string {
	switch t.System {
	case "dvbt", "dvbt2":
		return fmt.Sprintf("%g MHz %s bw=%dMHz", t.FreqMHz, t.System, t.BandwidthMHz)
	case "dvbs", "dvbs2":
		return fmt.Sprintf("%g MHz %s %s sr=%dkS/s", t.FreqMHz, t.System, t.Polarization, t.SymbolRateKS)
	case "dvbc", "dvbc2":
		return fmt.Sprintf("%g MHz %s sr=%dkS/s", t.FreqMHz, t.System, t.SymbolRateKS)
	default:
		return fmt.Sprintf("%g MHz %s", t.FreqMHz, t.System)
	}
}

// rtspURL builds the tuning URL. pids is the PID list ("0,16,17" for SI-only, "all" for full scan).
func (t transponder) rtspURL(host, pids string) string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "rtsp://%s/?freq=%g&msys=%s&mtype=%s&pids=%s",
		host, t.FreqMHz, t.System, t.Modulation, pids)
	switch t.System {
	case "dvbt", "dvbt2":
		fmt.Fprintf(&b, "&bw=%d", t.BandwidthMHz)
	case "dvbs", "dvbs2":
		fmt.Fprintf(&b, "&pol=%s&sr=%d", t.Polarization, t.SymbolRateKS)
	case "dvbc", "dvbc2":
		fmt.Fprintf(&b, "&sr=%d", t.SymbolRateKS)
	}
	return b.String()
}

// streamComponent describes one elementary stream in a PMT.
type streamComponent struct {
	PID        uint16
	StreamType uint8
	Language   string // ISO 639 if present
	TypeName   string
}

// channel is a discovered DVB service with full metadata.
type channel struct {
	Name        string
	ServiceID   uint16
	ServiceType uint8
	Encrypted   bool // HasFreeCSAMode from SDT: service is CA-controlled
	PMTPID      uint16
	PCRPID      uint16
	Streams     []streamComponent
	Transponder transponder
}

func serviceTypStr(t uint8) string {
	if s, ok := serviceTypeName[t]; ok {
		return s
	}
	return fmt.Sprintf("0x%02x", t)
}

func streamTypStr(t uint8) string {
	if s, ok := streamTypeName[t]; ok {
		return s
	}
	return fmt.Sprintf("0x%02x", t)
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

// parseTerrestrialDelivery parses descriptor tag 0x5A. EN 300 468 §6.2.13.4
func parseTerrestrialDelivery(b []byte) (transponder, bool) {
	if len(b) < 11 {
		return transponder{}, false
	}
	freqHz := uint64(binary.BigEndian.Uint32(b[0:4])) * 10
	freqMHz := float64(freqHz) / 1e6
	bwCode := (b[4] >> 5) & 0x07
	bwMHz := [8]int{8, 7, 6, 5, 0, 0, 0, 0}[bwCode]
	constCode := (b[5] >> 6) & 0x03
	modulation := [4]string{"qpsk", "16qam", "64qam", ""}[constCode]
	return transponder{
		FreqMHz:      freqMHz,
		System:       "dvbt",
		Modulation:   modulation,
		BandwidthMHz: bwMHz,
	}, true
}

// parseSatelliteDelivery parses descriptor tag 0x43. EN 300 468 §6.2.13.2
func parseSatelliteDelivery(b []byte) (transponder, bool) {
	if len(b) < 11 {
		return transponder{}, false
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
	return transponder{
		FreqMHz:      freqMHz,
		System:       sys,
		Modulation:   modulation,
		SymbolRateKS: srKS,
		Polarization: pol,
	}, true
}

// parseCableDelivery parses descriptor tag 0x44. EN 300 468 §6.2.13.1
func parseCableDelivery(b []byte) (transponder, bool) {
	if len(b) < 11 {
		return transponder{}, false
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
	return transponder{
		FreqMHz:      freqMHz,
		System:       "dvbc",
		Modulation:   modulation,
		SymbolRateKS: srKS,
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

// rtspResponse holds a parsed RTSP response.
type rtspResponse struct {
	status  int
	headers map[string]string
	body    []byte
}

// rtspClient is a minimal RTSP client for SAT>IP scanning over TCP.
type rtspClient struct {
	conn net.Conn
	br   *bufio.Reader
	cseq int
}

func dialRTSP(host string, timeout time.Duration) (*rtspClient, error) {
	conn, err := net.DialTimeout("tcp", host, timeout)
	if err != nil {
		return nil, err
	}
	return &rtspClient{conn: conn, br: bufio.NewReader(conn)}, nil
}

func (c *rtspClient) close() { c.conn.Close() }

// teardown sends a TEARDOWN request without reading the response. This is safe to call
// while an RTP reader goroutine is still active on the same connection, because TCP is
// full-duplex — writing does not race with the goroutine's reads on the bufio.Reader.
func (c *rtspClient) teardown(controlURL, session string) {
	c.cseq++
	req := fmt.Sprintf("TEARDOWN %s RTSP/1.0\r\nCSeq: %d\r\nUser-Agent: dvbscan\r\nSession: %s\r\n\r\n",
		controlURL, c.cseq, session)
	c.conn.Write([]byte(req)) //nolint
}

func (c *rtspClient) send(method, url string, extra map[string]string, body []byte) (*rtspResponse, error) {
	c.cseq++
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s %s RTSP/1.0\r\nCSeq: %d\r\nUser-Agent: dvbscan\r\n", method, url, c.cseq)
	for k, v := range extra {
		fmt.Fprintf(&sb, "%s: %s\r\n", k, v)
	}
	if len(body) > 0 {
		fmt.Fprintf(&sb, "Content-Length: %d\r\n", len(body))
	}
	sb.WriteString("\r\n")
	if _, err := c.conn.Write([]byte(sb.String())); err != nil {
		return nil, err
	}
	if len(body) > 0 {
		if _, err := c.conn.Write(body); err != nil {
			return nil, err
		}
	}
	return c.readResponse()
}

func (c *rtspClient) readResponse() (*rtspResponse, error) {
	line, err := c.br.ReadString('\n')
	if err != nil {
		return nil, err
	}
	parts := strings.SplitN(strings.TrimSpace(line), " ", 3)
	if len(parts) < 2 {
		return nil, fmt.Errorf("bad status line: %q", line)
	}
	status, _ := strconv.Atoi(parts[1])
	hdrs := map[string]string{}
	for {
		line, err = c.br.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if kv := strings.SplitN(line, ":", 2); len(kv) == 2 {
			hdrs[strings.ToLower(strings.TrimSpace(kv[0]))] = strings.TrimSpace(kv[1])
		}
	}
	var body []byte
	if cl, ok := hdrs["content-length"]; ok {
		n, _ := strconv.Atoi(cl)
		if n > 0 {
			body = make([]byte, n)
			if _, err = io.ReadFull(c.br, body); err != nil {
				return nil, err
			}
		}
	}
	return &rtspResponse{status: status, headers: hdrs, body: body}, nil
}

func (c *rtspClient) readInterleaved() ([]byte, error) {
	for {
		b, err := c.br.ReadByte()
		if err != nil {
			return nil, err
		}
		if b != '$' {
			continue
		}
		ch, err := c.br.ReadByte()
		if err != nil {
			return nil, err
		}
		var lenBuf [2]byte
		if _, err = io.ReadFull(c.br, lenBuf[:]); err != nil {
			return nil, err
		}
		length := binary.BigEndian.Uint16(lenBuf[:])
		data := make([]byte, length)
		if _, err = io.ReadFull(c.br, data); err != nil {
			return nil, err
		}
		if ch == 0 {
			return data, nil
		}
	}
}

func stripRTPHeader(pkt []byte) ([]byte, error) {
	if len(pkt) < 12 {
		return nil, fmt.Errorf("RTP packet too short")
	}
	cc := int(pkt[0] & 0x0f)
	offset := 12 + cc*4
	if pkt[0]&0x10 != 0 {
		if len(pkt) < offset+4 {
			return nil, fmt.Errorf("RTP extension header too short")
		}
		extLen := int(binary.BigEndian.Uint16(pkt[offset+2:])) * 4
		offset += 4 + extLen
	}
	if offset > len(pkt) {
		return nil, fmt.Errorf("RTP header overruns packet")
	}
	return pkt[offset:], nil
}

// scanResult is the output of scanning one transponder.
type scanResult struct {
	tp        transponder
	channels  []channel
	nitMuxes  []transponder
	networkID uint16
	err       error
}

// buildPMTURL replaces pids=0,16,17 with pids=0,16,17,<pmtPIDs...>
func buildPMTURL(base string, pmtPIDs []uint16) string {
	extra := ""
	for _, pid := range pmtPIDs {
		extra += fmt.Sprintf(",%d", pid)
	}
	return strings.Replace(base, "pids=0,16,17", "pids=0,16,17"+extra, 1)
}

// scanTransponder tunes via SAT>IP RTSP and collects PAT + SDT + NIT + PMT (for stream metadata).
// pids controls what Minisatip sends: "0,16,17" for SI-only (fast NIT), "all" for full metadata.
// parentCtx allows callers to cancel this scan early (e.g. when a parallel race winner is found).
func scanTransponder(parentCtx context.Context, host string, tp transponder, timeout time.Duration, pids string, verbose bool) scanResult {
	result := scanResult{tp: tp}

	c, err := dialRTSP(host, 5*time.Second)
	if err != nil {
		result.err = err
		return result
	}
	defer c.close()
	c.conn.SetDeadline(time.Now().Add(15 * time.Second))

	logf := func(format string, args ...any) {
		if verbose {
			fmt.Fprintf(os.Stderr, "[dbg] "+format+"\n", args...)
		}
	}

	rtspURL := tp.rtspURL(host, pids)
	resp, err := c.send("DESCRIBE", rtspURL, map[string]string{"Accept": "application/sdp"}, nil)
	if err != nil {
		result.err = err
		return result
	}
	logf("DESCRIBE %d", resp.status)
	if verbose && len(resp.body) > 0 {
		fmt.Fprintf(os.Stderr, "[sdp]\n%s\n", resp.body)
	}
	if resp.status != 200 {
		result.err = fmt.Errorf("DESCRIBE returned %d", resp.status)
		return result
	}

	controlURL := fmt.Sprintf("rtsp://%s/stream=1", host)
	for _, line := range strings.Split(string(resp.body), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "a=control:") {
			ctrl := strings.TrimPrefix(line, "a=control:")
			if strings.HasPrefix(ctrl, "rtsp://") {
				controlURL = ctrl
			} else {
				controlURL = fmt.Sprintf("rtsp://%s/%s", host, ctrl)
			}
		}
	}

	session := resp.headers["session"]
	resp, err = c.send("SETUP", controlURL, map[string]string{
		"Session":   session,
		"Transport": "RTP/AVP/TCP;unicast;interleaved=0-1",
	}, nil)
	if err != nil {
		result.err = err
		return result
	}
	if resp.status != 200 {
		result.err = fmt.Errorf("SETUP returned %d", resp.status)
		return result
	}
	if s := resp.headers["session"]; s != "" {
		session = strings.SplitN(s, ";", 2)[0]
	}

	resp, err = c.send("PLAY", rtspURL, map[string]string{
		"Session": session,
		"Range":   "npt=0.000-",
	}, nil)
	if err != nil {
		result.err = err
		return result
	}
	logf("PLAY %d", resp.status)
	if resp.status != 200 {
		c.teardown(controlURL, session)
		result.err = fmt.Errorf("PLAY returned %d", resp.status)
		return result
	}

	c.conn.SetDeadline(time.Now().Add(timeout))

	pr, pw := io.Pipe()
	ctx, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()

	go func() {
		defer pw.Close()
		for {
			pkt, err := c.readInterleaved()
			if err != nil {
				return
			}
			payload, err := stripRTPHeader(pkt)
			if err != nil || len(payload) == 0 {
				continue
			}
			if len(payload)%188 == 0 && payload[0] == 0x47 {
				pw.Write(payload) //nolint
			}
		}
	}()

	programs := map[uint16]uint16{} // serviceID → pmtPID
	services := map[uint16]string{}
	svcTypes := map[uint16]uint8{}
	svcEncrypted := map[uint16]bool{}
	pmtData := map[uint16]*astits.PMTData{} // serviceID → PMT
	patDone, nitDone, sdtReceived := false, false, false
	nitMuxSeen := map[string]bool{}
	pmtPending := map[uint16]bool{} // pmtPID → collected?

	dmx := astits.NewDemuxer(ctx, pr, astits.DemuxerOptPacketSize(188))
	for {
		d, err := dmx.NextData()
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) ||
				errors.Is(err, context.Canceled) ||
				errors.Is(err, io.EOF) ||
				errors.Is(err, io.ErrClosedPipe) {
				break
			}
			continue
		}
		if d == nil {
			continue
		}

		if d.PAT != nil && !patDone {
			for _, p := range d.PAT.Programs {
				if p.ProgramNumber != 0 {
					programs[p.ProgramNumber] = p.ProgramMapID
					pmtPending[p.ProgramMapID] = false
				}
			}
			patDone = true
		}

		// Accumulate all SDT packets — PID 17 carries table 0x42 (current TS) and
		// 0x46 (other TSes). Keep merging; don't overwrite names already found.
		if d.SDT != nil {
			for _, s := range d.SDT.Services {
				if s.HasFreeCSAMode {
					svcEncrypted[s.ServiceID] = true
				}
				for _, desc := range s.Descriptors {
					if desc.Tag == astits.DescriptorTagService && desc.Service != nil {
						if services[s.ServiceID] == "" {
							services[s.ServiceID] = dvbString(desc.Service.Name)
							svcTypes[s.ServiceID] = desc.Service.Type
						}
					}
				}
			}
			sdtReceived = true
		}

		// Accumulate NIT muxes. PID 16 carries NIT-actual (0x40) and NIT-other (0x41).
		// Only collect muxes from the same network ID as the first NIT received;
		// this avoids adding unreachable national-network muxes from NIT-other.
		if d.NIT != nil {
			if !nitDone {
				result.networkID = d.NIT.NetworkID
				nitDone = true
			}
			if d.NIT.NetworkID != result.networkID {
				goto afterNIT
			}
			for _, ts := range d.NIT.TransportStreams {
				for _, desc := range ts.TransportDescriptors {
					if desc.Unknown == nil {
						continue
					}
					var mux transponder
					var ok bool
					switch desc.Unknown.Tag {
					case tagTerrestrialDelivery:
						mux, ok = parseTerrestrialDelivery(desc.Unknown.Content)
					case tagSatelliteDelivery:
						mux, ok = parseSatelliteDelivery(desc.Unknown.Content)
					case tagCableDelivery:
						mux, ok = parseCableDelivery(desc.Unknown.Content)
					}
					if ok {
						k := fmt.Sprintf("%g/%s", mux.FreqMHz, mux.System)
						if !nitMuxSeen[k] {
							nitMuxSeen[k] = true
							result.nitMuxes = append(result.nitMuxes, mux)
						}
					}
				}
			}
		}
	afterNIT:

		if d.PMT != nil {
			for svcID, pmtPID := range programs {
				if d.PMT.ProgramNumber == svcID && !pmtPending[pmtPID] {
					cp := *d.PMT
					pmtData[svcID] = &cp
					pmtPending[pmtPID] = true
					break
				}
			}
		}

		// For SI-only scans (pids=0,16,17) PMT PIDs are never sent; break on PAT+NIT.
		// For full scans, also require all PMTs and at least one SDT batch.
		if pids != "all" {
			if patDone && nitDone {
				break
			}
		} else {
			allPMTDone := patDone
			if allPMTDone {
				for _, collected := range pmtPending {
					if !collected {
						allPMTDone = false
						break
					}
				}
			}
			if patDone && nitDone && sdtReceived && allPMTDone {
				break
			}
		}
	}

	for svcID, pmtPID := range programs {
		name := services[svcID]
		if name == "" {
			name = fmt.Sprintf("SID:%d", svcID)
		}
		ch := channel{
			Name:        name,
			ServiceID:   svcID,
			ServiceType: svcTypes[svcID],
			Encrypted:   svcEncrypted[svcID],
			PMTPID:      pmtPID,
			Transponder: tp,
		}
		if pmt, ok := pmtData[svcID]; ok {
			ch.PCRPID = pmt.PCRPID
			for _, es := range pmt.ElementaryStreams {
				comp := streamComponent{
					PID:        es.ElementaryPID,
					StreamType: uint8(es.StreamType),
					TypeName:   streamTypStr(uint8(es.StreamType)),
				}
				for _, desc := range es.ElementaryStreamDescriptors {
					if desc.ISO639LanguageAndAudioType != nil {
						comp.Language = string(desc.ISO639LanguageAndAudioType.Language[:])
					}
				}
				ch.Streams = append(ch.Streams, comp)
			}
		}
		result.channels = append(result.channels, ch)
	}
	c.teardown(controlURL, session)
	sort.Slice(result.channels, func(i, j int) bool {
		return result.channels[i].ServiceID < result.channels[j].ServiceID
	})
	return result
}

// scanParallel scans multiple transponders with up to maxParallel concurrent sessions.
func scanParallel(host string, tps []transponder, maxParallel int, timeout time.Duration, verbose bool) []scanResult {
	if maxParallel < 1 {
		maxParallel = 1
	}
	sem := make(chan struct{}, maxParallel)
	results := make([]scanResult, len(tps))
	var wg sync.WaitGroup
	for i, tp := range tps {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, tp transponder) {
			defer wg.Done()
			defer func() { <-sem }()
			results[idx] = scanTransponder(context.Background(), host, tp, timeout, "all", verbose)
		}(i, tp)
	}
	wg.Wait()
	return results
}

// fetchSatIPCaps fetches server capabilities from the UPnP desc.xml.
// Returns a map of canonical system name → tuner count.
// e.g. {"dvbt2": 4, "dvbc": 4}
func fetchSatIPCaps(httpBase string) (map[string]int, error) {
	resp, err := http.Get(httpBase + "/desc.xml")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	re := regexp.MustCompile(`X_SATIPCAP[^>]*>([^<]+)<`)
	m := re.FindSubmatch(body)
	if m == nil {
		return nil, fmt.Errorf("X_SATIPCAP not found in desc.xml")
	}
	caps := map[string]int{}
	for _, part := range strings.Split(string(m[1]), ",") {
		part = strings.TrimSpace(part)
		dashIdx := strings.LastIndex(part, "-")
		if dashIdx < 0 {
			continue
		}
		sys := strings.ToLower(part[:dashIdx])
		n, _ := strconv.Atoi(part[dashIdx+1:])
		caps[sys] += n
	}
	return caps, nil
}

// defaultSeeds returns seed transponders to try for each system type.
var defaultSeeds = map[string][]transponder{
	"dvbt": {
		{FreqMHz: 490, System: "dvbt", Modulation: "64qam", BandwidthMHz: 8},
		{FreqMHz: 514, System: "dvbt", Modulation: "64qam", BandwidthMHz: 8},
		{FreqMHz: 474, System: "dvbt", Modulation: "64qam", BandwidthMHz: 8},
		{FreqMHz: 482, System: "dvbt", Modulation: "64qam", BandwidthMHz: 8},
		{FreqMHz: 498, System: "dvbt", Modulation: "64qam", BandwidthMHz: 8},
		{FreqMHz: 506, System: "dvbt", Modulation: "64qam", BandwidthMHz: 8},
		{FreqMHz: 522, System: "dvbt", Modulation: "64qam", BandwidthMHz: 8},
		{FreqMHz: 530, System: "dvbt", Modulation: "64qam", BandwidthMHz: 8},
		{FreqMHz: 538, System: "dvbt", Modulation: "64qam", BandwidthMHz: 8},
		{FreqMHz: 546, System: "dvbt", Modulation: "64qam", BandwidthMHz: 8},
		{FreqMHz: 554, System: "dvbt", Modulation: "64qam", BandwidthMHz: 8},
		{FreqMHz: 562, System: "dvbt", Modulation: "64qam", BandwidthMHz: 8},
		{FreqMHz: 570, System: "dvbt", Modulation: "64qam", BandwidthMHz: 8},
		{FreqMHz: 578, System: "dvbt", Modulation: "64qam", BandwidthMHz: 8},
	},
	// dvbt2 seeds: 256qam only — 64qam seeds lock to DVB-T signal and win incorrectly.
	"dvbt2": {
		{FreqMHz: 545.8, System: "dvbt2", Modulation: "256qam", BandwidthMHz: 8},
		{FreqMHz: 562, System: "dvbt2", Modulation: "256qam", BandwidthMHz: 8},
		{FreqMHz: 578, System: "dvbt2", Modulation: "256qam", BandwidthMHz: 8},
	},
	"dvbs2": {
		// Astra 28.2E — common UK satellite (Sky/Freesat)
		{FreqMHz: 10773, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 23000, Polarization: "h"},
		{FreqMHz: 10847, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 23000, Polarization: "h"},
		{FreqMHz: 11778, System: "dvbs2", Modulation: "qpsk", SymbolRateKS: 27500, Polarization: "h"},
	},
	"dvbc": {
		{FreqMHz: 114, System: "dvbc", Modulation: "256qam", SymbolRateKS: 6952},
		{FreqMHz: 138, System: "dvbc", Modulation: "256qam", SymbolRateKS: 6952},
		{FreqMHz: 162, System: "dvbc", Modulation: "256qam", SymbolRateKS: 6952},
	},
}

// scanGroup is a set of muxes that can be scanned in parallel on the same physical signal source.
// For DVB-T/C this is the whole network. For DVB-S it's muxes sharing the same pol+band.
type scanGroup struct {
	label string
	muxes []transponder
}

// buildScanGroups returns one scan group containing all muxes.
// All DVB types (T, C, S) are fully parallel within a network:
// - DVB-T/C: shared aerial/cable, all independent
// - DVB-S/S2: Minisatip handles DiSEqC/LNB switching per-session internally;
//   all transponders on the same satellite share a NIT and scan in parallel.
func buildScanGroups(muxes []transponder) []scanGroup {
	return []scanGroup{{label: "all", muxes: muxes}}
}

// nitResult is the output of a single NIT seed scan.
type nitResult struct {
	sys       string
	seed      transponder
	networkID uint16
	nitMuxes  []transponder
	channels  int
	err       error
}

// discoverNIT runs NIT seed scans for all active system types simultaneously.
// maxParallel limits how many seed scans run at once across ALL types (0 = unlimited).
func discoverNIT(host string, caps map[string]int, nitTimeout time.Duration, maxParallel int, verbose bool) []nitResult {
	type task struct {
		sys   string
		count int
	}
	var tasks []task
	for sys, count := range caps {
		if count > 0 {
			if _, ok := defaultSeeds[sys]; ok {
				tasks = append(tasks, task{sys, count})
			}
		}
	}

	// Global semaphore across all types; nil means unlimited.
	var globalSem chan struct{}
	if maxParallel > 0 {
		globalSem = make(chan struct{}, maxParallel)
	}

	results := make([]nitResult, len(tasks))
	var outerWg sync.WaitGroup

	for i, t := range tasks {
		outerWg.Add(1)
		go func(idx int, sys string, count int) {
			defer outerWg.Done()
			seeds := defaultSeeds[sys]
			label := count
			if maxParallel > 0 && maxParallel < count {
				label = maxParallel
			}
			fmt.Fprintf(os.Stderr, "  [%s] trying %d seeds (max %d concurrent)\n", sys, len(seeds), label)

			sysCtx, sysCancel := context.WithCancel(context.Background())
			found := make(chan nitResult, 1)
			var innerWg sync.WaitGroup
			for _, seed := range seeds {
				innerWg.Add(1)
				go func(seed transponder) {
					defer innerWg.Done()
					if globalSem != nil {
						select {
						case globalSem <- struct{}{}:
							defer func() { <-globalSem }()
						case <-sysCtx.Done():
							return
						}
					}
					r := scanTransponder(sysCtx, host, seed, nitTimeout, "0,16,17", verbose)
					if r.err != nil || (len(r.nitMuxes) == 0 && r.networkID == 0) {
						if verbose {
							fmt.Fprintf(os.Stderr, "  [%s] %s → no signal\n", sys, seed)
						}
						return
					}
					nr := nitResult{
						sys:       sys,
						seed:      seed,
						networkID: r.networkID,
						nitMuxes:  r.nitMuxes,
						channels:  len(r.channels),
					}
					fmt.Fprintf(os.Stderr, "  [%s] %s → %d ch, network 0x%04x, NIT has %d muxes\n",
						sys, seed, nr.channels, nr.networkID, len(nr.nitMuxes))
					select {
					case found <- nr:
					default:
					}
				}(seed)
			}
			go func() { innerWg.Wait(); close(found) }()

			if nr, ok := <-found; ok {
				sysCancel()
				results[idx] = nr
			} else {
				results[idx] = nitResult{sys: sys, err: fmt.Errorf("no signal on any seed")}
			}
			sysCancel()
			innerWg.Wait()
		}(i, t.sys, t.count)
	}

	outerWg.Wait()
	return results
}

func main() {
	host := flag.String("host", "192.168.1.149:554", "Minisatip RTSP host:port")
	httpPort := flag.Int("http-port", 8875, "Minisatip HTTP port (for capability discovery)")
	timeout := flag.Duration("timeout", 15*time.Second, "Per-transponder scan timeout")
	nitTimeout := flag.Duration("nit-timeout", 20*time.Second, "NIT seed scan timeout")
	parallel := flag.Int("parallel", 4, "Max parallel scans per scan group")
	verbose := flag.Bool("v", false, "Verbose RTSP exchange")
	jsonOut := flag.Bool("json", false, "Output results as JSON")
	baseline := flag.String("baseline", "", "Path to save/load baseline JSON for per-mux comparison")
	nitOnly := flag.Bool("nit-only", false, "Run NIT discovery only, print mux list, then exit")
	nitParallel := flag.Int("nit-parallel", 0, "Max concurrent NIT seed scans across all types (0 = unlimited)")
	flag.Parse()

	rtspHost := *host
	httpHost := strings.Split(rtspHost, ":")[0]
	httpBase := fmt.Sprintf("http://%s:%d", httpHost, *httpPort)

	// Step 1: capabilities
	fmt.Fprintf(os.Stderr, "Querying %s...\n", rtspHost)
	caps, err := fetchSatIPCaps(httpBase)
	if err != nil {
		fmt.Fprintf(os.Stderr, "HTTP caps failed (%v); assuming DVB-T2 + DVB-C\n", err)
		caps = map[string]int{"dvbt2": 1, "dvbc": 1}
	}
	fmt.Fprintf(os.Stderr, "Capabilities:")
	for sys, n := range caps {
		fmt.Fprintf(os.Stderr, "  %s×%d", sys, n)
	}
	fmt.Fprintf(os.Stderr, "\n")

	// DVB-T2 hardware handles DVB-T too
	if caps["dvbt2"] > 0 && caps["dvbt"] == 0 {
		caps["dvbt"] = caps["dvbt2"]
	}

	// Step 2: NIT discovery — all system types in parallel
	fmt.Fprintf(os.Stderr, "\nNIT discovery (all types in parallel)...\n")
	nitResults := discoverNIT(rtspHost, caps, *nitTimeout, *nitParallel, *verbose)

	// Step 3: build networks — group NIT results by network_id to detect overlap
	type network struct {
		id        uint16
		systems   []string
		muxes     []transponder
		muxesSeen map[string]bool
	}
	networks := map[uint16]*network{}

	for _, nr := range nitResults {
		if nr.err != nil || nr.channels == 0 {
			continue
		}
		net, exists := networks[nr.networkID]
		if !exists {
			net = &network{id: nr.networkID, muxesSeen: map[string]bool{}}
			networks[nr.networkID] = net
		}
		net.systems = appendUnique(net.systems, nr.sys)

		addMux := func(m transponder) {
			k := fmt.Sprintf("%g/%s", m.FreqMHz, m.System)
			if !net.muxesSeen[k] {
				net.muxes = append(net.muxes, m)
				net.muxesSeen[k] = true
			}
		}
		addMux(nr.seed)
		for _, m := range nr.nitMuxes {
			addMux(m)
		}
	}

	if len(networks) == 0 {
		fmt.Fprintf(os.Stderr, "\nNo networks discovered.\n")
		os.Exit(0)
	}

	fmt.Fprintf(os.Stderr, "\nDiscovered %d network(s):\n", len(networks))
	totalMuxes := 0
	for _, net := range networks {
		overlap := ""
		if len(net.systems) > 1 {
			overlap = " ← OVERLAP (same network, scanning once)"
		}
		fmt.Fprintf(os.Stderr, "  Network 0x%04x: %d muxes via %v%s\n",
			net.id, len(net.muxes), net.systems, overlap)
		for _, m := range net.muxes {
			fmt.Fprintf(os.Stderr, "    %s\n", m)
		}
		totalMuxes += len(net.muxes)
	}
	fmt.Fprintf(os.Stderr, "Total muxes to scan: %d\n", totalMuxes)

	if *nitOnly {
		os.Exit(0)
	}

	// Step 4: full channel scan, grouped for parallelism
	fmt.Fprintf(os.Stderr, "\nScanning all muxes...\n")
	var allChannels []channel

	for _, net := range networks {
		groups := buildScanGroups(net.muxes)
		for _, grp := range groups {
			if len(groups) > 1 {
				fmt.Fprintf(os.Stderr, "  Group %q (%d muxes):\n", grp.label, len(grp.muxes))
			}
			results := scanParallel(rtspHost, grp.muxes, *parallel, *timeout, *verbose)
			for _, r := range results {
				if r.err != nil {
					fmt.Fprintf(os.Stderr, "  %s: %v\n", r.tp, r.err)
					continue
				}
				if len(r.channels) == 0 {
					fmt.Fprintf(os.Stderr, "  %s: no signal\n", r.tp)
					continue
				}
				fmt.Fprintf(os.Stderr, "  %s: %d channels\n", r.tp, len(r.channels))
				allChannels = append(allChannels, r.channels...)
			}
		}
	}

	sort.Slice(allChannels, func(i, j int) bool {
		if allChannels[i].Transponder.FreqMHz != allChannels[j].Transponder.FreqMHz {
			return allChannels[i].Transponder.FreqMHz < allChannels[j].Transponder.FreqMHz
		}
		return allChannels[i].ServiceID < allChannels[j].ServiceID
	})

	fmt.Fprintf(os.Stderr, "\nTotal: %d channels across %d networks\n\n",
		len(allChannels), len(networks))

	// Per-mux summary: always printed so regressions are visible at a glance.
	printMuxSummary(os.Stderr, allChannels)

	// Baseline comparison: load reference JSON if provided, diff per mux.
	if *baseline != "" {
		if _, err := os.Stat(*baseline); os.IsNotExist(err) {
			// Baseline doesn't exist yet — save current scan as reference.
			if err := saveBaseline(*baseline, rtspHost, allChannels); err != nil {
				fmt.Fprintf(os.Stderr, "WARNING: could not save baseline: %v\n", err)
			} else {
				fmt.Fprintf(os.Stderr, "Baseline saved to %s\n", *baseline)
			}
		} else {
			// Baseline exists — compare.
			compareBaseline(os.Stderr, *baseline, allChannels)
		}
	}

	if *jsonOut {
		printJSON(rtspHost, allChannels)
	} else {
		printTable(allChannels)
	}
}

// muxKey returns a stable string key for a transponder.
func muxKey(t transponder) string {
	return fmt.Sprintf("%g/%s", t.FreqMHz, t.System)
}

// printMuxSummary writes a per-mux channel count table to w.
func printMuxSummary(w *os.File, channels []channel) {
	type muxStat struct {
		label   string
		total   int
		named   int
		encrypted int
	}
	order := []string{}
	stats := map[string]*muxStat{}
	for _, ch := range channels {
		k := muxKey(ch.Transponder)
		if _, ok := stats[k]; !ok {
			stats[k] = &muxStat{label: ch.Transponder.String()}
			order = append(order, k)
		}
		stats[k].total++
		if !strings.HasPrefix(ch.Name, "SID:") {
			stats[k].named++
		}
		if ch.Encrypted {
			stats[k].encrypted++
		}
	}
	fmt.Fprintf(w, "Per-mux summary:\n")
	for _, k := range order {
		s := stats[k]
		enc := ""
		if s.encrypted > 0 {
			enc = fmt.Sprintf(", %d encrypted", s.encrypted)
		}
		unnamed := s.total - s.named
		unnamedStr := ""
		if unnamed > 0 {
			unnamedStr = fmt.Sprintf(", %d unnamed", unnamed)
		}
		fmt.Fprintf(w, "  %-35s %d channels%s%s\n", s.label, s.total, unnamedStr, enc)
	}
	fmt.Fprintf(w, "\n")
}

// baselineChannel is a minimal per-channel record for baseline comparison.
type baselineChannel struct {
	Name      string  `json:"name"`
	ServiceID uint16  `json:"service_id"`
	MuxKey    string  `json:"mux_key"`
	MuxLabel  string  `json:"mux_label"`
	Encrypted bool    `json:"encrypted"`
}

type baselineFile struct {
	ScannedAt string            `json:"scanned_at"`
	Total     int               `json:"total"`
	Channels  []baselineChannel `json:"channels"`
}

func saveBaseline(path, host string, channels []channel) error {
	bf := baselineFile{
		ScannedAt: time.Now().Format(time.RFC3339),
		Total:     len(channels),
	}
	for _, ch := range channels {
		bf.Channels = append(bf.Channels, baselineChannel{
			Name:      ch.Name,
			ServiceID: ch.ServiceID,
			MuxKey:    muxKey(ch.Transponder),
			MuxLabel:  ch.Transponder.String(),
			Encrypted: ch.Encrypted,
		})
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(bf)
}

func compareBaseline(w *os.File, path string, current []channel) {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(w, "WARNING: could not open baseline %s: %v\n", path, err)
		return
	}
	defer f.Close()
	var bf baselineFile
	if err := json.NewDecoder(f).Decode(&bf); err != nil {
		fmt.Fprintf(w, "WARNING: could not parse baseline: %v\n", err)
		return
	}

	// Index baseline by mux key → set of SIDs
	type chanInfo struct{ name string; encrypted bool }
	baseBySID := map[uint16]chanInfo{}
	baseMux := map[string][]uint16{}
	for _, bc := range bf.Channels {
		baseBySID[bc.ServiceID] = chanInfo{bc.Name, bc.Encrypted}
		baseMux[bc.MuxKey] = append(baseMux[bc.MuxKey], bc.ServiceID)
	}
	curBySID := map[uint16]chanInfo{}
	curMux := map[string][]uint16{}
	curMuxLabel := map[string]string{}
	for _, ch := range current {
		k := muxKey(ch.Transponder)
		curBySID[ch.ServiceID] = chanInfo{ch.Name, ch.Encrypted}
		curMux[k] = append(curMux[k], ch.ServiceID)
		curMuxLabel[k] = ch.Transponder.String()
	}

	// Collect all mux keys from both
	allMuxes := map[string]bool{}
	for k := range baseMux { allMuxes[k] = true }
	for k := range curMux  { allMuxes[k] = true }

	allOK := true
	fmt.Fprintf(w, "Comparison vs baseline (%s, %d channels):\n", bf.ScannedAt, bf.Total)
	for k := range allMuxes {
		base := baseMux[k]
		cur := curMux[k]
		label := curMuxLabel[k]
		if label == "" {
			// mux in baseline but missing from current scan
			fmt.Fprintf(w, "  MISSING MUX %-30s (baseline had %d channels)\n", k, len(base))
			allOK = false
			continue
		}
		baseSet := map[uint16]bool{}
		for _, sid := range base { baseSet[sid] = true }
		curSet := map[uint16]bool{}
		for _, sid := range cur { curSet[sid] = true }

		var lost, gained []uint16
		for _, sid := range base {
			if !curSet[sid] { lost = append(lost, sid) }
		}
		for _, sid := range cur {
			if !baseSet[sid] { gained = append(gained, sid) }
		}

		status := "OK"
		if len(lost) > 0 || len(gained) > 0 {
			status = "CHANGED"
			allOK = false
		}
		fmt.Fprintf(w, "  %-35s base=%d cur=%d [%s]\n", label, len(base), len(cur), status)
		for _, sid := range lost {
			info := baseBySID[sid]
			fmt.Fprintf(w, "    - LOST    SID:%d (%s)\n", sid, info.name)
		}
		for _, sid := range gained {
			info := curBySID[sid]
			fmt.Fprintf(w, "    + GAINED  SID:%d (%s)\n", sid, info.name)
		}
	}
	if allOK {
		fmt.Fprintf(w, "  All muxes match baseline.\n")
	}
	fmt.Fprintf(w, "\n")
}

// jsonStream is the JSON representation of one elementary stream.
type jsonStream struct {
	PID      uint16 `json:"pid"`
	Type     uint8  `json:"type"`
	TypeName string `json:"type_name"`
	Language string `json:"language,omitempty"`
}

// jsonChannel is the full JSON representation of a scanned channel.
type jsonChannel struct {
	Name        string          `json:"name"`
	ServiceID   uint16          `json:"service_id"`
	ServiceType string          `json:"service_type"`
	Encrypted   bool            `json:"encrypted"`
	PMTPID      uint16          `json:"pmt_pid"`
	PCRPID      uint16          `json:"pcr_pid"`
	URL         string          `json:"url"`
	Transponder jsonTransponder `json:"transponder"`
	Streams     []jsonStream    `json:"streams"`
}

type jsonTransponder struct {
	FreqMHz      float64 `json:"freq_mhz"`
	System       string  `json:"system"`
	Modulation   string  `json:"modulation"`
	BandwidthMHz int     `json:"bandwidth_mhz,omitempty"`
	SymbolRateKS int     `json:"symbol_rate_ks,omitempty"`
	Polarization string  `json:"polarization,omitempty"`
}

func channelRTSPURL(host string, ch channel) string {
	// Build PID list: PAT + PMT + all elementary streams
	pids := fmt.Sprintf("0,%d", ch.PMTPID)
	for _, s := range ch.Streams {
		pids += fmt.Sprintf(",%d", s.PID)
	}
	if len(ch.Streams) == 0 {
		pids = "all"
	}
	return ch.Transponder.rtspURL(host, pids)
}

func printJSON(host string, channels []channel) {
	out := make([]jsonChannel, 0, len(channels))
	for _, ch := range channels {
		jch := jsonChannel{
			Name:        ch.Name,
			ServiceID:   ch.ServiceID,
			ServiceType: serviceTypStr(ch.ServiceType),
			Encrypted:   ch.Encrypted,
			PMTPID:      ch.PMTPID,
			PCRPID:      ch.PCRPID,
			URL:         channelRTSPURL(host, ch),
			Transponder: jsonTransponder{
				FreqMHz:      ch.Transponder.FreqMHz,
				System:       ch.Transponder.System,
				Modulation:   ch.Transponder.Modulation,
				BandwidthMHz: ch.Transponder.BandwidthMHz,
				SymbolRateKS: ch.Transponder.SymbolRateKS,
				Polarization: ch.Transponder.Polarization,
			},
		}
		for _, s := range ch.Streams {
			lang := strings.TrimRight(s.Language, "\x00")
			jch.Streams = append(jch.Streams, jsonStream{
				PID:      s.PID,
				Type:     s.StreamType,
				TypeName: s.TypeName,
				Language: lang,
			})
		}
		out = append(out, jch)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(out) //nolint
}

func printTable(channels []channel) {
	fmt.Printf("%-35s %-10s %-6s %-6s %-22s %s\n",
		"Name", "Type", "SID", "PCR", "Transponder", "Streams")
	fmt.Println(strings.Repeat("-", 115))
	for _, ch := range channels {
		fmt.Printf("%-35s %-10s %-6d %-6d %-22s %s\n",
			ch.Name, serviceTypStr(ch.ServiceType), ch.ServiceID, ch.PCRPID,
			ch.Transponder.String(), formatStreams(ch.Streams))
	}
}

func formatStreams(comps []streamComponent) string {
	if len(comps) == 0 {
		return "(no PMT)"
	}
	var parts []string
	for _, s := range comps {
		p := s.TypeName
		if s.Language != "" {
			p += "[" + strings.TrimRight(s.Language, "\x00") + "]"
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, " | ")
}

func appendUnique(slice []string, s string) []string {
	for _, v := range slice {
		if v == s {
			return slice
		}
	}
	return append(slice, s)
}
