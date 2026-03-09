package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/gavinmcnair/tvproxy/pkg/database"
	"github.com/gavinmcnair/tvproxy/pkg/models"
)

type StreamRepository struct {
	db *database.DB
}

func NewStreamRepository(db *database.DB) *StreamRepository {
	return &StreamRepository{db: db}
}

func (r *StreamRepository) Create(ctx context.Context, stream *models.Stream) error {
	now := time.Now()
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO streams (m3u_account_id, name, url, "group", logo, tvg_id, tvg_name, content_hash, is_active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		stream.M3UAccountID, stream.Name, stream.URL, stream.Group, stream.Logo,
		stream.TvgID, stream.TvgName, stream.ContentHash, stream.IsActive, now, now,
	)
	if err != nil {
		return fmt.Errorf("creating stream: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("getting last insert id: %w", err)
	}
	stream.ID = id
	stream.CreatedAt = now
	stream.UpdatedAt = now
	return nil
}

func (r *StreamRepository) GetByID(ctx context.Context, id int64) (*models.Stream, error) {
	stream := &models.Stream{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, m3u_account_id, name, url, "group", logo, tvg_id, tvg_name, content_hash, is_active, created_at, updated_at
		FROM streams WHERE id = ?`, id,
	).Scan(
		&stream.ID, &stream.M3UAccountID, &stream.Name, &stream.URL,
		&stream.Group, &stream.Logo, &stream.TvgID, &stream.TvgName,
		&stream.ContentHash, &stream.IsActive, &stream.CreatedAt, &stream.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("stream not found: %w", err)
		}
		return nil, fmt.Errorf("getting stream by id: %w", err)
	}
	return stream, nil
}

func (r *StreamRepository) List(ctx context.Context) ([]models.Stream, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, m3u_account_id, name, url, "group", logo, tvg_id, tvg_name, content_hash, is_active, created_at, updated_at
		FROM streams ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing streams: %w", err)
	}
	defer rows.Close()

	var streams []models.Stream
	for rows.Next() {
		var s models.Stream
		if err := rows.Scan(
			&s.ID, &s.M3UAccountID, &s.Name, &s.URL, &s.Group,
			&s.Logo, &s.TvgID, &s.TvgName, &s.ContentHash,
			&s.IsActive, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning stream: %w", err)
		}
		streams = append(streams, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating streams: %w", err)
	}
	return streams, nil
}

func (r *StreamRepository) ListByAccountID(ctx context.Context, accountID int64) ([]models.Stream, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, m3u_account_id, name, url, "group", logo, tvg_id, tvg_name, content_hash, is_active, created_at, updated_at
		FROM streams WHERE m3u_account_id = ? ORDER BY id`, accountID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing streams by account id: %w", err)
	}
	defer rows.Close()

	var streams []models.Stream
	for rows.Next() {
		var s models.Stream
		if err := rows.Scan(
			&s.ID, &s.M3UAccountID, &s.Name, &s.URL, &s.Group,
			&s.Logo, &s.TvgID, &s.TvgName, &s.ContentHash,
			&s.IsActive, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning stream: %w", err)
		}
		streams = append(streams, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating streams: %w", err)
	}
	return streams, nil
}

func (r *StreamRepository) GetByContentHash(ctx context.Context, hash string) (*models.Stream, error) {
	stream := &models.Stream{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, m3u_account_id, name, url, "group", logo, tvg_id, tvg_name, content_hash, is_active, created_at, updated_at
		FROM streams WHERE content_hash = ?`, hash,
	).Scan(
		&stream.ID, &stream.M3UAccountID, &stream.Name, &stream.URL,
		&stream.Group, &stream.Logo, &stream.TvgID, &stream.TvgName,
		&stream.ContentHash, &stream.IsActive, &stream.CreatedAt, &stream.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("stream not found: %w", err)
		}
		return nil, fmt.Errorf("getting stream by content hash: %w", err)
	}
	return stream, nil
}

func (r *StreamRepository) Update(ctx context.Context, stream *models.Stream) error {
	now := time.Now()
	_, err := r.db.ExecContext(ctx,
		`UPDATE streams SET m3u_account_id = ?, name = ?, url = ?, "group" = ?, logo = ?, tvg_id = ?, tvg_name = ?, content_hash = ?, is_active = ?, updated_at = ?
		WHERE id = ?`,
		stream.M3UAccountID, stream.Name, stream.URL, stream.Group, stream.Logo,
		stream.TvgID, stream.TvgName, stream.ContentHash, stream.IsActive, now, stream.ID,
	)
	if err != nil {
		return fmt.Errorf("updating stream: %w", err)
	}
	stream.UpdatedAt = now
	return nil
}

func (r *StreamRepository) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM streams WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting stream: %w", err)
	}
	return nil
}

func (r *StreamRepository) DeleteByAccountID(ctx context.Context, accountID int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM streams WHERE m3u_account_id = ?`, accountID)
	if err != nil {
		return fmt.Errorf("deleting streams by account id: %w", err)
	}
	return nil
}

func (r *StreamRepository) BulkCreate(ctx context.Context, streams []models.Stream) error {
	return r.db.InTx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx,
			`INSERT INTO streams (m3u_account_id, name, url, "group", logo, tvg_id, tvg_name, content_hash, is_active, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		)
		if err != nil {
			return fmt.Errorf("preparing statement: %w", err)
		}
		defer stmt.Close()

		now := time.Now()
		for i := range streams {
			result, err := stmt.ExecContext(ctx,
				streams[i].M3UAccountID, streams[i].Name, streams[i].URL, streams[i].Group,
				streams[i].Logo, streams[i].TvgID, streams[i].TvgName, streams[i].ContentHash,
				streams[i].IsActive, now, now,
			)
			if err != nil {
				return fmt.Errorf("inserting stream %d: %w", i, err)
			}
			id, err := result.LastInsertId()
			if err != nil {
				return fmt.Errorf("getting last insert id for stream %d: %w", i, err)
			}
			streams[i].ID = id
			streams[i].CreatedAt = now
			streams[i].UpdatedAt = now
		}
		return nil
	})
}
