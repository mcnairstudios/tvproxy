package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/gavinmcnair/tvproxy/pkg/database"
	"github.com/gavinmcnair/tvproxy/pkg/models"
)

type LogoRepository struct {
	db *database.DB
}

func NewLogoRepository(db *database.DB) *LogoRepository {
	return &LogoRepository{db: db}
}

func (r *LogoRepository) Create(ctx context.Context, logo *models.Logo) error {
	now := time.Now()
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO logos (name, url, created_at) VALUES (?, ?, ?)`,
		logo.Name, logo.URL, now,
	)
	if err != nil {
		return fmt.Errorf("creating logo: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("getting last insert id: %w", err)
	}
	logo.ID = id
	logo.CreatedAt = now
	return nil
}

func (r *LogoRepository) GetByID(ctx context.Context, id int64) (*models.Logo, error) {
	logo := &models.Logo{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, url, created_at FROM logos WHERE id = ?`, id,
	).Scan(&logo.ID, &logo.Name, &logo.URL, &logo.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("logo not found: %w", err)
		}
		return nil, fmt.Errorf("getting logo by id: %w", err)
	}
	return logo, nil
}

func (r *LogoRepository) List(ctx context.Context) ([]models.Logo, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, url, created_at FROM logos ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing logos: %w", err)
	}
	defer rows.Close()

	var logos []models.Logo
	for rows.Next() {
		var l models.Logo
		if err := rows.Scan(&l.ID, &l.Name, &l.URL, &l.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning logo: %w", err)
		}
		logos = append(logos, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating logos: %w", err)
	}
	return logos, nil
}

func (r *LogoRepository) Update(ctx context.Context, logo *models.Logo) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE logos SET name = ?, url = ? WHERE id = ?`,
		logo.Name, logo.URL, logo.ID,
	)
	if err != nil {
		return fmt.Errorf("updating logo: %w", err)
	}
	return nil
}

func (r *LogoRepository) GetByURL(ctx context.Context, url string) (*models.Logo, error) {
	logo := &models.Logo{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, url, created_at FROM logos WHERE url = ?`, url,
	).Scan(&logo.ID, &logo.Name, &logo.URL, &logo.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("getting logo by url: %w", err)
	}
	return logo, nil
}

func (r *LogoRepository) Delete(ctx context.Context, id int64) error {
	// Clear channel references first (SQLite ALTER TABLE FK constraints aren't enforced)
	if _, err := r.db.ExecContext(ctx, `UPDATE channels SET logo_id = NULL WHERE logo_id = ?`, id); err != nil {
		return fmt.Errorf("clearing channel logo references: %w", err)
	}
	_, err := r.db.ExecContext(ctx, `DELETE FROM logos WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting logo: %w", err)
	}
	return nil
}
