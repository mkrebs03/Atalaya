package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mkrebs03/atalaya/internal/auth"
)

var _ auth.AdministratorRepository = (*Database)(nil)

// CountActiveAdministrators returns the number of active administrators after
// validating the current instance schema and master key.
func (database *Database) CountActiveAdministrators(ctx context.Context) (int, error) {
	if database == nil || database.sql == nil {
		return 0, fmt.Errorf("database is closed")
	}
	transaction, err := database.sql.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return 0, fmt.Errorf("begin administrator count: %w", err)
	}
	defer transaction.Rollback()
	validation, err := validateAtalaya(ctx, transaction, database.key, database.migrations)
	if err != nil {
		return 0, err
	}
	if err := transaction.Commit(); err != nil {
		return 0, fmt.Errorf("commit administrator count: %w", err)
	}
	return validation.activeAdministrators, nil
}

// CreateAdministrator implements auth.AdministratorRepository using a short,
// immediately locked transaction that revalidates key, schema, and singleton.
func (database *Database) CreateAdministrator(ctx context.Context, administrator auth.Administrator) error {
	if database == nil || database.sql == nil {
		return fmt.Errorf("database is closed")
	}
	if err := auth.ValidateNewAdministrator(administrator); err != nil {
		return err
	}

	transaction, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin administrator creation: %w", err)
	}
	defer transaction.Rollback()

	observed, err := validateWritable(ctx, transaction, database.key, database.migrations, false)
	if err != nil {
		return fmt.Errorf("revalidate administrator creation: %w", err)
	}
	if observed.State != StateAtalaya || observed.SchemaVersion != len(database.migrations) {
		return fmt.Errorf("administrator creation requires a current Atalaya schema")
	}
	if observed.HasActiveAdministrator {
		return ErrAdministratorExists
	}

	_, err = transaction.ExecContext(ctx, `
		INSERT INTO administrators(
			id, username, password_hash, created_at, updated_at,
			password_changed_at, disabled_at, version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		administrator.ID,
		administrator.Username,
		administrator.PasswordHash,
		administrator.CreatedAt,
		administrator.UpdatedAt,
		administrator.PasswordChangedAt,
		administrator.DisabledAt,
		administrator.Version,
	)
	if err != nil {
		return fmt.Errorf("insert administrator: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit administrator creation: %w", err)
	}
	return nil
}
