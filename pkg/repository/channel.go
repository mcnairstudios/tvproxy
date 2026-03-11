package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/gavinmcnair/tvproxy/pkg/database"
	"github.com/gavinmcnair/tvproxy/pkg/models"
)

type ChannelRepository struct {
	db *database.DB
}

func NewChannelRepository(db *database.DB) *ChannelRepository {
	return &ChannelRepository{db: db}
}

func (r *ChannelRepository) Create(ctx context.Context, channel *models.Channel) error {
	now := time.Now()
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO channels (channel_number, name, logo_id, tvg_id, channel_group_id, channel_profile_id, is_enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		channel.ChannelNumber, channel.Name, channel.LogoID, channel.TvgID,
		channel.ChannelGroupID, channel.ChannelProfileID, channel.IsEnabled, now, now,
	)
	if err != nil {
		return fmt.Errorf("creating channel: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("getting last insert id: %w", err)
	}
	channel.ID = id
	channel.CreatedAt = now
	channel.UpdatedAt = now
	return nil
}

func (r *ChannelRepository) GetByID(ctx context.Context, id int64) (*models.Channel, error) {
	channel := &models.Channel{}
	var logoID sql.NullInt64
	var groupID sql.NullInt64
	var profileID sql.NullInt64
	err := r.db.QueryRowContext(ctx,
		`SELECT c.id, c.channel_number, c.name, c.logo_id, COALESCE(l.url, ''), c.tvg_id, c.channel_group_id, c.channel_profile_id, c.is_enabled, c.created_at, c.updated_at
		FROM channels c LEFT JOIN logos l ON c.logo_id = l.id WHERE c.id = ?`, id,
	).Scan(
		&channel.ID, &channel.ChannelNumber, &channel.Name, &logoID, &channel.Logo,
		&channel.TvgID, &groupID, &profileID,
		&channel.IsEnabled, &channel.CreatedAt, &channel.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("channel not found: %w", err)
		}
		return nil, fmt.Errorf("getting channel by id: %w", err)
	}
	if logoID.Valid {
		channel.LogoID = &logoID.Int64
	}
	if groupID.Valid {
		channel.ChannelGroupID = &groupID.Int64
	}
	if profileID.Valid {
		channel.ChannelProfileID = &profileID.Int64
	}
	return channel, nil
}

func (r *ChannelRepository) GetByNumber(ctx context.Context, number int) (*models.Channel, error) {
	channel := &models.Channel{}
	var logoID sql.NullInt64
	var groupID sql.NullInt64
	var profileID sql.NullInt64
	err := r.db.QueryRowContext(ctx,
		`SELECT c.id, c.channel_number, c.name, c.logo_id, COALESCE(l.url, ''), c.tvg_id, c.channel_group_id, c.channel_profile_id, c.is_enabled, c.created_at, c.updated_at
		FROM channels c LEFT JOIN logos l ON c.logo_id = l.id WHERE c.channel_number = ?`, number,
	).Scan(
		&channel.ID, &channel.ChannelNumber, &channel.Name, &logoID, &channel.Logo,
		&channel.TvgID, &groupID, &profileID,
		&channel.IsEnabled, &channel.CreatedAt, &channel.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("channel not found: %w", err)
		}
		return nil, fmt.Errorf("getting channel by number: %w", err)
	}
	if logoID.Valid {
		channel.LogoID = &logoID.Int64
	}
	if groupID.Valid {
		channel.ChannelGroupID = &groupID.Int64
	}
	if profileID.Valid {
		channel.ChannelProfileID = &profileID.Int64
	}
	return channel, nil
}

func (r *ChannelRepository) List(ctx context.Context) ([]models.Channel, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT c.id, c.channel_number, c.name, c.logo_id, COALESCE(l.url, ''), c.tvg_id, c.channel_group_id, c.channel_profile_id, c.is_enabled, c.created_at, c.updated_at
		FROM channels c LEFT JOIN logos l ON c.logo_id = l.id ORDER BY c.channel_number`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing channels: %w", err)
	}
	defer rows.Close()

	var channels []models.Channel
	for rows.Next() {
		var c models.Channel
		var logoID sql.NullInt64
		var groupID sql.NullInt64
		var profileID sql.NullInt64
		if err := rows.Scan(
			&c.ID, &c.ChannelNumber, &c.Name, &logoID, &c.Logo, &c.TvgID,
			&groupID, &profileID,
			&c.IsEnabled, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning channel: %w", err)
		}
		if logoID.Valid {
			c.LogoID = &logoID.Int64
		}
		if groupID.Valid {
			c.ChannelGroupID = &groupID.Int64
		}
		if profileID.Valid {
			c.ChannelProfileID = &profileID.Int64
		}
		channels = append(channels, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating channels: %w", err)
	}
	return channels, nil
}

func (r *ChannelRepository) Update(ctx context.Context, channel *models.Channel) error {
	now := time.Now()
	_, err := r.db.ExecContext(ctx,
		`UPDATE channels SET channel_number = ?, name = ?, logo_id = ?, tvg_id = ?, channel_group_id = ?, channel_profile_id = ?, is_enabled = ?, updated_at = ?
		WHERE id = ?`,
		channel.ChannelNumber, channel.Name, channel.LogoID, channel.TvgID,
		channel.ChannelGroupID, channel.ChannelProfileID, channel.IsEnabled, now, channel.ID,
	)
	if err != nil {
		return fmt.Errorf("updating channel: %w", err)
	}
	channel.UpdatedAt = now
	return nil
}

func (r *ChannelRepository) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM channels WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting channel: %w", err)
	}
	return nil
}

func (r *ChannelRepository) AssignStreams(ctx context.Context, channelID int64, streamIDs []int64, priorities []int) error {
	if len(streamIDs) != len(priorities) {
		return fmt.Errorf("streamIDs and priorities must have the same length")
	}
	return r.db.InTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM channel_streams WHERE channel_id = ?`, channelID); err != nil {
			return fmt.Errorf("deleting existing channel streams: %w", err)
		}
		stmt, err := tx.PrepareContext(ctx,
			`INSERT INTO channel_streams (channel_id, stream_id, priority) VALUES (?, ?, ?)`,
		)
		if err != nil {
			return fmt.Errorf("preparing statement: %w", err)
		}
		defer stmt.Close()

		for i := range streamIDs {
			if _, err := stmt.ExecContext(ctx, channelID, streamIDs[i], priorities[i]); err != nil {
				return fmt.Errorf("inserting channel stream %d: %w", i, err)
			}
		}
		return nil
	})
}

func (r *ChannelRepository) GetStreams(ctx context.Context, channelID int64) ([]models.ChannelStream, error) {
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

func (r *ChannelRepository) GetNextChannelNumber(ctx context.Context) (int, error) {
	var maxNum sql.NullInt64
	err := r.db.QueryRowContext(ctx, `SELECT MAX(channel_number) FROM channels`).Scan(&maxNum)
	if err != nil {
		return 0, fmt.Errorf("getting max channel number: %w", err)
	}
	if !maxNum.Valid {
		return 1, nil
	}
	return int(maxNum.Int64) + 1, nil
}
