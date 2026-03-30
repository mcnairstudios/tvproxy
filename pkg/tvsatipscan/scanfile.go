package tvsatipscan

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var DVBTablesDir = envOr("TVPROXY_DVB_TABLES_DIR", "/usr/share/dvb")

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ListTransmitters returns the sorted list of transmitter names for the given
// delivery system directory (e.g. "dvb-t", "dvb-s", "dvb-c").
func ListTransmitters(system string) ([]string, error) {
	dir := filepath.Join(DVBTablesDir, system)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading transmitter directory %s: %w", dir, err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// ParseTransmitterFile parses a dtv-scan-tables file and returns the list of
// transponders. transmitterFile is a relative path like "dvb-t/uk-CrystalPalace".
func ParseTransmitterFile(transmitterFile string) ([]Transponder, error) {
	path := filepath.Join(DVBTablesDir, transmitterFile)
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening transmitter file %s: %w", path, err)
	}
	defer f.Close()

	var transponders []Transponder
	current := map[string]string{}
	inBlock := false

	flush := func() {
		if !inBlock || len(current) == 0 {
			return
		}
		tp, ok := parseTransponderEntry(current)
		if ok {
			transponders = append(transponders, tp)
		}
		current = map[string]string{}
		inBlock = false
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			flush()
			inBlock = true
			continue
		}
		if inBlock {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				current[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		}
	}
	flush()

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading transmitter file: %w", err)
	}
	return transponders, nil
}

func parseTransponderEntry(fields map[string]string) (Transponder, bool) {
	system := normaliseSystem(fields["DELIVERY_SYSTEM"])
	if system == "" {
		return Transponder{}, false
	}

	freqStr := fields["FREQUENCY"]
	freqHz, err := strconv.ParseFloat(freqStr, 64)
	if err != nil {
		return Transponder{}, false
	}

	tp := Transponder{
		System:     system,
		Modulation: normaliseModulation(fields["MODULATION"]),
	}

	switch system {
	case "dvbt", "dvbt2":
		tp.FreqMHz = freqHz / 1e6
		bwStr := fields["BANDWIDTH_HZ"]
		if bw, err := strconv.ParseFloat(bwStr, 64); err == nil {
			tp.BandwidthMHz = int(bw / 1e6)
		}
		if tp.BandwidthMHz == 0 {
			tp.BandwidthMHz = 8
		}
		if tp.Modulation == "" {
			if system == "dvbt2" {
				tp.Modulation = "256qam"
			} else {
				tp.Modulation = "64qam"
			}
		}
		if system == "dvbt2" {
			if sid, err := strconv.Atoi(fields["STREAM_ID"]); err == nil && sid >= 0 {
				tp.PLPID = sid
			}
		}
	case "dvbs", "dvbs2":
		tp.FreqMHz = freqHz / 1e3
		if sr, err := strconv.ParseFloat(fields["SYMBOL_RATE"], 64); err == nil {
			tp.SymbolRateKS = int(sr / 1e3)
		}
		tp.Polarization = normalisePolarization(fields["POLARIZATION"])
		if tp.Modulation == "" {
			tp.Modulation = "qpsk"
		}
	case "dvbc", "dvbc2":
		tp.FreqMHz = freqHz / 1e6
		if sr, err := strconv.ParseFloat(fields["SYMBOL_RATE"], 64); err == nil {
			tp.SymbolRateKS = int(sr / 1e3)
		}
		if tp.Modulation == "" {
			tp.Modulation = "256qam"
		}
	}

	return tp, true
}

func normaliseSystem(s string) string {
	switch strings.ToUpper(s) {
	case "DVBT":
		return "dvbt"
	case "DVBT2":
		return "dvbt2"
	case "DVBS":
		return "dvbs"
	case "DVBS2":
		return "dvbs2"
	case "DVBC_ANNEX_A", "DVBC_ANNEX_B", "DVBC":
		return "dvbc"
	default:
		return ""
	}
}

func normaliseModulation(s string) string {
	switch strings.ToUpper(s) {
	case "QAM/256", "QAM256":
		return "256qam"
	case "QAM/64", "QAM64":
		return "64qam"
	case "QAM/32", "QAM32":
		return "32qam"
	case "QAM/16", "QAM16":
		return "16qam"
	case "QPSK":
		return "qpsk"
	case "8VSB":
		return "8vsb"
	default:
		return ""
	}
}

func normalisePolarization(s string) string {
	switch strings.ToUpper(s) {
	case "HORIZONTAL", "H":
		return "h"
	case "VERTICAL", "V":
		return "v"
	default:
		return ""
	}
}
