package database

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFailedMigrationRollsBackOnlyItsTransaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database.db")
	migrations := append([]migration(nil), embeddedMigrations...)
	migrations = append(migrations, newMigrationWithValidator(2, `
		CREATE TABLE must_roll_back (id INTEGER);
		INSERT INTO table_that_does_not_exist(id) VALUES (1);`, validateSchemaV1))

	_, err := open(context.Background(), path, testMasterKey(), openOptions{
		clock:      func() time.Time { return testInstant },
		random:     bytes.NewReader(make([]byte, 12)),
		migrations: migrations,
	})
	if err == nil {
		t.Fatal("open() error = nil")
	}

	raw := openRawDatabase(t, path)
	var migrationCount, rolledBackTable int
	if err := raw.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&migrationCount); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if err := raw.QueryRow(
		"SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name = 'must_roll_back'",
	).Scan(&rolledBackTable); err != nil {
		t.Fatal(err)
	}
	if migrationCount != 1 || rolledBackTable != 0 {
		t.Fatalf("after failed v2: migrations = %d, rolled-back table count = %d", migrationCount, rolledBackTable)
	}
}

func TestInitialMigrationRollsBackWhenSentinelGenerationFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database.db")
	_, err := open(context.Background(), path, testMasterKey(), openOptions{
		clock:      func() time.Time { return testInstant },
		random:     bytes.NewReader(nil),
		migrations: embeddedMigrations,
	})
	if err == nil {
		t.Fatal("open() error = nil")
	}

	result, preflightErr := Preflight(context.Background(), path, testMasterKey())
	if preflightErr != nil {
		t.Fatalf("Preflight() after rollback error = %v", preflightErr)
	}
	if result.State != StateFresh {
		t.Fatalf("Preflight() after rollback = %+v, want fresh", result)
	}
}

func TestWritableRevalidationRejectsDatabaseChangedAfterPreflight(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database.db")
	database := openTestDatabase(t, path)
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	_, err := open(context.Background(), path, testMasterKey(), openOptions{
		clock:      func() time.Time { return testInstant },
		random:     bytes.NewReader(make([]byte, 12)),
		migrations: embeddedMigrations,
		afterPreflight: func() error {
			raw, openErr := sql.Open("sqlite", writableURI(path))
			if openErr != nil {
				return openErr
			}
			defer raw.Close()
			_, updateErr := raw.Exec("UPDATE schema_migrations SET checksum = 'changed'")
			return updateErr
		},
	})
	if !errors.Is(err, ErrCorruptDatabase) {
		t.Fatalf("open() error = %v, want ErrCorruptDatabase", err)
	}
}

func TestWritableRevalidationRejectsPathReplacedAfterPreflight(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database.db")
	database := openTestDatabase(t, path)
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	_, err := open(context.Background(), path, testMasterKey(), openOptions{
		clock:      func() time.Time { return testInstant },
		random:     bytes.NewReader(make([]byte, 12)),
		migrations: embeddedMigrations,
		afterPreflight: func() error {
			if err := os.Rename(path, path+".preflight"); err != nil {
				return err
			}
			return os.WriteFile(path, nil, 0o600)
		},
	})
	if !errors.Is(err, ErrDatabaseChanged) {
		t.Fatalf("open() error = %v, want ErrDatabaseChanged", err)
	}

	info, statErr := os.Stat(path)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if info.Size() != 0 {
		t.Fatalf("replacement database size = %d, want zero after rejected revalidation", info.Size())
	}
}

func TestWritableRevalidationRejectsInitializedEmptySQLiteAfterFreshPreflight(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database.db")
	_, err := open(context.Background(), path, testMasterKey(), openOptions{
		clock:      func() time.Time { return testInstant },
		random:     bytes.NewReader(make([]byte, 12)),
		migrations: embeddedMigrations,
		afterWritablePathPrepared: func() error {
			before, statErr := os.Stat(path)
			if statErr != nil {
				return statErr
			}
			raw, openErr := sql.Open("sqlite", sqliteFileURI(path, nil))
			if openErr != nil {
				return openErr
			}
			if _, vacuumErr := raw.Exec("VACUUM"); vacuumErr != nil {
				raw.Close()
				return vacuumErr
			}
			if closeErr := raw.Close(); closeErr != nil {
				return closeErr
			}
			after, statErr := os.Stat(path)
			if statErr != nil {
				return statErr
			}
			if !os.SameFile(before, after) {
				return fmt.Errorf("test setup replaced the database inode")
			}
			return nil
		},
	})
	if !errors.Is(err, ErrDatabaseChanged) {
		t.Fatalf("open() error = %v, want ErrDatabaseChanged", err)
	}

	raw := openRawDatabase(t, path)
	transaction, transactionErr := raw.BeginTx(context.Background(), nil)
	if transactionErr != nil {
		t.Fatal(transactionErr)
	}
	if _, validationErr := validateWritable(
		context.Background(), transaction, testMasterKey(), embeddedMigrations, false,
	); !errors.Is(validationErr, ErrForeignDatabase) {
		transaction.Rollback()
		t.Fatalf("validateWritable(initialized empty SQLite) error = %v, want ErrForeignDatabase", validationErr)
	}
	if rollbackErr := transaction.Rollback(); rollbackErr != nil {
		t.Fatal(rollbackErr)
	}
	var objects int
	if err := raw.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'").Scan(&objects); err != nil {
		t.Fatal(err)
	}
	if objects != 0 {
		t.Fatalf("foreign empty SQLite gained %d schema objects", objects)
	}
}

func TestSchemaValidationTracksAppliedMigrationVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database.db")
	v1Database, err := open(context.Background(), path, testMasterKey(), openOptions{
		clock:      func() time.Time { return testInstant },
		random:     bytes.NewReader(make([]byte, 12)),
		migrations: embeddedMigrations,
	})
	if err != nil {
		t.Fatalf("open(v1) error = %v", err)
	}
	if err := v1Database.Close(); err != nil {
		t.Fatal(err)
	}

	validateSchemaV2 := func(ctx context.Context, query queryer) error {
		if err := validateSchemaV1(ctx, query); err != nil {
			return err
		}
		exists, err := tableExists(ctx, query, "migration_v2_marker")
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("migration v2 marker is missing")
		}
		return nil
	}
	migrations := append([]migration(nil), embeddedMigrations...)
	migrations = append(migrations, newMigrationWithValidator(2, `
		CREATE TABLE migration_v2_marker (
			id INTEGER PRIMARY KEY,
			created_at TEXT NOT NULL
		);`, validateSchemaV2))

	database, err := open(context.Background(), path, testMasterKey(), openOptions{
		clock:      func() time.Time { return testInstant.Add(time.Hour) },
		random:     bytes.NewReader(make([]byte, 12)),
		migrations: migrations,
	})
	if err != nil {
		t.Fatalf("open() error = %v", err)
	}
	var v1AppliedAt string
	if err := database.sql.QueryRow("SELECT applied_at FROM schema_migrations WHERE version = 1").Scan(&v1AppliedAt); err != nil {
		t.Fatal(err)
	}
	if v1AppliedAt != testInstant.Format(time.RFC3339Nano) {
		t.Fatalf("v1 applied_at changed during upgrade: %q", v1AppliedAt)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	result, err := preflight(context.Background(), path, testMasterKey(), migrations)
	if err != nil {
		t.Fatalf("preflight(v2) error = %v", err)
	}
	if result.SchemaVersion != 2 {
		t.Fatalf("preflight(v2) version = %d", result.SchemaVersion)
	}

	raw := openRawDatabase(t, path)
	if _, err := raw.Exec("DROP TABLE migration_v2_marker"); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := preflight(context.Background(), path, testMasterKey(), migrations); !errors.Is(err, ErrCorruptDatabase) {
		t.Fatalf("preflight(malformed v2) error = %v, want ErrCorruptDatabase", err)
	}
}

func TestMigrationSetRejectsNonContiguousOrDivergentDefinitions(t *testing.T) {
	tests := []struct {
		name       string
		migrations []migration
	}{
		{name: "noncontiguous", migrations: []migration{newMigrationWithValidator(2, "SELECT 1", validateSchemaV1)}},
		{name: "divergent checksum", migrations: []migration{{version: 1, sql: "SELECT 1", checksum: "bad"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := open(context.Background(), filepath.Join(t.TempDir(), "database.db"), testMasterKey(), openOptions{
				clock: time.Now, random: bytes.NewReader(make([]byte, 12)), migrations: test.migrations,
			})
			if err == nil {
				t.Fatal("open() error = nil")
			}
		})
	}
}

func TestValidationRejectsNonContiguousStoredHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database.db")
	database := openTestDatabase(t, path)
	extraMigrations := append([]migration(nil), embeddedMigrations...)
	extraMigrations = append(
		extraMigrations,
		newMigrationWithValidator(2, "SELECT 2", validateSchemaV1),
		newMigrationWithValidator(3, "SELECT 3", validateSchemaV1),
	)
	if _, err := database.sql.Exec(
		"INSERT INTO schema_migrations(version, checksum, applied_at) VALUES (3, ?, ?)",
		extraMigrations[2].checksum, testInstant.Format(time.RFC3339Nano),
	); err != nil {
		t.Fatal(err)
	}

	_, err := preflight(context.Background(), path, testMasterKey(), extraMigrations)
	if !errors.Is(err, ErrCorruptDatabase) {
		t.Fatalf("preflight() error = %v, want ErrCorruptDatabase", err)
	}
}
