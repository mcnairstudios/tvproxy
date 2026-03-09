package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/gavinmcnair/tvproxy/pkg/database"
	"github.com/gavinmcnair/tvproxy/pkg/models"
)

type HDHRDeviceRepository struct {
	db *database.DB
}

func NewHDHRDeviceRepository(db *database.DB) *HDHRDeviceRepository {
	return &HDHRDeviceRepository{db: db}
}

func (r *HDHRDeviceRepository) Create(ctx context.Context, device *models.HDHRDevice) error {
	now := time.Now()
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO hdhr_devices (name, device_id, device_auth, firmware_version, tuner_count, channel_profile_id, is_enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		device.Name, device.DeviceID, device.DeviceAuth, device.FirmwareVersion,
		device.TunerCount, device.ChannelProfileID, device.IsEnabled, now, now,
	)
	if err != nil {
		return fmt.Errorf("creating hdhr device: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("getting last insert id: %w", err)
	}
	device.ID = id
	device.CreatedAt = now
	device.UpdatedAt = now
	return nil
}

func (r *HDHRDeviceRepository) GetByID(ctx context.Context, id int64) (*models.HDHRDevice, error) {
	device := &models.HDHRDevice{}
	var profileID sql.NullInt64
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, device_id, device_auth, firmware_version, tuner_count, channel_profile_id, is_enabled, created_at, updated_at
		FROM hdhr_devices WHERE id = ?`, id,
	).Scan(
		&device.ID, &device.Name, &device.DeviceID, &device.DeviceAuth,
		&device.FirmwareVersion, &device.TunerCount, &profileID,
		&device.IsEnabled, &device.CreatedAt, &device.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("hdhr device not found: %w", err)
		}
		return nil, fmt.Errorf("getting hdhr device by id: %w", err)
	}
	if profileID.Valid {
		device.ChannelProfileID = &profileID.Int64
	}
	return device, nil
}

func (r *HDHRDeviceRepository) GetByDeviceID(ctx context.Context, deviceID string) (*models.HDHRDevice, error) {
	device := &models.HDHRDevice{}
	var profileID sql.NullInt64
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, device_id, device_auth, firmware_version, tuner_count, channel_profile_id, is_enabled, created_at, updated_at
		FROM hdhr_devices WHERE device_id = ?`, deviceID,
	).Scan(
		&device.ID, &device.Name, &device.DeviceID, &device.DeviceAuth,
		&device.FirmwareVersion, &device.TunerCount, &profileID,
		&device.IsEnabled, &device.CreatedAt, &device.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("hdhr device not found: %w", err)
		}
		return nil, fmt.Errorf("getting hdhr device by device id: %w", err)
	}
	if profileID.Valid {
		device.ChannelProfileID = &profileID.Int64
	}
	return device, nil
}

func (r *HDHRDeviceRepository) List(ctx context.Context) ([]models.HDHRDevice, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, device_id, device_auth, firmware_version, tuner_count, channel_profile_id, is_enabled, created_at, updated_at
		FROM hdhr_devices ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing hdhr devices: %w", err)
	}
	defer rows.Close()

	var devices []models.HDHRDevice
	for rows.Next() {
		var d models.HDHRDevice
		var profileID sql.NullInt64
		if err := rows.Scan(
			&d.ID, &d.Name, &d.DeviceID, &d.DeviceAuth,
			&d.FirmwareVersion, &d.TunerCount, &profileID,
			&d.IsEnabled, &d.CreatedAt, &d.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning hdhr device: %w", err)
		}
		if profileID.Valid {
			d.ChannelProfileID = &profileID.Int64
		}
		devices = append(devices, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating hdhr devices: %w", err)
	}
	return devices, nil
}

func (r *HDHRDeviceRepository) Update(ctx context.Context, device *models.HDHRDevice) error {
	now := time.Now()
	_, err := r.db.ExecContext(ctx,
		`UPDATE hdhr_devices SET name = ?, device_id = ?, device_auth = ?, firmware_version = ?, tuner_count = ?, channel_profile_id = ?, is_enabled = ?, updated_at = ?
		WHERE id = ?`,
		device.Name, device.DeviceID, device.DeviceAuth, device.FirmwareVersion,
		device.TunerCount, device.ChannelProfileID, device.IsEnabled, now, device.ID,
	)
	if err != nil {
		return fmt.Errorf("updating hdhr device: %w", err)
	}
	device.UpdatedAt = now
	return nil
}

func (r *HDHRDeviceRepository) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM hdhr_devices WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting hdhr device: %w", err)
	}
	return nil
}
