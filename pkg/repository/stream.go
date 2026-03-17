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

type StreamRepository struct {
	db *database.DB
}

func NewStreamRepository(db *database.DB) *StreamRepository {
	return &StreamRepository{db: db}
}

func (r *StreamRepository) Checkpoint(ctx context.Context) {
	r.db.Checkpoint(ctx)
}

func (r *StreamRepository) Create(ctx context.Context, stream *models.Stream) error {
	now := time.Now()
	stream.ID = uuid.New().String()
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO streams (id, m3u_account_id, name, url, "group", logo, tvg_id, tvg_name, content_hash, is_active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		stream.ID, stream.M3UAccountID, stream.Name, stream.URL, stream.Group, stream.Logo,
		stream.TvgID, stream.TvgName, stream.ContentHash, stream.IsActive, now, now,
	)
	if err != nil {
		return fmt.Errorf("creating stream: %w", err)
	}
	stream.CreatedAt = now
	stream.UpdatedAt = now
	return nil
}

func (r *StreamRepository) GetByID(ctx context.Context, id string) (*models.Stream, error) {
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
		FROM streams ORDER BY created_at`,
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

func (r *StreamRepository) ListByAccountID(ctx context.Context, accountID string) ([]models.Stream, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, m3u_account_id, name, url, "group", logo, tvg_id, tvg_name, content_hash, is_active, created_at, updated_at
		FROM streams WHERE m3u_account_id = ? ORDER BY created_at`, accountID,
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

// StreamSummary is a lightweight struct for list views.
type StreamSummary struct {
	ID           string `json:"id"`
	M3UAccountID string `json:"m3u_account_id"`
	Name         string `json:"name"`
	Group        string `json:"group"`
}

// ListSummaries returns only id, name, group, and account_id for all streams.
func (r *StreamRepository) ListSummaries(ctx context.Context) ([]StreamSummary, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, m3u_account_id, name, "group" FROM streams ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing stream summaries: %w", err)
	}
	defer rows.Close()

	var streams []StreamSummary
	for rows.Next() {
		var s StreamSummary
		if err := rows.Scan(&s.ID, &s.M3UAccountID, &s.Name, &s.Group); err != nil {
			return nil, fmt.Errorf("scanning stream summary: %w", err)
		}
		streams = append(streams, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating stream summaries: %w", err)
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

func (r *StreamRepository) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM streams WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting stream: %w", err)
	}
	return nil
}

func (r *StreamRepository) DeleteByAccountID(ctx context.Context, accountID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM streams WHERE m3u_account_id = ?`, accountID)
	if err != nil {
		return fmt.Errorf("deleting streams by account id: %w", err)
	}
	return nil
}

func (r *StreamRepository) BulkUpsert(ctx context.Context, streams []models.Stream) error {
	const batchSize = 5000
	for start := 0; start < len(streams); start += batchSize {
		end := start + batchSize
		if end > len(streams) {
			end = len(streams)
		}
		batch := streams[start:end]
		if err := r.db.InTx(ctx, func(tx *sql.Tx) error {
			stmt, err := tx.PrepareContext(ctx,
				`INSERT INTO streams (id, m3u_account_id, name, url, "group", logo, tvg_id, tvg_name, content_hash, is_active, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(id) DO UPDATE SET
					name = excluded.name,
					url = excluded.url,
					"group" = excluded."group",
					logo = excluded.logo,
					tvg_id = excluded.tvg_id,
					tvg_name = excluded.tvg_name,
					content_hash = excluded.content_hash,
					is_active = excluded.is_active,
					updated_at = excluded.updated_at`,
			)
			if err != nil {
				return fmt.Errorf("preparing statement: %w", err)
			}
			defer stmt.Close()

			now := time.Now()
			for i := range batch {
				if _, err := stmt.ExecContext(ctx,
					batch[i].ID, batch[i].M3UAccountID, batch[i].Name, batch[i].URL, batch[i].Group,
					batch[i].Logo, batch[i].TvgID, batch[i].TvgName, batch[i].ContentHash,
					batch[i].IsActive, now, now,
				); err != nil {
					return fmt.Errorf("upserting stream %d: %w", start+i, err)
				}
				batch[i].CreatedAt = now
				batch[i].UpdatedAt = now
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func (r *StreamRepository) DeleteStaleByAccountID(ctx context.Context, accountID string, keepIDs []string) error {
	if len(keepIDs) == 0 {
		return r.DeleteByAccountID(ctx, accountID)
	}
	return r.db.InTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE IF NOT EXISTS keep_ids (id TEXT PRIMARY KEY)`); err != nil {
			return fmt.Errorf("creating temp table: %w", err)
		}
		defer tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp.keep_ids`)

		stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO temp.keep_ids (id) VALUES (?)`)
		if err != nil {
			return fmt.Errorf("preparing insert: %w", err)
		}
		defer stmt.Close()

		for _, id := range keepIDs {
			if _, err := stmt.ExecContext(ctx, id); err != nil {
				return fmt.Errorf("inserting keep id: %w", err)
			}
		}

		if _, err := tx.ExecContext(ctx,
			`DELETE FROM streams WHERE m3u_account_id = ? AND id NOT IN (SELECT id FROM temp.keep_ids)`,
			accountID,
		); err != nil {
			return fmt.Errorf("deleting stale streams: %w", err)
		}
		return nil
	})
}
