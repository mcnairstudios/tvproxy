package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/gavinmcnair/tvproxy/pkg/database"
	"github.com/gavinmcnair/tvproxy/pkg/models"
)

type StreamProfileRepository struct {
	db *database.DB
}

func NewStreamProfileRepository(db *database.DB) *StreamProfileRepository {
	return &StreamProfileRepository{db: db}
}

func (r *StreamProfileRepository) Create(ctx context.Context, profile *models.StreamProfile) error {
	now := time.Now()
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO stream_profiles (name, command, args, is_default, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		profile.Name, profile.Command, profile.Args, profile.IsDefault, now, now,
	)
	if err != nil {
		return fmt.Errorf("creating stream profile: %w", err)
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

func (r *StreamProfileRepository) GetByID(ctx context.Context, id int64) (*models.StreamProfile, error) {
	profile := &models.StreamProfile{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, command, args, is_default, created_at, updated_at
		FROM stream_profiles WHERE id = ?`, id,
	).Scan(&profile.ID, &profile.Name, &profile.Command, &profile.Args, &profile.IsDefault, &profile.CreatedAt, &profile.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("stream profile not found: %w", err)
		}
		return nil, fmt.Errorf("getting stream profile by id: %w", err)
	}
	return profile, nil
}

func (r *StreamProfileRepository) List(ctx context.Context) ([]models.StreamProfile, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, command, args, is_default, created_at, updated_at
		FROM stream_profiles ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing stream profiles: %w", err)
	}
	defer rows.Close()

	var profiles []models.StreamProfile
	for rows.Next() {
		var p models.StreamProfile
		if err := rows.Scan(&p.ID, &p.Name, &p.Command, &p.Args, &p.IsDefault, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning stream profile: %w", err)
		}
		profiles = append(profiles, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating stream profiles: %w", err)
	}
	return profiles, nil
}

func (r *StreamProfileRepository) Update(ctx context.Context, profile *models.StreamProfile) error {
	now := time.Now()
	_, err := r.db.ExecContext(ctx,
		`UPDATE stream_profiles SET name = ?, command = ?, args = ?, is_default = ?, updated_at = ?
		WHERE id = ?`,
		profile.Name, profile.Command, profile.Args, profile.IsDefault, now, profile.ID,
	)
	if err != nil {
		return fmt.Errorf("updating stream profile: %w", err)
	}
	profile.UpdatedAt = now
	return nil
}

func (r *StreamProfileRepository) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM stream_profiles WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting stream profile: %w", err)
	}
	return nil
}

func (r *StreamProfileRepository) GetDefault(ctx context.Context) (*models.StreamProfile, error) {
	profile := &models.StreamProfile{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, command, args, is_default, created_at, updated_at
		FROM stream_profiles WHERE is_default = 1 LIMIT 1`,
	).Scan(&profile.ID, &profile.Name, &profile.Command, &profile.Args, &profile.IsDefault, &profile.CreatedAt, &profile.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("default stream profile not found: %w", err)
		}
		return nil, fmt.Errorf("getting default stream profile: %w", err)
	}
	return profile, nil
}
