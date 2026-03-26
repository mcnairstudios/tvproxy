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

const streamProfileColumns = `id, name, stream_mode, source_type, hwaccel, video_codec, container, deinterlace, fps_mode, use_custom_args, custom_args, command, args, is_default, is_system, is_client, created_at, updated_at`

type StreamProfileRepository struct {
	db *database.DB
}

func NewStreamProfileRepository(db *database.DB) *StreamProfileRepository {
	return &StreamProfileRepository{db: db}
}

type streamProfileScanner interface {
	Scan(dest ...any) error
}

func scanStreamProfile(row streamProfileScanner, p *models.StreamProfile) error {
	return row.Scan(&p.ID, &p.Name, &p.StreamMode, &p.SourceType, &p.HWAccel, &p.VideoCodec, &p.Container, &p.Deinterlace, &p.FPSMode, &p.UseCustomArgs, &p.CustomArgs, &p.Command, &p.Args, &p.IsDefault, &p.IsSystem, &p.IsClient, &p.CreatedAt, &p.UpdatedAt)
}

func (r *StreamProfileRepository) Create(ctx context.Context, profile *models.StreamProfile) error {
	now := time.Now()
	profile.ID = uuid.New().String()
	if profile.FPSMode == "" {
		profile.FPSMode = "auto"
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO stream_profiles (`+streamProfileColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		profile.ID, profile.Name, profile.StreamMode, profile.SourceType, profile.HWAccel, profile.VideoCodec, profile.Container, profile.Deinterlace, profile.FPSMode, profile.UseCustomArgs, profile.CustomArgs, profile.Command, profile.Args, profile.IsDefault, profile.IsSystem, profile.IsClient, now, now,
	)
	if err != nil {
		return fmt.Errorf("creating stream profile: %w", err)
	}
	profile.CreatedAt = now
	profile.UpdatedAt = now
	return nil
}

func (r *StreamProfileRepository) GetByID(ctx context.Context, id string) (*models.StreamProfile, error) {
	profile := &models.StreamProfile{}
	err := scanStreamProfile(r.db.QueryRowContext(ctx,
		`SELECT `+streamProfileColumns+` FROM stream_profiles WHERE id = ?`, id,
	), profile)
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
		`SELECT `+streamProfileColumns+` FROM stream_profiles ORDER BY CASE WHEN is_system=1 THEN 0 WHEN is_client=1 THEN 1 ELSE 2 END, name`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing stream profiles: %w", err)
	}
	defer rows.Close()

	var profiles []models.StreamProfile
	for rows.Next() {
		var p models.StreamProfile
		if err := scanStreamProfile(rows, &p); err != nil {
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
	if profile.FPSMode == "" {
		profile.FPSMode = "auto"
	}
	_, err := r.db.ExecContext(ctx,
		`UPDATE stream_profiles SET name = ?, stream_mode = ?, source_type = ?, hwaccel = ?, video_codec = ?, container = ?, deinterlace = ?, fps_mode = ?, use_custom_args = ?, custom_args = ?, command = ?, args = ?, is_default = ?, is_system = ?, is_client = ?, updated_at = ?
		WHERE id = ?`,
		profile.Name, profile.StreamMode, profile.SourceType, profile.HWAccel, profile.VideoCodec, profile.Container, profile.Deinterlace, profile.FPSMode, profile.UseCustomArgs, profile.CustomArgs, profile.Command, profile.Args, profile.IsDefault, profile.IsSystem, profile.IsClient, now, profile.ID,
	)
	if err != nil {
		return fmt.Errorf("updating stream profile: %w", err)
	}
	profile.UpdatedAt = now
	return nil
}

func (r *StreamProfileRepository) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM stream_profiles WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting stream profile: %w", err)
	}
	return nil
}

func (r *StreamProfileRepository) GetByName(ctx context.Context, name string) (*models.StreamProfile, error) {
	profile := &models.StreamProfile{}
	err := scanStreamProfile(r.db.QueryRowContext(ctx,
		`SELECT `+streamProfileColumns+` FROM stream_profiles WHERE name = ?`, name,
	), profile)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("stream profile not found: %w", err)
		}
		return nil, fmt.Errorf("getting stream profile by name: %w", err)
	}
	return profile, nil
}

func (r *StreamProfileRepository) GetDefault(ctx context.Context) (*models.StreamProfile, error) {
	profile := &models.StreamProfile{}
	err := scanStreamProfile(r.db.QueryRowContext(ctx,
		`SELECT `+streamProfileColumns+` FROM stream_profiles WHERE is_default = 1 LIMIT 1`,
	), profile)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("default stream profile not found: %w", err)
		}
		return nil, fmt.Errorf("getting default stream profile: %w", err)
	}
	return profile, nil
}
