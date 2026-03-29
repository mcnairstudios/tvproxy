package tvsatipscan

import (
	"fmt"
	"time"
)

// typeOrder controls the order system types are tried as BFS entry points.
var typeOrder = []string{"dvbt2", "dvbt", "dvbs2", "dvbs", "dvbc", "dvbc2"}

// defaultSeeds holds seed transponders for each system type.
// DVB-T/T2: full UHF sweep, channels 21–68 (474–850 MHz, 8 MHz steps) — works at any European transmitter.
// DVB-T2 seeds use 256qam only — 64qam seeds lock to DVB-T signal and win incorrectly.
// DVB-S2: populated after capability discovery and satellite detection (see scanner.go).
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

// muxKey returns a stable string key for a transponder used for deduplication.
// DVB-T/T2 frequencies from NIT may differ by a few hundred kHz from the seed
// (e.g. NIT reports 529.833 MHz while the seed is 530 MHz for the same mux).
// Round DVB-T/T2 to the nearest MHz. DVB-S/C keep full precision because their
// transponders can be legitimately 1 MHz apart.
func muxKey(t Transponder) string {
	if t.System == "dvbt" || t.System == "dvbt2" {
		return fmt.Sprintf("%.0f/%s", t.FreqMHz, t.System)
	}
	return fmt.Sprintf("%g/%s", t.FreqMHz, t.System)
}

// workItem is one pending scan task in the worker pool.
type workItem struct {
	tp         Transponder
	timeout    time.Duration
	signalOnly bool // use pids=0 (PAT-only confirmation, no NIT)
}
