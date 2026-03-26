package service

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"strconv"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
	"github.com/gavinmcnair/tvproxy/pkg/xmlutil"
)

var ErrNoHDHRDevice = errors.New("no enabled HDHR device configured")

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

type LineupEntry struct {
	GuideNumber string `json:"GuideNumber"`
	GuideName   string `json:"GuideName"`
	URL         string `json:"URL"`
}

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

type HDHRService struct {
	hdhrDeviceRepo *repository.HDHRDeviceRepository
	channelRepo    *repository.ChannelRepository
}

func NewHDHRService(
	hdhrDeviceRepo *repository.HDHRDeviceRepository,
	channelRepo *repository.ChannelRepository,
) *HDHRService {
	return &HDHRService{
		hdhrDeviceRepo: hdhrDeviceRepo,
		channelRepo:    channelRepo,
	}
}

func (s *HDHRService) NextAvailablePort(ctx context.Context) (int, error) {
	port, err := s.hdhrDeviceRepo.NextAvailablePort(ctx)
	if err != nil {
		return 0, fmt.Errorf("getting next available port: %w", err)
	}
	return port, nil
}

func (s *HDHRService) SetChannelGroups(ctx context.Context, deviceID string, groupIDs []string) error {
	if err := s.hdhrDeviceRepo.SetChannelGroups(ctx, deviceID, groupIDs); err != nil {
		return fmt.Errorf("setting channel groups: %w", err)
	}
	return nil
}

func (s *HDHRService) CreateDevice(ctx context.Context, device *models.HDHRDevice) error {
	if err := s.hdhrDeviceRepo.Create(ctx, device); err != nil {
		return fmt.Errorf("creating hdhr device: %w", err)
	}
	return nil
}

func (s *HDHRService) GetDevice(ctx context.Context, id string) (*models.HDHRDevice, error) {
	device, err := s.hdhrDeviceRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("getting hdhr device: %w", err)
	}
	return device, nil
}

func (s *HDHRService) GetDeviceByDeviceID(ctx context.Context, deviceID string) (*models.HDHRDevice, error) {
	device, err := s.hdhrDeviceRepo.GetByDeviceID(ctx, deviceID)
	if err != nil {
		return nil, fmt.Errorf("getting hdhr device by device id: %w", err)
	}
	return device, nil
}

func (s *HDHRService) ListDevices(ctx context.Context) ([]models.HDHRDevice, error) {
	devices, err := s.hdhrDeviceRepo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing hdhr devices: %w", err)
	}
	return devices, nil
}

func (s *HDHRService) UpdateDevice(ctx context.Context, device *models.HDHRDevice) error {
	if err := s.hdhrDeviceRepo.Update(ctx, device); err != nil {
		return fmt.Errorf("updating hdhr device: %w", err)
	}
	return nil
}

func (s *HDHRService) DeleteDevice(ctx context.Context, id string) error {
	if err := s.hdhrDeviceRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("deleting hdhr device: %w", err)
	}
	return nil
}

func (s *HDHRService) GetDiscoverData(ctx context.Context, baseURL string) (*DiscoverData, error) {
	device, err := s.firstEnabledDevice(ctx)
	if err != nil {
		return nil, err
	}
	return s.GetDiscoverDataForDevice(ctx, device, baseURL)
}

func (s *HDHRService) GetLineup(ctx context.Context, baseURL string) ([]LineupEntry, error) {
	return s.buildLineup(ctx, baseURL, nil)
}

func (s *HDHRService) GetDeviceXML(ctx context.Context, baseURL string) (*DeviceXML, error) {
	device, err := s.firstEnabledDevice(ctx)
	if err != nil {
		return nil, err
	}
	return s.GetDeviceXMLForDevice(ctx, device, baseURL)
}

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

func (s *HDHRService) GetLineupForDevice(ctx context.Context, device *models.HDHRDevice, baseURL string) ([]LineupEntry, error) {
	var groupSet map[string]bool
	if len(device.ChannelGroupIDs) > 0 {
		groupSet = xmlutil.ToStringSet(device.ChannelGroupIDs)
	}
	return s.buildLineup(ctx, baseURL, groupSet)
}

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

func (s *HDHRService) firstEnabledDevice(ctx context.Context) (*models.HDHRDevice, error) {
	devices, err := s.hdhrDeviceRepo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing devices: %w", err)
	}
	if len(devices) == 0 {
		return nil, ErrNoHDHRDevice
	}
	for i := range devices {
		if devices[i].IsEnabled {
			return &devices[i], nil
		}
	}
	return nil, ErrNoHDHRDevice
}

func (s *HDHRService) buildLineup(ctx context.Context, baseURL string, groupFilter map[string]bool) ([]LineupEntry, error) {
	channels, err := s.channelRepo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing channels: %w", err)
	}

	lineup := make([]LineupEntry, 0, len(channels))
	guideNum := 1
	for _, ch := range channels {
		if !ch.IsEnabled {
			continue
		}
		if groupFilter != nil {
			if ch.ChannelGroupID == nil || !groupFilter[*ch.ChannelGroupID] {
				continue
			}
		}

		streamURL := ResolveChannelURL(ch.ID, baseURL)

		lineup = append(lineup, LineupEntry{
			GuideNumber: strconv.Itoa(guideNum),
			GuideName:   ch.Name,
			URL:         streamURL,
		})
		guideNum++
	}

	return lineup, nil
}
