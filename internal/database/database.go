// Package database owns Atalaya's SQLite connection, migrations, and queries.
package database

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	_ "modernc.org/sqlite"
)

var (
	// ErrForeignDatabase means a non-empty file is not an Atalaya database.
	ErrForeignDatabase = errors.New("database is not an Atalaya database")
	// ErrCorruptDatabase means Atalaya metadata or required schema is invalid.
	ErrCorruptDatabase = errors.New("atalaya database is corrupt")
	// ErrFutureSchema means the database was created by a newer Atalaya version.
	ErrFutureSchema = errors.New("database schema is newer than this Atalaya version")
	// ErrWrongMasterKey means the instance sentinel did not authenticate.
	ErrWrongMasterKey = errors.New("master key does not match database")
	// ErrAdministratorExists means an active v1 administrator already exists.
	ErrAdministratorExists = errors.New("an active administrator already exists")
	// ErrDatabaseChanged means the path changed after its read-only preflight.
	ErrDatabaseChanged = errors.New("database changed after preflight")
)

// State is the read-only classification of a database path.
type State uint8

const (
	StateFresh State = iota
	StateAtalaya
)

// PreflightResult contains information safe to use before opening for writes.
type PreflightResult struct {
	State                  State
	SchemaVersion          int
	HasActiveAdministrator bool
}

// Database owns the writable SQLite pool and the key needed to revalidate writes.
type Database struct {
	sql        *sql.DB
	key        []byte
	migrations []migration
	path       string
	file       os.FileInfo
}

type openOptions struct {
	clock                     func() time.Time
	random                    io.Reader
	migrations                []migration
	afterPreflight            func() error
	afterWritablePathPrepared func() error
}

// Preflight classifies and validates a path using a read-only SQLite connection.
func Preflight(ctx context.Context, path string, key []byte) (PreflightResult, error) {
	return preflight(ctx, path, key, embeddedMigrations)
}

// Open preflights, migrates, enables WAL, and returns the writable database owner.
func Open(ctx context.Context, path string, key []byte) (*Database, error) {
	return open(ctx, path, key, openOptions{
		clock:      time.Now,
		random:     rand.Reader,
		migrations: embeddedMigrations,
	})
}

func open(ctx context.Context, path string, key []byte, options openOptions) (*Database, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("master key must contain exactly 32 bytes")
	}
	if options.clock == nil || options.random == nil || len(options.migrations) == 0 {
		return nil, fmt.Errorf("database open dependencies are required")
	}
	if err := validateMigrationSet(options.migrations); err != nil {
		return nil, err
	}
	initialFile, initiallyMissing, err := inspectPathBeforePreflight(path)
	if err != nil {
		return nil, err
	}
	preflightResult, err := preflight(ctx, path, key, options.migrations)
	if err != nil {
		return nil, err
	}
	if options.afterPreflight != nil {
		if err := options.afterPreflight(); err != nil {
			return nil, fmt.Errorf("after database preflight: %w", err)
		}
	}
	writableFile, err := prepareWritablePath(path, preflightResult, initialFile, initiallyMissing)
	if err != nil {
		return nil, err
	}
	if options.afterWritablePathPrepared != nil {
		if err := options.afterWritablePathPrepared(); err != nil {
			return nil, fmt.Errorf("after writable database path preparation: %w", err)
		}
	}

	db, err := sql.Open("sqlite", writableURI(path))
	if err != nil {
		return nil, fmt.Errorf("open writable database: %w", err)
	}
	owner := &Database{
		sql: db, key: append([]byte(nil), key...), migrations: options.migrations,
		path: path, file: writableFile,
	}
	if err := owner.applyMigrations(ctx, options.clock, options.random, preflightResult); err != nil {
		owner.Close()
		return nil, err
	}
	if err := enableWAL(ctx, db); err != nil {
		owner.Close()
		return nil, err
	}
	return owner, nil
}

func inspectPathBeforePreflight(path string) (os.FileInfo, bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("inspect database path before preflight: %w", err)
	}
	return info, false, nil
}

func prepareWritablePath(
	path string,
	preflight PreflightResult,
	initial os.FileInfo,
	initiallyMissing bool,
) (os.FileInfo, error) {
	current, err := os.Stat(path)
	if initiallyMissing {
		if err == nil {
			return nil, fmt.Errorf("%w: database path appeared after preflight", ErrDatabaseChanged)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("inspect database path after preflight: %w", err)
		}
		file, createErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // path is the database_path from server.yaml, set by the operator
		if createErr != nil {
			if errors.Is(createErr, os.ErrExist) {
				return nil, fmt.Errorf("%w: database path appeared while opening", ErrDatabaseChanged)
			}
			return nil, fmt.Errorf("create fresh database placeholder: %w", createErr)
		}
		if closeErr := file.Close(); closeErr != nil {
			return nil, fmt.Errorf("close fresh database placeholder: %w", closeErr)
		}
		current, err = os.Stat(path)
	}
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: database path disappeared after preflight", ErrDatabaseChanged)
		}
		return nil, fmt.Errorf("inspect database path after preflight: %w", err)
	}
	if !current.Mode().IsRegular() || initial != nil && !os.SameFile(initial, current) {
		return nil, fmt.Errorf("%w: database path identity differs", ErrDatabaseChanged)
	}
	if preflight.State == StateFresh && current.Size() != 0 {
		return nil, fmt.Errorf("%w: fresh database path is no longer empty", ErrDatabaseChanged)
	}
	return current, nil
}

// Close releases all SQLite connections and erases the in-memory key copy.
func (database *Database) Close() error {
	if database == nil {
		return nil
	}
	for index := range database.key {
		database.key[index] = 0
	}
	if database.sql == nil {
		return nil
	}
	return database.sql.Close()
}

func preflight(ctx context.Context, path string, key []byte, migrations []migration) (PreflightResult, error) {
	if len(key) != 32 {
		return PreflightResult{}, fmt.Errorf("master key must contain exactly 32 bytes")
	}
	if err := ctx.Err(); err != nil {
		return PreflightResult{}, err
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return PreflightResult{State: StateFresh}, nil
	}
	if err != nil {
		return PreflightResult{}, fmt.Errorf("inspect database path: %w", err)
	}
	if !info.Mode().IsRegular() {
		return PreflightResult{}, fmt.Errorf("database path must be a regular file")
	}
	if info.Size() == 0 {
		return PreflightResult{State: StateFresh}, nil
	}

	db, err := sql.Open("sqlite", readOnlyURI(path))
	if err != nil {
		return PreflightResult{}, fmt.Errorf("open database preflight: %w", err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()

	transaction, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return PreflightResult{}, classifyPreflightOpenError(ctx, err)
	}
	defer transaction.Rollback()

	validation, err := validateAtalaya(ctx, transaction, key, migrations)
	if err != nil {
		return PreflightResult{}, err
	}
	return PreflightResult{
		State:                  StateAtalaya,
		SchemaVersion:          validation.version,
		HasActiveAdministrator: validation.activeAdministrators != 0,
	}, nil
}

func enableWAL(ctx context.Context, db *sql.DB) error {
	var journalMode string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&journalMode); err != nil {
		return fmt.Errorf("enable WAL journal mode: %w", err)
	}
	if journalMode != "wal" {
		return fmt.Errorf("enable WAL journal mode: SQLite reported %q", journalMode)
	}
	return nil
}
