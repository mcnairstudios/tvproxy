package tvsatipscan

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// detectSatellite probes one seed per known satellite in parallel.
// The first seed that returns a NIT response wins; all others are cancelled.
// Returns the satellite identifier (e.g. "S28.2E"), its NIT network name, and
// its full seed list for use in BFS discovery.
func detectSatellite(host string, timeout time.Duration, verbose bool) (id, networkName string, seeds []Transponder) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type result struct {
		id          string
		networkName string
	}
	ch := make(chan result, len(satelliteDetectionSeeds))

	for satID, seed := range satelliteDetectionSeeds {
		go func(satID string, seed Transponder) {
			r := scanTransponder(ctx, host, seed, timeout, "0,16,17", verbose)
			if r.networkID != 0 {
				ch <- result{satID, r.networkName}
			} else {
				ch <- result{}
			}
		}(satID, seed)
	}

	for range satelliteDetectionSeeds {
		r := <-ch
		if r.id != "" {
			cancel()
			return r.id, r.networkName, europeanSatellites[r.id]
		}
	}
	return "", "", nil
}

// discoverMuxes finds all live muxes via NIT BFS using a parallel worker pool.
//
// Pass 1: all seeds scanned in parallel at seedTimeout (fast). Successes
// trigger BFS: newly discovered muxes are queued at muxTimeout and processed
// within the same pass. Failed seeds are collected for retry.
//
// Pass 2: failed seeds retried in parallel at muxTimeout. Only seeds that are
// worth retrying are included: (a) muxes mentioned in a NIT during pass 1,
// (b) dvbt2 seeds near the frequency band of found dvbt muxes (±t2Margin MHz),
// (c) dvbs2/dvbc2 seeds (small count, always worth retrying).
//
// At most workerCount(caps) scans run concurrently — one per physical tuner.
func discoverMuxes(host string, caps map[string]int, seedTimeout, muxTimeout time.Duration, verbose bool) ([]Transponder, string) {
	// When both dvbt and dvbt2 are available, defer dvbt2 seeds from Pass 1.
	// Pass 1 finds dvbt muxes first; Pass 2 retries dvbt2 only in that frequency band.
	// This avoids scanning all 48 dvbt2 UHF seeds upfront when they will all return "no signal".
	deferT2 := caps["dvbt2"] > 0 && caps["dvbt"] > 0
	var deferredT2Seeds []Transponder

	seen := map[string]bool{}
	var allSeeds []Transponder
	for _, sys := range typeOrder {
		if caps[sys] > 0 {
			if seeds, ok := defaultSeeds[sys]; ok {
				if sys == "dvbt2" && deferT2 {
					deferredT2Seeds = seeds
					seen[sys] = true
					continue
				}
				allSeeds = append(allSeeds, seeds...)
				seen[sys] = true
			}
		}
	}
	for sys := range caps {
		if !seen[sys] {
			if seeds, ok := defaultSeeds[sys]; ok {
				allSeeds = append(allSeeds, seeds...)
			}
		}
	}

	workers := workerCount(caps)
	work := make(chan workItem, workers)
	resultsCh := make(chan scanResult, workers)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range work {
				pids := "0,16,17"
				if item.signalOnly {
					pids = "0"
				}
				resultsCh <- scanTransponder(context.Background(), host, item.tp, item.timeout, pids, verbose)
			}
		}()
	}

	var detectedNetwork string

	// runPool drains initial items (and any BFS muxes they discover) through the
	// worker pool. prevScanned carries mux keys already handled in a prior pass so
	// they are not re-scanned. retryOnFail causes failed initial seeds to be
	// returned in failedSeeds for the caller to decide whether to retry.
	//
	// Returns found muxes, failed initial seeds, and the set of all mux keys
	// mentioned in any NIT response (used to decide which seeds are worth retrying).
	runPool := func(initial []workItem, prevScanned map[string]bool, retryOnFail bool) (found []Transponder, failedSeeds []Transponder, nitMentioned map[string]bool) {
		nitMentioned = map[string]bool{}
		discoveryComplete := false
		initialKeys := map[string]bool{}
		for _, item := range initial {
			k := muxKey(item.tp)
			prevScanned[k] = true
			initialKeys[k] = true
		}
		pending := make([]workItem, len(initial))
		copy(pending, initial)
		inFlight := 0

		enqueue := func(tp Transponder, timeout time.Duration) {
			k := muxKey(tp)
			nitMentioned[k] = true
			if !prevScanned[k] {
				prevScanned[k] = true
				var t time.Duration
				var signalOnly bool
				if strings.HasSuffix(tp.System, "2") {
					t = timeout
				} else {
					t = seedTimeout
					signalOnly = discoveryComplete
				}
				pending = append([]workItem{{tp, t, signalOnly}}, pending...)
			}
		}

		fill := func() {
			for inFlight < workers && len(pending) > 0 {
				item := pending[0]
				pending = pending[1:]
				work <- item
				inFlight++
			}
		}

		fill()
		for inFlight > 0 {
			r := <-resultsCh
			inFlight--

			var noSignal bool
			if r.signalOnly {
				noSignal = r.err != nil || !r.patReceived
			} else {
				noSignal = r.err != nil || (len(r.nitMuxes) == 0 && r.networkID == 0)
			}
			elapsed := r.elapsed.Round(time.Millisecond)

			if noSignal {
				fmt.Fprintf(os.Stderr, "  %s → no signal (%s)\n", r.tp, elapsed)
				if retryOnFail && initialKeys[muxKey(r.tp)] {
					failedSeeds = append(failedSeeds, r.tp)
				}
			} else {
				if r.signalOnly {
					fmt.Fprintf(os.Stderr, "  %s → signal (%s)\n", r.tp, elapsed)
				} else {
					fmt.Fprintf(os.Stderr, "  %s → signal, NIT has %d muxes (%s)\n", r.tp, len(r.nitMuxes), elapsed)
				}
				found = append(found, r.tp)
				if detectedNetwork == "" && r.networkName != "" {
					detectedNetwork = r.networkName
				}
				if r.nitComplete {
					discoveryComplete = true
				}
				for _, m := range r.nitMuxes {
					enqueue(m, muxTimeout)
				}
			}

			fill()
		}
		return found, failedSeeds, nitMentioned
	}

	scanned := map[string]bool{}

	fmt.Fprintf(os.Stderr, "  Pass 1 (fast %s, %d workers)...\n", seedTimeout, workers)
	var pass1 []workItem
	for _, seed := range allSeeds {
		pass1 = append(pass1, workItem{seed, seedTimeout, false})
	}
	found1, failed1, nitMentioned := runPool(pass1, scanned, true)

	allFound := append([]Transponder(nil), found1...)

	// Pass 2: retry seeds worth a slow scan:
	//   (a) appeared in a NIT during pass 1 — confirmed on the network
	//   (b) dvbt2: hardware locks slowly; only retry seeds within the frequency
	//       band of dvbt muxes found in pass 1 ± t2Margin MHz. T2 is always
	//       adjacent to the T cluster — no need to sweep the whole UHF band.
	//   (c) dvbs2/dvbc2: small seed count, retry all.
	const t2Margin = 16.0 // MHz (~2 UHF channels)
	var minT, maxT float64
	for _, f := range found1 {
		if f.System == "dvbt" {
			if minT == 0 || f.FreqMHz < minT {
				minT = f.FreqMHz
			}
			if f.FreqMHz > maxT {
				maxT = f.FreqMHz
			}
		}
	}
	var pass2 []workItem
	for _, seed := range failed1 {
		var keep bool
		switch {
		case nitMentioned[muxKey(seed)]:
			keep = true
		case seed.System == "dvbt2":
			keep = minT > 0 && seed.FreqMHz >= minT-t2Margin && seed.FreqMHz <= maxT+t2Margin
		case strings.HasSuffix(seed.System, "2"):
			keep = true // dvbs2, dvbc2: small seed count, retry all
		}
		if keep {
			pass2 = append(pass2, workItem{seed, muxTimeout, false})
		}
	}
	// Add deferred dvbt2 seeds (not in pass 1) for frequencies near found dvbt muxes.
	for _, seed := range deferredT2Seeds {
		k := muxKey(seed)
		if scanned[k] {
			continue
		}
		var keep bool
		if nitMentioned[k] {
			keep = true
		} else if minT > 0 && seed.FreqMHz >= minT-t2Margin && seed.FreqMHz <= maxT+t2Margin {
			keep = true
		}
		if keep {
			pass2 = append(pass2, workItem{seed, muxTimeout, false})
		}
	}
	if len(pass2) > 0 {
		fmt.Fprintf(os.Stderr, "  Pass 2 (slow retry %s, %d workers, %d candidates)...\n", muxTimeout, workers, len(pass2))
		found2, _, _ := runPool(pass2, scanned, false)
		allFound = append(allFound, found2...)
	}

	close(work)
	wg.Wait()

	return allFound, detectedNetwork
}
