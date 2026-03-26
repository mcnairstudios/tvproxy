package service

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
	"github.com/gavinmcnair/tvproxy/pkg/store"
)

type ChannelService struct {
	channelRepo      *repository.ChannelRepository
	channelGroupRepo *repository.ChannelGroupRepository
	streamStore      store.StreamReader
	log              zerolog.Logger
	rev              *store.Revision
	groupRev         *store.Revision
}

func NewChannelService(
	channelRepo *repository.ChannelRepository,
	channelGroupRepo *repository.ChannelGroupRepository,
	streamStore store.StreamReader,
	log zerolog.Logger,
) *ChannelService {
	return &ChannelService{
		channelRepo:      channelRepo,
		channelGroupRepo: channelGroupRepo,
		streamStore:      streamStore,
		log:              log.With().Str("service", "channel").Logger(),
		rev:              store.NewRevision(),
		groupRev:         store.NewRevision(),
	}
}

func (s *ChannelService) ETag() string {
	return s.rev.ETag()
}

func (s *ChannelService) GroupETag() string {
	return s.groupRev.ETag()
}

func (s *ChannelService) CreateChannel(ctx context.Context, channel *models.Channel) error {
	if err := s.channelRepo.Create(ctx, channel); err != nil {
		return fmt.Errorf("creating channel: %w", err)
	}
	s.rev.Bump()
	s.log.Info().Str("id", channel.ID).Str("name", channel.Name).Msg("channel created")
	return nil
}

func (s *ChannelService) GetChannel(ctx context.Context, id string) (*models.Channel, error) {
	channel, err := s.channelRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("getting channel: %w", err)
	}
	return channel, nil
}

func (s *ChannelService) ListChannels(ctx context.Context) ([]models.Channel, error) {
	channels, err := s.channelRepo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing channels: %w", err)
	}
	return channels, nil
}

func (s *ChannelService) UpdateChannel(ctx context.Context, channel *models.Channel) error {
	if err := s.channelRepo.Update(ctx, channel); err != nil {
		return fmt.Errorf("updating channel: %w", err)
	}
	s.rev.Bump()
	return nil
}

func (s *ChannelService) DeleteChannel(ctx context.Context, id string) error {
	if err := s.channelRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("deleting channel: %w", err)
	}
	s.rev.Bump()
	return nil
}

func (s *ChannelService) AssignStreams(ctx context.Context, channelID string, streamIDs []string) error {
	if _, err := s.channelRepo.GetByID(ctx, channelID); err != nil {
		return fmt.Errorf("channel not found: %w", err)
	}
	return s.assignStreams(ctx, channelID, streamIDs)
}

func (s *ChannelService) GetChannelStreams(ctx context.Context, channelID string) ([]models.Stream, error) {
	channelStreams, err := s.channelRepo.GetStreams(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("getting channel streams: %w", err)
	}

	streams := make([]models.Stream, 0, len(channelStreams))
	for _, cs := range channelStreams {
		stream, err := s.streamStore.GetByID(ctx, cs.StreamID)
		if err != nil {
			s.log.Warn().Err(err).Str("stream_id", cs.StreamID).Msg("stream not found, skipping")
			continue
		}
		streams = append(streams, *stream)
	}

	return streams, nil
}

func (s *ChannelService) ListChannelsForUser(ctx context.Context, userID string) ([]models.Channel, error) {
	channels, err := s.channelRepo.ListByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("listing channels for user: %w", err)
	}
	s.log.Debug().Str("user_id", userID).Int("count", len(channels)).Msg("listing channels for user")
	return channels, nil
}

func (s *ChannelService) GetChannelForUser(ctx context.Context, id, userID string) (*models.Channel, error) {
	channel, err := s.channelRepo.GetByIDForUser(ctx, id, userID)
	if err != nil {
		return nil, fmt.Errorf("getting channel for user: %w", err)
	}
	return channel, nil
}

func (s *ChannelService) UpdateChannelForUser(ctx context.Context, channel *models.Channel, userID string) error {
	if err := s.channelRepo.UpdateForUser(ctx, channel, userID); err != nil {
		return fmt.Errorf("updating channel for user: %w", err)
	}
	s.rev.Bump()
	return nil
}

func (s *ChannelService) DeleteChannelForUser(ctx context.Context, id, userID string) error {
	if err := s.channelRepo.DeleteForUser(ctx, id, userID); err != nil {
		return fmt.Errorf("deleting channel for user: %w", err)
	}
	s.rev.Bump()
	return nil
}

func (s *ChannelService) AssignStreamsForUser(ctx context.Context, channelID string, streamIDs []string, userID string) error {
	if _, err := s.channelRepo.GetByIDForUser(ctx, channelID, userID); err != nil {
		return fmt.Errorf("channel not found: %w", err)
	}
	return s.assignStreams(ctx, channelID, streamIDs)
}

func (s *ChannelService) GetChannelStreamsForUser(ctx context.Context, channelID, userID string) ([]models.Stream, error) {
	if _, err := s.channelRepo.GetByIDForUser(ctx, channelID, userID); err != nil {
		return nil, fmt.Errorf("channel not found: %w", err)
	}
	return s.GetChannelStreams(ctx, channelID)
}

func (s *ChannelService) ListChannelGroupsForUser(ctx context.Context, userID string) ([]models.ChannelGroup, error) {
	groups, err := s.channelGroupRepo.ListByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("listing channel groups for user: %w", err)
	}
	return groups, nil
}

func (s *ChannelService) GetChannelGroupForUser(ctx context.Context, id, userID string) (*models.ChannelGroup, error) {
	group, err := s.channelGroupRepo.GetByIDForUser(ctx, id, userID)
	if err != nil {
		return nil, fmt.Errorf("getting channel group for user: %w", err)
	}
	return group, nil
}

func (s *ChannelService) UpdateChannelGroupForUser(ctx context.Context, group *models.ChannelGroup, userID string) error {
	if err := s.channelGroupRepo.UpdateForUser(ctx, group, userID); err != nil {
		return fmt.Errorf("updating channel group for user: %w", err)
	}
	s.groupRev.Bump()
	return nil
}

func (s *ChannelService) DeleteChannelGroupForUser(ctx context.Context, id, userID string) error {
	if err := s.channelGroupRepo.DeleteForUser(ctx, id, userID); err != nil {
		return fmt.Errorf("deleting channel group for user: %w", err)
	}
	s.groupRev.Bump()
	return nil
}

func (s *ChannelService) CreateChannelGroup(ctx context.Context, group *models.ChannelGroup) error {
	if err := s.channelGroupRepo.Create(ctx, group); err != nil {
		return fmt.Errorf("creating channel group: %w", err)
	}
	s.groupRev.Bump()
	return nil
}

func (s *ChannelService) GetChannelGroup(ctx context.Context, id string) (*models.ChannelGroup, error) {
	group, err := s.channelGroupRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("getting channel group: %w", err)
	}
	return group, nil
}

func (s *ChannelService) ListChannelGroups(ctx context.Context) ([]models.ChannelGroup, error) {
	groups, err := s.channelGroupRepo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing channel groups: %w", err)
	}
	return groups, nil
}

func (s *ChannelService) UpdateChannelGroup(ctx context.Context, group *models.ChannelGroup) error {
	if err := s.channelGroupRepo.Update(ctx, group); err != nil {
		return fmt.Errorf("updating channel group: %w", err)
	}
	s.groupRev.Bump()
	return nil
}

func (s *ChannelService) DeleteChannelGroup(ctx context.Context, id string) error {
	if err := s.channelGroupRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("deleting channel group: %w", err)
	}
	s.groupRev.Bump()
	return nil
}

func (s *ChannelService) IncrementChannelFailCount(ctx context.Context, id string) error {
	if err := s.channelRepo.IncrementFailCount(ctx, id); err != nil {
		return err
	}
	s.rev.Bump()
	return nil
}

func (s *ChannelService) ResetChannelFailCount(ctx context.Context, id string) error {
	if err := s.channelRepo.ResetFailCount(ctx, id); err != nil {
		return err
	}
	s.rev.Bump()
	return nil
}

func (s *ChannelService) assignStreams(ctx context.Context, channelID string, streamIDs []string) error {
	for _, streamID := range streamIDs {
		if _, err := s.streamStore.GetByID(ctx, streamID); err != nil {
			return fmt.Errorf("stream %s not found: %w", streamID, err)
		}
	}

	prios := make([]int, len(streamIDs))
	for i := range streamIDs {
		prios[i] = i + 1
	}
	if err := s.channelRepo.AssignStreams(ctx, channelID, streamIDs, prios); err != nil {
		return fmt.Errorf("assigning streams to channel: %w", err)
	}

	s.rev.Bump()
	s.log.Info().Str("channel_id", channelID).Int("stream_count", len(streamIDs)).Msg("streams assigned to channel")
	return nil
}
