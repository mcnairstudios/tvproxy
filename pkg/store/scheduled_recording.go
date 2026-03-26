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

type ScheduledRecordingStore interface {
	Create(ctx context.Context, rec *models.ScheduledRecording) error
	GetByID(ctx context.Context, id string) (*models.ScheduledRecording, error)
	List(ctx context.Context) ([]models.ScheduledRecording, error)
	ListByUserID(ctx context.Context, userID string) ([]models.ScheduledRecording, error)
	ListPending(ctx context.Context, before time.Time) ([]models.ScheduledRecording, error)
	ListByStatus(ctx context.Context, status string) ([]models.ScheduledRecording, error)
	ListByChannelAndTimeRange(ctx context.Context, channelID, userID string, start, stop time.Time) ([]models.ScheduledRecording, error)
	UpdateStatus(ctx context.Context, id, status, lastError string) error
	UpdateRecordingState(ctx context.Context, id, sessionID, segmentID string) error
	UpdateFilePath(ctx context.Context, id, filePath string) error
	Delete(ctx context.Context, id string) error
}

type ScheduledRecordingStoreImpl struct {
	filePath   string
	recordings []models.ScheduledRecording
	mu         sync.RWMutex
}

func NewScheduledRecordingStore(filePath string) *ScheduledRecordingStoreImpl {
	return &ScheduledRecordingStoreImpl{filePath: filePath}
}

func (s *ScheduledRecordingStoreImpl) Load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return json.Unmarshal(data, &s.recordings)
}

func (s *ScheduledRecordingStoreImpl) save() error {
	data, err := json.MarshalIndent(s.recordings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, data, 0644)
}

func (s *ScheduledRecordingStoreImpl) Create(_ context.Context, rec *models.ScheduledRecording) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rec.ID == "" {
		rec.ID = uuid.New().String()
	}
	if rec.Status == "" {
		rec.Status = "pending"
	}
	now := time.Now()
	rec.CreatedAt = now
	rec.UpdatedAt = now
	s.recordings = append(s.recordings, *rec)
	return s.save()
}

func (s *ScheduledRecordingStoreImpl) GetByID(_ context.Context, id string) (*models.ScheduledRecording, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.recordings {
		if s.recordings[i].ID == id {
			r := s.recordings[i]
			return &r, nil
		}
	}
	return nil, fmt.Errorf("scheduled recording not found")
}

func (s *ScheduledRecordingStoreImpl) List(_ context.Context) ([]models.ScheduledRecording, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]models.ScheduledRecording, len(s.recordings))
	copy(result, s.recordings)
	return result, nil
}

func (s *ScheduledRecordingStoreImpl) ListByUserID(_ context.Context, userID string) ([]models.ScheduledRecording, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []models.ScheduledRecording
	for _, r := range s.recordings {
		if r.UserID == userID {
			result = append(result, r)
		}
	}
	return result, nil
}

func (s *ScheduledRecordingStoreImpl) ListPending(_ context.Context, before time.Time) ([]models.ScheduledRecording, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []models.ScheduledRecording
	for _, r := range s.recordings {
		if r.Status == "pending" && !r.StartAt.After(before) {
			result = append(result, r)
		}
	}
	return result, nil
}

func (s *ScheduledRecordingStoreImpl) ListByStatus(_ context.Context, status string) ([]models.ScheduledRecording, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []models.ScheduledRecording
	for _, r := range s.recordings {
		if r.Status == status {
			result = append(result, r)
		}
	}
	return result, nil
}

func (s *ScheduledRecordingStoreImpl) ListByChannelAndTimeRange(_ context.Context, channelID, userID string, start, stop time.Time) ([]models.ScheduledRecording, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	excludeStatus := map[string]bool{"cancelled": true, "failed": true, "completed": true}
	var result []models.ScheduledRecording
	for _, r := range s.recordings {
		if r.ChannelID != channelID || r.UserID != userID {
			continue
		}
		if excludeStatus[r.Status] {
			continue
		}
		if r.StartAt.Before(stop) && r.StopAt.After(start) {
			result = append(result, r)
		}
	}
	return result, nil
}

func (s *ScheduledRecordingStoreImpl) updateField(id string, fn func(*models.ScheduledRecording)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.recordings {
		if s.recordings[i].ID == id {
			fn(&s.recordings[i])
			s.recordings[i].UpdatedAt = time.Now()
			return s.save()
		}
	}
	return fmt.Errorf("scheduled recording not found")
}

func (s *ScheduledRecordingStoreImpl) UpdateStatus(_ context.Context, id, status, lastError string) error {
	return s.updateField(id, func(r *models.ScheduledRecording) {
		r.Status = status
		r.LastError = lastError
	})
}

func (s *ScheduledRecordingStoreImpl) UpdateRecordingState(_ context.Context, id, sessionID, segmentID string) error {
	return s.updateField(id, func(r *models.ScheduledRecording) {
		r.SessionID = sessionID
		r.SegmentID = segmentID
	})
}

func (s *ScheduledRecordingStoreImpl) UpdateFilePath(_ context.Context, id, filePath string) error {
	return s.updateField(id, func(r *models.ScheduledRecording) {
		r.FilePath = filePath
	})
}

func (s *ScheduledRecordingStoreImpl) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.recordings {
		if s.recordings[i].ID == id {
			s.recordings = append(s.recordings[:i], s.recordings[i+1:]...)
			return s.save()
		}
	}
	return nil
}
