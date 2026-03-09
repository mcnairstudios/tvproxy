package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/gavinmcnair/tvproxy/pkg/database"
	"github.com/gavinmcnair/tvproxy/pkg/models"
)

type M3UAccountRepository struct {
	db *database.DB
}

func NewM3UAccountRepository(db *database.DB) *M3UAccountRepository {
	return &M3UAccountRepository{db: db}
}

func (r *M3UAccountRepository) Create(ctx context.Context, account *models.M3UAccount) error {
	now := time.Now()
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO m3u_accounts (name, url, type, username, password, max_streams, is_enabled, last_refreshed, stream_count, refresh_interval, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		account.Name, account.URL, account.Type, account.Username, account.Password,
		account.MaxStreams, account.IsEnabled, account.LastRefreshed, account.StreamCount,
		account.RefreshInterval, now, now,
	)
	if err != nil {
		return fmt.Errorf("creating m3u account: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("getting last insert id: %w", err)
	}
	account.ID = id
	account.CreatedAt = now
	account.UpdatedAt = now
	return nil
}

func (r *M3UAccountRepository) GetByID(ctx context.Context, id int64) (*models.M3UAccount, error) {
	account := &models.M3UAccount{}
	var lastRefreshed sql.NullTime
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, url, type, username, password, max_streams, is_enabled, last_refreshed, stream_count, refresh_interval, created_at, updated_at
		FROM m3u_accounts WHERE id = ?`, id,
	).Scan(
		&account.ID, &account.Name, &account.URL, &account.Type,
		&account.Username, &account.Password, &account.MaxStreams,
		&account.IsEnabled, &lastRefreshed, &account.StreamCount,
		&account.RefreshInterval, &account.CreatedAt, &account.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("m3u account not found: %w", err)
		}
		return nil, fmt.Errorf("getting m3u account by id: %w", err)
	}
	if lastRefreshed.Valid {
		account.LastRefreshed = &lastRefreshed.Time
	}
	return account, nil
}

func (r *M3UAccountRepository) List(ctx context.Context) ([]models.M3UAccount, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, url, type, username, password, max_streams, is_enabled, last_refreshed, stream_count, refresh_interval, created_at, updated_at
		FROM m3u_accounts ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing m3u accounts: %w", err)
	}
	defer rows.Close()

	var accounts []models.M3UAccount
	for rows.Next() {
		var a models.M3UAccount
		var lastRefreshed sql.NullTime
		if err := rows.Scan(
			&a.ID, &a.Name, &a.URL, &a.Type, &a.Username, &a.Password,
			&a.MaxStreams, &a.IsEnabled, &lastRefreshed, &a.StreamCount,
			&a.RefreshInterval, &a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning m3u account: %w", err)
		}
		if lastRefreshed.Valid {
			a.LastRefreshed = &lastRefreshed.Time
		}
		accounts = append(accounts, a)
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

func (r *M3UAccountRepository) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM m3u_accounts WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting m3u account: %w", err)
	}
	return nil
}

func (r *M3UAccountRepository) UpdateLastRefreshed(ctx context.Context, id int64, lastRefreshed time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE m3u_accounts SET last_refreshed = ?, updated_at = ? WHERE id = ?`,
		lastRefreshed, time.Now(), id,
	)
	if err != nil {
		return fmt.Errorf("updating last refreshed: %w", err)
	}
	return nil
}

func (r *M3UAccountRepository) UpdateStreamCount(ctx context.Context, id int64, count int) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE m3u_accounts SET stream_count = ?, updated_at = ? WHERE id = ?`,
		count, time.Now(), id,
	)
	if err != nil {
		return fmt.Errorf("updating stream count: %w", err)
	}
	return nil
}
