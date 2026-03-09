package service

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
)

// ChannelService handles channel management and stream assignment.
type ChannelService struct {
	channelRepo      *repository.ChannelRepository
	channelGroupRepo *repository.ChannelGroupRepository
	streamRepo       *repository.StreamRepository
	log              zerolog.Logger
}

// NewChannelService creates a new ChannelService.
func NewChannelService(
	channelRepo *repository.ChannelRepository,
	channelGroupRepo *repository.ChannelGroupRepository,
	streamRepo *repository.StreamRepository,
	log zerolog.Logger,
) *ChannelService {
	return &ChannelService{
		channelRepo:      channelRepo,
		channelGroupRepo: channelGroupRepo,
		streamRepo:       streamRepo,
		log:              log.With().Str("service", "channel").Logger(),
	}
}

// CreateChannel creates a new channel. If no channel number is provided, the
// next available number is assigned automatically.
func (s *ChannelService) CreateChannel(ctx context.Context, channel *models.Channel) error {
	if channel.ChannelNumber == 0 {
		next, err := s.channelRepo.GetNextChannelNumber(ctx)
		if err != nil {
			return fmt.Errorf("getting next channel number: %w", err)
		}
		channel.ChannelNumber = next
	}

	if err := s.channelRepo.Create(ctx, channel); err != nil {
		return fmt.Errorf("creating channel: %w", err)
	}

	s.log.Info().Int64("id", channel.ID).Int("number", channel.ChannelNumber).Str("name", channel.Name).Msg("channel created")
	return nil
}

// GetChannel returns a channel by ID.
func (s *ChannelService) GetChannel(ctx context.Context, id int64) (*models.Channel, error) {
	channel, err := s.channelRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("getting channel: %w", err)
	}
	return channel, nil
}

// GetChannelByNumber returns a channel by its channel number.
func (s *ChannelService) GetChannelByNumber(ctx context.Context, number int) (*models.Channel, error) {
	channel, err := s.channelRepo.GetByNumber(ctx, number)
	if err != nil {
		return nil, fmt.Errorf("getting channel by number: %w", err)
	}
	return channel, nil
}

// ListChannels returns all channels.
func (s *ChannelService) ListChannels(ctx context.Context) ([]models.Channel, error) {
	channels, err := s.channelRepo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing channels: %w", err)
	}
	return channels, nil
}

// UpdateChannel updates an existing channel.
func (s *ChannelService) UpdateChannel(ctx context.Context, channel *models.Channel) error {
	if err := s.channelRepo.Update(ctx, channel); err != nil {
		return fmt.Errorf("updating channel: %w", err)
	}
	return nil
}

// DeleteChannel deletes a channel by ID.
func (s *ChannelService) DeleteChannel(ctx context.Context, id int64) error {
	if err := s.channelRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("deleting channel: %w", err)
	}
	return nil
}

// AssignStreams assigns a list of streams (by stream ID) to a channel with
// priority determined by their position in the slice. Existing assignments
// for the channel are replaced.
func (s *ChannelService) AssignStreams(ctx context.Context, channelID int64, streamIDs []int64) error {
	// Verify the channel exists
	_, err := s.channelRepo.GetByID(ctx, channelID)
	if err != nil {
		return fmt.Errorf("channel not found: %w", err)
	}

	// Verify all streams exist
	for _, streamID := range streamIDs {
		if _, err := s.streamRepo.GetByID(ctx, streamID); err != nil {
			return fmt.Errorf("stream %d not found: %w", streamID, err)
		}
	}

	// Build channel stream assignments with priority
	channelStreams := make([]models.ChannelStream, len(streamIDs))
	for i, streamID := range streamIDs {
		channelStreams[i] = models.ChannelStream{
			ChannelID: channelID,
			StreamID:  streamID,
			Priority:  i + 1,
		}
	}

	ids := make([]int64, len(channelStreams))
	prios := make([]int, len(channelStreams))
	for i, cs := range channelStreams {
		ids[i] = cs.StreamID
		prios[i] = cs.Priority
	}
	if err := s.channelRepo.AssignStreams(ctx, channelID, ids, prios); err != nil {
		return fmt.Errorf("assigning streams to channel: %w", err)
	}

	s.log.Info().Int64("channel_id", channelID).Int("stream_count", len(streamIDs)).Msg("streams assigned to channel")
	return nil
}

// GetChannelStreams returns the streams assigned to a channel, ordered by priority.
func (s *ChannelService) GetChannelStreams(ctx context.Context, channelID int64) ([]models.Stream, error) {
	channelStreams, err := s.channelRepo.GetStreams(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("getting channel streams: %w", err)
	}

	streams := make([]models.Stream, 0, len(channelStreams))
	for _, cs := range channelStreams {
		stream, err := s.streamRepo.GetByID(ctx, cs.StreamID)
		if err != nil {
			s.log.Warn().Err(err).Int64("stream_id", cs.StreamID).Msg("stream not found, skipping")
			continue
		}
		streams = append(streams, *stream)
	}

	return streams, nil
}

// CreateChannelGroup creates a new channel group.
func (s *ChannelService) CreateChannelGroup(ctx context.Context, group *models.ChannelGroup) error {
	if err := s.channelGroupRepo.Create(ctx, group); err != nil {
		return fmt.Errorf("creating channel group: %w", err)
	}
	return nil
}

// GetChannelGroup returns a channel group by ID.
func (s *ChannelService) GetChannelGroup(ctx context.Context, id int64) (*models.ChannelGroup, error) {
	group, err := s.channelGroupRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("getting channel group: %w", err)
	}
	return group, nil
}

// ListChannelGroups returns all channel groups.
func (s *ChannelService) ListChannelGroups(ctx context.Context) ([]models.ChannelGroup, error) {
	groups, err := s.channelGroupRepo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing channel groups: %w", err)
	}
	return groups, nil
}

// UpdateChannelGroup updates an existing channel group.
func (s *ChannelService) UpdateChannelGroup(ctx context.Context, group *models.ChannelGroup) error {
	if err := s.channelGroupRepo.Update(ctx, group); err != nil {
		return fmt.Errorf("updating channel group: %w", err)
	}
	return nil
}

// DeleteChannelGroup deletes a channel group by ID.
func (s *ChannelService) DeleteChannelGroup(ctx context.Context, id int64) error {
	if err := s.channelGroupRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("deleting channel group: %w", err)
	}
	return nil
}
