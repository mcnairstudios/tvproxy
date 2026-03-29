package tvsatipscan

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	astits "github.com/asticode/go-astits"
)

// scanResult is the output of scanning one transponder.
type scanResult struct {
	tp          Transponder
	channels    []Channel
	nitMuxes    []Transponder
	networkID   uint16
	networkName string // from NIT network_name descriptor (tag 0x40)
	elapsed     time.Duration
	err         error
	patReceived bool // PAT was received — confirms the mux is on air
	nitComplete bool // all NIT sections received (complete mux list)
	signalOnly  bool // was scanned in signal-only mode (pids=0)
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
// pids controls what the SAT>IP server sends: "0,16,17" for SI-only (fast NIT), "all" for full
// metadata, "0" for signal-only (PAT confirmation). parentCtx allows callers to cancel early
// (e.g. when a parallel race winner is found).
func scanTransponder(parentCtx context.Context, host string, tp Transponder, timeout time.Duration, pids string, verbose bool) (result scanResult) {
	start := time.Now()
	result.tp = tp
	defer func() { result.elapsed = time.Since(start) }()

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

	rtspURL := tp.RTSPURL(host, pids)
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
	result.signalOnly = (pids == "0")
	patDone, nitDone, sdtReceived := false, false, false
	nitMuxSeen := map[string]bool{}
	nitSectionsSeen := map[uint8]bool{}
	nitLastSection := uint8(0)
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
			result.patReceived = true
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
		// Track section_number / last_section_number from the raw packet payload so
		// we know when the full multi-section NIT table has been received.
		if d.NIT != nil {
			if result.networkID == 0 {
				result.networkID = d.NIT.NetworkID
				for _, desc := range d.NIT.NetworkDescriptors {
					if desc.NetworkName != nil {
						result.networkName = dvbString(desc.NetworkName.Name)
						break
					}
				}
			}
			if d.NIT.NetworkID != result.networkID {
				goto afterNIT
			}
			// Extract section_number (byte 6) and last_section_number (byte 7)
			// from the PSI section header in the raw first-packet payload.
			// Layout after pointer_field: table_id(1), flags+len(2), tid_ext(2),
			// version+cni(1), section_number(1), last_section_number(1).
			if p := d.FirstPacket; p != nil && p.Header.PayloadUnitStartIndicator && len(p.Payload) >= 9 {
				ptr := int(p.Payload[0])
				base := 1 + ptr
				if base+7 < len(p.Payload) {
					secNum := p.Payload[base+6]
					nitLastSection = p.Payload[base+7]
					nitSectionsSeen[secNum] = true
					if len(nitSectionsSeen) > int(nitLastSection) {
						nitDone = true
						result.nitComplete = true
					}
				}
			}
			for _, ts := range d.NIT.TransportStreams {
				for _, desc := range ts.TransportDescriptors {
					var mux Transponder
					var ok bool
					if desc.Unknown != nil {
						switch desc.Unknown.Tag {
						case tagTerrestrialDelivery:
							mux, ok = parseTerrestrialDelivery(desc.Unknown.Content)
						case tagSatelliteDelivery:
							mux, ok = parseSatelliteDelivery(desc.Unknown.Content)
						case tagCableDelivery:
							mux, ok = parseCableDelivery(desc.Unknown.Content)
						}
					} else if desc.Extension != nil && desc.Extension.Tag == tagExtT2Delivery && desc.Extension.Unknown != nil {
						mux, ok = parseT2Delivery(*desc.Extension.Unknown)
					}
					if ok {
						k := muxKey(mux)
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

		// pids=0: signal-only — just confirm the mux is on air via PAT.
		// pids=0,16,17: SI-only — wait for complete NIT.
		// pids=all: full scan — wait for PAT + NIT + SDT + all PMTs.
		if pids == "0" {
			if patDone {
				break
			}
		} else if pids != "all" {
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
		ch := Channel{
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
				comp := StreamComponent{
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
func scanParallel(host string, tps []Transponder, maxParallel int, timeout time.Duration, verbose bool) []scanResult {
	if maxParallel < 1 {
		maxParallel = 1
	}
	sem := make(chan struct{}, maxParallel)
	results := make([]scanResult, len(tps))
	var wg sync.WaitGroup
	for i, tp := range tps {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, tp Transponder) {
			defer wg.Done()
			defer func() { <-sem }()
			results[idx] = scanTransponder(context.Background(), host, tp, timeout, "all", verbose)
		}(i, tp)
	}
	wg.Wait()
	return results
}
