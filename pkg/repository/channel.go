package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/gavinmcnair/tvproxy/pkg/database"
	"github.com/gavinmcnair/tvproxy/pkg/models"
)

const channelSelect = `SELECT c.id, c.user_id, c.name, c.logo_id, COALESCE(l.url, ''), COALESCE(l.cached_filename, ''), c.tvg_id, c.channel_group_id, c.stream_profile_id, c.fail_count, c.is_enabled, c.created_at, c.updated_at
		FROM channels c LEFT JOIN logos l ON c.logo_id = l.id`

type channelScanner interface {
	Scan(dest ...any) error
}

func scanChannel(s channelScanner) (*models.Channel, error) {
	c := &models.Channel{}
	var logoID, groupID, profileID sql.NullString
	if err := s.Scan(
		&c.ID, &c.UserID, &c.Name, &logoID, &c.Logo, &c.LogoCached, &c.TvgID,
		&groupID, &profileID, &c.FailCount, &c.IsEnabled, &c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if logoID.Valid {
		c.LogoID = &logoID.String
	}
	if groupID.Valid {
		c.ChannelGroupID = &groupID.String
	}
	if profileID.Valid {
		c.StreamProfileID = &profileID.String
	}
	return c, nil
}

type ChannelRepository struct {
	db *database.DB
}

func NewChannelRepository(db *database.DB) *ChannelRepository {
	return &ChannelRepository{db: db}
}

func (r *ChannelRepository) Create(ctx context.Context, channel *models.Channel) error {
	now := time.Now()
	channel.ID = uuid.New().String()
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO channels (id, user_id, name, logo_id, tvg_id, channel_group_id, stream_profile_id, is_enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		channel.ID, channel.UserID, channel.Name, channel.LogoID, channel.TvgID,
		channel.ChannelGroupID, channel.StreamProfileID, channel.IsEnabled, now, now,
	)
	if err != nil {
		return fmt.Errorf("creating channel: %w", err)
	}
	channel.CreatedAt = now
	channel.UpdatedAt = now
	return nil
}

func (r *ChannelRepository) GetByID(ctx context.Context, id string) (*models.Channel, error) {
	ch, err := scanChannel(r.db.QueryRowContext(ctx, channelSelect+` WHERE c.id = ?`, id))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("channel not found: %w", err)
		}
		return nil, fmt.Errorf("getting channel by id: %w", err)
	}
	return ch, nil
}

func (r *ChannelRepository) GetByIDForUser(ctx context.Context, id, userID string) (*models.Channel, error) {
	ch, err := scanChannel(r.db.QueryRowContext(ctx, channelSelect+` WHERE c.id = ? AND c.user_id = ?`, id, userID))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("channel not found: %w", err)
		}
		return nil, fmt.Errorf("getting channel for user: %w", err)
	}
	return ch, nil
}

func (r *ChannelRepository) List(ctx context.Context) ([]models.Channel, error) {
	rows, err := r.db.QueryContext(ctx, channelSelect+` ORDER BY c.name COLLATE NOCASE`)
	if err != nil {
		return nil, fmt.Errorf("listing channels: %w", err)
	}
	defer rows.Close()
	return r.scanRows(rows)
}

func (r *ChannelRepository) ListByUserID(ctx context.Context, userID string) ([]models.Channel, error) {
	rows, err := r.db.QueryContext(ctx, channelSelect+` WHERE c.user_id = ? ORDER BY c.name COLLATE NOCASE`, userID)
	if err != nil {
		return nil, fmt.Errorf("listing channels by user: %w", err)
	}
	defer rows.Close()
	return r.scanRows(rows)
}

func (r *ChannelRepository) Update(ctx context.Context, channel *models.Channel) error {
	now := time.Now()
	_, err := r.db.ExecContext(ctx,
		`UPDATE channels SET name = ?, logo_id = ?, tvg_id = ?, channel_group_id = ?, stream_profile_id = ?, is_enabled = ?, updated_at = ?
		WHERE id = ?`,
		channel.Name, channel.LogoID, channel.TvgID,
		channel.ChannelGroupID, channel.StreamProfileID, channel.IsEnabled, now, channel.ID,
	)
	if err != nil {
		return fmt.Errorf("updating channel: %w", err)
	}
	channel.UpdatedAt = now
	return nil
}

func (r *ChannelRepository) UpdateForUser(ctx context.Context, channel *models.Channel, userID string) error {
	now := time.Now()
	_, err := r.db.ExecContext(ctx,
		`UPDATE channels SET name = ?, logo_id = ?, tvg_id = ?, channel_group_id = ?, stream_profile_id = ?, is_enabled = ?, updated_at = ?
		WHERE id = ? AND user_id = ?`,
		channel.Name, channel.LogoID, channel.TvgID,
		channel.ChannelGroupID, channel.StreamProfileID, channel.IsEnabled, now, channel.ID, userID,
	)
	if err != nil {
		return fmt.Errorf("updating channel for user: %w", err)
	}
	channel.UpdatedAt = now
	return nil
}

func (r *ChannelRepository) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM channels WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting channel: %w", err)
	}
	return nil
}

func (r *ChannelRepository) DeleteForUser(ctx context.Context, id, userID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM channels WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("deleting channel for user: %w", err)
	}
	return nil
}

func (r *ChannelRepository) AssignStreams(ctx context.Context, channelID string, streamIDs []string, priorities []int) error {
	if len(streamIDs) != len(priorities) {
		return fmt.Errorf("streamIDs and priorities must have the same length")
	}
	return r.db.InTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM channel_streams WHERE channel_id = ?`, channelID); err != nil {
			return fmt.Errorf("deleting existing channel streams: %w", err)
		}
		stmt, err := tx.PrepareContext(ctx,
			`INSERT INTO channel_streams (id, channel_id, stream_id, priority) VALUES (?, ?, ?, ?)`,
		)
		if err != nil {
			return fmt.Errorf("preparing statement: %w", err)
		}
		defer stmt.Close()

		for i := range streamIDs {
			if _, err := stmt.ExecContext(ctx, uuid.New().String(), channelID, streamIDs[i], priorities[i]); err != nil {
				return fmt.Errorf("inserting channel stream %d: %w", i, err)
			}
		}
		return nil
	})
}

func (r *ChannelRepository) GetStreams(ctx context.Context, channelID string) ([]models.ChannelStream, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, channel_id, stream_id, priority
		FROM channel_streams WHERE channel_id = ? ORDER BY priority`, channelID,
	)
	if err != nil {
		return nil, fmt.Errorf("getting channel streams: %w", err)
	}
	defer rows.Close()

	var channelStreams []models.ChannelStream
	for rows.Next() {
		var cs models.ChannelStream
		if err := rows.Scan(&cs.ID, &cs.ChannelID, &cs.StreamID, &cs.Priority); err != nil {
			return nil, fmt.Errorf("scanning channel stream: %w", err)
		}
		channelStreams = append(channelStreams, cs)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating channel streams: %w", err)
	}
	return channelStreams, nil
}

func (r *ChannelRepository) IncrementFailCount(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE channels SET fail_count = fail_count + 1 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("incrementing channel fail count: %w", err)
	}
	return nil
}

func (r *ChannelRepository) ResetFailCount(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE channels SET fail_count = 0 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("resetting channel fail count: %w", err)
	}
	return nil
}

func (r *ChannelRepository) RemoveStreamMappings(ctx context.Context, streamIDs []string) error {
	if len(streamIDs) == 0 {
		return nil
	}
	return r.db.InTx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `DELETE FROM channel_streams WHERE stream_id = ?`)
		if err != nil {
			return fmt.Errorf("preparing statement: %w", err)
		}
		defer stmt.Close()
		for _, id := range streamIDs {
			if _, err := stmt.ExecContext(ctx, id); err != nil {
				return fmt.Errorf("deleting channel_stream mapping for stream %s: %w", id, err)
			}
		}
		return nil
	})
}

func (r *ChannelRepository) scanRows(rows *sql.Rows) ([]models.Channel, error) {
	var channels []models.Channel
	for rows.Next() {
		ch, err := scanChannel(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning channel: %w", err)
		}
		channels = append(channels, *ch)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating channels: %w", err)
	}
	return channels, nil
}
