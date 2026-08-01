package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	atalayacrypto "github.com/mkrebs03/atalaya/internal/crypto"
	"github.com/mkrebs03/atalaya/internal/system"
)

// Verification is the safe result of a read-only database verification.
type Verification struct {
	SchemaVersion          int
	DatabaseSizeBytes      int64
	HasActiveAdministrator bool
}

// Verify validates an existing Atalaya database without migrating or writing
// to it. It is deliberately separate from Open, whose contract includes
// migrations and WAL setup.
func Verify(ctx context.Context, path string, key []byte) (Verification, error) {
	if len(key) != 32 {
		return Verification{}, fmt.Errorf("master key must contain exactly 32 bytes")
	}
	preflight, err := Preflight(ctx, path, key)
	if err != nil {
		return Verification{}, err
	}
	if preflight.State != StateAtalaya || preflight.SchemaVersion != len(embeddedMigrations) {
		return Verification{}, fmt.Errorf("database schema is not current")
	}

	info, err := os.Stat(path)
	if err != nil {
		return Verification{}, fmt.Errorf("inspect database after verification: %w", err)
	}
	db, err := sql.Open("sqlite", readOnlyURI(path))
	if err != nil {
		return Verification{}, fmt.Errorf("open database for verification: %w", err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()

	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Verification{}, fmt.Errorf("begin database verification: %w", err)
	}
	defer tx.Rollback()
	if err := verifySQLiteIntegrity(ctx, tx); err != nil {
		return Verification{}, err
	}
	validation, err := validateAtalaya(ctx, tx, key, embeddedMigrations)
	if err != nil {
		return Verification{}, err
	}
	if err := verifyEncryptedSecrets(ctx, tx, key); err != nil {
		return Verification{}, err
	}
	if err := tx.Commit(); err != nil {
		return Verification{}, fmt.Errorf("commit database verification: %w", err)
	}
	return Verification{
		SchemaVersion:          validation.version,
		DatabaseSizeBytes:      info.Size(),
		HasActiveAdministrator: validation.activeAdministrators > 0,
	}, nil
}

func verifySQLiteIntegrity(ctx context.Context, query queryer) error {
	var result string
	if err := query.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return fmt.Errorf("database integrity check: %w", err)
	}
	if result != "ok" {
		return corruptError("SQLite integrity check failed", nil)
	}
	rows, err := query.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("database foreign-key check: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		return corruptError("SQLite foreign-key check failed", nil)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("database foreign-key check: %w", err)
	}
	return nil
}

func verifyEncryptedSecrets(ctx context.Context, query queryer, key []byte) error {
	rows, err := query.QueryContext(ctx, "SELECT id, agent_credential FROM hosts")
	if err != nil {
		return corruptQueryError(ctx, "read encrypted agent credentials", err)
	}
	for rows.Next() {
		var id string
		var envelope []byte
		if err := rows.Scan(&id, &envelope); err != nil {
			rows.Close()
			return corruptQueryError(ctx, "decode encrypted agent credential", err)
		}
		plaintext, err := atalayacrypto.Open(key, "agent", id, envelope)
		clear(plaintext)
		if err != nil {
			rows.Close()
			return corruptError("encrypted agent credential cannot be decrypted", nil)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return corruptQueryError(ctx, "read encrypted agent credentials", err)
	}
	if err := rows.Close(); err != nil {
		return corruptQueryError(ctx, "close encrypted agent credentials", err)
	}

	rows, err = query.QueryContext(ctx, "SELECT id, encrypted_config FROM notification_channels")
	if err != nil {
		return corruptQueryError(ctx, "read encrypted notification credentials", err)
	}
	for rows.Next() {
		var id string
		var envelope []byte
		if err := rows.Scan(&id, &envelope); err != nil {
			rows.Close()
			return corruptQueryError(ctx, "decode encrypted notification credential", err)
		}
		plaintext, err := atalayacrypto.Open(key, "notification-channel", id, envelope)
		clear(plaintext)
		if err != nil {
			rows.Close()
			return corruptError("encrypted notification credential cannot be decrypted", nil)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return corruptQueryError(ctx, "read encrypted notification credentials", err)
	}
	if err := rows.Close(); err != nil {
		return corruptQueryError(ctx, "close encrypted notification credentials", err)
	}
	return nil
}

// Snapshot creates a consistent SQLite snapshot with VACUUM INTO. The target
// must not already exist and is returned with mode 0600.
func Snapshot(ctx context.Context, path, target string) error {
	if path == "" || target == "" {
		return fmt.Errorf("database and snapshot paths are required")
	}
	sourceAbs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve database path: %w", err)
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve snapshot path: %w", err)
	}
	if sourceAbs == targetAbs {
		return fmt.Errorf("snapshot path must differ from database path")
	}
	if _, err := os.Lstat(targetAbs); err == nil {
		return fmt.Errorf("snapshot path already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect snapshot path: %w", err)
	}
	if _, err := os.Stat(sourceAbs); err != nil {
		return fmt.Errorf("inspect database path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(targetAbs), 0o700); err != nil {
		return fmt.Errorf("prepare snapshot directory: %w", err)
	}

	db, err := sql.Open("sqlite", writableURI(sourceAbs))
	if err != nil {
		return fmt.Errorf("open database for snapshot: %w", err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", targetAbs); err != nil {
		_ = os.Remove(targetAbs)
		return fmt.Errorf("create consistent SQLite snapshot: %w", err)
	}
	if err := os.Chmod(targetAbs, 0o600); err != nil {
		_ = os.Remove(targetAbs)
		return fmt.Errorf("restrict SQLite snapshot: %w", err)
	}
	return nil
}

// SystemStatus implements the authenticated operational status endpoint.
func (database *Database) SystemStatus(ctx context.Context, version string) (system.Status, error) {
	if database == nil || database.sql == nil {
		return system.Status{}, fmt.Errorf("database is closed")
	}
	info, err := os.Stat(database.path)
	if err != nil {
		return system.Status{}, fmt.Errorf("inspect database size: %w", err)
	}
	tx, err := database.sql.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return system.Status{}, fmt.Errorf("begin system status: %w", err)
	}
	defer tx.Rollback()
	validation, err := validateAtalaya(ctx, tx, database.key, database.migrations)
	if err != nil {
		return system.Status{}, err
	}
	status := system.Status{
		Version:           version,
		SchemaVersion:     validation.version,
		DatabaseSizeBytes: info.Size(),
		Collection:        system.WorkerStatus{State: "unknown", ErrorCode: "not_started"},
		Retention:         system.WorkerStatus{State: "unknown", ErrorCode: "not_started"},
		Checks:            system.WorkerStatus{State: "unknown", ErrorCode: "not_started"},
		Notifications:     system.WorkerStatus{State: "unknown", ErrorCode: "not_started"},
	}
	if err := readCollectionStatus(ctx, tx, &status); err != nil {
		return system.Status{}, err
	}
	if err := readRetentionStatus(ctx, tx, &status.Retention); err != nil {
		return system.Status{}, err
	}
	if err := readCheckStatus(ctx, tx, &status.Checks); err != nil {
		return system.Status{}, err
	}
	if err := readNotificationStatus(ctx, tx, &status.Notifications); err != nil {
		return system.Status{}, err
	}
	status.LastHeartbeatAt = latestHeartbeat(status.Collection.LastHeartbeatAt, status.Retention.LastHeartbeatAt, status.Checks.LastHeartbeatAt, status.Notifications.LastHeartbeatAt)
	if err := tx.Commit(); err != nil {
		return system.Status{}, fmt.Errorf("commit system status: %w", err)
	}
	return status, nil
}

func readCollectionStatus(ctx context.Context, tx *sql.Tx, status *system.Status) error {
	var targets, successes, fresh, healthy int
	var heartbeat sql.NullString
	now := dbTime(time.Now().UTC())
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*),
		       count(last_success_received_at),
		       coalesce(sum(CASE WHEN last_success_received_at IS NOT NULL AND stale=0 AND freshness_deadline_at IS NOT NULL AND freshness_deadline_at>? THEN 1 ELSE 0 END),0),
		       coalesce(sum(CASE WHEN last_success_received_at IS NOT NULL AND stale=0 AND freshness_deadline_at IS NOT NULL AND freshness_deadline_at>? AND observed_state='healthy' THEN 1 ELSE 0 END),0),
		       max(last_success_received_at)
		FROM hosts
		WHERE archived_at IS NULL AND administrative_state IN ('pending','active')`, now, now).Scan(&targets, &successes, &fresh, &healthy, &heartbeat); err != nil {
		return fmt.Errorf("read collection status: %w", err)
	}
	if heartbeat.Valid {
		status.Collection.LastHeartbeatAt = canonicalStatusTime(heartbeat.String)
	}
	switch {
	case targets == 0:
		status.Collection.State, status.Collection.ErrorCode = "idle", "no_targets"
	case successes == 0:
		status.Collection.State, status.Collection.ErrorCode = "unknown", "no_success"
	case fresh < targets || healthy < targets:
		status.Collection.State, status.Collection.ErrorCode = "degraded", "target_unavailable"
	default:
		status.Collection.State, status.Collection.ErrorCode = "healthy", ""
	}
	return nil
}

func readRetentionStatus(ctx context.Context, tx *sql.Tx, status *system.WorkerStatus) error {
	var heartbeat sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT updated_at FROM retention_progress WHERE id=1").Scan(&heartbeat); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			status.State, status.ErrorCode = "unknown", "not_started"
			return nil
		}
		return fmt.Errorf("read retention status: %w", err)
	}
	if heartbeat.Valid {
		status.LastHeartbeatAt = canonicalStatusTime(heartbeat.String)
	}
	if heartbeat.Valid && heartbeat.String == "1970-01-01T00:00:00Z" {
		status.State, status.ErrorCode = "unknown", "not_started"
		return nil
	}
	status.State, status.ErrorCode = "healthy", ""
	return nil
}

func readCheckStatus(ctx context.Context, tx *sql.Tx, status *system.WorkerStatus) error {
	var checks, results, fresh, down int
	var heartbeat sql.NullString
	now := dbTime(time.Now().UTC())
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*),
		       count(last_result_at),
		       coalesce(sum(CASE WHEN last_result_at IS NOT NULL AND freshness_deadline_at IS NOT NULL AND freshness_deadline_at>? AND observed_state IN ('up','down') THEN 1 ELSE 0 END),0),
		       coalesce(sum(CASE WHEN last_result_at IS NOT NULL AND freshness_deadline_at IS NOT NULL AND freshness_deadline_at>? AND observed_state='down' THEN 1 ELSE 0 END),0),
		       max(last_result_at)
		FROM service_checks
		WHERE archived_at IS NULL AND enabled=1`, now, now).Scan(&checks, &results, &fresh, &down, &heartbeat); err != nil {
		return fmt.Errorf("read checks status: %w", err)
	}
	if heartbeat.Valid {
		status.LastHeartbeatAt = canonicalStatusTime(heartbeat.String)
	}
	switch {
	case checks == 0:
		status.State, status.ErrorCode = "idle", "no_checks"
	case results == 0:
		status.State, status.ErrorCode = "unknown", "no_result"
	case fresh < checks:
		status.State, status.ErrorCode = "degraded", "stale"
	case down > 0:
		status.State, status.ErrorCode = "degraded", "check_failed"
	default:
		status.State, status.ErrorCode = "healthy", ""
	}
	return nil
}

func readNotificationStatus(ctx context.Context, tx *sql.Tx, status *system.WorkerStatus) error {
	var channels, failed int
	var heartbeat sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM notification_channels WHERE archived_at IS NULL AND enabled=1`).Scan(&channels); err != nil {
		return fmt.Errorf("read notification channels status: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*),max(updated_at) FROM notification_deliveries WHERE state='failed'`).Scan(&failed, &heartbeat); err != nil {
		return fmt.Errorf("read notification delivery status: %w", err)
	}
	if heartbeat.Valid {
		status.LastHeartbeatAt = canonicalStatusTime(heartbeat.String)
	}
	switch {
	case channels == 0:
		status.State, status.ErrorCode = "idle", "no_channels"
	case failed > 0:
		status.State, status.ErrorCode = "degraded", "delivery_failed"
	default:
		status.State, status.ErrorCode = "healthy", ""
	}
	return nil
}

func canonicalStatusTime(value string) *string {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil
	}
	formatted := parsed.UTC().Format(time.RFC3339Nano)
	return &formatted
}

func latestHeartbeat(values ...*string) *string {
	var latest *string
	var latestTime time.Time
	for _, value := range values {
		if value == nil {
			continue
		}
		parsed, err := time.Parse(time.RFC3339Nano, *value)
		if err != nil || latest != nil && !parsed.After(latestTime) {
			continue
		}
		copy := *value
		latest, latestTime = &copy, parsed
	}
	return latest
}
