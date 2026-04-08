package tvsatipscan

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

func detectSatellite(host string, timeout time.Duration, log zerolog.Logger) (id, networkName string, seeds []Transponder) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type result struct {
		id          string
		networkName string
	}
	ch := make(chan result, len(satelliteDetectionSeeds))

	for satID, seed := range satelliteDetectionSeeds {
		go func(satID string, seed Transponder) {
			r := scanTransponder(ctx, host, seed, timeout, "0,16,17", log)
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

func discoverMuxes(host string, caps map[string]int, seedTimeout, muxTimeout time.Duration, log zerolog.Logger) ([]Transponder, string) {
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
				resultsCh <- scanTransponder(context.Background(), host, item.tp, item.timeout, pids, log)
			}
		}()
	}

	var detectedNetwork string

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
				log.Debug().Str("mux", r.tp.String()).Dur("elapsed", elapsed).Msg("no signal")
				if retryOnFail && initialKeys[muxKey(r.tp)] {
					failedSeeds = append(failedSeeds, r.tp)
				}
			} else {
				if r.signalOnly {
					log.Debug().Str("mux", r.tp.String()).Dur("elapsed", elapsed).Msg("signal")
				} else {
					log.Debug().Str("mux", r.tp.String()).Int("nit_muxes", len(r.nitMuxes)).Dur("elapsed", elapsed).Msg("signal")
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

	log.Info().Dur("seed_timeout", seedTimeout).Int("workers", workers).Msg("pass 1")
	var pass1 []workItem
	for _, seed := range allSeeds {
		pass1 = append(pass1, workItem{seed, seedTimeout, false})
	}
	found1, failed1, nitMentioned := runPool(pass1, scanned, true)

	allFound := append([]Transponder(nil), found1...)

	const t2Margin = 16.0
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
			keep = true
		}
		if keep {
			pass2 = append(pass2, workItem{seed, muxTimeout, false})
		}
	}
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
		log.Info().Dur("mux_timeout", muxTimeout).Int("workers", workers).Int("candidates", len(pass2)).Msg("pass 2")
		found2, _, _ := runPool(pass2, scanned, false)
		allFound = append(allFound, found2...)
	}

	close(work)
	wg.Wait()

	return allFound, detectedNetwork
}
