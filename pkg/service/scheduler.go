package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/config"
	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
)

const (
	RecStatusPending   = "pending"
	RecStatusRecording = "recording"
	RecStatusCompleted = "completed"
	RecStatusFailed    = "failed"
)

var (
	ErrScheduleConflict  = errors.New("overlapping recording already scheduled")
	ErrRecordingNotFound = errors.New("scheduled recording not found")
)

type SchedulerService struct {
	recRepo     *repository.ScheduledRecordingRepository
	channelRepo *repository.ChannelRepository
	vodService  *VODService
	config      *config.Config
	log         zerolog.Logger
}

func NewSchedulerService(
	recRepo *repository.ScheduledRecordingRepository,
	channelRepo *repository.ChannelRepository,
	vodService *VODService,
	cfg *config.Config,
	log zerolog.Logger,
) *SchedulerService {
	return &SchedulerService{
		recRepo:     recRepo,
		channelRepo: channelRepo,
		vodService:  vodService,
		config:      cfg,
		log:         log.With().Str("service", "scheduler").Logger(),
	}
}

func (s *SchedulerService) Schedule(ctx context.Context, rec *models.ScheduledRecording) error {
	channel, err := s.channelRepo.GetByID(ctx, rec.ChannelID)
	if err != nil {
		return fmt.Errorf("channel not found: %w", err)
	}
	if !channel.IsEnabled {
		return fmt.Errorf("channel %s is disabled", rec.ChannelID)
	}

	startCutoff := 5 * time.Minute
	if s.config.Settings != nil {
		startCutoff = s.config.Settings.Recording.StartCutoff
	}
	if rec.StartAt.Before(time.Now().Add(-startCutoff)) {
		return fmt.Errorf("start time is too far in the past")
	}
	if !rec.StopAt.After(rec.StartAt) {
		return fmt.Errorf("stop time must be after start time")
	}

	existing, err := s.recRepo.ListByChannelAndTimeRange(ctx, rec.ChannelID, rec.UserID, rec.StartAt, rec.StopAt)
	if err != nil {
		return fmt.Errorf("checking duplicates: %w", err)
	}
	if len(existing) > 0 {
		return ErrScheduleConflict
	}

	if err := s.recRepo.Create(ctx, rec); err != nil {
		return err
	}

	leadTime := 30 * time.Second
	if s.config.Settings != nil {
		leadTime = s.config.Settings.Recording.LeadTime
	}
	if rec.StartAt.Before(time.Now().Add(leadTime)) {
		s.startRecording(ctx, rec)
	}

	s.log.Info().Str("id", rec.ID).Str("channel", rec.ChannelName).Str("program", rec.ProgramTitle).Time("start", rec.StartAt).Time("stop", rec.StopAt).Msg("recording scheduled")
	return nil
}

func (s *SchedulerService) Delete(ctx context.Context, id, userID string, isAdmin bool) error {
	rec, err := s.recRepo.GetByID(ctx, id)
	if err != nil {
		return ErrRecordingNotFound
	}
	if !isAdmin && rec.UserID != userID {
		return ErrNotAuthorized
	}

	if rec.Status == RecStatusRecording {
		s.vodService.StopRecording(rec.ChannelID)
	}

	if rec.FilePath != "" {
		os.Remove(rec.FilePath)
	}

	return s.recRepo.Delete(ctx, id)
}

func (s *SchedulerService) List(ctx context.Context, userID string, isAdmin bool) ([]models.ScheduledRecording, error) {
	if isAdmin {
		return s.recRepo.List(ctx)
	}
	return s.recRepo.ListByUserID(ctx, userID)
}

func (s *SchedulerService) Get(ctx context.Context, id, userID string, isAdmin bool) (*models.ScheduledRecording, error) {
	rec, err := s.recRepo.GetByID(ctx, id)
	if err != nil {
		return nil, ErrRecordingNotFound
	}
	if !isAdmin && rec.UserID != userID {
		return nil, ErrNotAuthorized
	}
	return rec, nil
}

func (s *SchedulerService) Tick(ctx context.Context) {
	s.startPendingRecordings(ctx)
	s.monitorActiveRecordings(ctx)
}

func (s *SchedulerService) startPendingRecordings(ctx context.Context) {
	now := time.Now()

	expired, err := s.recRepo.ListByStatus(ctx, RecStatusPending)
	if err != nil {
		s.log.Error().Err(err).Msg("failed to list pending recordings for cleanup")
	} else {
		for _, rec := range expired {
			if rec.StopAt.Before(now) {
				s.recRepo.Delete(ctx, rec.ID)
				s.log.Info().Str("id", rec.ID).Str("program", rec.ProgramTitle).Msg("removed missed pending recording")
			}
		}
	}

	pending, err := s.recRepo.ListPending(ctx, now)
	if err != nil {
		s.log.Error().Err(err).Msg("failed to list pending recordings")
		return
	}
	for i := range pending {
		s.startRecording(ctx, &pending[i])
	}
}

func (s *SchedulerService) monitorActiveRecordings(ctx context.Context) {
	active, err := s.recRepo.ListByStatus(ctx, RecStatusRecording)
	if err != nil {
		s.log.Error().Err(err).Msg("failed to list active recordings")
		return
	}

	for _, rec := range active {
		if rec.StopAt.Before(time.Now()) {
			s.vodService.StopRecording(rec.ChannelID)
			s.recRepo.UpdateStatus(ctx, rec.ID, RecStatusCompleted, "")
			s.log.Info().Str("id", rec.ID).Msg("scheduled recording completed")
			continue
		}

		sess := s.vodService.GetSession(rec.ChannelID)
		if sess == nil {
			if rec.StopAt.Before(time.Now()) {
				s.recRepo.UpdateStatus(ctx, rec.ID, RecStatusFailed, "recording session lost")
			} else {
				s.startRecording(ctx, &rec)
			}
		}
	}
}

func (s *SchedulerService) startRecording(ctx context.Context, rec *models.ScheduledRecording) {
	if err := s.recRepo.UpdateStatus(ctx, rec.ID, RecStatusRecording, ""); err != nil {
		s.log.Error().Err(err).Str("id", rec.ID).Msg("failed to update status to recording")
		return
	}

	err := s.vodService.StartRecording(ctx, rec.ChannelID, rec.ProgramTitle, rec.ChannelName, rec.UserID, rec.StopAt)
	if err != nil {
		if !errors.Is(err, ErrAlreadyRecording) {
			s.log.Error().Err(err).Str("id", rec.ID).Msg("failed to start recording session")
			s.recRepo.UpdateStatus(ctx, rec.ID, RecStatusFailed, err.Error())
			return
		}
	}

	if err := s.recRepo.UpdateRecordingState(ctx, rec.ID, rec.ChannelID, ""); err != nil {
		s.log.Error().Err(err).Str("id", rec.ID).Msg("failed to update recording state")
	}

	s.log.Info().Str("id", rec.ID).Str("channel_id", rec.ChannelID).Msg("scheduled recording started")
}
