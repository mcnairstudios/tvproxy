package tvsatipscan

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// fetchSatIPCaps queries the UPnP desc.xml for SAT>IP server capabilities.
// Returns a map of canonical system name → tuner count, e.g. {"dvbt2": 4, "dvbc": 4}.
func fetchSatIPCaps(httpBase string) (map[string]int, error) {
	resp, err := http.Get(httpBase + "/desc.xml")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	re := regexp.MustCompile(`X_SATIPCAP[^>]*>([^<]+)<`)
	m := re.FindSubmatch(body)
	if m == nil {
		return nil, fmt.Errorf("X_SATIPCAP not found in desc.xml")
	}
	caps := map[string]int{}
	for _, part := range strings.Split(string(m[1]), ",") {
		part = strings.TrimSpace(part)
		dashIdx := strings.LastIndex(part, "-")
		if dashIdx < 0 {
			continue
		}
		sys := strings.ToLower(part[:dashIdx])
		n, _ := strconv.Atoi(part[dashIdx+1:])
		caps[sys] += n
	}
	return caps, nil
}

// workerCount derives the physical tuner count from SAT>IP caps.
// All reported types share the same hardware pool, so return the max across all types.
func workerCount(caps map[string]int) int {
	max := 1
	for _, n := range caps {
		if n > max {
			max = n
		}
	}
	return max
}
