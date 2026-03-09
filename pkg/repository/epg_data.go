package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/gavinmcnair/tvproxy/pkg/database"
	"github.com/gavinmcnair/tvproxy/pkg/models"
)

type EPGDataRepository struct {
	db *database.DB
}

func NewEPGDataRepository(db *database.DB) *EPGDataRepository {
	return &EPGDataRepository{db: db}
}

func (r *EPGDataRepository) Create(ctx context.Context, data *models.EPGData) error {
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO epg_data (epg_source_id, channel_id, name, icon)
		VALUES (?, ?, ?, ?)`,
		data.EPGSourceID, data.ChannelID, data.Name, data.Icon,
	)
	if err != nil {
		return fmt.Errorf("creating epg data: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("getting last insert id: %w", err)
	}
	data.ID = id
	return nil
}

func (r *EPGDataRepository) GetByID(ctx context.Context, id int64) (*models.EPGData, error) {
	data := &models.EPGData{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, epg_source_id, channel_id, name, icon
		FROM epg_data WHERE id = ?`, id,
	).Scan(&data.ID, &data.EPGSourceID, &data.ChannelID, &data.Name, &data.Icon)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("epg data not found: %w", err)
		}
		return nil, fmt.Errorf("getting epg data by id: %w", err)
	}
	return data, nil
}

func (r *EPGDataRepository) List(ctx context.Context) ([]models.EPGData, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, epg_source_id, channel_id, name, icon
		FROM epg_data ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing epg data: %w", err)
	}
	defer rows.Close()

	var items []models.EPGData
	for rows.Next() {
		var d models.EPGData
		if err := rows.Scan(&d.ID, &d.EPGSourceID, &d.ChannelID, &d.Name, &d.Icon); err != nil {
			return nil, fmt.Errorf("scanning epg data: %w", err)
		}
		items = append(items, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating epg data: %w", err)
	}
	return items, nil
}

func (r *EPGDataRepository) ListBySourceID(ctx context.Context, sourceID int64) ([]models.EPGData, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, epg_source_id, channel_id, name, icon
		FROM epg_data WHERE epg_source_id = ? ORDER BY id`, sourceID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing epg data by source id: %w", err)
	}
	defer rows.Close()

	var items []models.EPGData
	for rows.Next() {
		var d models.EPGData
		if err := rows.Scan(&d.ID, &d.EPGSourceID, &d.ChannelID, &d.Name, &d.Icon); err != nil {
			return nil, fmt.Errorf("scanning epg data: %w", err)
		}
		items = append(items, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating epg data: %w", err)
	}
	return items, nil
}

func (r *EPGDataRepository) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM epg_data WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting epg data: %w", err)
	}
	return nil
}

func (r *EPGDataRepository) DeleteBySourceID(ctx context.Context, sourceID int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM epg_data WHERE epg_source_id = ?`, sourceID)
	if err != nil {
		return fmt.Errorf("deleting epg data by source id: %w", err)
	}
	return nil
}

func (r *EPGDataRepository) BulkCreate(ctx context.Context, items []models.EPGData) error {
	return r.db.InTx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx,
			`INSERT INTO epg_data (epg_source_id, channel_id, name, icon)
			VALUES (?, ?, ?, ?)`,
		)
		if err != nil {
			return fmt.Errorf("preparing statement: %w", err)
		}
		defer stmt.Close()

		for i := range items {
			result, err := stmt.ExecContext(ctx,
				items[i].EPGSourceID, items[i].ChannelID, items[i].Name, items[i].Icon,
			)
			if err != nil {
				return fmt.Errorf("inserting epg data %d: %w", i, err)
			}
			id, err := result.LastInsertId()
			if err != nil {
				return fmt.Errorf("getting last insert id for epg data %d: %w", i, err)
			}
			items[i].ID = id
		}
		return nil
	})
}
