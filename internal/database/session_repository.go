package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mkrebs03/atalaya/internal/auth"
)

var _ auth.Repository = (*Database)(nil)

func (database *Database) FindActiveAdministrator(ctx context.Context, username string) (auth.Administrator, error) {
	var administrator auth.Administrator
	err := database.sql.QueryRowContext(ctx, `SELECT id, username, password_hash, created_at, updated_at, password_changed_at, disabled_at, version FROM administrators WHERE username=? AND disabled_at IS NULL`, username).Scan(&administrator.ID, &administrator.Username, &administrator.PasswordHash, &administrator.CreatedAt, &administrator.UpdatedAt, &administrator.PasswordChangedAt, &administrator.DisabledAt, &administrator.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return auth.Administrator{}, auth.ErrAdministratorNotFound
	}
	if err != nil {
		return auth.Administrator{}, err
	}
	return administrator, nil
}

func (database *Database) CreateSessionIfAdministratorVersion(ctx context.Context, session auth.Session, version int) error {
	transaction, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	var count int
	if err := transaction.QueryRowContext(ctx, `SELECT count(*) FROM administrators WHERE id=? AND version=? AND disabled_at IS NULL`, session.AdministratorID, version).Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return auth.ErrAdministratorVersionConflict
	}
	if err := insertSession(ctx, transaction, session); err != nil {
		return err
	}
	return transaction.Commit()
}

func (database *Database) ValidateAndTouchSession(ctx context.Context, tokenHash []byte, now, idleExpiry time.Time) (auth.Administrator, auth.Session, error) {
	transaction, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return auth.Administrator{}, auth.Session{}, err
	}
	defer transaction.Rollback()
	administrator, session, err := readSession(ctx, transaction, tokenHash)
	if err != nil {
		return auth.Administrator{}, auth.Session{}, err
	}
	if session.RevokedAt != nil || !now.Before(session.IdleExpiresAt) || !now.Before(session.AbsoluteExpiresAt) {
		return auth.Administrator{}, auth.Session{}, auth.ErrSessionExpired
	}
	if idleExpiry.After(session.AbsoluteExpiresAt) {
		idleExpiry = session.AbsoluteExpiresAt
	}
	result, err := transaction.ExecContext(ctx, `UPDATE sessions SET last_activity_at=?, idle_expires_at=? WHERE id=? AND revoked_at IS NULL`, formatTime(now), formatTime(idleExpiry), session.ID)
	if err != nil {
		return auth.Administrator{}, auth.Session{}, err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return auth.Administrator{}, auth.Session{}, auth.ErrSessionExpired
	}
	session.LastActivityAt, session.IdleExpiresAt = now, idleExpiry
	if err := transaction.Commit(); err != nil {
		return auth.Administrator{}, auth.Session{}, err
	}
	return administrator, session, nil
}

func (database *Database) RevokeSession(ctx context.Context, tokenHash []byte, now time.Time) error {
	result, err := database.sql.ExecContext(ctx, `UPDATE sessions SET revoked_at=? WHERE token_hash=? AND revoked_at IS NULL`, formatTime(now), tokenHash)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return auth.ErrAuthenticationRequired
	}
	return nil
}

func (database *Database) ChangePasswordAndReplaceSessions(ctx context.Context, administratorID string, version int, passwordHash string, now time.Time, replacement auth.Session) error {
	transaction, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	result, err := transaction.ExecContext(ctx, `UPDATE administrators SET password_hash=?, updated_at=?, password_changed_at=?, version=version+1 WHERE id=? AND version=? AND disabled_at IS NULL`, passwordHash, formatTime(now), formatTime(now), administratorID, version)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return auth.ErrAdministratorVersionConflict
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE sessions SET revoked_at=? WHERE administrator_id=? AND revoked_at IS NULL`, formatTime(now), administratorID); err != nil {
		return err
	}
	if err := insertSession(ctx, transaction, replacement); err != nil {
		return err
	}
	return transaction.Commit()
}

func (database *Database) ResetPasswordAndRevokeSessions(ctx context.Context, passwordHash string, now time.Time) error {
	transaction, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	var administratorID string
	if err := transaction.QueryRowContext(ctx, `SELECT id FROM administrators WHERE disabled_at IS NULL`).Scan(&administratorID); err != nil {
		return fmt.Errorf("active administrator required: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE administrators SET password_hash=?, updated_at=?, password_changed_at=?, version=version+1 WHERE id=?`, passwordHash, formatTime(now), formatTime(now), administratorID); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE sessions SET revoked_at=? WHERE administrator_id=? AND revoked_at IS NULL`, formatTime(now), administratorID); err != nil {
		return err
	}
	return transaction.Commit()
}

type sqlQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func insertSession(ctx context.Context, query sqlQueryer, session auth.Session) error {
	_, err := query.ExecContext(ctx, `INSERT INTO sessions(id, administrator_id, token_hash, created_at, last_activity_at, idle_expires_at, absolute_expires_at, revoked_at, remote_addr, user_agent, tailscale_login, tailscale_name, proxy_identity_informational) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, session.ID, session.AdministratorID, session.TokenHash, formatTime(session.CreatedAt), formatTime(session.LastActivityAt), formatTime(session.IdleExpiresAt), formatTime(session.AbsoluteExpiresAt), nullableTime(session.RevokedAt), session.RemoteAddr, session.UserAgent, session.TailscaleLogin, session.TailscaleName, boolInt(session.ProxyIdentityInformational))
	return err
}

func readSession(ctx context.Context, query sqlQueryer, tokenHash []byte) (auth.Administrator, auth.Session, error) {
	var administrator auth.Administrator
	var session auth.Session
	var created, activity, idle, absolute string
	var revoked sql.NullString
	var proxy int
	err := query.QueryRowContext(ctx, `SELECT a.id,a.username,a.password_hash,a.created_at,a.updated_at,a.password_changed_at,a.disabled_at,a.version,s.id,s.administrator_id,s.token_hash,s.created_at,s.last_activity_at,s.idle_expires_at,s.absolute_expires_at,s.revoked_at,s.remote_addr,s.user_agent,s.tailscale_login,s.tailscale_name,s.proxy_identity_informational FROM sessions s JOIN administrators a ON a.id=s.administrator_id WHERE s.token_hash=? AND a.disabled_at IS NULL`, tokenHash).Scan(&administrator.ID, &administrator.Username, &administrator.PasswordHash, &administrator.CreatedAt, &administrator.UpdatedAt, &administrator.PasswordChangedAt, &administrator.DisabledAt, &administrator.Version, &session.ID, &session.AdministratorID, &session.TokenHash, &created, &activity, &idle, &absolute, &revoked, &session.RemoteAddr, &session.UserAgent, &session.TailscaleLogin, &session.TailscaleName, &proxy)
	if errors.Is(err, sql.ErrNoRows) {
		return auth.Administrator{}, auth.Session{}, auth.ErrAuthenticationRequired
	}
	if err != nil {
		return auth.Administrator{}, auth.Session{}, err
	}
	for _, item := range []struct {
		text   string
		target *time.Time
	}{{created, &session.CreatedAt}, {activity, &session.LastActivityAt}, {idle, &session.IdleExpiresAt}, {absolute, &session.AbsoluteExpiresAt}} {
		parsed, parseErr := time.Parse(time.RFC3339Nano, item.text)
		if parseErr != nil {
			return auth.Administrator{}, auth.Session{}, parseErr
		}
		*item.target = parsed
	}
	if revoked.Valid {
		parsed, parseErr := time.Parse(time.RFC3339Nano, revoked.String)
		if parseErr != nil {
			return auth.Administrator{}, auth.Session{}, parseErr
		}
		session.RevokedAt = &parsed
	}
	session.ProxyIdentityInformational = proxy == 1
	return administrator, session, nil
}

func formatTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }
func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return formatTime(*value)
}
func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
