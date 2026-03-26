package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/rs/zerolog"
	_ "modernc.org/sqlite"

	"github.com/gavinmcnair/tvproxy/pkg/defaults"
	"github.com/gavinmcnair/tvproxy/pkg/store"
)

type DB struct {
	*sql.DB
	log            zerolog.Logger
	clientDefaults *defaults.ClientDefaults
	profileStore   store.ProfileStore
	clientStore    store.ClientStore
}

func (db *DB) SetClientDefaults(defs *defaults.ClientDefaults) {
	db.clientDefaults = defs
}

func (db *DB) SetProfileStore(ps store.ProfileStore) {
	db.profileStore = ps
}

func (db *DB) SetClientStore(cs store.ClientStore) {
	db.clientStore = cs
}

func New(ctx context.Context, dbPath string, log zerolog.Logger) (*DB, error) {
	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_busy_timeout=30000&_foreign_keys=on"+
		"&_pragma=synchronous(NORMAL)"+
		"&_pragma=cache_size(-64000)"+
		"&_pragma=temp_store(MEMORY)"+
		"&_pragma=mmap_size(268435456)"+
		"&_pragma=wal_autocheckpoint(0)", dbPath)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	if err := sqlDB.PingContext(ctx); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	db := &DB{DB: sqlDB, log: log}
	if err := db.migrate(ctx); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	log.Info().Str("path", dbPath).Msg("database connected")
	return db, nil
}

func (db *DB) InTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			db.log.Error().Err(rbErr).Msg("rollback failed")
		}
		return err
	}
	return tx.Commit()
}

func (db *DB) SoftReset(ctx context.Context) error {
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return fmt.Errorf("disabling foreign keys: %w", err)
	}
	defer db.ExecContext(ctx, "PRAGMA foreign_keys = ON") //nolint:errcheck

	if err := db.InTx(ctx, func(tx *sql.Tx) error {
		tables := []string{
			"channel_streams", "channels",
			"client_match_rules", "clients", "hdhr_device_channel_groups", "hdhr_devices",
			"channel_groups", "logos", "stream_profiles",
		}
		for _, t := range tables {
			if _, err := tx.ExecContext(ctx, "DELETE FROM "+t); err != nil {
				return fmt.Errorf("deleting from %s: %w", t, err)
			}
		}
		if _, err := tx.ExecContext(ctx, "UPDATE m3u_accounts SET stream_count = 0, last_refreshed = NULL"); err != nil {
			return fmt.Errorf("resetting m3u_accounts: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "UPDATE epg_sources SET channel_count = 0, program_count = 0, last_refreshed = NULL"); err != nil {
			return fmt.Errorf("resetting epg_sources: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	if err := seedData(ctx, db.DB); err != nil {
		return err
	}
	return SeedClientDefaults(ctx, db.clientDefaults, db.profileStore, db.clientStore)
}

func (db *DB) HardReset(ctx context.Context) error {
	rows, err := db.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name != 'sqlite_sequence'")
	if err != nil {
		return fmt.Errorf("listing tables: %w", err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return fmt.Errorf("scanning table name: %w", err)
		}
		tables = append(tables, name)
	}
	rows.Close()

	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return fmt.Errorf("disabling foreign keys: %w", err)
	}
	defer db.ExecContext(ctx, "PRAGMA foreign_keys = ON") //nolint:errcheck

	for _, t := range tables {
		if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS "+t); err != nil {
			return fmt.Errorf("dropping table %s: %w", t, err)
		}
	}

	if err := db.migrate(ctx); err != nil {
		return err
	}
	return SeedClientDefaults(ctx, db.clientDefaults, db.profileStore, db.clientStore)
}

func (db *DB) Checkpoint(ctx context.Context) {
	if _, err := db.ExecContext(ctx, "PRAGMA wal_checkpoint(PASSIVE)"); err != nil {
		db.log.Warn().Err(err).Msg("wal checkpoint failed")
	}
}

func (db *DB) migrate(ctx context.Context) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("creating migrations table: %w", err)
	}

	for i, m := range migrations {
		version := i + 1
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version).Scan(&count); err != nil {
			return fmt.Errorf("checking migration %d: %w", version, err)
		}
		if count > 0 {
			continue
		}
		db.log.Info().Int("version", version).Str("name", m.name).Msg("applying migration")
		if m.fn != nil {
			if err := m.fn(ctx, db.DB); err != nil {
				return fmt.Errorf("applying migration %d (%s): %w", version, m.name, err)
			}
		} else if _, err := db.ExecContext(ctx, m.sql); err != nil {
			return fmt.Errorf("applying migration %d (%s): %w", version, m.name, err)
		}
		if _, err := db.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES (?)", version); err != nil {
			return fmt.Errorf("recording migration %d: %w", version, err)
		}
	}
	return nil
}
