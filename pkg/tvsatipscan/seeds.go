package tvsatipscan

import (
	"fmt"
	"time"
)

var typeOrder = []string{"dvbt2", "dvbt", "dvbs2", "dvbs", "dvbc", "dvbc2"}

var defaultSeeds map[string][]Transponder

func init() {
	dvbt := make([]Transponder, 0, 48)
	dvbt2 := make([]Transponder, 0, 48)
	for ch := 21; ch <= 68; ch++ {
		freq := float64(474 + (ch-21)*8)
		dvbt = append(dvbt, Transponder{FreqMHz: freq, System: "dvbt", Modulation: "64qam", BandwidthMHz: 8})
		dvbt2 = append(dvbt2, Transponder{FreqMHz: freq, System: "dvbt2", Modulation: "256qam", BandwidthMHz: 8})
	}
	defaultSeeds = map[string][]Transponder{
		"dvbt":  dvbt,
		"dvbt2": dvbt2,
		"dvbc": {
			{FreqMHz: 114, System: "dvbc", Modulation: "256qam", SymbolRateKS: 6952},
			{FreqMHz: 138, System: "dvbc", Modulation: "256qam", SymbolRateKS: 6952},
			{FreqMHz: 162, System: "dvbc", Modulation: "256qam", SymbolRateKS: 6952},
		},
	}
}

func muxKey(t Transponder) string {
	if t.System == "dvbt2" {
		return fmt.Sprintf("%.0f/%s/%d", t.FreqMHz, t.System, t.PLPID)
	}
	if t.System == "dvbt" {
		return fmt.Sprintf("%.0f/%s", t.FreqMHz, t.System)
	}
	return fmt.Sprintf("%g/%s", t.FreqMHz, t.System)
}

type workItem struct {
	tp         Transponder
	timeout    time.Duration
	signalOnly bool
}
