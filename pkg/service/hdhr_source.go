package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/media"
	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/store"
)

type HDHRSourceService struct {
	hdhrSourceStore store.HDHRSourceStore
	streamStore     store.StreamStore
	channelStore    store.ChannelStore
	probeCache      store.ProbeCache
	log             zerolog.Logger
	StatusTracker
}

func NewHDHRSourceService(
	hdhrSourceStore store.HDHRSourceStore,
	streamStore store.StreamStore,
	channelStore store.ChannelStore,
	probeCache store.ProbeCache,
	log zerolog.Logger,
) *HDHRSourceService {
	return &HDHRSourceService{
		hdhrSourceStore: hdhrSourceStore,
		streamStore:     streamStore,
		channelStore:    channelStore,
		probeCache:      probeCache,
		log:             log.With().Str("service", "hdhr_source").Logger(),
		StatusTracker:   NewStatusTracker(),
	}
}

func (s *HDHRSourceService) Log() *zerolog.Logger { return &s.log }

func (s *HDHRSourceService) CreateSource(ctx context.Context, source *models.HDHRSource) error {
	if err := s.hdhrSourceStore.Create(ctx, source); err != nil {
		return err
	}
	if len(source.Devices) == 0 {
		s.autoLinkDevices(ctx, source)
	}
	return nil
}

func (s *HDHRSourceService) autoLinkDevices(ctx context.Context, source *models.HDHRSource) {
	ips, err := udpDiscoverHDHR(s.log)
	if err != nil {
		return
	}
	existingDeviceIDs := make(map[string]bool)
	sources, _ := s.hdhrSourceStore.List(ctx)
	for _, src := range sources {
		for _, d := range src.Devices {
			existingDeviceIDs[d.DeviceID] = true
		}
	}
	for _, ip := range ips {
		baseURL := "http://" + ip
		discover, err := s.fetchDiscover(ctx, baseURL)
		if err != nil {
			continue
		}
		if existingDeviceIDs[discover.DeviceID] {
			continue
		}
		source.Devices = append(source.Devices, models.HDHRTuner{
			Host:            ip,
			DeviceID:        discover.DeviceID,
			DeviceModel:     discover.ModelNumber,
			FirmwareVersion: discover.FirmwareVersion,
			TunerCount:      discover.TunerCount,
		})
		existingDeviceIDs[discover.DeviceID] = true
	}
	if len(source.Devices) > 0 {
		source.TunerCount = 0
		for _, d := range source.Devices {
			source.TunerCount += d.TunerCount
		}
		s.hdhrSourceStore.Update(ctx, source)
		s.log.Info().Int("devices", len(source.Devices)).Str("source", source.Name).Msg("auto-linked discovered HDHR devices")
	}
}

func (s *HDHRSourceService) GetSource(ctx context.Context, id string) (*models.HDHRSource, error) {
	return s.hdhrSourceStore.GetByID(ctx, id)
}

func (s *HDHRSourceService) ListSources(ctx context.Context) ([]models.HDHRSource, error) {
	return s.hdhrSourceStore.List(ctx)
}

func (s *HDHRSourceService) UpdateSource(ctx context.Context, source *models.HDHRSource) error {
	return s.hdhrSourceStore.Update(ctx, source)
}

func (s *HDHRSourceService) DeleteSource(ctx context.Context, id string) error {
	if err := s.streamStore.DeleteByHDHRSourceID(ctx, id); err != nil {
		s.log.Error().Err(err).Str("source_id", id).Msg("failed to delete streams for hdhr source")
	}
	if err := s.streamStore.Save(); err != nil {
		s.log.Error().Err(err).Msg("failed to save stream store after hdhr source delete")
	}
	return s.hdhrSourceStore.Delete(ctx, id)
}

func (s *HDHRSourceService) ClearSource(ctx context.Context, id string) error {
	if err := s.streamStore.DeleteByHDHRSourceID(ctx, id); err != nil {
		return fmt.Errorf("deleting streams: %w", err)
	}
	if err := s.streamStore.Save(); err != nil {
		s.log.Error().Err(err).Msg("failed to save after clear")
	}
	s.hdhrSourceStore.UpdateStreamCount(ctx, id, 0)
	return nil
}

type hdhrDiscoverResp struct {
	FriendlyName    string `json:"FriendlyName"`
	ModelNumber     string `json:"ModelNumber"`
	FirmwareVersion string `json:"FirmwareVersion"`
	FirmwareName    string `json:"FirmwareName"`
	DeviceID        string `json:"DeviceID"`
	TunerCount      int    `json:"TunerCount"`
	BaseURL         string `json:"BaseURL"`
	LineupURL       string `json:"LineupURL"`
}

type hdhrLineupEntry struct {
	GuideNumber string `json:"GuideNumber"`
	GuideName   string `json:"GuideName"`
	VideoCodec  string `json:"VideoCodec"`
	AudioCodec  string `json:"AudioCodec"`
	URL         string `json:"URL"`
	HD          int    `json:"HD"`
	Favorite    int    `json:"Favorite"`
	DRM         int    `json:"DRM"`
}

func (s *HDHRSourceService) ScanSource(ctx context.Context, sourceID string) error {
	source, err := s.hdhrSourceStore.GetByID(ctx, sourceID)
	if err != nil {
		return fmt.Errorf("source not found: %w", err)
	}

	s.Set(sourceID, RefreshStatus{State: "running", Message: "Connecting to device..."})
	s.hdhrSourceStore.UpdateLastError(ctx, sourceID, "")

	if len(source.Devices) == 0 {
		s.Set(sourceID, RefreshStatus{State: "error", Message: "no devices configured"})
		return fmt.Errorf("no devices configured")
	}

	baseURL := normalizeHDHRHost(source.Devices[0].Host)
	s.log.Info().Str("source", source.Name).Str("url", baseURL).Int("devices", len(source.Devices)).Msg("scanning hdhr device")

	discover, err := s.fetchDiscover(ctx, baseURL)
	if err != nil {
		s.Set(sourceID, RefreshStatus{State: "error", Message: err.Error()})
		s.hdhrSourceStore.UpdateLastError(ctx, sourceID, err.Error())
		return fmt.Errorf("discover failed: %w", err)
	}

	s.Set(sourceID, RefreshStatus{State: "running", Message: "Fetching channel lineup..."})

	lineupURL := baseURL + "/lineup.json"
	if discover.LineupURL != "" {
		lineupURL = discover.LineupURL
	}

	lineup, err := s.fetchLineup(ctx, lineupURL)
	if err != nil {
		s.Set(sourceID, RefreshStatus{State: "error", Message: err.Error()})
		s.hdhrSourceStore.UpdateLastError(ctx, sourceID, err.Error())
		return fmt.Errorf("lineup failed: %w", err)
	}

	s.log.Info().Int("channels", len(lineup)).Msg("fetched hdhr lineup")
	s.Set(sourceID, RefreshStatus{State: "running", Message: fmt.Sprintf("Processing %d channels...", len(lineup)), Total: len(lineup)})

	var streams []models.Stream
	var keepIDs []string
	for i, entry := range lineup {
		if entry.URL == "" || entry.DRM == 1 {
			continue
		}

		contentHash := sourceID + ":" + entry.GuideNumber
		id := deterministicStreamID(contentHash)
		keepIDs = append(keepIDs, id)

		var group string
		if entry.VideoCodec == "" || strings.EqualFold(entry.VideoCodec, "none") {
			group = "Radio"
		} else if entry.HD == 1 {
			group = "HD"
		} else {
			group = "SD"
		}

		streams = append(streams, models.Stream{
			ID:           id,
			HDHRSourceID: sourceID,
			Name:         entry.GuideName,
			URL:          entry.URL,
			Group:        group,
			TvgID:        entry.GuideNumber,
			IsActive:     true,
		})

		if s.probeCache != nil {
			s.probeCache.SaveProbe(id, hdhrProbeFromLineup(entry))
		}

		if (i+1)%50 == 0 {
			s.Set(sourceID, RefreshStatus{State: "running", Message: fmt.Sprintf("Processing %d/%d...", i+1, len(lineup)), Progress: i + 1, Total: len(lineup)})
		}
	}

	if s.probeCache != nil && len(streams) > 0 {
		var captureURL string
		for _, entry := range lineup {
			if entry.HD == 1 && entry.URL != "" && entry.DRM != 1 {
				captureURL = entry.URL
				break
			}
		}
		if captureURL == "" {
			for _, entry := range lineup {
				if entry.URL != "" && entry.DRM != 1 {
					captureURL = entry.URL
					break
				}
			}
		}
		if captureURL != "" {
			s.Set(sourceID, RefreshStatus{State: "running", Message: "Capturing stream header for fast startup..."})
			header, err := media.CaptureTPSHeader(ctx, captureURL, 10*time.Second)
			if err != nil {
				s.log.Warn().Err(err).Msg("failed to capture TS header")
			} else {
				for _, st := range streams {
					s.probeCache.SaveTSHeader(st.ID, header)
				}
				s.log.Info().Int("size", len(header)).Int("channels", len(streams)).Msg("captured TS header for fast startup")
			}
		}
	}

	if err := s.streamStore.BulkUpsert(ctx, streams); err != nil {
		s.Set(sourceID, RefreshStatus{State: "error", Message: err.Error()})
		s.hdhrSourceStore.UpdateLastError(ctx, sourceID, err.Error())
		return fmt.Errorf("upserting streams: %w", err)
	}

	deletedIDs, err := s.streamStore.DeleteStaleByHDHRSourceID(ctx, sourceID, keepIDs)
	if err != nil {
		s.log.Error().Err(err).Msg("failed to delete stale hdhr streams")
	}

	if len(deletedIDs) > 0 && s.channelStore != nil {
		if err := s.channelStore.RemoveStreamMappings(ctx, deletedIDs); err != nil {
			s.log.Error().Err(err).Int("count", len(deletedIDs)).Msg("failed to clean up channel stream mappings")
		}
	}

	if err := s.streamStore.Save(); err != nil {
		s.log.Error().Err(err).Msg("failed to save stream store")
	}

	now := time.Now()
	s.hdhrSourceStore.UpdateLastScanned(ctx, sourceID, now)
	s.hdhrSourceStore.UpdateStreamCount(ctx, sourceID, len(streams))

	s.Set(sourceID, RefreshStatus{State: "done", Message: fmt.Sprintf("Found %d channels", len(streams)), Progress: len(streams), Total: len(streams)})
	s.log.Info().Int("channels", len(streams)).Int("deleted", len(deletedIDs)).Msg("hdhr scan complete")
	return nil
}

func (s *HDHRSourceService) fetchDiscover(ctx context.Context, baseURL string) (*hdhrDiscoverResp, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/discover.json", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connecting to device: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("device returned %d", resp.StatusCode)
	}
	var d hdhrDiscoverResp
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return nil, fmt.Errorf("parsing discover response: %w", err)
	}
	return &d, nil
}

func (s *HDHRSourceService) fetchLineup(ctx context.Context, lineupURL string) ([]hdhrLineupEntry, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", lineupURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching lineup: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("lineup returned %d", resp.StatusCode)
	}
	var entries []hdhrLineupEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("parsing lineup: %w", err)
	}
	return entries, nil
}

type DiscoveredHDHR struct {
	Host            string `json:"host"`
	DeviceID        string `json:"device_id"`
	FriendlyName    string `json:"friendly_name"`
	ModelNumber     string `json:"model_number"`
	FirmwareVersion string `json:"firmware_version"`
	TunerCount      int    `json:"tuner_count"`
	AlreadyAdded    bool   `json:"already_added"`
}

func (s *HDHRSourceService) DiscoverDevices(ctx context.Context) ([]DiscoveredHDHR, error) {
	ips, err := udpDiscoverHDHR(s.log)
	if err != nil {
		return nil, err
	}

	existing, _ := s.hdhrSourceStore.List(ctx)
	existingDeviceIDs := make(map[string]bool)
	for _, src := range existing {
		for _, d := range src.Devices {
			existingDeviceIDs[d.DeviceID] = true
		}
	}

	var results []DiscoveredHDHR
	for _, ip := range ips {
		baseURL := "http://" + ip
		discover, err := s.fetchDiscover(ctx, baseURL)
		if err != nil {
			s.log.Debug().Str("ip", ip).Err(err).Msg("skipping unresponsive hdhr device")
			continue
		}
		results = append(results, DiscoveredHDHR{
			Host:            ip,
			DeviceID:        discover.DeviceID,
			FriendlyName:    discover.FriendlyName,
			ModelNumber:     discover.ModelNumber,
			FirmwareVersion: discover.FirmwareVersion,
			TunerCount:      discover.TunerCount,
			AlreadyAdded:    existingDeviceIDs[discover.DeviceID],
		})
	}
	return results, nil
}

func (s *HDHRSourceService) AddDevice(ctx context.Context, host string) error {
	baseURL := normalizeHDHRHost(host)
	discover, err := s.fetchDiscover(ctx, baseURL)
	if err != nil {
		return fmt.Errorf("connecting to device: %w", err)
	}

	device := models.HDHRTuner{
		Host:            host,
		DeviceID:        discover.DeviceID,
		DeviceModel:     discover.ModelNumber,
		FirmwareVersion: discover.FirmwareVersion,
		TunerCount:      discover.TunerCount,
	}

	sources, _ := s.hdhrSourceStore.List(ctx)
	if len(sources) == 0 {
		src := &models.HDHRSource{
			Name:      "HDHomeRun",
			Devices:   []models.HDHRTuner{device},
			IsEnabled: true,
			TunerCount: device.TunerCount,
		}
		return s.hdhrSourceStore.Create(ctx, src)
	}

	src := &sources[0]
	for _, d := range src.Devices {
		if d.DeviceID == discover.DeviceID {
			return nil
		}
	}
	src.Devices = append(src.Devices, device)
	src.TunerCount = 0
	for _, d := range src.Devices {
		src.TunerCount += d.TunerCount
	}
	return s.hdhrSourceStore.Update(ctx, src)
}

var hdhrDiscoverPacket = []byte{
	0x00, 0x02, 0x00, 0x0c,
	0x01, 0x04, 0xff, 0xff, 0xff, 0xff,
	0x02, 0x04, 0xff, 0xff, 0xff, 0xff,
	0x73, 0xcc, 0x7d, 0x8f,
}

func udpDiscoverHDHR(log zerolog.Logger) ([]string, error) {
	conn, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		return nil, fmt.Errorf("listen udp: %w", err)
	}
	defer conn.Close()

	dst := &net.UDPAddr{IP: net.IPv4(255, 255, 255, 255), Port: 65001}
	if _, err := conn.WriteTo(hdhrDiscoverPacket, dst); err != nil {
		return nil, fmt.Errorf("broadcast: %w", err)
	}

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))

	seen := make(map[string]bool)
	buf := make([]byte, 1024)
	for {
		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			break
		}
		if n < 2 || buf[1] != 0x03 {
			continue
		}
		ip := addr.(*net.UDPAddr).IP.String()
		if !seen[ip] {
			seen[ip] = true
			log.Info().Str("ip", ip).Msg("discovered hdhr device")
		}
	}

	var ips []string
	for ip := range seen {
		ips = append(ips, ip)
	}
	return ips, nil
}

func hdhrProbeFromLineup(entry hdhrLineupEntry) *media.ProbeResult {
	probe := &media.ProbeResult{
		FormatName: "mpegts",
	}

	vcodec := strings.ToLower(entry.VideoCodec)
	if vcodec == "" || vcodec == "none" {
		probe.HasVideo = false
	} else {
		probe.HasVideo = true
		switch vcodec {
		case "mpeg2":
			probe.Video = &media.VideoInfo{Codec: "mpeg2video"}
		case "h264":
			probe.Video = &media.VideoInfo{Codec: "h264"}
		case "hevc", "h265":
			probe.Video = &media.VideoInfo{Codec: "hevc"}
		default:
			probe.Video = &media.VideoInfo{Codec: "h264"}
		}
	}

	acodec := strings.ToLower(entry.AudioCodec)
	switch acodec {
	case "aac":
		probe.AudioTracks = []media.AudioTrack{{Codec: "aac_latm", Language: "eng"}}
	case "mpeg", "mp2":
		probe.AudioTracks = []media.AudioTrack{{Codec: "mp2", Language: "eng"}}
	case "ac3":
		probe.AudioTracks = []media.AudioTrack{{Codec: "ac3", Language: "eng"}}
	default:
		probe.AudioTracks = []media.AudioTrack{{Codec: "aac_latm", Language: "eng"}}
	}

	return probe
}

func (s *HDHRSourceService) RetuneDevice(ctx context.Context, sourceID string, deviceIdx int) error {
	source, err := s.hdhrSourceStore.GetByID(ctx, sourceID)
	if err != nil {
		s.Set(sourceID, RefreshStatus{State: "error", Message: "source not found"})
		return fmt.Errorf("source not found: %w", err)
	}
	if deviceIdx >= len(source.Devices) {
		s.Set(sourceID, RefreshStatus{State: "error", Message: "no devices configured"})
		return fmt.Errorf("device index %d out of range", deviceIdx)
	}

	device := source.Devices[deviceIdx]
	baseURL := normalizeHDHRHost(device.Host)
	s.Set(sourceID, RefreshStatus{State: "running", Message: fmt.Sprintf("Starting channel scan on %s...", device.Host)})
	s.hdhrSourceStore.UpdateLastError(ctx, sourceID, "")

	scanURL := baseURL + "/lineup.post?scan=start&source=Antenna"
	req, err := http.NewRequestWithContext(ctx, "POST", scanURL, nil)
	if err != nil {
		s.Set(sourceID, RefreshStatus{State: "error", Message: "Failed to create request: " + err.Error()})
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.Set(sourceID, RefreshStatus{State: "error", Message: "Failed to start scan: " + err.Error()})
		s.hdhrSourceStore.UpdateLastError(ctx, sourceID, err.Error())
		return err
	}
	resp.Body.Close()

	for i := 0; i < 600; i++ {
		time.Sleep(2 * time.Second)
		statusURL := baseURL + "/lineup_status.json"
		statusResp, err := http.Get(statusURL)
		if err != nil {
			continue
		}
		var status struct {
			ScanInProgress int    `json:"ScanInProgress"`
			Progress       int    `json:"Progress"`
			Found          int    `json:"Found"`
			ScanPossible   int    `json:"ScanPossible"`
		}
		json.NewDecoder(statusResp.Body).Decode(&status)
		statusResp.Body.Close()

		if status.ScanInProgress == 1 {
			s.Set(sourceID, RefreshStatus{
				State:    "running",
				Message:  fmt.Sprintf("Scanning... %d%% (%d channels found)", status.Progress, status.Found),
				Progress: status.Progress,
				Total:    100,
			})
			continue
		}

		s.log.Info().Int("found", status.Found).Msg("hdhr channel scan complete")
		break
	}

	return s.ScanSource(ctx, sourceID)
}

func normalizeHDHRHost(host string) string {
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		return strings.TrimRight(host, "/")
	}
	return "http://" + strings.TrimRight(host, "/")
}
