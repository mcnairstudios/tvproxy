package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/gavinmcnair/tvproxy/pkg/database"
	"github.com/gavinmcnair/tvproxy/pkg/models"
)

type UserAgentRepository struct {
	db *database.DB
}

func NewUserAgentRepository(db *database.DB) *UserAgentRepository {
	return &UserAgentRepository{db: db}
}

func (r *UserAgentRepository) Create(ctx context.Context, ua *models.UserAgent) error {
	now := time.Now()
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO user_agents (name, user_agent, is_default, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)`,
		ua.Name, ua.UserAgent, ua.IsDefault, now, now,
	)
	if err != nil {
		return fmt.Errorf("creating user agent: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("getting last insert id: %w", err)
	}
	ua.ID = id
	ua.CreatedAt = now
	ua.UpdatedAt = now
	return nil
}

func (r *UserAgentRepository) GetByID(ctx context.Context, id int64) (*models.UserAgent, error) {
	ua := &models.UserAgent{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, user_agent, is_default, created_at, updated_at
		FROM user_agents WHERE id = ?`, id,
	).Scan(&ua.ID, &ua.Name, &ua.UserAgent, &ua.IsDefault, &ua.CreatedAt, &ua.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user agent not found: %w", err)
		}
		return nil, fmt.Errorf("getting user agent by id: %w", err)
	}
	return ua, nil
}

func (r *UserAgentRepository) List(ctx context.Context) ([]models.UserAgent, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, user_agent, is_default, created_at, updated_at
		FROM user_agents ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing user agents: %w", err)
	}
	defer rows.Close()

	var agents []models.UserAgent
	for rows.Next() {
		var ua models.UserAgent
		if err := rows.Scan(&ua.ID, &ua.Name, &ua.UserAgent, &ua.IsDefault, &ua.CreatedAt, &ua.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning user agent: %w", err)
		}
		agents = append(agents, ua)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating user agents: %w", err)
	}
	return agents, nil
}

func (r *UserAgentRepository) Update(ctx context.Context, ua *models.UserAgent) error {
	now := time.Now()
	_, err := r.db.ExecContext(ctx,
		`UPDATE user_agents SET name = ?, user_agent = ?, is_default = ?, updated_at = ?
		WHERE id = ?`,
		ua.Name, ua.UserAgent, ua.IsDefault, now, ua.ID,
	)
	if err != nil {
		return fmt.Errorf("updating user agent: %w", err)
	}
	ua.UpdatedAt = now
	return nil
}

func (r *UserAgentRepository) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM user_agents WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting user agent: %w", err)
	}
	return nil
}

func (r *UserAgentRepository) GetDefault(ctx context.Context) (*models.UserAgent, error) {
	ua := &models.UserAgent{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, user_agent, is_default, created_at, updated_at
		FROM user_agents WHERE is_default = 1 LIMIT 1`,
	).Scan(&ua.ID, &ua.Name, &ua.UserAgent, &ua.IsDefault, &ua.CreatedAt, &ua.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("default user agent not found: %w", err)
		}
		return nil, fmt.Errorf("getting default user agent: %w", err)
	}
	return ua, nil
}
