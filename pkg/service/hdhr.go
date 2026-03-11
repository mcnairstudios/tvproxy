package service

import (
	"context"
	"encoding/xml"
	"fmt"
	"strconv"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/config"
	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
)

// DiscoverData represents the HDHomeRun discover.json response.
type DiscoverData struct {
	FriendlyName    string `json:"FriendlyName"`
	Manufacturer    string `json:"Manufacturer"`
	ManufacturerURL string `json:"ManufacturerURL"`
	ModelNumber     string `json:"ModelNumber"`
	FirmwareName    string `json:"FirmwareName"`
	FirmwareVersion string `json:"FirmwareVersion"`
	DeviceID        string `json:"DeviceID"`
	DeviceAuth      string `json:"DeviceAuth"`
	BaseURL         string `json:"BaseURL"`
	LineupURL       string `json:"LineupURL"`
	TunerCount      int    `json:"TunerCount"`
}

// LineupEntry represents a single entry in the HDHomeRun lineup.json response.
type LineupEntry struct {
	GuideNumber string `json:"GuideNumber"`
	GuideName   string `json:"GuideName"`
	URL         string `json:"URL"`
}

// DeviceXML represents the HDHomeRun device.xml response.
type DeviceXML struct {
	XMLName     xml.Name       `xml:"root"`
	XMLNS       string         `xml:"xmlns,attr"`
	URLBase     string         `xml:"URLBase"`
	SpecVersion specVersion    `xml:"specVersion"`
	Device      deviceXMLInner `xml:"device"`
}

type specVersion struct {
	Major int `xml:"major"`
	Minor int `xml:"minor"`
}

type deviceXMLInner struct {
	DeviceType   string `xml:"deviceType"`
	FriendlyName string `xml:"friendlyName"`
	Manufacturer string `xml:"manufacturer"`
	ModelName    string `xml:"modelName"`
	ModelNumber  string `xml:"modelNumber"`
	SerialNumber string `xml:"serialNumber"`
	UDN          string `xml:"UDN"`
}

// HDHRService handles HDHomeRun device emulation.
type HDHRService struct {
	hdhrDeviceRepo     *repository.HDHRDeviceRepository
	channelRepo        *repository.ChannelRepository
	streamRepo         *repository.StreamRepository
	channelProfileRepo *repository.ChannelProfileRepository
	streamProfileRepo  *repository.StreamProfileRepository
	config             *config.Config
	log                zerolog.Logger
}

// NewHDHRService creates a new HDHRService.
func NewHDHRService(
	hdhrDeviceRepo *repository.HDHRDeviceRepository,
	channelRepo *repository.ChannelRepository,
	streamRepo *repository.StreamRepository,
	channelProfileRepo *repository.ChannelProfileRepository,
	streamProfileRepo *repository.StreamProfileRepository,
	cfg *config.Config,
	log zerolog.Logger,
) *HDHRService {
	return &HDHRService{
		hdhrDeviceRepo:     hdhrDeviceRepo,
		channelRepo:        channelRepo,
		streamRepo:         streamRepo,
		channelProfileRepo: channelProfileRepo,
		streamProfileRepo:  streamProfileRepo,
		config:             cfg,
		log:                log.With().Str("service", "hdhr").Logger(),
	}
}

// CreateDevice creates a new HDHR device.
func (s *HDHRService) CreateDevice(ctx context.Context, device *models.HDHRDevice) error {
	if err := s.hdhrDeviceRepo.Create(ctx, device); err != nil {
		return fmt.Errorf("creating hdhr device: %w", err)
	}
	return nil
}

// GetDevice returns an HDHR device by ID.
func (s *HDHRService) GetDevice(ctx context.Context, id int64) (*models.HDHRDevice, error) {
	device, err := s.hdhrDeviceRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("getting hdhr device: %w", err)
	}
	return device, nil
}

// GetDeviceByDeviceID returns an HDHR device by its device ID string.
func (s *HDHRService) GetDeviceByDeviceID(ctx context.Context, deviceID string) (*models.HDHRDevice, error) {
	device, err := s.hdhrDeviceRepo.GetByDeviceID(ctx, deviceID)
	if err != nil {
		return nil, fmt.Errorf("getting hdhr device by device id: %w", err)
	}
	return device, nil
}

// ListDevices returns all HDHR devices.
func (s *HDHRService) ListDevices(ctx context.Context) ([]models.HDHRDevice, error) {
	devices, err := s.hdhrDeviceRepo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing hdhr devices: %w", err)
	}
	return devices, nil
}

// UpdateDevice updates an existing HDHR device.
func (s *HDHRService) UpdateDevice(ctx context.Context, device *models.HDHRDevice) error {
	if err := s.hdhrDeviceRepo.Update(ctx, device); err != nil {
		return fmt.Errorf("updating hdhr device: %w", err)
	}
	return nil
}

// DeleteDevice deletes an HDHR device by ID.
func (s *HDHRService) DeleteDevice(ctx context.Context, id int64) error {
	if err := s.hdhrDeviceRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("deleting hdhr device: %w", err)
	}
	return nil
}

// GetDiscoverData returns the discover.json response for HDHomeRun device emulation.
func (s *HDHRService) GetDiscoverData(ctx context.Context, baseURL string) (*DiscoverData, error) {
	devices, err := s.hdhrDeviceRepo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing devices: %w", err)
	}

	if len(devices) == 0 {
		return nil, fmt.Errorf("no hdhr devices configured")
	}

	// Use the first enabled device
	var device *models.HDHRDevice
	for i := range devices {
		if devices[i].IsEnabled {
			device = &devices[i]
			break
		}
	}
	if device == nil {
		return nil, fmt.Errorf("no enabled hdhr devices")
	}

	return &DiscoverData{
		FriendlyName:    device.Name,
		Manufacturer:    "Silicondust",
		ManufacturerURL: "https://www.silicondust.com/",
		ModelNumber:     "HDTC-2US",
		FirmwareName:    "hdhomerun_atsc",
		FirmwareVersion: device.FirmwareVersion,
		DeviceID:        device.DeviceID,
		DeviceAuth:      device.DeviceAuth,
		BaseURL:         baseURL,
		LineupURL:       baseURL + "/lineup.json",
		TunerCount:      device.TunerCount,
	}, nil
}

// resolveSourceURL returns the URL of the first active stream for the channel.
func (s *HDHRService) resolveSourceURL(ctx context.Context, channelID int64) string {
	streams, err := s.channelRepo.GetStreams(ctx, channelID)
	if err != nil || len(streams) == 0 {
		return ""
	}
	for _, cs := range streams {
		stream, err := s.streamRepo.GetByID(ctx, cs.StreamID)
		if err != nil || !stream.IsActive {
			continue
		}
		return stream.URL
	}
	return ""
}

// GetLineup returns the lineup.json response for the given HDHR device.
func (s *HDHRService) GetLineup(ctx context.Context, baseURL string) ([]LineupEntry, error) {
	channels, err := s.channelRepo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing channels: %w", err)
	}

	lineup := make([]LineupEntry, 0, len(channels))
	for _, ch := range channels {
		if !ch.IsEnabled {
			continue
		}

		streamURL := fmt.Sprintf("%s/channel/%d", baseURL, ch.ID)

		// For direct channels, point Plex straight at the source
		mode, _ := ResolveStreamMode(ctx, &ch, s.channelProfileRepo, s.streamProfileRepo, s.log)
		if mode == "direct" {
			if src := s.resolveSourceURL(ctx, ch.ID); src != "" {
				streamURL = src
			}
		}

		lineup = append(lineup, LineupEntry{
			GuideNumber: strconv.Itoa(ch.ChannelNumber),
			GuideName:   ch.Name,
			URL:         streamURL,
		})
	}

	return lineup, nil
}

// GetDeviceXML returns the device.xml response for UPnP/SSDP discovery.
func (s *HDHRService) GetDeviceXML(ctx context.Context, baseURL string) (*DeviceXML, error) {
	devices, err := s.hdhrDeviceRepo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing devices: %w", err)
	}

	if len(devices) == 0 {
		return nil, fmt.Errorf("no hdhr devices configured")
	}

	var device *models.HDHRDevice
	for i := range devices {
		if devices[i].IsEnabled {
			device = &devices[i]
			break
		}
	}
	if device == nil {
		return nil, fmt.Errorf("no enabled hdhr devices")
	}

	return s.GetDeviceXMLForDevice(ctx, device, baseURL)
}

// GetDiscoverDataForDevice returns discover.json for a specific device.
func (s *HDHRService) GetDiscoverDataForDevice(ctx context.Context, device *models.HDHRDevice, baseURL string) (*DiscoverData, error) {
	return &DiscoverData{
		FriendlyName:    device.Name,
		Manufacturer:    "Silicondust",
		ManufacturerURL: "https://www.silicondust.com/",
		ModelNumber:     "HDTC-2US",
		FirmwareName:    "hdhomerun_atsc",
		FirmwareVersion: device.FirmwareVersion,
		DeviceID:        device.DeviceID,
		DeviceAuth:      device.DeviceAuth,
		BaseURL:         baseURL,
		LineupURL:       baseURL + "/lineup.json",
		TunerCount:      device.TunerCount,
	}, nil
}

// GetLineupForDevice returns lineup.json for a specific device,
// filtering channels by ChannelGroupID if set.
func (s *HDHRService) GetLineupForDevice(ctx context.Context, device *models.HDHRDevice, baseURL string) ([]LineupEntry, error) {
	channels, err := s.channelRepo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing channels: %w", err)
	}

	lineup := make([]LineupEntry, 0, len(channels))
	for _, ch := range channels {
		if !ch.IsEnabled {
			continue
		}

		// Filter by channel groups if device has any set
		if len(device.ChannelGroupIDs) > 0 {
			groupSet := make(map[int64]bool, len(device.ChannelGroupIDs))
			for _, gid := range device.ChannelGroupIDs {
				groupSet[gid] = true
			}
			if ch.ChannelGroupID == nil || !groupSet[*ch.ChannelGroupID] {
				continue
			}
		}

		streamURL := fmt.Sprintf("%s/channel/%d", baseURL, ch.ID)

		mode, _ := ResolveStreamMode(ctx, &ch, s.channelProfileRepo, s.streamProfileRepo, s.log)
		if mode == "direct" {
			if src := s.resolveSourceURL(ctx, ch.ID); src != "" {
				streamURL = src
			}
		}

		lineup = append(lineup, LineupEntry{
			GuideNumber: strconv.Itoa(ch.ChannelNumber),
			GuideName:   ch.Name,
			URL:         streamURL,
		})
	}

	return lineup, nil
}

// GetDeviceXMLForDevice returns device.xml for a specific device.
func (s *HDHRService) GetDeviceXMLForDevice(ctx context.Context, device *models.HDHRDevice, baseURL string) (*DeviceXML, error) {
	return &DeviceXML{
		XMLNS:   "urn:schemas-upnp-org:device-1-0",
		URLBase: baseURL,
		SpecVersion: specVersion{
			Major: 1,
			Minor: 0,
		},
		Device: deviceXMLInner{
			DeviceType:   "urn:schemas-upnp-org:device:MediaServer:1",
			FriendlyName: device.Name,
			Manufacturer: "Silicondust",
			ModelName:    "HDTC-2US",
			ModelNumber:  "HDTC-2US",
			SerialNumber: device.DeviceID,
			UDN:          "uuid:" + device.DeviceID,
		},
	}, nil
}
