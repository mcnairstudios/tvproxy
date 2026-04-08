package tvsatipscan

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	astits "github.com/asticode/go-astits"
	"github.com/rs/zerolog"
)

type scanResult struct {
	tp          Transponder
	channels    []Channel
	nitMuxes    []Transponder
	networkID   uint16
	networkName string
	elapsed     time.Duration
	err         error
	patReceived bool
	nitComplete bool
	signalOnly  bool
	pmtData     map[uint16]*astits.PMTData
	programs    map[uint16]uint16
}

func scanTransponder(parentCtx context.Context, host string, tp Transponder, timeout time.Duration, pids string, log zerolog.Logger) (result scanResult) {
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

	rtspURL := tp.RTSPURL(host, pids)
	resp, err := c.send("DESCRIBE", rtspURL, map[string]string{"Accept": "application/sdp"}, nil)
	if err != nil {
		result.err = err
		return result
	}
	log.Debug().Str("method", "DESCRIBE").Int("status", resp.status).Msg("rtsp")
	if log.GetLevel() <= zerolog.DebugLevel && len(resp.body) > 0 {
		log.Debug().Str("sdp", string(resp.body)).Msg("sdp")
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
	log.Debug().Str("method", "PLAY").Int("status", resp.status).Msg("rtsp")
	if resp.status != 200 {
		c.teardown(controlURL, session)
		result.err = fmt.Errorf("PLAY returned %d", resp.status)
		return result
	}

	lockEnd := time.Now().Add(2 * time.Second)
	for time.Now().Before(lockEnd) {
		c.conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		c.readInterleaved() //nolint:errcheck
	}
	c.conn.SetDeadline(time.Now().Add(timeout + 5*time.Second))

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

	programs := map[uint16]uint16{}
	services := map[uint16]string{}
	svcTypes := map[uint16]uint8{}
	svcEncrypted := map[uint16]bool{}
	pmtData := map[uint16]*astits.PMTData{}
	result.signalOnly = (pids == "0")
	patDone, nitDone, sdtReceived := false, false, false
	sdtSeenSvcIDs := map[uint16]bool{}
	sdtSectionsSeen := map[uint8]bool{}
	sdtLastSection := uint8(0)
	nitMuxSeen := map[string]bool{}
	nitSectionsSeen := map[uint8]bool{}
	nitLastSection := uint8(0)
	pmtPending := map[uint16]bool{}

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

		if d.SDT != nil && patDone {
			hasOwnService := false
			for _, s := range d.SDT.Services {
				if _, ok := programs[s.ServiceID]; ok {
					hasOwnService = true
					break
				}
			}
			if hasOwnService {
				if p := d.FirstPacket; p != nil && p.Header.PayloadUnitStartIndicator && len(p.Payload) >= 9 {
					ptr := int(p.Payload[0])
					base := 1 + ptr
					if base+7 < len(p.Payload) {
						secNum := p.Payload[base+6]
						sdtLastSection = p.Payload[base+7]
						sdtSectionsSeen[secNum] = true
					}
				}

				for _, s := range d.SDT.Services {
					sdtSeenSvcIDs[s.ServiceID] = true
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

				allNamed := true
				for svcID := range programs {
					if services[svcID] == "" {
						allNamed = false
						break
					}
				}
				allSections := len(sdtSectionsSeen) > int(sdtLastSection)
				if allNamed || allSections {
					sdtReceived = true
				}
			}
		}

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

		if pids == "0" {
			if patDone {
				break
			}
		} else if pids == "0,16,17" {
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
			if patDone && sdtReceived && allPMTDone {
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
					Category:   streamCategory(uint8(es.StreamType)),
				}
				for _, desc := range es.ElementaryStreamDescriptors {
					if desc.ISO639LanguageAndAudioType != nil {
						comp.Language = strings.TrimRight(string(desc.ISO639LanguageAndAudioType.Language[:]), "\x00")
						comp.AudioType = desc.ISO639LanguageAndAudioType.Type
						if comp.Category == "" {
							comp.Category = "audio"
						}
					}
					if desc.Component != nil && len(desc.Component.Text) > 0 {
						comp.Label = dvbString(desc.Component.Text)
					}
					if desc.Subtitling != nil {
						comp.Category = "subtitle"
						if len(desc.Subtitling.Items) > 0 && len(desc.Subtitling.Items[0].Language) > 0 {
							comp.Language = strings.TrimRight(string(desc.Subtitling.Items[0].Language[:]), "\x00")
						}
					}
					if desc.Teletext != nil || desc.VBITeletext != nil {
						comp.Category = "teletext"
					}
				}
				ch.Streams = append(ch.Streams, comp)
			}
		}
		result.channels = append(result.channels, ch)
	}
	result.pmtData = pmtData
	result.programs = programs
	c.teardown(controlURL, session)
	sort.Slice(result.channels, func(i, j int) bool {
		return result.channels[i].ServiceID < result.channels[j].ServiceID
	})
	return result
}

func scanParallel(host string, tps []Transponder, maxParallel int, timeout time.Duration, log zerolog.Logger, progressFn func(done, total int)) []scanResult {
	if maxParallel < 1 {
		maxParallel = 1
	}
	sem := make(chan struct{}, maxParallel)
	results := make([]scanResult, len(tps))
	var wg sync.WaitGroup
	var completed atomic.Int32
	total := len(tps)

	for i, tp := range tps {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, tp Transponder) {
			defer wg.Done()
			defer func() { <-sem }()
			results[idx] = scanTransponder(context.Background(), host, tp, timeout, "all", log)
			done := int(completed.Add(1))
			if progressFn != nil {
				progressFn(done, total)
			}
		}(i, tp)
	}
	wg.Wait()
	return results
}
