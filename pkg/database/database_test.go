package database

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	log := zerolog.New(os.Stderr).Level(zerolog.Disabled)

	db, err := New(context.Background(), dbPath, log)
	require.NoError(t, err)
	defer db.Close()

	var count int
	err = db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM schema_migrations").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, len(migrations), count)
}

func TestMigrationsIdempotent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	log := zerolog.New(os.Stderr).Level(zerolog.Disabled)

	db, err := New(context.Background(), dbPath, log)
	require.NoError(t, err)
	db.Close()

	db, err = New(context.Background(), dbPath, log)
	require.NoError(t, err)
	defer db.Close()

	var count int
	err = db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM schema_migrations").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, len(migrations), count)
}

func TestInTx(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	log := zerolog.New(os.Stderr).Level(zerolog.Disabled)

	db, err := New(context.Background(), dbPath, log)
	require.NoError(t, err)
	defer db.Close()

	ctx := context.Background()
	err = db.InTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO core_settings (key, value) VALUES ('test', 'value')")
		return err
	})
	require.NoError(t, err)

	var val string
	err = db.QueryRowContext(ctx, "SELECT value FROM core_settings WHERE key = 'test'").Scan(&val)
	require.NoError(t, err)
	assert.Equal(t, "value", val)
}

func TestTablesExist(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	log := zerolog.New(os.Stderr).Level(zerolog.Disabled)

	db, err := New(context.Background(), dbPath, log)
	require.NoError(t, err)
	defer db.Close()

	tables := []string{
		"users", "m3u_accounts", "channel_groups",
		"channels", "channel_streams", "logos",
		"stream_profiles", "epg_sources",
		"hdhr_devices", "hdhr_device_channel_groups",
		"clients", "client_match_rules",
		"core_settings", "scheduled_recordings",
	}

	for _, table := range tables {
		var name string
		err := db.QueryRowContext(context.Background(),
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		require.NoError(t, err, "table %s should exist", table)
		assert.Equal(t, table, name)
	}
}
