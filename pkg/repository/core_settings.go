package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/gavinmcnair/tvproxy/pkg/database"
	"github.com/gavinmcnair/tvproxy/pkg/models"
)

type CoreSettingsRepository struct {
	db *database.DB
}

func NewCoreSettingsRepository(db *database.DB) *CoreSettingsRepository {
	return &CoreSettingsRepository{db: db}
}

func (r *CoreSettingsRepository) Get(ctx context.Context, key string) (*models.CoreSetting, error) {
	setting := &models.CoreSetting{}
	err := r.db.QueryRowContext(ctx,
		`SELECT key, value FROM core_settings WHERE key = ?`, key,
	).Scan(&setting.Key, &setting.Value)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("core setting not found: %w", err)
		}
		return nil, fmt.Errorf("getting core setting: %w", err)
	}
	return setting, nil
}

func (r *CoreSettingsRepository) Set(ctx context.Context, key, value string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO core_settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	if err != nil {
		return fmt.Errorf("setting core setting: %w", err)
	}
	return nil
}

func (r *CoreSettingsRepository) List(ctx context.Context) ([]models.CoreSetting, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT key, value FROM core_settings ORDER BY key`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing core settings: %w", err)
	}
	defer rows.Close()

	var settings []models.CoreSetting
	for rows.Next() {
		var s models.CoreSetting
		if err := rows.Scan(&s.Key, &s.Value); err != nil {
			return nil, fmt.Errorf("scanning core setting: %w", err)
		}
		settings = append(settings, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating core settings: %w", err)
	}
	return settings, nil
}

func (r *CoreSettingsRepository) Delete(ctx context.Context, key string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM core_settings WHERE key = ?`, key)
	if err != nil {
		return fmt.Errorf("deleting core setting: %w", err)
	}
	return nil
}
