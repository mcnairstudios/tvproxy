package tvsatipscan

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type SignalInfo struct {
	Lock    bool `json:"lock"`
	Level   int  `json:"level"`
	Quality int  `json:"quality"`
	BER     int  `json:"ber"`

	FeID int `json:"fe_id"`

	FreqMHz float64 `json:"freq_mhz"`
	BwMHz   int     `json:"bw_mhz"`
	Msys    string  `json:"msys"`
	Mtype   string  `json:"mtype"`
	PLPID   string  `json:"plp_id"`
	T2ID    string  `json:"t2_id"`

	BitratKbps int  `json:"bitrate_kbps"`
	Active     bool `json:"active"`

	Server string `json:"server"`
}

func (s *SignalInfo) LevelPct() int {
	if s.Level == 0 {
		return 0
	}
	return s.Level * 100 / 255
}

func (s *SignalInfo) QualityPct() int {
	if s.Quality == 0 {
		return 0
	}
	return s.Quality * 100 / 15
}

func QuerySignal(rtspURL string, timeout time.Duration) (*SignalInfo, error) {
	host := extractHost(rtspURL)
	if host == "" {
		return nil, fmt.Errorf("cannot extract host from %q", rtspURL)
	}

	c, err := dialRTSP(host, timeout)
	if err != nil {
		return nil, err
	}
	defer c.close()
	c.conn.SetDeadline(time.Now().Add(timeout))

	resp, err := c.send("DESCRIBE", rtspURL, map[string]string{"Accept": "application/sdp"}, nil)
	if err != nil {
		return nil, err
	}
	if resp.status != 200 {
		return nil, fmt.Errorf("DESCRIBE returned %d", resp.status)
	}

	info := parseTunerSDP(string(resp.body))
	if info == nil {
		return nil, nil
	}
	if sv, ok := resp.headers["server"]; ok {
		info.Server = sv
	}
	return info, nil
}

func extractHost(u string) string {
	u = strings.TrimPrefix(u, "rtsp://")
	u = strings.TrimPrefix(u, "rtsps://")
	host := strings.SplitN(u, "/", 2)[0]
	host = strings.SplitN(host, "?", 2)[0]
	if !strings.Contains(host, ":") {
		host += ":554"
	}
	return host
}

func parseTunerSDP(sdp string) *SignalInfo {
	info := &SignalInfo{}
	found := false

	for _, line := range strings.Split(sdp, "\n") {
		line = strings.TrimSpace(line)

		switch {
		case strings.HasPrefix(line, "b=AS:"):
			if v, err := strconv.Atoi(strings.TrimPrefix(line, "b=AS:")); err == nil {
				info.BitratKbps = v
			}

		case line == "a=sendonly":
			info.Active = true

		case strings.HasPrefix(line, "a=fmtp:"):
			params := strings.SplitN(line, " ", 2)
			if len(params) < 2 {
				continue
			}
			for _, kv := range strings.Split(params[1], ";") {
				if !strings.HasPrefix(kv, "tuner=") {
					continue
				}
				fields := strings.Split(strings.TrimPrefix(kv, "tuner="), ",")
				if len(fields) < 4 {
					continue
				}
				found = true
				info.FeID, _ = strconv.Atoi(fields[0])
				info.Level, _ = strconv.Atoi(fields[1])
				if lockVal, _ := strconv.Atoi(fields[2]); lockVal == 1 {
					info.Lock = true
				}
				info.Quality, _ = strconv.Atoi(fields[3])
				if len(fields) > 4 {
					if f, err := strconv.ParseFloat(fields[4], 64); err == nil {
						info.FreqMHz = f
					}
				}
				if len(fields) > 5 {
					info.BwMHz, _ = strconv.Atoi(fields[5])
				}
				if len(fields) > 6 {
					info.Msys = fields[6]
				}
				for i := 7; i < len(fields); i++ {
					f := strings.TrimSpace(fields[i])
					if f == "" {
						continue
					}
					switch {
					case isModulationType(f):
						info.Mtype = f
					case looksLikePLP(f):
						info.PLPID = f
					}
				}
			}
		}
	}

	if !found {
		return nil
	}
	return info
}

func isModulationType(s string) bool {
	switch s {
	case "qpsk", "8psk", "16apsk", "32apsk",
		"16qam", "32qam", "64qam", "128qam", "256qam",
		"8vsb", "16vsb":
		return true
	}
	return false
}

func looksLikePLP(s string) bool {
	if _, err := strconv.Atoi(s); err == nil {
		return true
	}
	return false
}
