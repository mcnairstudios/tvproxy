package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/gavinmcnair/tvproxy/pkg/database"
	"github.com/gavinmcnair/tvproxy/pkg/models"
)

type ChannelProfileRepository struct {
	db *database.DB
}

func NewChannelProfileRepository(db *database.DB) *ChannelProfileRepository {
	return &ChannelProfileRepository{db: db}
}

func (r *ChannelProfileRepository) Create(ctx context.Context, profile *models.ChannelProfile) error {
	now := time.Now()
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO channel_profiles (name, stream_profile, sort_order, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)`,
		profile.Name, profile.StreamProfile, profile.SortOrder, now, now,
	)
	if err != nil {
		return fmt.Errorf("creating channel profile: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("getting last insert id: %w", err)
	}
	profile.ID = id
	profile.CreatedAt = now
	profile.UpdatedAt = now
	return nil
}

func (r *ChannelProfileRepository) GetByID(ctx context.Context, id int64) (*models.ChannelProfile, error) {
	profile := &models.ChannelProfile{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, stream_profile, sort_order, created_at, updated_at
		FROM channel_profiles WHERE id = ?`, id,
	).Scan(&profile.ID, &profile.Name, &profile.StreamProfile, &profile.SortOrder, &profile.CreatedAt, &profile.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("channel profile not found: %w", err)
		}
		return nil, fmt.Errorf("getting channel profile by id: %w", err)
	}
	return profile, nil
}

func (r *ChannelProfileRepository) List(ctx context.Context) ([]models.ChannelProfile, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, stream_profile, sort_order, created_at, updated_at
		FROM channel_profiles ORDER BY sort_order, name`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing channel profiles: %w", err)
	}
	defer rows.Close()

	var profiles []models.ChannelProfile
	for rows.Next() {
		var p models.ChannelProfile
		if err := rows.Scan(&p.ID, &p.Name, &p.StreamProfile, &p.SortOrder, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning channel profile: %w", err)
		}
		profiles = append(profiles, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating channel profiles: %w", err)
	}
	return profiles, nil
}

func (r *ChannelProfileRepository) Update(ctx context.Context, profile *models.ChannelProfile) error {
	now := time.Now()
	_, err := r.db.ExecContext(ctx,
		`UPDATE channel_profiles SET name = ?, stream_profile = ?, sort_order = ?, updated_at = ?
		WHERE id = ?`,
		profile.Name, profile.StreamProfile, profile.SortOrder, now, profile.ID,
	)
	if err != nil {
		return fmt.Errorf("updating channel profile: %w", err)
	}
	profile.UpdatedAt = now
	return nil
}

func (r *ChannelProfileRepository) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM channel_profiles WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting channel profile: %w", err)
	}
	return nil
}
