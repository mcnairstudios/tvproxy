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

const m3uAccountColumns = `id, name, url, type, username, password, max_streams, is_enabled, last_refreshed, stream_count, refresh_interval, last_error, created_at, updated_at`

type m3uAccountScanner interface {
	Scan(dest ...any) error
}

func scanM3UAccount(s m3uAccountScanner) (*models.M3UAccount, error) {
	a := &models.M3UAccount{}
	var lastRefreshed sql.NullTime
	if err := s.Scan(
		&a.ID, &a.Name, &a.URL, &a.Type, &a.Username, &a.Password,
		&a.MaxStreams, &a.IsEnabled, &lastRefreshed, &a.StreamCount,
		&a.RefreshInterval, &a.LastError, &a.CreatedAt, &a.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if lastRefreshed.Valid {
		a.LastRefreshed = &lastRefreshed.Time
	}
	return a, nil
}

type M3UAccountRepository struct {
	db *database.DB
}

func NewM3UAccountRepository(db *database.DB) *M3UAccountRepository {
	return &M3UAccountRepository{db: db}
}

func (r *M3UAccountRepository) Create(ctx context.Context, account *models.M3UAccount) error {
	now := time.Now()
	account.ID = uuid.New().String()
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO m3u_accounts (`+m3uAccountColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		account.ID, account.Name, account.URL, account.Type, account.Username, account.Password,
		account.MaxStreams, account.IsEnabled, account.LastRefreshed, account.StreamCount,
		account.RefreshInterval, account.LastError, now, now,
	)
	if err != nil {
		return fmt.Errorf("creating m3u account: %w", err)
	}
	account.CreatedAt = now
	account.UpdatedAt = now
	return nil
}

func (r *M3UAccountRepository) GetByID(ctx context.Context, id string) (*models.M3UAccount, error) {
	a, err := scanM3UAccount(r.db.QueryRowContext(ctx,
		`SELECT `+m3uAccountColumns+` FROM m3u_accounts WHERE id = ?`, id,
	))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("m3u account not found: %w", err)
		}
		return nil, fmt.Errorf("getting m3u account by id: %w", err)
	}
	return a, nil
}

func (r *M3UAccountRepository) List(ctx context.Context) ([]models.M3UAccount, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+m3uAccountColumns+` FROM m3u_accounts ORDER BY created_at`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing m3u accounts: %w", err)
	}
	defer rows.Close()

	var accounts []models.M3UAccount
	for rows.Next() {
		a, err := scanM3UAccount(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning m3u account: %w", err)
		}
		accounts = append(accounts, *a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating m3u accounts: %w", err)
	}
	return accounts, nil
}

func (r *M3UAccountRepository) Update(ctx context.Context, account *models.M3UAccount) error {
	now := time.Now()
	_, err := r.db.ExecContext(ctx,
		`UPDATE m3u_accounts SET name = ?, url = ?, type = ?, username = ?, password = ?, max_streams = ?, is_enabled = ?, refresh_interval = ?, updated_at = ?
		WHERE id = ?`,
		account.Name, account.URL, account.Type, account.Username, account.Password,
		account.MaxStreams, account.IsEnabled, account.RefreshInterval, now, account.ID,
	)
	if err != nil {
		return fmt.Errorf("updating m3u account: %w", err)
	}
	account.UpdatedAt = now
	return nil
}

func (r *M3UAccountRepository) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM m3u_accounts WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting m3u account: %w", err)
	}
	return nil
}

func (r *M3UAccountRepository) UpdateLastRefreshed(ctx context.Context, id string, lastRefreshed time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE m3u_accounts SET last_refreshed = ?, updated_at = ? WHERE id = ?`,
		lastRefreshed, time.Now(), id,
	)
	if err != nil {
		return fmt.Errorf("updating last refreshed: %w", err)
	}
	return nil
}

func (r *M3UAccountRepository) UpdateLastError(ctx context.Context, id, lastError string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE m3u_accounts SET last_error = ?, updated_at = ? WHERE id = ?`,
		lastError, time.Now(), id,
	)
	if err != nil {
		return fmt.Errorf("updating last error: %w", err)
	}
	return nil
}

func (r *M3UAccountRepository) UpdateStreamCount(ctx context.Context, id string, count int) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE m3u_accounts SET stream_count = ?, updated_at = ? WHERE id = ?`,
		count, time.Now(), id,
	)
	if err != nil {
		return fmt.Errorf("updating stream count: %w", err)
	}
	return nil
}
