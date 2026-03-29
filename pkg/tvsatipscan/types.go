package tvsatipscan

import (
	"bytes"
	"fmt"
	"time"
)

// Transponder holds the full tuning parameters for one multiplex.
type Transponder struct {
	FreqMHz      float64
	System       string // dvbt, dvbt2, dvbs, dvbs2, dvbc
	Modulation   string
	BandwidthMHz int    // DVB-T/T2
	SymbolRateKS int    // DVB-S/C (kSym/s)
	Polarization string // h or v (DVB-S)
}

func (t Transponder) String() string {
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

// RTSPURL builds the SAT>IP tuning URL. pids is the PID list ("0,16,17" for
// SI-only, "all" for full scan, or a custom comma-separated list).
func (t Transponder) RTSPURL(host, pids string) string {
	// "sdt" is an internal scan mode — map to SI-only PIDs for the actual request.
	if pids == "sdt" {
		pids = "0,16,17"
	}
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

// MuxKey returns a stable deduplication key for this transponder.
// DVB-T/T2 frequencies from NIT may differ by a few hundred kHz from the seed
// (e.g. NIT reports 529.833 MHz while the seed is 530 MHz for the same mux).
// DVB-T/T2 are rounded to the nearest MHz; DVB-S/C keep full precision because
// their transponders can be legitimately 1 MHz apart.
func (t Transponder) MuxKey() string {
	return muxKey(t)
}

// StreamComponent describes one elementary stream in a PMT.
type StreamComponent struct {
	PID        uint16
	StreamType uint8
	Language   string
	TypeName   string
}

// Channel is a discovered DVB service with full metadata.
type Channel struct {
	Name        string
	ServiceID   uint16
	ServiceType uint8
	Encrypted   bool
	PMTPID      uint16
	PCRPID      uint16
	Streams     []StreamComponent
	Transponder Transponder
}

// RTSPURL builds the SAT>IP URL for this specific channel, requesting only the
// PIDs needed to decode it (PAT + PMT + all elementary streams). If stream
// metadata is unavailable, falls back to "all".
func (ch Channel) RTSPURL(host string) string {
	pids := fmt.Sprintf("0,%d", ch.PMTPID)
	for _, s := range ch.Streams {
		pids += fmt.Sprintf(",%d", s.PID)
	}
	if len(ch.Streams) == 0 {
		pids = "all"
	}
	return ch.Transponder.RTSPURL(host, pids)
}

// ScanResult is the output of a complete scan.
type ScanResult struct {
	Host        string
	NetworkName string
	Muxes       []Transponder
	Channels    []Channel
}

// Config configures a scan operation.
type Config struct {
	SeedTimeout time.Duration // timeout for blind seed scans (fast pass)
	MuxTimeout  time.Duration // timeout for discovered muxes and slow retry
	Timeout     time.Duration // per-transponder timeout for the final channel scan
	Parallel    int           // max parallel scans during final channel scan
	Verbose     bool          // log RTSP exchange to stderr
	Satellite   string        // satellite ID for DVB-S/S2 ("" = auto-detect)
}

// ServiceTypeName returns the human-readable name for a DVB service type byte.
func ServiceTypeName(t uint8) string {
	return serviceTypStr(t)
}

// StreamTypeName returns the human-readable name for an ISO 13818-1 stream type byte.
func StreamTypeName(t uint8) string {
	return streamTypStr(t)
}
