package database

import (
	"context"
	"fmt"
	"strings"
)

type expectedColumn struct {
	name       string
	typeName   string
	notNull    bool
	primaryKey int
}

func validateSchemaV1(ctx context.Context, query queryer) error {
	if err := validateTable(ctx, query, "instance_metadata", []expectedColumn{
		{name: "key", typeName: "TEXT", notNull: true, primaryKey: 1},
		{name: "value", typeName: "BLOB", notNull: true},
	}); err != nil {
		return corruptQueryError(ctx, "invalid instance metadata table", err)
	}
	if err := validateTable(ctx, query, "administrators", []expectedColumn{
		{name: "id", typeName: "TEXT", notNull: true, primaryKey: 1},
		{name: "username", typeName: "TEXT", notNull: true},
		{name: "password_hash", typeName: "TEXT", notNull: true},
		{name: "created_at", typeName: "TEXT", notNull: true},
		{name: "updated_at", typeName: "TEXT", notNull: true},
		{name: "password_changed_at", typeName: "TEXT", notNull: true},
		{name: "disabled_at", typeName: "TEXT"},
		{name: "version", typeName: "INTEGER", notNull: true},
	}); err != nil {
		return corruptQueryError(ctx, "invalid administrators table", err)
	}

	var indexSQL string
	if err := query.QueryRowContext(ctx,
		"SELECT sql FROM sqlite_schema WHERE type = 'index' AND name = 'administrators_one_active'",
	).Scan(&indexSQL); err != nil {
		return corruptQueryError(ctx, "missing active administrator singleton index", err)
	}
	const exactIndex = "CREATE UNIQUE INDEX administrators_one_active ON administrators ((1)) WHERE disabled_at IS NULL"
	if strings.Join(strings.Fields(indexSQL), " ") != exactIndex {
		return corruptError("active administrator singleton index differs", nil)
	}
	if err := validateUniqueUsernameIndex(ctx, query); err != nil {
		return corruptQueryError(ctx, "administrator username unique index differs", err)
	}
	return nil
}

func validateSchemaV2(ctx context.Context, query queryer) error {
	if err := validateSchemaV1(ctx, query); err != nil {
		return err
	}
	if err := validateTable(ctx, query, "sessions", []expectedColumn{
		{name: "id", typeName: "TEXT", notNull: true, primaryKey: 1},
		{name: "administrator_id", typeName: "TEXT", notNull: true},
		{name: "token_hash", typeName: "BLOB", notNull: true},
		{name: "created_at", typeName: "TEXT", notNull: true},
		{name: "last_activity_at", typeName: "TEXT", notNull: true},
		{name: "idle_expires_at", typeName: "TEXT", notNull: true},
		{name: "absolute_expires_at", typeName: "TEXT", notNull: true},
		{name: "revoked_at", typeName: "TEXT"},
		{name: "remote_addr", typeName: "TEXT", notNull: true},
		{name: "user_agent", typeName: "TEXT", notNull: true},
		{name: "tailscale_login", typeName: "TEXT"},
		{name: "tailscale_name", typeName: "TEXT"},
		{name: "proxy_identity_informational", typeName: "INTEGER", notNull: true},
	}); err != nil {
		return corruptQueryError(ctx, "invalid sessions table", err)
	}
	var tableSQL, indexSQL string
	if err := query.QueryRowContext(ctx, "SELECT sql FROM sqlite_schema WHERE type='table' AND name='sessions'").Scan(&tableSQL); err != nil {
		return corruptQueryError(ctx, "missing sessions table", err)
	}
	const exactTable = "CREATE TABLE sessions ( id TEXT PRIMARY KEY NOT NULL, administrator_id TEXT NOT NULL REFERENCES administrators(id), token_hash BLOB NOT NULL UNIQUE CHECK(length(token_hash) = 32), created_at TEXT NOT NULL, last_activity_at TEXT NOT NULL, idle_expires_at TEXT NOT NULL, absolute_expires_at TEXT NOT NULL, revoked_at TEXT, remote_addr TEXT NOT NULL, user_agent TEXT NOT NULL CHECK(length(CAST(user_agent AS BLOB)) <= 512), tailscale_login TEXT, tailscale_name TEXT, proxy_identity_informational INTEGER NOT NULL CHECK(proxy_identity_informational IN (0, 1)) )"
	if strings.Join(strings.Fields(tableSQL), " ") != exactTable {
		return corruptError("sessions table definition differs", nil)
	}
	if err := query.QueryRowContext(ctx, "SELECT sql FROM sqlite_schema WHERE type='index' AND name='sessions_administrator_active'").Scan(&indexSQL); err != nil {
		return corruptQueryError(ctx, "missing active sessions index", err)
	}
	const exact = "CREATE INDEX sessions_administrator_active ON sessions (administrator_id, revoked_at, absolute_expires_at)"
	if strings.Join(strings.Fields(indexSQL), " ") != exact {
		return corruptError("active sessions index differs", nil)
	}
	return nil
}

func validateSchemaV3(ctx context.Context, query queryer) error {
	if err := validateSchemaV2(ctx, query); err != nil {
		return err
	}
	for _, table := range []struct {
		name    string
		columns []expectedColumn
	}{
		{"hosts", []expectedColumn{{"id", "TEXT", true, 1}, {"display_name", "TEXT", true, 0}, {"source", "TEXT", true, 0}, {"agent_url", "TEXT", true, 0}, {"agent_credential", "BLOB", true, 0}, {"administrative_state", "TEXT", true, 0}, {"observed_state", "TEXT", true, 0}, {"interval_seconds", "INTEGER", true, 0}, {"timeout_seconds", "INTEGER", true, 0}, {"last_attempt_at", "TEXT", false, 0}, {"last_success_received_at", "TEXT", false, 0}, {"freshness_deadline_at", "TEXT", false, 0}, {"consecutive_failures", "INTEGER", true, 0}, {"stale", "INTEGER", true, 0}, {"archived_at", "TEXT", false, 0}, {"bootstrap_completed_at", "TEXT", false, 0}, {"created_at", "TEXT", true, 0}, {"updated_at", "TEXT", true, 0}, {"collection_generation", "INTEGER", true, 0}, {"version", "INTEGER", true, 0}}},
		{"host_snapshots", []expectedColumn{{"host_id", "TEXT", true, 1}, {"received_at", "TEXT", true, 0}, {"observed_at", "TEXT", true, 0}, {"hostname", "TEXT", true, 0}, {"boot_id", "TEXT", false, 0}, {"uptime_seconds", "INTEGER", false, 0}, {"cpu_used_percent", "REAL", false, 0}, {"memory_used_bytes", "INTEGER", false, 0}, {"memory_total_bytes", "INTEGER", false, 0}, {"tailscale_state", "TEXT", true, 0}, {"tailscale_hostname", "TEXT", false, 0}, {"agent_version", "TEXT", true, 0}, {"protocol_version", "INTEGER", true, 0}}},
		{"host_snapshot_warnings", []expectedColumn{{"host_id", "TEXT", true, 1}, {"code", "TEXT", true, 2}}},
		{"host_snapshot_addresses", []expectedColumn{{"host_id", "TEXT", true, 1}, {"address", "TEXT", true, 2}}},
		{"filesystems", []expectedColumn{{"id", "TEXT", true, 1}, {"host_id", "TEXT", true, 0}, {"identity_kind", "TEXT", true, 0}, {"identity_key", "BLOB", true, 0}, {"fs_uuid", "TEXT", false, 0}, {"mount_root", "TEXT", true, 0}, {"mount_point", "TEXT", true, 0}, {"major_minor", "TEXT", true, 0}, {"fs_type", "TEXT", true, 0}, {"device", "TEXT", true, 0}, {"total_bytes", "INTEGER", true, 0}, {"used_bytes", "INTEGER", true, 0}, {"available_bytes", "INTEGER", true, 0}, {"discovered_at", "TEXT", true, 0}, {"selected", "INTEGER", true, 0}, {"present", "INTEGER", true, 0}, {"last_observed_at", "TEXT", true, 0}, {"version", "INTEGER", true, 0}}},
		{"schedule_periods", []expectedColumn{{"id", "TEXT", true, 1}, {"target_type", "TEXT", true, 0}, {"target_id", "TEXT", true, 0}, {"starts_at", "TEXT", true, 0}, {"ends_at", "TEXT", false, 0}, {"effective_interval_seconds", "INTEGER", true, 0}, {"effective_timeout_seconds", "INTEGER", true, 0}}},
		{"schedule_slots", []expectedColumn{{"id", "TEXT", true, 1}, {"target_type", "TEXT", true, 0}, {"target_id", "TEXT", true, 0}, {"schedule_period_id", "TEXT", true, 0}, {"nominal_at", "TEXT", true, 0}, {"not_before_at", "TEXT", true, 0}, {"deadline_at", "TEXT", true, 0}, {"effective_interval_seconds", "INTEGER", true, 0}, {"effective_timeout_seconds", "INTEGER", true, 0}, {"state", "TEXT", true, 0}, {"lease_until", "TEXT", false, 0}, {"process_attempts", "INTEGER", true, 0}, {"cancellation_reason", "TEXT", false, 0}, {"started_at", "TEXT", false, 0}, {"finished_at", "TEXT", false, 0}}},
	} {
		if err := validateTable(ctx, query, table.name, table.columns); err != nil {
			return corruptQueryError(ctx, "invalid "+table.name+" table", err)
		}
	}
	return validateMigrationObjects(ctx, query, hostMigrationSQL)
}

// validateMigrationObjects compares every explicit CREATE statement with SQLite's
// normalized schema text. This detects weakened CHECK, FK, partial-index, and unique
// definitions that column metadata alone cannot see.
func validateMigrationObjects(ctx context.Context, query queryer, source string) error {
	for _, statement := range splitCreateStatements(source) {
		if statement == "" {
			continue
		}
		fields := strings.Fields(statement)
		if len(fields) < 3 || fields[0] != "CREATE" {
			return corruptError("host migration contains an unexpected statement", nil)
		}
		kindIndex := 1
		if fields[kindIndex] == "UNIQUE" {
			kindIndex++
		}
		if kindIndex+1 >= len(fields) {
			return corruptError("host migration contains an invalid CREATE statement", nil)
		}
		kind, name := strings.ToLower(fields[kindIndex]), fields[kindIndex+1]
		var actual string
		if err := query.QueryRowContext(ctx, "SELECT sql FROM sqlite_schema WHERE type=? AND name=?", kind, name).Scan(&actual); err != nil {
			return corruptQueryError(ctx, "missing host schema object "+name, err)
		}
		if strings.Join(strings.Fields(actual), " ") != strings.Join(strings.Fields(statement), " ") {
			return corruptError("host schema object "+name+" differs", nil)
		}
	}
	return nil
}

func splitCreateStatements(source string) []string {
	var statements []string
	var current []string
	trigger := false
	for _, line := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(line)
		if len(current) == 0 && trimmed == "" {
			continue
		}
		if len(current) == 0 {
			trigger = strings.HasPrefix(trimmed, "CREATE TRIGGER ")
		}
		current = append(current, line)
		terminal := !trigger && strings.HasSuffix(trimmed, ";") || trigger && trimmed == "END;"
		if terminal {
			statements = append(statements, strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(strings.Join(current, "\n")), ";")))
			current = nil
			trigger = false
		}
	}
	return statements
}

func validateUniqueUsernameIndex(ctx context.Context, query queryer) error {
	rows, err := query.QueryContext(ctx, `
		SELECT name
		FROM pragma_index_list('administrators')
		WHERE "unique" = 1 AND origin = 'u' AND partial = 0`)
	if err != nil {
		return err
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	for _, name := range names {
		var columns, usernameColumns int
		if err := query.QueryRowContext(ctx, `
			SELECT count(*), sum(CASE WHEN name = 'username' THEN 1 ELSE 0 END)
			FROM pragma_index_info(?)`, name,
		).Scan(&columns, &usernameColumns); err != nil {
			return err
		}
		if columns == 1 && usernameColumns == 1 {
			return nil
		}
	}
	return fmt.Errorf("missing single-column username unique constraint")
}

func validateTable(ctx context.Context, query queryer, table string, expected []expectedColumn) error {
	rows, err := query.QueryContext(ctx, fmt.Sprintf("PRAGMA table_xinfo(%q)", table))
	if err != nil {
		return err
	}
	defer rows.Close()

	actual := make([]expectedColumn, 0, len(expected))
	for rows.Next() {
		var cid, notNull, primaryKey, hidden int
		var name, typeName string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typeName, &notNull, &defaultValue, &primaryKey, &hidden); err != nil {
			return err
		}
		if cid != len(actual) || defaultValue != nil || hidden != 0 {
			return fmt.Errorf("unexpected column metadata")
		}
		actual = append(actual, expectedColumn{
			name: name, typeName: strings.ToUpper(typeName), notNull: notNull == 1, primaryKey: primaryKey,
		})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(actual) != len(expected) {
		return fmt.Errorf("unexpected column count")
	}
	for index := range expected {
		if actual[index] != expected[index] {
			return fmt.Errorf("column %d differs", index)
		}
	}
	return nil
}
