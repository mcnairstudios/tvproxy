package tvsatipscan

import (
	"bytes"
	"fmt"
	"time"

	"github.com/rs/zerolog"
)

type Transponder struct {
	FreqMHz      float64
	System       string
	Modulation   string
	BandwidthMHz int
	SymbolRateKS int
	Polarization string
	PLPID        int
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

func (t Transponder) RTSPURL(host, pids string) string {
	if pids == "sdt" {
		pids = "0,16,17"
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "rtsp://%s/?freq=%g&msys=%s&mtype=%s&pids=%s",
		host, t.FreqMHz, t.System, t.Modulation, pids)
	switch t.System {
	case "dvbt", "dvbt2":
		fmt.Fprintf(&b, "&bw=%d", t.BandwidthMHz)
		if t.System == "dvbt2" {
			fmt.Fprintf(&b, "&plp=%d", t.PLPID)
		}
	case "dvbs", "dvbs2":
		fmt.Fprintf(&b, "&pol=%s&sr=%d", t.Polarization, t.SymbolRateKS)
	case "dvbc", "dvbc2":
		fmt.Fprintf(&b, "&sr=%d", t.SymbolRateKS)
	}
	return b.String()
}

func (t Transponder) MuxKey() string {
	return muxKey(t)
}

type StreamComponent struct {
	PID        uint16
	StreamType uint8
	Language   string
	AudioType  uint8
	Label      string
	Category   string
	TypeName   string
}

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

type ScanResult struct {
	Host          string
	NetworkName   string
	Muxes         []Transponder
	Channels      []Channel
	NoSignalMuxes []Transponder
	ErrorMuxes    []Transponder
}

type Config struct {
	SeedTimeout     time.Duration
	MuxTimeout      time.Duration
	Timeout         time.Duration
	Parallel        int
	Verbose         bool
	Satellite       string
	TransmitterFile string
	Log             zerolog.Logger
	OnMuxScanned    func(done, total int)
}

func ServiceTypeName(t uint8) string {
	return serviceTypStr(t)
}

func StreamTypeName(t uint8) string {
	return streamTypStr(t)
}
