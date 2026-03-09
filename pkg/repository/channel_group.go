package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/gavinmcnair/tvproxy/pkg/database"
	"github.com/gavinmcnair/tvproxy/pkg/models"
)

type ChannelGroupRepository struct {
	db *database.DB
}

func NewChannelGroupRepository(db *database.DB) *ChannelGroupRepository {
	return &ChannelGroupRepository{db: db}
}

func (r *ChannelGroupRepository) Create(ctx context.Context, group *models.ChannelGroup) error {
	now := time.Now()
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO channel_groups (name, is_enabled, sort_order, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)`,
		group.Name, group.IsEnabled, group.SortOrder, now, now,
	)
	if err != nil {
		return fmt.Errorf("creating channel group: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("getting last insert id: %w", err)
	}
	group.ID = id
	group.CreatedAt = now
	group.UpdatedAt = now
	return nil
}

func (r *ChannelGroupRepository) GetByID(ctx context.Context, id int64) (*models.ChannelGroup, error) {
	group := &models.ChannelGroup{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, is_enabled, sort_order, created_at, updated_at
		FROM channel_groups WHERE id = ?`, id,
	).Scan(&group.ID, &group.Name, &group.IsEnabled, &group.SortOrder, &group.CreatedAt, &group.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("channel group not found: %w", err)
		}
		return nil, fmt.Errorf("getting channel group by id: %w", err)
	}
	return group, nil
}

func (r *ChannelGroupRepository) GetByName(ctx context.Context, name string) (*models.ChannelGroup, error) {
	group := &models.ChannelGroup{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, is_enabled, sort_order, created_at, updated_at
		FROM channel_groups WHERE name = ?`, name,
	).Scan(&group.ID, &group.Name, &group.IsEnabled, &group.SortOrder, &group.CreatedAt, &group.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("channel group not found: %w", err)
		}
		return nil, fmt.Errorf("getting channel group by name: %w", err)
	}
	return group, nil
}

func (r *ChannelGroupRepository) List(ctx context.Context) ([]models.ChannelGroup, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, is_enabled, sort_order, created_at, updated_at
		FROM channel_groups ORDER BY sort_order, name`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing channel groups: %w", err)
	}
	defer rows.Close()

	var groups []models.ChannelGroup
	for rows.Next() {
		var g models.ChannelGroup
		if err := rows.Scan(&g.ID, &g.Name, &g.IsEnabled, &g.SortOrder, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning channel group: %w", err)
		}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating channel groups: %w", err)
	}
	return groups, nil
}

func (r *ChannelGroupRepository) Update(ctx context.Context, group *models.ChannelGroup) error {
	now := time.Now()
	_, err := r.db.ExecContext(ctx,
		`UPDATE channel_groups SET name = ?, is_enabled = ?, sort_order = ?, updated_at = ?
		WHERE id = ?`,
		group.Name, group.IsEnabled, group.SortOrder, now, group.ID,
	)
	if err != nil {
		return fmt.Errorf("updating channel group: %w", err)
	}
	group.UpdatedAt = now
	return nil
}

func (r *ChannelGroupRepository) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM channel_groups WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting channel group: %w", err)
	}
	return nil
}
