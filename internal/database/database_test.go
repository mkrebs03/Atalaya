package database

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

var testInstant = time.Date(2026, time.July, 16, 12, 9, 10, 123, time.UTC)

const testPasswordPHC = "$argon2id$v=19$m=65536,t=3,p=4$AAECAwQFBgcICQoLDA0ODw$hTsnKkTbFCHAKWJmmlXrCZTzyrOF7RxMeSU+7hm6tJ4"

func testMasterKey() []byte {
	return bytes.Repeat([]byte{0x42}, 32)
}

func openTestDatabase(t *testing.T, path string) *Database {
	t.Helper()
	database, err := open(context.Background(), path, testMasterKey(), openOptions{
		clock:      func() time.Time { return testInstant },
		random:     bytes.NewReader(make([]byte, 12)),
		migrations: embeddedMigrations,
	})
	if err != nil {
		t.Fatalf("open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return database
}

func openRawDatabase(t *testing.T, path string) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", sqliteFileURI(path, nil))
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func TestOpenMigratesFreshDatabaseAndIsIdempotent(t *testing.T) {
	for _, test := range []struct {
		name      string
		makeEmpty bool
	}{
		{name: "missing"},
		{name: "zero length", makeEmpty: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "database.db")
			if test.makeEmpty {
				if err := os.WriteFile(path, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			database := openTestDatabase(t, path)

			var checksum, appliedAt string
			if err := database.sql.QueryRow(
				"SELECT checksum, applied_at FROM schema_migrations WHERE version = 1",
			).Scan(&checksum, &appliedAt); err != nil {
				t.Fatalf("read migration: %v", err)
			}
			if checksum != embeddedMigrations[0].checksum || appliedAt != testInstant.Format(time.RFC3339Nano) {
				t.Fatalf("migration = checksum %q at %q", checksum, appliedAt)
			}
			var sentinel []byte
			if err := database.sql.QueryRow(
				"SELECT value FROM instance_metadata WHERE key = ?", masterKeyVerifierName,
			).Scan(&sentinel); err != nil {
				t.Fatalf("read sentinel: %v", err)
			}
			if len(sentinel) < 13 || !bytes.Equal(sentinel[1:13], make([]byte, 12)) {
				t.Fatalf("sentinel nonce = %x, want deterministic zeros", sentinel)
			}

			result, err := Preflight(context.Background(), path, testMasterKey())
			if err != nil {
				t.Fatalf("Preflight() error = %v", err)
			}
			if result.State != StateAtalaya || result.SchemaVersion != len(embeddedMigrations) || result.HasActiveAdministrator {
				t.Fatalf("Preflight() = %+v", result)
			}

			second, err := Open(context.Background(), path, testMasterKey())
			if err != nil {
				t.Fatalf("second Open() error = %v", err)
			}
			defer second.Close()
			var secondAppliedAt string
			if err := second.sql.QueryRow("SELECT applied_at FROM schema_migrations WHERE version = 1").Scan(&secondAppliedAt); err != nil {
				t.Fatal(err)
			}
			if secondAppliedAt != appliedAt {
				t.Fatalf("idempotent open changed applied_at from %q to %q", appliedAt, secondAppliedAt)
			}
		})
	}
}

func TestOpenAppliesPragmasOnEveryConnection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pragmas #1?.db")
	database := openTestDatabase(t, path)
	database.sql.SetMaxOpenConns(2)

	first, err := database.sql.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := database.sql.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	for index, connection := range []*sql.Conn{first, second} {
		var foreignKeys, busyTimeout int
		var journalMode string
		if err := connection.QueryRowContext(context.Background(), "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
			t.Fatal(err)
		}
		if err := connection.QueryRowContext(context.Background(), "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
			t.Fatal(err)
		}
		if err := connection.QueryRowContext(context.Background(), "PRAGMA journal_mode").Scan(&journalMode); err != nil {
			t.Fatal(err)
		}
		if foreignKeys != 1 || busyTimeout != 5000 || journalMode != "wal" {
			t.Fatalf("connection %d pragmas = foreign_keys %d, busy_timeout %d, journal_mode %q", index, foreignKeys, busyTimeout, journalMode)
		}
	}
}

func TestOpenRejectsInvalidKeyLengthWithoutCreatingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database.db")
	if _, err := Open(context.Background(), path, make([]byte, 31)); err == nil {
		t.Fatal("Open() error = nil")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("database path exists or stat failed: %v", err)
	}
}
