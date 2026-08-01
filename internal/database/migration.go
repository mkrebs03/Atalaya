package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"time"

	atalayacrypto "github.com/mkrebs03/atalaya/internal/crypto"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type migration struct {
	version  int
	sql      string
	checksum string
	validate schemaValidator
}

type schemaValidator func(context.Context, queryer) error

var embeddedMigrations = mustLoadMigrations()
var hostMigrationSQL = mustReadMigration("migrations/003_hosts.sql")
var enrollmentMigrationSQL = mustReadMigration("migrations/004_enrollment.sql")
var metricsMigrationSQL = mustReadMigration("migrations/005_metrics.sql")
var retentionProgressMigrationSQL = mustReadMigration("migrations/006_retention_progress.sql")
var checksMigrationSQL = mustReadMigration("migrations/007_checks.sql")

func mustReadMigration(name string) string {
	contents, err := migrationFiles.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return string(contents)
}

func mustLoadMigrations() []migration {
	initial, err := migrationFiles.ReadFile("migrations/001_initial.sql")
	if err != nil {
		panic(err)
	}
	sessions, err := migrationFiles.ReadFile("migrations/002_sessions.sql")
	if err != nil {
		panic(err)
	}
	return []migration{
		newMigrationWithValidator(1, string(initial), validateSchemaV1),
		newMigrationWithValidator(2, string(sessions), validateSchemaV2),
		newMigrationWithValidator(3, hostMigrationSQL, validateSchemaV3),
		newMigrationWithValidator(4, enrollmentMigrationSQL, validateSchemaV4),
		newMigrationWithValidator(5, metricsMigrationSQL, validateSchemaV5),
		newMigrationWithValidator(6, retentionProgressMigrationSQL, validateSchemaV6),
		newMigrationWithValidator(7, checksMigrationSQL, validateSchemaV7),
	}
}

func newMigrationWithValidator(version int, statement string, validate schemaValidator) migration {
	return migration{
		version: version, sql: statement, checksum: migrationChecksum(statement), validate: validate,
	}
}

func migrationChecksum(statement string) string {
	digest := sha256.Sum256([]byte(statement))
	return hex.EncodeToString(digest[:])
}

func validateMigrationSet(migrations []migration) error {
	for index, current := range migrations {
		if current.version != index+1 {
			return fmt.Errorf("embedded migrations must have contiguous versions from 1")
		}
		if current.sql == "" || current.validate == nil || current.checksum != migrationChecksum(current.sql) {
			return fmt.Errorf("embedded migration %d has an invalid checksum", current.version)
		}
	}
	return nil
}

func (database *Database) applyMigrations(
	ctx context.Context,
	clock func() time.Time,
	random io.Reader,
	expected PreflightResult,
) error {
	expectedFile := database.file
	for _, current := range database.migrations {
		updated, err := database.applyMigration(ctx, current, clock, random, expected, expectedFile)
		if err != nil {
			return err
		}
		expected = updated
		expectedFile = nil
	}
	database.file = nil
	return nil
}

func (database *Database) applyMigration(
	ctx context.Context,
	current migration,
	clock func() time.Time,
	random io.Reader,
	expected PreflightResult,
	expectedFile os.FileInfo,
) (PreflightResult, error) {
	allowFreshInitializedPage := false
	if expectedFile != nil && expected.State == StateFresh {
		currentFile, statErr := os.Stat(database.path)
		if statErr != nil || !os.SameFile(expectedFile, currentFile) || currentFile.Size() != 0 {
			return PreflightResult{}, fmt.Errorf("revalidate migration %d: %w", current.version, ErrDatabaseChanged)
		}
		allowFreshInitializedPage = true
	}
	transaction, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return PreflightResult{}, fmt.Errorf("begin migration %d: %w", current.version, err)
	}
	defer transaction.Rollback()
	if expectedFile != nil {
		currentFile, statErr := os.Stat(database.path)
		if statErr != nil || !os.SameFile(expectedFile, currentFile) {
			return PreflightResult{}, fmt.Errorf("revalidate migration %d: %w", current.version, ErrDatabaseChanged)
		}
	}

	observed, err := validateWritable(
		ctx, transaction, database.key, database.migrations, allowFreshInitializedPage,
	)
	if err != nil {
		return PreflightResult{}, fmt.Errorf("revalidate migration %d: %w", current.version, err)
	}
	if err := requireExpectedState(expected, observed); err != nil {
		return PreflightResult{}, fmt.Errorf("revalidate migration %d: %w", current.version, err)
	}
	if observed.State == StateAtalaya && observed.SchemaVersion >= current.version {
		if err := transaction.Commit(); err != nil {
			return PreflightResult{}, fmt.Errorf("commit migration %d validation: %w", current.version, err)
		}
		return observed, nil
	}
	if current.version != observed.SchemaVersion+1 {
		return PreflightResult{}, fmt.Errorf(
			"migration %d cannot follow schema version %d", current.version, observed.SchemaVersion,
		)
	}

	if _, err := transaction.ExecContext(ctx, current.sql); err != nil {
		return PreflightResult{}, fmt.Errorf("execute migration %d: %w", current.version, err)
	}
	if current.version == 1 {
		if err := insertSentinel(ctx, transaction, database.key, random); err != nil {
			return PreflightResult{}, err
		}
	}
	appliedAt := clock().UTC().Format(time.RFC3339Nano)
	if _, err := transaction.ExecContext(ctx,
		"INSERT INTO schema_migrations(version, checksum, applied_at) VALUES (?, ?, ?)",
		current.version, current.checksum, appliedAt,
	); err != nil {
		return PreflightResult{}, fmt.Errorf("record migration %d: %w", current.version, err)
	}
	validated, err := validateAtalaya(ctx, transaction, database.key, database.migrations)
	if err != nil {
		return PreflightResult{}, fmt.Errorf("validate migration %d output: %w", current.version, err)
	}
	if validated.version != current.version {
		return PreflightResult{}, fmt.Errorf("validate migration %d output: recorded version %d", current.version, validated.version)
	}
	if err := transaction.Commit(); err != nil {
		return PreflightResult{}, fmt.Errorf("commit migration %d: %w", current.version, err)
	}
	return PreflightResult{
		State:                  StateAtalaya,
		SchemaVersion:          current.version,
		HasActiveAdministrator: validated.activeAdministrators != 0,
	}, nil
}

func insertSentinel(ctx context.Context, transaction *sql.Tx, key []byte, random io.Reader) error {
	envelope, err := atalayacrypto.SealWithRandom(
		key,
		atalayacrypto.SentinelSecretType,
		atalayacrypto.SentinelEntityID,
		[]byte(atalayacrypto.SentinelPlaintext),
		random,
	)
	if err != nil {
		return fmt.Errorf("create instance key verifier: %w", err)
	}
	if _, err := transaction.ExecContext(ctx,
		"INSERT INTO instance_metadata(key, value) VALUES (?, ?)",
		masterKeyVerifierName, envelope,
	); err != nil {
		return fmt.Errorf("store instance key verifier: %w", err)
	}
	return nil
}

func validateWritable(
	ctx context.Context,
	transaction *sql.Tx,
	key []byte,
	migrations []migration,
	allowFreshInitializedPage bool,
) (PreflightResult, error) {
	exists, err := tableExists(ctx, transaction, "schema_migrations")
	if err != nil {
		return PreflightResult{}, corruptQueryError(ctx, "inspect schema history", err)
	}
	if exists {
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

	var objects int
	if err := transaction.QueryRowContext(ctx,
		"SELECT count(*) FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'",
	).Scan(&objects); err != nil {
		return PreflightResult{}, corruptQueryError(ctx, "inspect fresh database", err)
	}
	if objects != 0 {
		return PreflightResult{}, ErrForeignDatabase
	}
	var pageCount int
	if err := transaction.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount); err != nil {
		return PreflightResult{}, corruptQueryError(ctx, "inspect fresh database pages", err)
	}
	if pageCount != 0 && !(allowFreshInitializedPage && pageCount == 1) {
		return PreflightResult{}, ErrForeignDatabase
	}
	return PreflightResult{State: StateFresh}, nil
}

func requireExpectedState(expected, observed PreflightResult) error {
	if expected != observed {
		return fmt.Errorf("%w: expected %+v, observed %+v", ErrDatabaseChanged, expected, observed)
	}
	return nil
}
