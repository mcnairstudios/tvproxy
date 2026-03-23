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

type UserRepository struct {
	db *database.DB
}

func NewUserRepository(db *database.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) Create(ctx context.Context, user *models.User) error {
	now := time.Now()
	user.ID = uuid.New().String()
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO users (id, username, password_hash, is_admin, invite_token, invite_expires_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		user.ID, user.Username, user.PasswordHash, user.IsAdmin, user.InviteToken, user.InviteExpiresAt, now, now,
	)
	if err != nil {
		return fmt.Errorf("creating user: %w", err)
	}
	user.CreatedAt = now
	user.UpdatedAt = now
	return nil
}

func (r *UserRepository) GetByID(ctx context.Context, id string) (*models.User, error) {
	user := &models.User{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, is_admin, invite_token, invite_expires_at, created_at, updated_at
		FROM users WHERE id = ?`, id,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.IsAdmin, &user.InviteToken, &user.InviteExpiresAt, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user not found: %w", err)
		}
		return nil, fmt.Errorf("getting user by id: %w", err)
	}
	return user, nil
}

func (r *UserRepository) GetByUsername(ctx context.Context, username string) (*models.User, error) {
	user := &models.User{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, is_admin, invite_token, invite_expires_at, created_at, updated_at
		FROM users WHERE username = ?`, username,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.IsAdmin, &user.InviteToken, &user.InviteExpiresAt, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user not found: %w", err)
		}
		return nil, fmt.Errorf("getting user by username: %w", err)
	}
	return user, nil
}

func (r *UserRepository) GetByInviteToken(ctx context.Context, token string) (*models.User, error) {
	user := &models.User{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, is_admin, invite_token, invite_expires_at, created_at, updated_at
		FROM users WHERE invite_token = ?`, token,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.IsAdmin, &user.InviteToken, &user.InviteExpiresAt, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("invite not found: %w", err)
		}
		return nil, fmt.Errorf("getting user by invite token: %w", err)
	}
	return user, nil
}

func (r *UserRepository) List(ctx context.Context) ([]models.User, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, username, password_hash, is_admin, invite_token, invite_expires_at, created_at, updated_at
		FROM users ORDER BY created_at`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing users: %w", err)
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.IsAdmin, &u.InviteToken, &u.InviteExpiresAt, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning user: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating users: %w", err)
	}
	return users, nil
}

func (r *UserRepository) GetFirstAdmin(ctx context.Context) (*models.User, error) {
	user := &models.User{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, is_admin, invite_token, invite_expires_at, created_at, updated_at
		FROM users WHERE is_admin = 1 ORDER BY created_at LIMIT 1`,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.IsAdmin, &user.InviteToken, &user.InviteExpiresAt, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("no admin user found: %w", err)
		}
		return nil, fmt.Errorf("getting first admin: %w", err)
	}
	return user, nil
}

func (r *UserRepository) Update(ctx context.Context, user *models.User) error {
	now := time.Now()
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET username = ?, password_hash = ?, is_admin = ?, invite_token = ?, invite_expires_at = ?, updated_at = ?
		WHERE id = ?`,
		user.Username, user.PasswordHash, user.IsAdmin, user.InviteToken, user.InviteExpiresAt, now, user.ID,
	)
	if err != nil {
		return fmt.Errorf("updating user: %w", err)
	}
	user.UpdatedAt = now
	return nil
}

func (r *UserRepository) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting user: %w", err)
	}
	return nil
}

func (r *UserRepository) GetGroupIDsForUser(ctx context.Context, userID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT channel_group_id FROM user_channel_groups WHERE user_id = ?`, userID)
	if err != nil {
		return nil, fmt.Errorf("getting channel groups for user: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning channel group id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *UserRepository) SetGroupIDsForUser(ctx context.Context, userID string, groupIDs []string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("starting transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM user_channel_groups WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("clearing user channel groups: %w", err)
	}
	for _, gid := range groupIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO user_channel_groups (user_id, channel_group_id) VALUES (?, ?)`,
			userID, gid); err != nil {
			return fmt.Errorf("inserting user channel group: %w", err)
		}
	}
	return tx.Commit()
}
