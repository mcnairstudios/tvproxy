package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/gavinmcnair/tvproxy/pkg/models"
)

type HDHRDeviceStore interface {
	Create(ctx context.Context, device *models.HDHRDevice) error
	GetByID(ctx context.Context, id string) (*models.HDHRDevice, error)
	GetByDeviceID(ctx context.Context, deviceID string) (*models.HDHRDevice, error)
	List(ctx context.Context) ([]models.HDHRDevice, error)
	Update(ctx context.Context, device *models.HDHRDevice) error
	Delete(ctx context.Context, id string) error
	NextAvailablePort(ctx context.Context) (int, error)
	SetChannelGroups(ctx context.Context, deviceID string, groupIDs []string) error
}

type HDHRDeviceStoreImpl struct {
	filePath string
	devices  []models.HDHRDevice
	mu       sync.RWMutex
}

func NewHDHRDeviceStore(filePath string) *HDHRDeviceStoreImpl {
	return &HDHRDeviceStoreImpl{filePath: filePath}
}

func (s *HDHRDeviceStoreImpl) Load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return json.Unmarshal(data, &s.devices)
}

func (s *HDHRDeviceStoreImpl) save() error {
	data, err := json.MarshalIndent(s.devices, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, data, 0644)
}

func (s *HDHRDeviceStoreImpl) Create(_ context.Context, device *models.HDHRDevice) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if device.ID == "" {
		device.ID = uuid.New().String()
	}
	now := time.Now()
	device.CreatedAt = now
	device.UpdatedAt = now
	s.devices = append(s.devices, *device)
	return s.save()
}

func (s *HDHRDeviceStoreImpl) GetByID(_ context.Context, id string) (*models.HDHRDevice, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.devices {
		if s.devices[i].ID == id {
			d := s.devices[i]
			return &d, nil
		}
	}
	return nil, fmt.Errorf("hdhr device not found")
}

func (s *HDHRDeviceStoreImpl) GetByDeviceID(_ context.Context, deviceID string) (*models.HDHRDevice, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.devices {
		if s.devices[i].DeviceID == deviceID {
			d := s.devices[i]
			return &d, nil
		}
	}
	return nil, fmt.Errorf("hdhr device not found")
}

func (s *HDHRDeviceStoreImpl) List(_ context.Context) ([]models.HDHRDevice, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]models.HDHRDevice, len(s.devices))
	copy(result, s.devices)
	return result, nil
}

func (s *HDHRDeviceStoreImpl) Update(_ context.Context, device *models.HDHRDevice) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.devices {
		if s.devices[i].ID == device.ID {
			device.UpdatedAt = time.Now()
			device.CreatedAt = s.devices[i].CreatedAt
			s.devices[i] = *device
			return s.save()
		}
	}
	return fmt.Errorf("hdhr device not found")
}

func (s *HDHRDeviceStoreImpl) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.devices {
		if s.devices[i].ID == id {
			s.devices = append(s.devices[:i], s.devices[i+1:]...)
			return s.save()
		}
	}
	return nil
}

func (s *HDHRDeviceStoreImpl) NextAvailablePort(_ context.Context) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	maxPort := 5003
	for _, d := range s.devices {
		if d.Port >= maxPort {
			maxPort = d.Port + 1
		}
	}
	return maxPort, nil
}

func (s *HDHRDeviceStoreImpl) SetChannelGroups(_ context.Context, deviceID string, groupIDs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.devices {
		if s.devices[i].ID == deviceID {
			s.devices[i].ChannelGroupIDs = groupIDs
			return s.save()
		}
	}
	return fmt.Errorf("hdhr device not found")
}

func (s *HDHRDeviceStoreImpl) ClearAndSave() error {
	s.mu.Lock()
	s.devices = nil
	s.mu.Unlock()
	return s.save()
}
