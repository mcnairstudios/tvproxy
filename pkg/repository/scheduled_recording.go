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

type ScheduledRecordingRepository struct {
	db *database.DB
}

func NewScheduledRecordingRepository(db *database.DB) *ScheduledRecordingRepository {
	return &ScheduledRecordingRepository{db: db}
}

func (r *ScheduledRecordingRepository) Create(ctx context.Context, rec *models.ScheduledRecording) error {
	now := time.Now()
	rec.ID = uuid.New().String()
	rec.CreatedAt = now
	rec.UpdatedAt = now
	if rec.Status == "" {
		rec.Status = "pending"
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO scheduled_recordings (id, user_id, channel_id, channel_name, program_title, start_at, stop_at, status, session_id, segment_id, file_path, last_error, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.ID, rec.UserID, rec.ChannelID, rec.ChannelName, rec.ProgramTitle,
		rec.StartAt, rec.StopAt, rec.Status, rec.SessionID, rec.SegmentID,
		rec.FilePath, rec.LastError, now, now,
	)
	if err != nil {
		return fmt.Errorf("creating scheduled recording: %w", err)
	}
	return nil
}

func (r *ScheduledRecordingRepository) GetByID(ctx context.Context, id string) (*models.ScheduledRecording, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, user_id, channel_id, channel_name, program_title, start_at, stop_at, status, session_id, segment_id, file_path, last_error, created_at, updated_at
		FROM scheduled_recordings WHERE id = ?`, id)
	return scanScheduledRecording(row)
}

func (r *ScheduledRecordingRepository) List(ctx context.Context) ([]models.ScheduledRecording, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, channel_id, channel_name, program_title, start_at, stop_at, status, session_id, segment_id, file_path, last_error, created_at, updated_at
		FROM scheduled_recordings ORDER BY start_at`)
	if err != nil {
		return nil, fmt.Errorf("listing scheduled recordings: %w", err)
	}
	defer rows.Close()
	return scanScheduledRecordings(rows)
}

func (r *ScheduledRecordingRepository) ListByUserID(ctx context.Context, userID string) ([]models.ScheduledRecording, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, channel_id, channel_name, program_title, start_at, stop_at, status, session_id, segment_id, file_path, last_error, created_at, updated_at
		FROM scheduled_recordings WHERE user_id = ? ORDER BY start_at`, userID)
	if err != nil {
		return nil, fmt.Errorf("listing scheduled recordings by user: %w", err)
	}
	defer rows.Close()
	return scanScheduledRecordings(rows)
}

func (r *ScheduledRecordingRepository) ListPending(ctx context.Context, before time.Time) ([]models.ScheduledRecording, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, channel_id, channel_name, program_title, start_at, stop_at, status, session_id, segment_id, file_path, last_error, created_at, updated_at
		FROM scheduled_recordings WHERE status = 'pending' AND start_at <= ? ORDER BY start_at`, before)
	if err != nil {
		return nil, fmt.Errorf("listing pending recordings: %w", err)
	}
	defer rows.Close()
	return scanScheduledRecordings(rows)
}

func (r *ScheduledRecordingRepository) ListByStatus(ctx context.Context, status string) ([]models.ScheduledRecording, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, channel_id, channel_name, program_title, start_at, stop_at, status, session_id, segment_id, file_path, last_error, created_at, updated_at
		FROM scheduled_recordings WHERE status = ? ORDER BY start_at`, status)
	if err != nil {
		return nil, fmt.Errorf("listing recordings by status: %w", err)
	}
	defer rows.Close()
	return scanScheduledRecordings(rows)
}

func (r *ScheduledRecordingRepository) UpdateStatus(ctx context.Context, id, status, lastError string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE scheduled_recordings SET status = ?, last_error = ?, updated_at = ? WHERE id = ?`,
		status, lastError, time.Now(), id)
	if err != nil {
		return fmt.Errorf("updating scheduled recording status: %w", err)
	}
	return nil
}

func (r *ScheduledRecordingRepository) UpdateRecordingState(ctx context.Context, id, sessionID, segmentID string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE scheduled_recordings SET session_id = ?, segment_id = ?, updated_at = ? WHERE id = ?`,
		sessionID, segmentID, time.Now(), id)
	if err != nil {
		return fmt.Errorf("updating recording state: %w", err)
	}
	return nil
}

func (r *ScheduledRecordingRepository) UpdateFilePath(ctx context.Context, id, filePath string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE scheduled_recordings SET file_path = ?, updated_at = ? WHERE id = ?`,
		filePath, time.Now(), id)
	if err != nil {
		return fmt.Errorf("updating file path: %w", err)
	}
	return nil
}

func (r *ScheduledRecordingRepository) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM scheduled_recordings WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting scheduled recording: %w", err)
	}
	return nil
}

func (r *ScheduledRecordingRepository) ListByChannelAndTimeRange(ctx context.Context, channelID, userID string, start, stop time.Time) ([]models.ScheduledRecording, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, channel_id, channel_name, program_title, start_at, stop_at, status, session_id, segment_id, file_path, last_error, created_at, updated_at
		FROM scheduled_recordings
		WHERE channel_id = ? AND user_id = ? AND start_at < ? AND stop_at > ? AND status NOT IN ('cancelled', 'failed', 'completed')
		ORDER BY start_at`, channelID, userID, stop, start)
	if err != nil {
		return nil, fmt.Errorf("listing by channel and time range: %w", err)
	}
	defer rows.Close()
	return scanScheduledRecordings(rows)
}

type scheduledRecordingScanner interface {
	Scan(dest ...any) error
}

func scanScheduledRecording(s scheduledRecordingScanner) (*models.ScheduledRecording, error) {
	rec := &models.ScheduledRecording{}
	err := s.Scan(
		&rec.ID, &rec.UserID, &rec.ChannelID, &rec.ChannelName, &rec.ProgramTitle,
		&rec.StartAt, &rec.StopAt, &rec.Status, &rec.SessionID, &rec.SegmentID,
		&rec.FilePath, &rec.LastError, &rec.CreatedAt, &rec.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("scheduled recording not found: %w", err)
		}
		return nil, fmt.Errorf("scanning scheduled recording: %w", err)
	}
	return rec, nil
}

func scanScheduledRecordings(rows *sql.Rows) ([]models.ScheduledRecording, error) {
	var list []models.ScheduledRecording
	for rows.Next() {
		rec, err := scanScheduledRecording(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating scheduled recordings: %w", err)
	}
	return list, nil
}
