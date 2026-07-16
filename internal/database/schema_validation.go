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
