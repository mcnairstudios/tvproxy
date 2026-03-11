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
		`INSERT INTO hdhr_devices (name, device_id, device_auth, firmware_version, tuner_count, port, channel_profile_id, is_enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		device.Name, device.DeviceID, device.DeviceAuth, device.FirmwareVersion,
		device.TunerCount, device.Port, device.ChannelProfileID, device.IsEnabled, now, now,
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
		`SELECT id, name, device_id, device_auth, firmware_version, tuner_count, port, channel_profile_id, is_enabled, created_at, updated_at
		FROM hdhr_devices WHERE id = ?`, id,
	).Scan(
		&device.ID, &device.Name, &device.DeviceID, &device.DeviceAuth,
		&device.FirmwareVersion, &device.TunerCount, &device.Port, &profileID,
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
	groupIDs, err := r.GetChannelGroups(ctx, device.ID)
	if err != nil {
		return nil, fmt.Errorf("getting channel groups for device: %w", err)
	}
	device.ChannelGroupIDs = groupIDs
	return device, nil
}

func (r *HDHRDeviceRepository) GetByDeviceID(ctx context.Context, deviceID string) (*models.HDHRDevice, error) {
	device := &models.HDHRDevice{}
	var profileID sql.NullInt64
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, device_id, device_auth, firmware_version, tuner_count, port, channel_profile_id, is_enabled, created_at, updated_at
		FROM hdhr_devices WHERE device_id = ?`, deviceID,
	).Scan(
		&device.ID, &device.Name, &device.DeviceID, &device.DeviceAuth,
		&device.FirmwareVersion, &device.TunerCount, &device.Port, &profileID,
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
	groupIDs, err := r.GetChannelGroups(ctx, device.ID)
	if err != nil {
		return nil, fmt.Errorf("getting channel groups for device: %w", err)
	}
	device.ChannelGroupIDs = groupIDs
	return device, nil
}

func (r *HDHRDeviceRepository) List(ctx context.Context) ([]models.HDHRDevice, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, device_id, device_auth, firmware_version, tuner_count, port, channel_profile_id, is_enabled, created_at, updated_at
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
			&d.FirmwareVersion, &d.TunerCount, &d.Port, &profileID,
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

	// Hydrate channel group IDs for all devices
	for i := range devices {
		groupIDs, err := r.GetChannelGroups(ctx, devices[i].ID)
		if err != nil {
			return nil, fmt.Errorf("getting channel groups for device %d: %w", devices[i].ID, err)
		}
		devices[i].ChannelGroupIDs = groupIDs
	}

	return devices, nil
}

func (r *HDHRDeviceRepository) Update(ctx context.Context, device *models.HDHRDevice) error {
	now := time.Now()
	_, err := r.db.ExecContext(ctx,
		`UPDATE hdhr_devices SET name = ?, device_id = ?, device_auth = ?, firmware_version = ?, tuner_count = ?, port = ?, channel_profile_id = ?, is_enabled = ?, updated_at = ?
		WHERE id = ?`,
		device.Name, device.DeviceID, device.DeviceAuth, device.FirmwareVersion,
		device.TunerCount, device.Port, device.ChannelProfileID, device.IsEnabled, now, device.ID,
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

func (r *HDHRDeviceRepository) NextAvailablePort(ctx context.Context) (int, error) {
	var port int
	err := r.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(port), 47600) + 1 FROM hdhr_devices`,
	).Scan(&port)
	if err != nil {
		return 0, fmt.Errorf("getting next available port: %w", err)
	}
	return port, nil
}

// SetChannelGroups replaces all channel group associations for a device.
func (r *HDHRDeviceRepository) SetChannelGroups(ctx context.Context, deviceID int64, groupIDs []int64) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM hdhr_device_channel_groups WHERE hdhr_device_id = ?`, deviceID); err != nil {
		return fmt.Errorf("clearing channel groups for device: %w", err)
	}
	for _, gid := range groupIDs {
		if _, err := r.db.ExecContext(ctx,
			`INSERT INTO hdhr_device_channel_groups (hdhr_device_id, channel_group_id) VALUES (?, ?)`,
			deviceID, gid); err != nil {
			return fmt.Errorf("inserting channel group for device: %w", err)
		}
	}
	return nil
}

// GetChannelGroups returns the channel group IDs associated with a device.
func (r *HDHRDeviceRepository) GetChannelGroups(ctx context.Context, deviceID int64) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT channel_group_id FROM hdhr_device_channel_groups WHERE hdhr_device_id = ? ORDER BY channel_group_id`,
		deviceID,
	)
	if err != nil {
		return nil, fmt.Errorf("getting channel groups for device: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning channel group id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating channel group ids: %w", err)
	}
	return ids, nil
}
