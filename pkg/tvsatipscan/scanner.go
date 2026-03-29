package tvsatipscan

import (
	"fmt"
	"os"
	"sort"
)

// Scan performs a complete channel scan against the SAT>IP server at host:rtspPort.
// httpPort is the UPnP HTTP port for capability discovery. cfg controls timing and
// parallelism. Progress is logged to stderr.
//
// The scan proceeds in three steps:
//  1. Capability discovery — determine which delivery systems the hardware supports.
//  2. NIT BFS mux discovery — find all live muxes via two-pass NIT scanning.
//  3. Full channel scan — collect PAT + SDT + PMT from every discovered mux.
func Scan(host string, httpPort int, cfg Config) (*ScanResult, error) {
	httpBase := fmt.Sprintf("http://%s:%d", splitHost(host), httpPort)

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

	applySatelliteSeeds(host, caps, cfg)

	if caps["dvbt2"] > 0 && caps["dvbt"] == 0 {
		caps["dvbt"] = caps["dvbt2"]
	}

	fmt.Fprintf(os.Stderr, "\nDiscovering muxes via NIT...\n")
	muxes, networkName := discoverMuxes(host, caps, cfg.SeedTimeout, cfg.MuxTimeout, cfg.Verbose)
	if networkName != "" {
		fmt.Fprintf(os.Stderr, "Network: %s\n", networkName)
	}

	if len(muxes) == 0 {
		fmt.Fprintf(os.Stderr, "\nNo muxes discovered.\n")
		return &ScanResult{Host: host, NetworkName: networkName}, nil
	}

	fmt.Fprintf(os.Stderr, "\nDiscovered %d mux(es) with signal:\n", len(muxes))
	for _, m := range muxes {
		fmt.Fprintf(os.Stderr, "  %s\n", m)
	}

	fmt.Fprintf(os.Stderr, "\nScanning all muxes...\n")
	parallel := cfg.Parallel
	if parallel < 1 {
		parallel = 4
	}
	results := scanParallel(host, muxes, parallel, cfg.Timeout, cfg.Verbose)

	var allChannels []Channel
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

	sort.Slice(allChannels, func(i, j int) bool {
		if allChannels[i].Transponder.FreqMHz != allChannels[j].Transponder.FreqMHz {
			return allChannels[i].Transponder.FreqMHz < allChannels[j].Transponder.FreqMHz
		}
		return allChannels[i].ServiceID < allChannels[j].ServiceID
	})

	return &ScanResult{
		Host:        host,
		NetworkName: networkName,
		Muxes:       muxes,
		Channels:    allChannels,
	}, nil
}

// DiscoverMuxes performs only the NIT BFS discovery step (steps 1–2 of Scan).
// Useful for quickly listing available muxes without a full channel scan.
func DiscoverMuxes(host string, httpPort int, cfg Config) ([]Transponder, string, error) {
	httpBase := fmt.Sprintf("http://%s:%d", splitHost(host), httpPort)

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

	applySatelliteSeeds(host, caps, cfg)

	if caps["dvbt2"] > 0 && caps["dvbt"] == 0 {
		caps["dvbt"] = caps["dvbt2"]
	}

	fmt.Fprintf(os.Stderr, "\nDiscovering muxes via NIT...\n")
	muxes, networkName := discoverMuxes(host, caps, cfg.SeedTimeout, cfg.MuxTimeout, cfg.Verbose)
	if networkName != "" {
		fmt.Fprintf(os.Stderr, "Network: %s\n", networkName)
	}
	return muxes, networkName, nil
}

// applySatelliteSeeds handles DVB-S satellite detection and populates defaultSeeds["dvbs2"].
func applySatelliteSeeds(host string, caps map[string]int, cfg Config) {
	hasSat := caps["dvbs2"] > 0 || caps["dvbs"] > 0
	if !hasSat {
		return
	}
	if cfg.Satellite != "" {
		defaultSeeds["dvbs2"] = europeanSatellites[cfg.Satellite]
		fmt.Fprintf(os.Stderr, "Satellite: %s (manual)\n", cfg.Satellite)
		return
	}
	fmt.Fprintf(os.Stderr, "Detecting satellite...")
	satID, satNetwork, satSeeds := detectSatellite(host, cfg.SeedTimeout, cfg.Verbose)
	if satID != "" {
		label := satID
		if satNetwork != "" {
			label += " — " + satNetwork
		}
		fmt.Fprintf(os.Stderr, " %s\n", label)
		defaultSeeds["dvbs2"] = satSeeds
	} else {
		fmt.Fprintf(os.Stderr, " none detected\n")
		delete(caps, "dvbs2")
		delete(caps, "dvbs")
	}
}

// splitHost extracts the hostname (without port) from a host:port string.
func splitHost(hostport string) string {
	for i := len(hostport) - 1; i >= 0; i-- {
		if hostport[i] == ':' {
			return hostport[:i]
		}
	}
	return hostport
}
