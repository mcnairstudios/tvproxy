package tvsatipscan

import (
	"fmt"
	"sort"
)

func Scan(host string, httpPort int, cfg Config) (*ScanResult, error) {
	muxes, networkName, err := resolveMuxes(host, httpPort, cfg)
	if err != nil {
		return nil, err
	}

	if len(muxes) == 0 {
		cfg.Log.Info().Msg("no muxes to scan")
		return &ScanResult{Host: host, NetworkName: networkName}, nil
	}

	cfg.Log.Info().Msg("scanning all muxes")
	parallel := cfg.Parallel
	if parallel < 1 {
		parallel = 4
	}
	results := scanParallel(host, muxes, parallel, cfg.Timeout, cfg.Log, cfg.OnMuxScanned)

	var allChannels []Channel
	var noSignalMuxes, errorMuxes []Transponder
	for _, r := range results {
		if r.err != nil {
			cfg.Log.Error().Err(r.err).Str("mux", r.tp.String()).Msg("mux scan error")
			errorMuxes = append(errorMuxes, r.tp)
			continue
		}
		if len(r.channels) == 0 {
			cfg.Log.Info().Str("mux", r.tp.String()).Msg("no signal")
			noSignalMuxes = append(noSignalMuxes, r.tp)
			continue
		}
		cfg.Log.Info().Str("mux", r.tp.String()).Int("channels", len(r.channels)).Msg("mux scan complete")
		allChannels = append(allChannels, r.channels...)
	}

	sort.Slice(allChannels, func(i, j int) bool {
		if allChannels[i].Transponder.FreqMHz != allChannels[j].Transponder.FreqMHz {
			return allChannels[i].Transponder.FreqMHz < allChannels[j].Transponder.FreqMHz
		}
		return allChannels[i].ServiceID < allChannels[j].ServiceID
	})

	return &ScanResult{
		Host:          host,
		NetworkName:   networkName,
		Muxes:         muxes,
		Channels:      allChannels,
		NoSignalMuxes: noSignalMuxes,
		ErrorMuxes:    errorMuxes,
	}, nil
}

func DiscoverMuxes(host string, httpPort int, cfg Config) ([]Transponder, string, error) {
	return resolveMuxes(host, httpPort, cfg)
}

func resolveMuxes(host string, httpPort int, cfg Config) ([]Transponder, string, error) {
	if cfg.TransmitterFile != "" {
		cfg.Log.Info().Str("file", cfg.TransmitterFile).Msg("loading muxes from transmitter file")
		muxes, err := ParseTransmitterFile(cfg.TransmitterFile)
		if err != nil {
			return nil, "", err
		}
		cfg.Log.Info().Int("count", len(muxes)).Msg("loaded muxes from transmitter file")
		return muxes, "", nil
	}

	return discoverMuxesViaNIT(host, httpPort, cfg)
}

func discoverMuxesViaNIT(host string, httpPort int, cfg Config) ([]Transponder, string, error) {
	httpBase := fmt.Sprintf("http://%s:%d", splitHost(host), httpPort)

	caps, err := fetchSatIPCaps(httpBase)
	if err != nil {
		cfg.Log.Warn().Err(err).Msg("HTTP caps failed; assuming DVB-T2 + DVB-C")
		caps = map[string]int{"dvbt2": 1, "dvbc": 1}
	}
	capsEvent := cfg.Log.Info()
	for sys, n := range caps {
		capsEvent = capsEvent.Int(sys, n)
	}
	capsEvent.Msg("capabilities")

	applySatelliteSeeds(host, caps, cfg)

	if caps["dvbt2"] > 0 && caps["dvbt"] == 0 {
		caps["dvbt"] = caps["dvbt2"]
	}

	cfg.Log.Info().Msg("discovering muxes via NIT")
	muxes, networkName := discoverMuxes(host, caps, cfg.SeedTimeout, cfg.MuxTimeout, cfg.Log)
	if networkName != "" {
		cfg.Log.Info().Str("network", networkName).Msg("network name")
	}
	if len(muxes) == 0 {
		cfg.Log.Info().Msg("no muxes discovered")
	} else {
		cfg.Log.Info().Int("count", len(muxes)).Msg("discovered muxes with signal")
	}
	return muxes, networkName, nil
}

func applySatelliteSeeds(host string, caps map[string]int, cfg Config) {
	hasSat := caps["dvbs2"] > 0 || caps["dvbs"] > 0
	if !hasSat {
		return
	}
	if cfg.Satellite != "" {
		defaultSeeds["dvbs2"] = europeanSatellites[cfg.Satellite]
		cfg.Log.Info().Str("satellite", cfg.Satellite).Msg("satellite set manually")
		return
	}
	cfg.Log.Info().Msg("detecting satellite")
	satID, satNetwork, satSeeds := detectSatellite(host, cfg.SeedTimeout, cfg.Log)
	if satID != "" {
		cfg.Log.Info().Str("satellite", satID).Str("network", satNetwork).Msg("satellite detected")
		defaultSeeds["dvbs2"] = satSeeds
	} else {
		cfg.Log.Info().Msg("no satellite detected")
		delete(caps, "dvbs2")
		delete(caps, "dvbs")
	}
}

func splitHost(hostport string) string {
	for i := len(hostport) - 1; i >= 0; i-- {
		if hostport[i] == ':' {
			return hostport[:i]
		}
	}
	return hostport
}
