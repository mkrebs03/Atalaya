package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"time"

	atalayacrypto "github.com/mkrebs03/atalaya/internal/crypto"
	"github.com/mkrebs03/atalaya/internal/hosts"
)

const agentSecretType = "agent"

var _ hosts.LocalHostRepository = (*Database)(nil)

func (database *Database) PrepareLocalHost(ctx context.Context, displayName, agentURL string, now time.Time, random io.Reader) (hosts.LocalHost, error) {
	if database == nil || database.sql == nil {
		return hosts.LocalHost{}, fmt.Errorf("database is closed")
	}
	if random == nil {
		return hosts.LocalHost{}, fmt.Errorf("random source is required")
	}
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return hosts.LocalHost{}, fmt.Errorf("begin local host bootstrap: %w", err)
	}
	defer tx.Rollback()
	if err := database.requireCurrentSchema(ctx, tx); err != nil {
		return hosts.LocalHost{}, err
	}
	host, found, err := database.readLocalHost(ctx, tx)
	if err != nil {
		return hosts.LocalHost{}, err
	}
	if found {
		return transferLocalHostCredential(host, func() error {
			var storedName, storedURL string
			if err := tx.QueryRowContext(ctx, "SELECT display_name, agent_url FROM hosts WHERE id=?", host.ID).Scan(&storedName, &storedURL); err != nil {
				return fmt.Errorf("read local host bootstrap parameters: %w", err)
			}
			if storedName != displayName || storedURL != agentURL {
				return hosts.ErrBootstrapMismatch
			}
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("commit local host lookup: %w", err)
			}
			return nil
		})
	}

	hostID, err := randomUUIDv4(random)
	if err != nil {
		return hosts.LocalHost{}, fmt.Errorf("generate local host ID: %w", err)
	}
	credential := make([]byte, hosts.CredentialBytes)
	if _, err := io.ReadFull(random, credential); err != nil {
		return hosts.LocalHost{}, fmt.Errorf("generate local-agent credential: %w", err)
	}
	envelope, err := atalayacrypto.SealWithRandom(database.key, agentSecretType, hostID, credential, random)
	if err != nil {
		clear(credential)
		return hosts.LocalHost{}, fmt.Errorf("encrypt local-agent credential: %w", err)
	}
	timestamp := now.UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `INSERT INTO hosts(
		id, display_name, source, agent_url, agent_credential,
		administrative_state, observed_state, interval_seconds, timeout_seconds,
		consecutive_failures, stale, created_at, updated_at, collection_generation, version
	) VALUES (?, ?, 'local', ?, ?, 'pending', 'unknown', 60, 10, 0, 0, ?, ?, 1, 1)`,
		hostID, displayName, agentURL, envelope, timestamp, timestamp)
	if err != nil {
		clear(credential)
		return hosts.LocalHost{}, fmt.Errorf("insert local host: %w", err)
	}
	if err := tx.Commit(); err != nil {
		clear(credential)
		return hosts.LocalHost{}, fmt.Errorf("commit local host bootstrap: %w", err)
	}
	return hosts.LocalHost{ID: hostID, Credential: credential}, nil
}

// transferLocalHostCredential makes credential ownership explicit: failures retain
// ownership here and erase the buffer; only success transfers it to the caller.
func transferLocalHostCredential(host hosts.LocalHost, finish func() error) (hosts.LocalHost, error) {
	owned := true
	defer func() {
		if owned {
			clear(host.Credential)
		}
	}()
	if err := finish(); err != nil {
		return hosts.LocalHost{}, err
	}
	owned = false
	return host, nil
}

func (database *Database) CompleteLocalHost(ctx context.Context, hostID string, now time.Time, random io.Reader) error {
	if database == nil || database.sql == nil {
		return fmt.Errorf("database is closed")
	}
	if random == nil {
		return fmt.Errorf("random source is required")
	}
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin local host completion: %w", err)
	}
	defer tx.Rollback()
	if err := database.requireCurrentSchema(ctx, tx); err != nil {
		return err
	}
	var completed sql.NullString
	var source, state string
	if err := tx.QueryRowContext(ctx, "SELECT source, administrative_state, bootstrap_completed_at FROM hosts WHERE id=?", hostID).Scan(&source, &state, &completed); err != nil {
		return fmt.Errorf("read local host for completion: %w", err)
	}
	if source != "local" {
		return fmt.Errorf("host is not local")
	}
	if completed.Valid {
		return tx.Commit()
	}
	if state != "pending" {
		return fmt.Errorf("incomplete local host must remain pending")
	}
	periodID, err := randomUUIDv4(random)
	if err != nil {
		return fmt.Errorf("generate schedule period ID: %w", err)
	}
	slotID, err := randomUUIDv4(random)
	if err != nil {
		return fmt.Errorf("generate schedule slot ID: %w", err)
	}
	jitterSeconds, err := uniformJitter6(random)
	if err != nil {
		return fmt.Errorf("generate schedule jitter: %w", err)
	}
	jitter := time.Duration(jitterSeconds) * time.Second
	nominal := now.UTC()
	formatted := nominal.Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, "UPDATE hosts SET bootstrap_completed_at=?, updated_at=? WHERE id=? AND bootstrap_completed_at IS NULL", formatted, formatted, hostID); err != nil {
		return fmt.Errorf("complete local host: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schedule_periods(id,target_type,target_id,starts_at,effective_interval_seconds,effective_timeout_seconds) VALUES(?,'host',?,?,60,10)`, periodID, hostID, formatted); err != nil {
		return fmt.Errorf("create local host schedule period: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schedule_slots(id,target_type,target_id,schedule_period_id,nominal_at,not_before_at,deadline_at,effective_interval_seconds,effective_timeout_seconds,state,process_attempts) VALUES(?,'host',?,?,?,?,?,60,10,'scheduled',0)`,
		slotID, hostID, periodID, formatted, nominal.Add(jitter).Format(time.RFC3339Nano), nominal.Add(15*time.Second).Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("create local host schedule slot: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit local host completion: %w", err)
	}
	return nil
}

func uniformJitter6(random io.Reader) (int, error) {
	if random == nil {
		return 0, fmt.Errorf("random source is required")
	}
	var value [1]byte
	for {
		if _, err := io.ReadFull(random, value[:]); err != nil {
			return 0, err
		}
		if value[0] < 252 {
			return int(value[0] % 6), nil
		}
	}
}

func (database *Database) readLocalHost(ctx context.Context, tx *sql.Tx) (hosts.LocalHost, bool, error) {
	var id string
	var envelope []byte
	var completed sql.NullString
	err := tx.QueryRowContext(ctx, "SELECT id, agent_credential, bootstrap_completed_at FROM hosts WHERE source='local'").Scan(&id, &envelope, &completed)
	if errors.Is(err, sql.ErrNoRows) {
		return hosts.LocalHost{}, false, nil
	}
	if err != nil {
		return hosts.LocalHost{}, false, fmt.Errorf("read local host: %w", err)
	}
	credential, err := atalayacrypto.Open(database.key, agentSecretType, id, envelope)
	if err != nil {
		return hosts.LocalHost{}, false, fmt.Errorf("decrypt local-agent credential: %w", err)
	}
	return hosts.LocalHost{ID: id, Credential: credential, BootstrapCompleted: completed.Valid}, true, nil
}

func (database *Database) requireCurrentSchema(ctx context.Context, tx *sql.Tx) error {
	observed, err := validateWritable(ctx, tx, database.key, database.migrations, false)
	if err != nil {
		return fmt.Errorf("revalidate host repository: %w", err)
	}
	if observed.State != StateAtalaya || observed.SchemaVersion != len(database.migrations) {
		return fmt.Errorf("host repository requires current Atalaya schema")
	}
	return nil
}

func randomUUIDv4(random io.Reader) (string, error) {
	var value [16]byte
	if _, err := io.ReadFull(random, value[:]); err != nil {
		return "", err
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}
