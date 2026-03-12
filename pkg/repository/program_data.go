package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/gavinmcnair/tvproxy/pkg/database"
	"github.com/gavinmcnair/tvproxy/pkg/models"
)

// GuideProgram represents a program with its EPG channel_id for guide display.
type GuideProgram struct {
	ChannelID   string    `json:"channel_id"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	Start       time.Time `json:"start"`
	Stop        time.Time `json:"stop"`
	Category    string    `json:"category,omitempty"`
}

type ProgramDataRepository struct {
	db *database.DB
}

func NewProgramDataRepository(db *database.DB) *ProgramDataRepository {
	return &ProgramDataRepository{db: db}
}

func (r *ProgramDataRepository) Create(ctx context.Context, program *models.ProgramData) error {
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO program_data (epg_data_id, title, description, start, stop, category, episode_num, icon)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		program.EPGDataID, program.Title, program.Description,
		program.Start, program.Stop, program.Category,
		program.EpisodeNum, program.Icon,
	)
	if err != nil {
		return fmt.Errorf("creating program data: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("getting last insert id: %w", err)
	}
	program.ID = id
	return nil
}

func (r *ProgramDataRepository) List(ctx context.Context) ([]models.ProgramData, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, epg_data_id, title, description, start, stop, category, episode_num, icon
		FROM program_data ORDER BY start`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing program data: %w", err)
	}
	defer rows.Close()

	var programs []models.ProgramData
	for rows.Next() {
		var p models.ProgramData
		if err := rows.Scan(
			&p.ID, &p.EPGDataID, &p.Title, &p.Description,
			&p.Start, &p.Stop, &p.Category, &p.EpisodeNum, &p.Icon,
		); err != nil {
			return nil, fmt.Errorf("scanning program data: %w", err)
		}
		programs = append(programs, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating program data: %w", err)
	}
	return programs, nil
}

func (r *ProgramDataRepository) ListByEPGDataID(ctx context.Context, epgDataID int64) ([]models.ProgramData, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, epg_data_id, title, description, start, stop, category, episode_num, icon
		FROM program_data WHERE epg_data_id = ? ORDER BY start`, epgDataID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing program data by epg data id: %w", err)
	}
	defer rows.Close()

	var programs []models.ProgramData
	for rows.Next() {
		var p models.ProgramData
		if err := rows.Scan(
			&p.ID, &p.EPGDataID, &p.Title, &p.Description,
			&p.Start, &p.Stop, &p.Category, &p.EpisodeNum, &p.Icon,
		); err != nil {
			return nil, fmt.Errorf("scanning program data: %w", err)
		}
		programs = append(programs, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating program data: %w", err)
	}
	return programs, nil
}

func (r *ProgramDataRepository) ListByTimeRange(ctx context.Context, start, stop time.Time) ([]models.ProgramData, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, epg_data_id, title, description, start, stop, category, episode_num, icon
		FROM program_data WHERE start < ? AND stop > ? ORDER BY start`, stop, start,
	)
	if err != nil {
		return nil, fmt.Errorf("listing program data by time range: %w", err)
	}
	defer rows.Close()

	var programs []models.ProgramData
	for rows.Next() {
		var p models.ProgramData
		if err := rows.Scan(
			&p.ID, &p.EPGDataID, &p.Title, &p.Description,
			&p.Start, &p.Stop, &p.Category, &p.EpisodeNum, &p.Icon,
		); err != nil {
			return nil, fmt.Errorf("scanning program data: %w", err)
		}
		programs = append(programs, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating program data: %w", err)
	}
	return programs, nil
}

func (r *ProgramDataRepository) GetNowByChannelID(ctx context.Context, channelID string, now time.Time) (*models.ProgramData, error) {
	var p models.ProgramData
	err := r.db.QueryRowContext(ctx,
		`SELECT p.id, p.epg_data_id, p.title, p.description, p.start, p.stop, p.category, p.episode_num, p.icon
		FROM program_data p
		JOIN epg_data e ON e.id = p.epg_data_id
		WHERE e.channel_id = ? AND p.start <= ? AND p.stop > ?
		ORDER BY p.start DESC LIMIT 1`,
		channelID, now, now,
	).Scan(&p.ID, &p.EPGDataID, &p.Title, &p.Description, &p.Start, &p.Stop, &p.Category, &p.EpisodeNum, &p.Icon)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ListForGuide returns programs within a time window with their EPG channel_id.
// Used by the EPG guide grid to display programs across channels.
func (r *ProgramDataRepository) ListForGuide(ctx context.Context, start, stop time.Time) ([]GuideProgram, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT e.channel_id, p.title, p.description, p.start, p.stop, p.category
		FROM program_data p
		JOIN epg_data e ON e.id = p.epg_data_id
		WHERE p.start < ? AND p.stop > ?
		ORDER BY e.channel_id, p.start`,
		stop, start,
	)
	if err != nil {
		return nil, fmt.Errorf("listing guide programs: %w", err)
	}
	defer rows.Close()

	var programs []GuideProgram
	for rows.Next() {
		var g GuideProgram
		if err := rows.Scan(&g.ChannelID, &g.Title, &g.Description, &g.Start, &g.Stop, &g.Category); err != nil {
			return nil, fmt.Errorf("scanning guide program: %w", err)
		}
		programs = append(programs, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating guide programs: %w", err)
	}
	return programs, nil
}

func (r *ProgramDataRepository) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM program_data WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting program data: %w", err)
	}
	return nil
}

func (r *ProgramDataRepository) DeleteByEPGDataID(ctx context.Context, epgDataID int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM program_data WHERE epg_data_id = ?`, epgDataID)
	if err != nil {
		return fmt.Errorf("deleting program data by epg data id: %w", err)
	}
	return nil
}

func (r *ProgramDataRepository) BulkCreate(ctx context.Context, programs []models.ProgramData) error {
	return r.db.InTx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx,
			`INSERT INTO program_data (epg_data_id, title, description, start, stop, category, episode_num, icon)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		)
		if err != nil {
			return fmt.Errorf("preparing statement: %w", err)
		}
		defer stmt.Close()

		for i := range programs {
			result, err := stmt.ExecContext(ctx,
				programs[i].EPGDataID, programs[i].Title, programs[i].Description,
				programs[i].Start, programs[i].Stop, programs[i].Category,
				programs[i].EpisodeNum, programs[i].Icon,
			)
			if err != nil {
				return fmt.Errorf("inserting program data %d: %w", i, err)
			}
			id, err := result.LastInsertId()
			if err != nil {
				return fmt.Errorf("getting last insert id for program data %d: %w", i, err)
			}
			programs[i].ID = id
		}
		return nil
	})
}
