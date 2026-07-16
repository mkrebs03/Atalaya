package database

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	atalayacrypto "github.com/mkrebs03/atalaya/internal/crypto"
)

const masterKeyVerifierName = "master_key_verifier"

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type validationResult struct {
	version              int
	activeAdministrators int
}

func validateAtalaya(ctx context.Context, query queryer, key []byte, migrations []migration) (validationResult, error) {
	exists, err := tableExists(ctx, query, "schema_migrations")
	if err != nil {
		return validationResult{}, classifyInitialSchemaError(ctx, err)
	}
	if !exists {
		return validationResult{}, ErrForeignDatabase
	}
	if err := validateTable(ctx, query, "schema_migrations", []expectedColumn{
		{name: "version", typeName: "INTEGER", primaryKey: 1},
		{name: "checksum", typeName: "TEXT", notNull: true},
		{name: "applied_at", typeName: "TEXT", notNull: true},
	}); err != nil {
		return validationResult{}, corruptQueryError(ctx, "invalid schema history table", err)
	}

	version, err := validateHistory(ctx, query, migrations)
	if err != nil {
		return validationResult{}, err
	}
	if version < 1 {
		return validationResult{}, corruptError("schema history is empty", nil)
	}
	if version > len(migrations) {
		return validationResult{}, ErrFutureSchema
	}
	if err := migrations[version-1].validate(ctx, query); err != nil {
		return validationResult{}, corruptQueryError(ctx, fmt.Sprintf("invalid schema version %d", version), err)
	}
	if err := validateSentinel(ctx, query, key); err != nil {
		return validationResult{}, err
	}

	var active int
	if err := query.QueryRowContext(ctx,
		"SELECT count(*) FROM administrators WHERE disabled_at IS NULL",
	).Scan(&active); err != nil {
		return validationResult{}, corruptQueryError(ctx, "count active administrators", err)
	}
	if active > 1 {
		return validationResult{}, corruptError("multiple active administrators", nil)
	}
	return validationResult{version: version, activeAdministrators: active}, nil
}

func validateHistory(ctx context.Context, query queryer, migrations []migration) (int, error) {
	var maximum sql.NullInt64
	if err := query.QueryRowContext(ctx, "SELECT max(version) FROM schema_migrations").Scan(&maximum); err != nil {
		return 0, corruptQueryError(ctx, "read maximum schema version", err)
	}
	if maximum.Valid && maximum.Int64 > int64(len(migrations)) {
		return 0, ErrFutureSchema
	}

	rows, err := query.QueryContext(ctx,
		"SELECT version, checksum, applied_at FROM schema_migrations ORDER BY version",
	)
	if err != nil {
		return 0, corruptQueryError(ctx, "read schema history", err)
	}
	defer rows.Close()

	version := 0
	for rows.Next() {
		var storedVersion int
		var checksum, appliedAt string
		if err := rows.Scan(&storedVersion, &checksum, &appliedAt); err != nil {
			return 0, corruptQueryError(ctx, "decode schema history", err)
		}
		if storedVersion != version+1 {
			return 0, corruptError("schema versions are not contiguous", nil)
		}
		if storedVersion > len(migrations) {
			return 0, ErrFutureSchema
		}
		if checksum != migrations[storedVersion-1].checksum {
			return 0, corruptError("schema migration checksum differs", nil)
		}
		if !validTimestamp(appliedAt) {
			return 0, corruptError("schema migration timestamp is invalid", nil)
		}
		version = storedVersion
	}
	if err := rows.Err(); err != nil {
		return 0, corruptQueryError(ctx, "read schema history", err)
	}
	return version, nil
}

func validateSentinel(ctx context.Context, query queryer, key []byte) error {
	var envelope []byte
	if err := query.QueryRowContext(ctx,
		"SELECT value FROM instance_metadata WHERE key = ?", masterKeyVerifierName,
	).Scan(&envelope); err != nil {
		return corruptQueryError(ctx, "instance key verifier is missing", err)
	}
	if len(envelope) < 29 || envelope[0] != atalayacrypto.EnvelopeVersionV1 {
		return corruptError("instance key verifier is malformed", nil)
	}
	plaintext, err := atalayacrypto.Open(
		key, atalayacrypto.SentinelSecretType, atalayacrypto.SentinelEntityID, envelope,
	)
	if err != nil {
		return ErrWrongMasterKey
	}
	if !bytes.Equal(plaintext, []byte(atalayacrypto.SentinelPlaintext)) {
		return corruptError("instance key verifier plaintext differs", nil)
	}
	return nil
}

func tableExists(ctx context.Context, query queryer, table string) (bool, error) {
	var exists int
	err := query.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type = 'table' AND name = ?)", table,
	).Scan(&exists)
	return exists == 1, err
}

func validTimestamp(value string) bool {
	if !strings.HasSuffix(value, "Z") {
		return false
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && parsed.UTC().Format(time.RFC3339Nano) == value
}

func corruptError(message string, cause error) error {
	if errors.Is(cause, ErrCorruptDatabase) {
		return cause
	}
	if cause == nil {
		return fmt.Errorf("%w: %s", ErrCorruptDatabase, message)
	}
	return fmt.Errorf("%w: %s: %v", ErrCorruptDatabase, message, cause)
}

func corruptQueryError(ctx context.Context, message string, cause error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s: %w", message, err)
	}
	if isOperationalSQLiteError(cause) {
		return fmt.Errorf("%s: %w", message, cause)
	}
	return corruptError(message, cause)
}

func classifyInitialSchemaError(ctx context.Context, cause error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("inspect database schema: %w", err)
	}
	if sqlitePrimaryCode(cause) == 26 {
		return fmt.Errorf("%w: invalid SQLite file", ErrForeignDatabase)
	}
	return fmt.Errorf("inspect database schema: %w", cause)
}

func classifyPreflightOpenError(ctx context.Context, cause error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("open database preflight: %w", err)
	}
	if sqlitePrimaryCode(cause) == 26 {
		return fmt.Errorf("%w: invalid SQLite file", ErrForeignDatabase)
	}
	return fmt.Errorf("open database preflight: %w", cause)
}

func isOperationalSQLiteError(err error) bool {
	switch sqlitePrimaryCode(err) {
	case 5, 6, 7, 8, 9, 10, 13, 14, 15:
		return true
	default:
		return false
	}
}

func sqlitePrimaryCode(err error) int {
	type codedError interface{ Code() int }
	var coded codedError
	if !errors.As(err, &coded) {
		return 0
	}
	return coded.Code() & 0xff
}
