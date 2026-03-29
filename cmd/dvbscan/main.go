package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gavinmcnair/tvproxy/pkg/tvsatipscan"
)

func main() {
	host := flag.String("host", "192.168.1.149:554", "Minisatip RTSP host:port")
	httpPort := flag.Int("http-port", 8875, "Minisatip HTTP port (for capability discovery)")
	timeout := flag.Duration("timeout", 15*time.Second, "Per-transponder scan timeout")
	seedTimeout := flag.Duration("seed-timeout", 5*time.Second, "Timeout for blind seed scans (fast pass)")
	muxTimeout := flag.Duration("mux-timeout", 20*time.Second, "Timeout for discovered muxes and slow retry")
	parallel := flag.Int("parallel", 4, "Max parallel scans per scan group")
	verbose := flag.Bool("v", false, "Verbose RTSP exchange")
	jsonOut := flag.Bool("json", false, "Output results as JSON")
	baseline := flag.String("baseline", "", "Path to save/load baseline JSON for per-mux comparison")
	nitOnly := flag.Bool("nit-only", false, "Run NIT discovery only, print mux list, then exit")
	satellite := flag.String("satellite", "", fmt.Sprintf("Satellite for DVB-S/S2 scanning (auto-detected if omitted). Supported: %s",
		strings.Join(tvsatipscan.Satellites(), ", ")))
	flag.Parse()

	if *satellite != "" {
		supported := tvsatipscan.Satellites()
		found := false
		for _, s := range supported {
			if s == *satellite {
				found = true
				break
			}
		}
		if !found {
			fmt.Fprintf(os.Stderr, "unknown satellite %q — supported: %s\n", *satellite, strings.Join(supported, ", "))
			os.Exit(1)
		}
	}

	cfg := tvsatipscan.Config{
		SeedTimeout: *seedTimeout,
		MuxTimeout:  *muxTimeout,
		Timeout:     *timeout,
		Parallel:    *parallel,
		Verbose:     *verbose,
		Satellite:   *satellite,
	}

	if *nitOnly {
		muxes, networkName, err := tvsatipscan.DiscoverMuxes(*host, *httpPort, cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if networkName != "" {
			fmt.Fprintf(os.Stderr, "Network: %s\n", networkName)
		}
		fmt.Fprintf(os.Stderr, "\nDiscovered %d mux(es):\n", len(muxes))
		for _, m := range muxes {
			fmt.Fprintf(os.Stderr, "  %s\n", m)
		}
		return
	}

	result, err := tvsatipscan.Scan(*host, *httpPort, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if len(result.Channels) == 0 {
		os.Exit(0)
	}

	fmt.Fprintf(os.Stderr, "\nTotal: %d channels across %d networks\n\n",
		len(result.Channels), len(result.Muxes))

	printMuxSummary(os.Stderr, result.Channels)

	if *baseline != "" {
		if _, err := os.Stat(*baseline); os.IsNotExist(err) {
			if err := saveBaseline(*baseline, result.Channels); err != nil {
				fmt.Fprintf(os.Stderr, "WARNING: could not save baseline: %v\n", err)
			} else {
				fmt.Fprintf(os.Stderr, "Baseline saved to %s\n", *baseline)
			}
		} else {
			compareBaseline(os.Stderr, *baseline, result.Channels)
		}
	}

	if *jsonOut {
		printJSON(result.Host, result.Channels)
	} else {
		printTable(result.Channels)
	}
}

func printMuxSummary(w *os.File, channels []tvsatipscan.Channel) {
	type muxStat struct {
		label     string
		total     int
		named     int
		encrypted int
	}
	order := []string{}
	stats := map[string]*muxStat{}
	for _, ch := range channels {
		k := ch.Transponder.MuxKey()
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

type baselineChannel struct {
	Name      string `json:"name"`
	ServiceID uint16 `json:"service_id"`
	MuxKey    string `json:"mux_key"`
	MuxLabel  string `json:"mux_label"`
	Encrypted bool   `json:"encrypted"`
}

type baselineFile struct {
	ScannedAt string            `json:"scanned_at"`
	Total     int               `json:"total"`
	Channels  []baselineChannel `json:"channels"`
}

func saveBaseline(path string, channels []tvsatipscan.Channel) error {
	bf := baselineFile{
		ScannedAt: time.Now().Format(time.RFC3339),
		Total:     len(channels),
	}
	for _, ch := range channels {
		bf.Channels = append(bf.Channels, baselineChannel{
			Name:      ch.Name,
			ServiceID: ch.ServiceID,
			MuxKey:    ch.Transponder.MuxKey(),
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

func compareBaseline(w *os.File, path string, current []tvsatipscan.Channel) {
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

	type chanInfo struct {
		name      string
		encrypted bool
	}
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
		k := ch.Transponder.MuxKey()
		curBySID[ch.ServiceID] = chanInfo{ch.Name, ch.Encrypted}
		curMux[k] = append(curMux[k], ch.ServiceID)
		curMuxLabel[k] = ch.Transponder.String()
	}

	allMuxes := map[string]bool{}
	for k := range baseMux {
		allMuxes[k] = true
	}
	for k := range curMux {
		allMuxes[k] = true
	}

	allOK := true
	fmt.Fprintf(w, "Comparison vs baseline (%s, %d channels):\n", bf.ScannedAt, bf.Total)
	for k := range allMuxes {
		base := baseMux[k]
		cur := curMux[k]
		label := curMuxLabel[k]
		if label == "" {
			fmt.Fprintf(w, "  MISSING MUX %-30s (baseline had %d channels)\n", k, len(base))
			allOK = false
			continue
		}
		baseSet := map[uint16]bool{}
		for _, sid := range base {
			baseSet[sid] = true
		}
		curSet := map[uint16]bool{}
		for _, sid := range cur {
			curSet[sid] = true
		}

		var lost, gained []uint16
		for _, sid := range base {
			if !curSet[sid] {
				lost = append(lost, sid)
			}
		}
		for _, sid := range cur {
			if !baseSet[sid] {
				gained = append(gained, sid)
			}
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

type jsonStream struct {
	PID      uint16 `json:"pid"`
	Type     uint8  `json:"type"`
	TypeName string `json:"type_name"`
	Language string `json:"language,omitempty"`
}

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

func printJSON(host string, channels []tvsatipscan.Channel) {
	out := make([]jsonChannel, 0, len(channels))
	for _, ch := range channels {
		jch := jsonChannel{
			Name:        ch.Name,
			ServiceID:   ch.ServiceID,
			ServiceType: tvsatipscan.ServiceTypeName(ch.ServiceType),
			Encrypted:   ch.Encrypted,
			PMTPID:      ch.PMTPID,
			PCRPID:      ch.PCRPID,
			URL:         ch.RTSPURL(host),
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
	enc.SetEscapeHTML(false)
	enc.Encode(out) //nolint
}

func printTable(channels []tvsatipscan.Channel) {
	fmt.Printf("%-35s %-10s %-6s %-6s %-22s %s\n",
		"Name", "Type", "SID", "PCR", "Transponder", "Streams")
	fmt.Println(strings.Repeat("-", 115))
	for _, ch := range channels {
		fmt.Printf("%-35s %-10s %-6d %-6d %-22s %s\n",
			ch.Name, tvsatipscan.ServiceTypeName(ch.ServiceType), ch.ServiceID, ch.PCRPID,
			ch.Transponder.String(), formatStreams(ch.Streams))
	}
}

func formatStreams(comps []tvsatipscan.StreamComponent) string {
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
