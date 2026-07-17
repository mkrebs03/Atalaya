package database

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPreflightClassifiesForeignCorruptAndFuture(t *testing.T) {
	t.Run("foreign SQLite", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "foreign.db")
		raw := openRawDatabase(t, path)
		if _, err := raw.Exec("CREATE TABLE something_else (id INTEGER)"); err != nil {
			t.Fatal(err)
		}
		raw.Close()
		assertPreflightError(t, path, ErrForeignDatabase)
	})

	t.Run("foreign bytes", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "foreign.db")
		if err := os.WriteFile(path, []byte("not sqlite"), 0o600); err != nil {
			t.Fatal(err)
		}
		assertPreflightError(t, path, ErrForeignDatabase)
	})

	t.Run("checksum divergence", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "corrupt.db")
		database := openTestDatabase(t, path)
		if _, err := database.sql.Exec("UPDATE schema_migrations SET checksum = ?", "0bad"); err != nil {
			t.Fatal(err)
		}
		assertPreflightError(t, path, ErrCorruptDatabase)
	})

	t.Run("missing verifier", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "corrupt.db")
		database := openTestDatabase(t, path)
		if _, err := database.sql.Exec("DELETE FROM instance_metadata"); err != nil {
			t.Fatal(err)
		}
		assertPreflightError(t, path, ErrCorruptDatabase)
	})

	t.Run("missing required table", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "corrupt.db")
		database := openTestDatabase(t, path)
		if _, err := database.sql.Exec("DROP TABLE administrators"); err != nil {
			t.Fatal(err)
		}
		assertPreflightError(t, path, ErrCorruptDatabase)
	})

	t.Run("missing username uniqueness", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "corrupt.db")
		database := openTestDatabase(t, path)
		if _, err := database.sql.Exec(`
			DROP INDEX administrators_one_active;
			ALTER TABLE administrators RENAME TO administrators_old;
			CREATE TABLE administrators (
				id TEXT PRIMARY KEY NOT NULL,
				username TEXT NOT NULL,
				password_hash TEXT NOT NULL,
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				password_changed_at TEXT NOT NULL,
				disabled_at TEXT,
				version INTEGER NOT NULL
			);
			CREATE UNIQUE INDEX administrators_one_active
			ON administrators ((1))
			WHERE disabled_at IS NULL;
			DROP TABLE administrators_old;`); err != nil {
			t.Fatal(err)
		}
		assertPreflightError(t, path, ErrCorruptDatabase)
	})

	t.Run("generated extra column", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "corrupt.db")
		database := openTestDatabase(t, path)
		if _, err := database.sql.Exec(`
			ALTER TABLE administrators
			ADD COLUMN generated_username TEXT
			GENERATED ALWAYS AS (username) VIRTUAL`); err != nil {
			t.Fatal(err)
		}
		assertPreflightError(t, path, ErrCorruptDatabase)
	})

	t.Run("future", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "future.db")
		database := openTestDatabase(t, path)
		if _, err := database.sql.Exec(
			"INSERT INTO schema_migrations(version, checksum, applied_at) VALUES (3, ?, ?)",
			"future", testInstant.Format("2006-01-02T15:04:05Z07:00"),
		); err != nil {
			t.Fatal(err)
		}
		assertPreflightError(t, path, ErrFutureSchema)
	})

	t.Run("future without prior versions", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "future-only.db")
		raw := openRawDatabase(t, path)
		if _, err := raw.Exec(`
			CREATE TABLE schema_migrations (
				version INTEGER PRIMARY KEY,
				checksum TEXT NOT NULL,
				applied_at TEXT NOT NULL
			);
			INSERT INTO schema_migrations VALUES (3, 'future', '2026-07-16T12:09:10Z')`); err != nil {
			t.Fatal(err)
		}
		raw.Close()
		assertPreflightError(t, path, ErrFutureSchema)
	})
}

func TestPreflightPreservesCanceledContextClassification(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Preflight(ctx, filepath.Join(t.TempDir(), "database.db"), testMasterKey())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Preflight() error = %v, want context.Canceled", err)
	}
	if errors.Is(err, ErrForeignDatabase) || errors.Is(err, ErrCorruptDatabase) {
		t.Fatalf("Preflight() mislabeled cancellation: %v", err)
	}
}

func TestSchemaValidationPreservesCancellationDuringSQLiteQuery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database.db")
	database := openTestDatabase(t, path)

	transaction, err := database.sql.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = validateSchemaV1(ctx, transaction)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("validateSchemaV1() error = %v, want context.Canceled", err)
	}
	if errors.Is(err, ErrCorruptDatabase) {
		t.Fatalf("validateSchemaV1() mislabeled cancellation: %v", err)
	}
}

func TestOperationalSQLiteErrorsAreNotClassifiedAsCorruption(t *testing.T) {
	cause := testSQLiteCodeError(5)
	err := corruptQueryError(context.Background(), "read schema", cause)
	if errors.Is(err, ErrCorruptDatabase) {
		t.Fatalf("corruptQueryError() mislabeled operational error: %v", err)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("corruptQueryError() did not preserve cause: %v", err)
	}
}

func TestValidationUsesSingleReadSnapshotAcrossConcurrentWALWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database.db")
	database := openTestDatabase(t, path)
	if _, err := database.sql.Exec("PRAGMA wal_autocheckpoint=0"); err != nil {
		t.Fatal(err)
	}

	reader, err := sql.Open("sqlite", readOnlyURI(path))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	reader.SetMaxOpenConns(1)
	transaction, err := reader.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()

	var version int
	if err := transaction.QueryRow("SELECT max(version) FROM schema_migrations").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if _, err := database.sql.Exec("UPDATE schema_migrations SET checksum = 'concurrent-change' WHERE version = 1"); err != nil {
		t.Fatal(err)
	}

	validation, err := validateAtalaya(context.Background(), transaction, testMasterKey(), embeddedMigrations)
	if err != nil {
		t.Fatalf("validateAtalaya(snapshot) error = %v", err)
	}
	if validation.version != 2 {
		t.Fatalf("validateAtalaya(snapshot) version = %d", validation.version)
	}
}

func TestWrongMasterKeyDoesNotChangeMainFileOrLogicalRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database.db")
	database := openTestDatabase(t, path)
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	beforeRows := logicalRowCount(t, path)

	wrongKey := bytes.Repeat([]byte{0x24}, 32)
	opened, err := Open(context.Background(), path, wrongKey)
	if opened != nil {
		opened.Close()
		t.Fatal("Open() returned a database with the wrong key")
	}
	if !errors.Is(err, ErrWrongMasterKey) {
		t.Fatalf("Open() error = %v, want ErrWrongMasterKey", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("wrong-key preflight changed the main database file")
	}
	if afterRows := logicalRowCount(t, path); afterRows != beforeRows {
		t.Fatalf("logical row count changed from %d to %d", beforeRows, afterRows)
	}
}

type testSQLiteCodeError int

func (err testSQLiteCodeError) Error() string { return "test SQLite error" }
func (err testSQLiteCodeError) Code() int     { return int(err) }

func TestWrongMasterKeyWithLiveWALDoesNotChangeMainFileOrLogicalRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database.db")
	database := openTestDatabase(t, path)
	if _, err := database.sql.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.sql.Exec("PRAGMA wal_autocheckpoint=0"); err != nil {
		t.Fatal(err)
	}
	administrator := testAdministrator(
		"00000000-0000-4000-8000-000000000055", "admin", testPasswordPHC,
	)
	if err := database.CreateAdministrator(context.Background(), administrator); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + "-wal"); err != nil {
		t.Fatalf("WAL sidecar absent: %v", err)
	}

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	beforeRows := logicalRowCount(t, path)
	wrongKey := bytes.Repeat([]byte{0x24}, 32)
	opened, err := Open(context.Background(), path, wrongKey)
	if opened != nil {
		opened.Close()
		t.Fatal("Open() returned a database with the wrong key and live WAL")
	}
	if !errors.Is(err, ErrWrongMasterKey) {
		t.Fatalf("Open() error = %v, want ErrWrongMasterKey", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("wrong-key preflight with live WAL changed the main database file")
	}
	if afterRows := logicalRowCount(t, path); afterRows != beforeRows {
		t.Fatalf("logical row count changed from %d to %d", beforeRows, afterRows)
	}
}

func TestPreflightReadsCurrentDatabaseWithWALSidecars(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database.db")
	database := openTestDatabase(t, path)
	if _, err := database.sql.Exec("PRAGMA wal_autocheckpoint=0"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.sql.Exec(`
		INSERT INTO administrators(
			id, username, password_hash, created_at, updated_at,
			password_changed_at, disabled_at, version
		) VALUES ('00000000-0000-4000-8000-000000000066', 'admin', ?, ?, ?, ?, NULL, 1)`,
		testPasswordPHC,
		testInstant.Format("2006-01-02T15:04:05.999999999Z07:00"),
		testInstant.Format("2006-01-02T15:04:05.999999999Z07:00"),
		testInstant.Format("2006-01-02T15:04:05.999999999Z07:00"),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + "-wal"); err != nil {
		t.Fatalf("WAL sidecar absent: %v", err)
	}

	result, err := Preflight(context.Background(), path, testMasterKey())
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if !result.HasActiveAdministrator {
		t.Fatal("Preflight() did not observe administrator in WAL")
	}
}

func TestPreflightReadsCurrentDatabaseWithoutWALSidecars(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database.db")
	database := openTestDatabase(t, path)
	if _, err := database.sql.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(path + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
	}

	result, err := Preflight(context.Background(), path, testMasterKey())
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if result.State != StateAtalaya || result.SchemaVersion != 2 {
		t.Fatalf("Preflight() = %+v", result)
	}
}

func assertPreflightError(t *testing.T, path string, expected error) {
	t.Helper()
	_, err := Preflight(context.Background(), path, testMasterKey())
	if !errors.Is(err, expected) {
		t.Fatalf("Preflight() error = %v, want %v", err, expected)
	}
}

func logicalRowCount(t *testing.T, path string) int {
	t.Helper()
	database, err := sql.Open("sqlite", readOnlyURI(path))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var count int
	if err := database.QueryRow(`
		SELECT
			(SELECT count(*) FROM schema_migrations) +
			(SELECT count(*) FROM instance_metadata) +
			(SELECT count(*) FROM administrators)`,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
