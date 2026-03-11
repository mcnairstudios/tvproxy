package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/gavinmcnair/tvproxy/pkg/database"
	"github.com/gavinmcnair/tvproxy/pkg/models"
)

type ClientRepository struct {
	db *database.DB
}

func NewClientRepository(db *database.DB) *ClientRepository {
	return &ClientRepository{db: db}
}

func (r *ClientRepository) Create(ctx context.Context, client *models.Client) error {
	now := time.Now()
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO clients (name, priority, stream_profile_id, is_enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		client.Name, client.Priority, client.StreamProfileID, client.IsEnabled, now, now,
	)
	if err != nil {
		return fmt.Errorf("creating client: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("getting last insert id: %w", err)
	}
	client.ID = id
	client.CreatedAt = now
	client.UpdatedAt = now
	return nil
}

func (r *ClientRepository) GetByID(ctx context.Context, id int64) (*models.Client, error) {
	client := &models.Client{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, priority, stream_profile_id, is_enabled, created_at, updated_at
		FROM clients WHERE id = ?`, id,
	).Scan(&client.ID, &client.Name, &client.Priority, &client.StreamProfileID,
		&client.IsEnabled, &client.CreatedAt, &client.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("client not found: %w", err)
		}
		return nil, fmt.Errorf("getting client by id: %w", err)
	}
	rules, err := r.getMatchRules(ctx, client.ID)
	if err != nil {
		return nil, err
	}
	client.MatchRules = rules
	return client, nil
}

func (r *ClientRepository) List(ctx context.Context) ([]models.Client, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, priority, stream_profile_id, is_enabled, created_at, updated_at
		FROM clients ORDER BY priority, id`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing clients: %w", err)
	}
	defer rows.Close()

	var clients []models.Client
	for rows.Next() {
		var c models.Client
		if err := rows.Scan(&c.ID, &c.Name, &c.Priority, &c.StreamProfileID,
			&c.IsEnabled, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning client: %w", err)
		}
		clients = append(clients, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating clients: %w", err)
	}

	for i := range clients {
		rules, err := r.getMatchRules(ctx, clients[i].ID)
		if err != nil {
			return nil, err
		}
		clients[i].MatchRules = rules
	}

	return clients, nil
}

func (r *ClientRepository) Update(ctx context.Context, client *models.Client) error {
	now := time.Now()
	_, err := r.db.ExecContext(ctx,
		`UPDATE clients SET name = ?, priority = ?, stream_profile_id = ?, is_enabled = ?, updated_at = ?
		WHERE id = ?`,
		client.Name, client.Priority, client.StreamProfileID, client.IsEnabled, now, client.ID,
	)
	if err != nil {
		return fmt.Errorf("updating client: %w", err)
	}
	client.UpdatedAt = now
	return nil
}

func (r *ClientRepository) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM clients WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting client: %w", err)
	}
	return nil
}

// SetMatchRules replaces all match rules for a client (delete-then-insert).
func (r *ClientRepository) SetMatchRules(ctx context.Context, clientID int64, rules []models.ClientMatchRule) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM client_match_rules WHERE client_id = ?`, clientID); err != nil {
		return fmt.Errorf("clearing match rules: %w", err)
	}
	for _, rule := range rules {
		result, err := r.db.ExecContext(ctx,
			`INSERT INTO client_match_rules (client_id, header_name, match_type, match_value) VALUES (?, ?, ?, ?)`,
			clientID, rule.HeaderName, rule.MatchType, rule.MatchValue)
		if err != nil {
			return fmt.Errorf("inserting match rule: %w", err)
		}
		id, _ := result.LastInsertId()
		rule.ID = id
	}
	return nil
}

// ListEnabledWithRules returns enabled clients ordered by priority with rules pre-loaded.
func (r *ClientRepository) ListEnabledWithRules(ctx context.Context) ([]models.Client, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, priority, stream_profile_id, is_enabled, created_at, updated_at
		FROM clients WHERE is_enabled = 1 ORDER BY priority, id`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing enabled clients: %w", err)
	}
	defer rows.Close()

	var clients []models.Client
	for rows.Next() {
		var c models.Client
		if err := rows.Scan(&c.ID, &c.Name, &c.Priority, &c.StreamProfileID,
			&c.IsEnabled, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning client: %w", err)
		}
		clients = append(clients, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating clients: %w", err)
	}

	for i := range clients {
		rules, err := r.getMatchRules(ctx, clients[i].ID)
		if err != nil {
			return nil, err
		}
		clients[i].MatchRules = rules
	}

	return clients, nil
}

// IsStreamProfileReferenced checks if a stream profile is referenced by any client.
func (r *ClientRepository) IsStreamProfileReferenced(ctx context.Context, profileID int64) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM clients WHERE stream_profile_id = ?`, profileID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("checking profile references: %w", err)
	}
	return count > 0, nil
}

func (r *ClientRepository) getMatchRules(ctx context.Context, clientID int64) ([]models.ClientMatchRule, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, client_id, header_name, match_type, match_value
		FROM client_match_rules WHERE client_id = ? ORDER BY id`, clientID,
	)
	if err != nil {
		return nil, fmt.Errorf("getting match rules: %w", err)
	}
	defer rows.Close()

	var rules []models.ClientMatchRule
	for rows.Next() {
		var rule models.ClientMatchRule
		if err := rows.Scan(&rule.ID, &rule.ClientID, &rule.HeaderName, &rule.MatchType, &rule.MatchValue); err != nil {
			return nil, fmt.Errorf("scanning match rule: %w", err)
		}
		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating match rules: %w", err)
	}
	return rules, nil
}
