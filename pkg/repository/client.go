package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

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
	client.ID = uuid.New().String()
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO clients (id, name, priority, stream_profile_id, is_enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		client.ID, client.Name, client.Priority, client.StreamProfileID, client.IsEnabled, now, now,
	)
	if err != nil {
		return fmt.Errorf("creating client: %w", err)
	}
	client.CreatedAt = now
	client.UpdatedAt = now
	return nil
}

func (r *ClientRepository) GetByID(ctx context.Context, id string) (*models.Client, error) {
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
		FROM clients ORDER BY priority, created_at`,
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

	if err := r.batchLoadMatchRules(ctx, clients); err != nil {
		return nil, err
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

func (r *ClientRepository) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM clients WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting client: %w", err)
	}
	return nil
}

func (r *ClientRepository) SetMatchRules(ctx context.Context, clientID string, rules []models.ClientMatchRule) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM client_match_rules WHERE client_id = ?`, clientID); err != nil {
		return fmt.Errorf("clearing match rules: %w", err)
	}
	for i := range rules {
		rules[i].ID = uuid.New().String()
		rules[i].ClientID = clientID
		_, err := r.db.ExecContext(ctx,
			`INSERT INTO client_match_rules (id, client_id, header_name, match_type, match_value) VALUES (?, ?, ?, ?, ?)`,
			rules[i].ID, clientID, rules[i].HeaderName, rules[i].MatchType, rules[i].MatchValue)
		if err != nil {
			return fmt.Errorf("inserting match rule: %w", err)
		}
	}
	return nil
}

func (r *ClientRepository) ListEnabledWithRules(ctx context.Context) ([]models.Client, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, priority, stream_profile_id, is_enabled, created_at, updated_at
		FROM clients WHERE is_enabled = 1 ORDER BY priority, created_at`,
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

	if err := r.batchLoadMatchRules(ctx, clients); err != nil {
		return nil, err
	}

	return clients, nil
}

func (r *ClientRepository) IsStreamProfileReferenced(ctx context.Context, profileID string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM clients WHERE stream_profile_id = ?`, profileID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("checking profile references: %w", err)
	}
	return count > 0, nil
}

func (r *ClientRepository) batchLoadMatchRules(ctx context.Context, clients []models.Client) error {
	if len(clients) == 0 {
		return nil
	}
	placeholders := make([]string, len(clients))
	args := make([]any, len(clients))
	idIndex := make(map[string]int, len(clients))
	for i, c := range clients {
		placeholders[i] = "?"
		args[i] = c.ID
		idIndex[c.ID] = i
	}
	query := fmt.Sprintf(
		`SELECT id, client_id, header_name, match_type, match_value FROM client_match_rules WHERE client_id IN (%s) ORDER BY client_id, header_name`,
		strings.Join(placeholders, ","),
	)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("batch loading match rules: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var rule models.ClientMatchRule
		if err := rows.Scan(&rule.ID, &rule.ClientID, &rule.HeaderName, &rule.MatchType, &rule.MatchValue); err != nil {
			return fmt.Errorf("scanning match rule: %w", err)
		}
		if idx, ok := idIndex[rule.ClientID]; ok {
			clients[idx].MatchRules = append(clients[idx].MatchRules, rule)
		}
	}
	return rows.Err()
}

func (r *ClientRepository) getMatchRules(ctx context.Context, clientID string) ([]models.ClientMatchRule, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, client_id, header_name, match_type, match_value
		FROM client_match_rules WHERE client_id = ? ORDER BY header_name`, clientID,
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
