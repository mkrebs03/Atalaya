package database

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/mkrebs03/atalaya/internal/collection"
	"github.com/mkrebs03/atalaya/internal/config"
	atalayacrypto "github.com/mkrebs03/atalaya/internal/crypto"
	"github.com/mkrebs03/atalaya/internal/hostquery"
	"github.com/mkrebs03/atalaya/internal/hosts"
)

var _ hosts.ManagementRepository = (*Database)(nil)

type managementRandom struct{ reader io.Reader }

const maxEnrollmentTokenPageSize = 100

func (r managementRandom) ID() (string, error) { return randomUUIDv4(r.reader) }

func (r managementRandom) Intn(max int) int {
	if max <= 1 {
		return 0
	}
	var value [1]byte
	limit := 256 - (256 % max)
	for {
		if _, err := io.ReadFull(r.reader, value[:]); err != nil {
			return 0
		}
		if int(value[0]) < limit {
			return int(value[0]) % max
		}
	}
}

func (database *Database) CreateEnrollmentToken(ctx context.Context, label, administratorID string, now time.Time, random io.Reader) (hosts.EnrollmentToken, string, error) {
	if database == nil || database.sql == nil {
		return hosts.EnrollmentToken{}, "", fmt.Errorf("database is closed")
	}
	if random == nil {
		return hosts.EnrollmentToken{}, "", fmt.Errorf("random source is required")
	}
	if label != "" {
		if len(label) > 128 {
			return hosts.EnrollmentToken{}, "", fmt.Errorf("%w: label is too long", hosts.ErrInvalidManagement)
		}
		if err := validateManagementText(label, 128); err != nil {
			return hosts.EnrollmentToken{}, "", err
		}
	}
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return hosts.EnrollmentToken{}, "", err
	}
	defer tx.Rollback()
	if err := database.requireCurrentSchema(ctx, tx); err != nil {
		return hosts.EnrollmentToken{}, "", err
	}
	var active int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM administrators WHERE id=? AND disabled_at IS NULL", administratorID).Scan(&active); err != nil {
		return hosts.EnrollmentToken{}, "", err
	}
	if active != 1 {
		return hosts.EnrollmentToken{}, "", fmt.Errorf("administrator is not active")
	}
	raw := make([]byte, hosts.EnrollmentTokenBytes)
	if _, err := io.ReadFull(random, raw); err != nil {
		return hosts.EnrollmentToken{}, "", err
	}
	secret := base64.RawURLEncoding.EncodeToString(raw)
	defer clear(raw)
	hash := sha256.Sum256([]byte(secret))
	id, err := randomUUIDv4(random)
	if err != nil {
		return hosts.EnrollmentToken{}, "", err
	}
	created := now.UTC()
	if _, err := tx.ExecContext(ctx, `INSERT INTO enrollment_tokens(id,token_hash,label,created_by,created_at,expires_at,version) VALUES(?,?,?,?,?,?,1)`, id, hash[:], nullableString(label), administratorID, dbTime(created), dbTime(created.Add(hosts.EnrollmentTTL))); err != nil {
		return hosts.EnrollmentToken{}, "", fmt.Errorf("create enrollment token: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return hosts.EnrollmentToken{}, "", err
	}
	return hosts.EnrollmentToken{ID: id, Label: nullableStringPointer(label), CreatedAt: created, ExpiresAt: created.Add(hosts.EnrollmentTTL), Version: 1}, secret, nil
}

func (database *Database) ListEnrollmentTokens(ctx context.Context, limit int, cursor string) ([]hosts.EnrollmentToken, error) {
	// The HTTP handler requests one extra row to determine whether a next page exists.
	if limit < 1 || limit > maxEnrollmentTokenPageSize+1 {
		return nil, fmt.Errorf("invalid enrollment token limit")
	}
	tx, err := database.sql.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	where := ""
	args := []any{}
	if cursor != "" {
		where = " WHERE id>?"
		args = append(args, cursor)
	}
	args = append(args, limit)
	rows, err := tx.QueryContext(ctx, `SELECT id,label,created_at,expires_at,revoked_at,consumed_at,replay_until,host_id,version FROM enrollment_tokens`+where+` ORDER BY id LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]hosts.EnrollmentToken, 0, limit)
	for rows.Next() {
		var item hosts.EnrollmentToken
		var label, created, expires, revoked, consumed, replay, hostID sql.NullString
		if err := rows.Scan(&item.ID, &label, &created, &expires, &revoked, &consumed, &replay, &hostID, &item.Version); err != nil {
			return nil, err
		}
		item.Label = managementStringPointer(label)
		item.CreatedAt, err = parseDBTime(created.String)
		if err != nil {
			return nil, err
		}
		item.ExpiresAt, err = parseDBTime(expires.String)
		if err != nil {
			return nil, err
		}
		item.RevokedAt, err = timePointer(revoked)
		if err != nil {
			return nil, err
		}
		item.ConsumedAt, err = timePointer(consumed)
		if err != nil {
			return nil, err
		}
		item.ReplayUntil, err = timePointer(replay)
		if err != nil {
			return nil, err
		}
		if hostID.Valid {
			value := hostID.String
			item.HostID = &value
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return items, tx.Commit()
}

func (database *Database) RevokeEnrollmentToken(ctx context.Context, id string, version int64, now time.Time) error {
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := database.requireCurrentSchema(ctx, tx); err != nil {
		return err
	}
	var current int64
	if err := tx.QueryRowContext(ctx, "SELECT version FROM enrollment_tokens WHERE id=?", id).Scan(&current); errors.Is(err, sql.ErrNoRows) {
		return hosts.ErrEnrollmentInvalid
	} else if err != nil {
		return err
	}
	if current != version {
		return hosts.ErrHostVersionConflict
	}
	if _, err := tx.ExecContext(ctx, "UPDATE enrollment_tokens SET revoked_at=?,version=version+1 WHERE id=? AND version=?", dbTime(now), id, version); err != nil {
		return err
	}
	return tx.Commit()
}

func (database *Database) EnrollAgent(ctx context.Context, request hosts.EnrollmentRequest, now time.Time, random io.Reader) (hosts.EnrollmentResponse, error) {
	if err := hosts.ValidateEnrollmentRequest(request); err != nil {
		return hosts.EnrollmentResponse{}, err
	}
	if err := config.ValidateAgentURL(request.AgentURL); err != nil {
		return hosts.EnrollmentResponse{}, fmt.Errorf("%w: agent URL", hosts.ErrInvalidManagement)
	}
	if random == nil {
		return hosts.EnrollmentResponse{}, fmt.Errorf("random source is required")
	}
	rawToken, err := base64.RawURLEncoding.DecodeString(request.Token)
	if err != nil || len(rawToken) != hosts.EnrollmentTokenBytes {
		return hosts.EnrollmentResponse{}, hosts.ErrEnrollmentInvalid
	}
	defer clear(rawToken)
	tokenHash := sha256.Sum256([]byte(request.Token))
	payloadHash := enrollmentPayloadHash(request)
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return hosts.EnrollmentResponse{}, err
	}
	defer tx.Rollback()
	if err := database.requireCurrentSchema(ctx, tx); err != nil {
		return hosts.EnrollmentResponse{}, err
	}
	var id, created, expires string
	var revoked, consumed, replay, storedRequest, storedHost sql.NullString
	var storedPayload []byte
	err = tx.QueryRowContext(ctx, `SELECT id,created_at,expires_at,revoked_at,consumed_at,replay_until,request_id,payload_hash,host_id FROM enrollment_tokens WHERE token_hash=?`, tokenHash[:]).Scan(&id, &created, &expires, &revoked, &consumed, &replay, &storedRequest, &storedPayload, &storedHost)
	if errors.Is(err, sql.ErrNoRows) {
		return hosts.EnrollmentResponse{}, hosts.ErrEnrollmentInvalid
	}
	if err != nil {
		return hosts.EnrollmentResponse{}, err
	}
	currentTime := now.UTC()
	if consumed.Valid {
		if revoked.Valid || !replay.Valid || !currentTime.Before(mustParseDBTime(replay.String)) || storedRequest.String != request.RequestID || subtle.ConstantTimeCompare(storedPayload, payloadHash[:]) != 1 || !storedHost.Valid {
			return hosts.EnrollmentResponse{}, hosts.ErrEnrollmentConsumed
		}
		var envelope []byte
		if err := tx.QueryRowContext(ctx, "SELECT agent_credential FROM hosts WHERE id=?", storedHost.String).Scan(&envelope); err != nil {
			return hosts.EnrollmentResponse{}, hosts.ErrEnrollmentConsumed
		}
		credential, err := atalayacrypto.Open(database.key, agentSecretType, storedHost.String, envelope)
		if err != nil {
			return hosts.EnrollmentResponse{}, err
		}
		if err := tx.Commit(); err != nil {
			clear(credential)
			return hosts.EnrollmentResponse{}, err
		}
		return hosts.EnrollmentResponse{HostID: storedHost.String, AgentCredential: credential, PollIntervalSeconds: 60}, nil
	}
	if revoked.Valid || !currentTime.Before(mustParseDBTime(expires)) {
		return hosts.EnrollmentResponse{}, hosts.ErrEnrollmentInvalid
	}
	hostID, err := randomUUIDv4(random)
	if err != nil {
		return hosts.EnrollmentResponse{}, err
	}
	credential := make([]byte, hosts.CredentialBytes)
	if _, err := io.ReadFull(random, credential); err != nil {
		return hosts.EnrollmentResponse{}, err
	}
	envelope, err := atalayacrypto.SealWithRandom(database.key, agentSecretType, hostID, credential, random)
	if err != nil {
		clear(credential)
		return hosts.EnrollmentResponse{}, err
	}
	createdAt := dbTime(currentTime)
	if _, err := tx.ExecContext(ctx, `INSERT INTO hosts(id,display_name,source,agent_url,agent_credential,administrative_state,observed_state,interval_seconds,timeout_seconds,consecutive_failures,stale,bootstrap_completed_at,created_at,updated_at,collection_generation,version) VALUES(?,?,?,?,?,'pending','unknown',60,10,0,0,?,?,?,1,1)`, hostID, request.DisplayName, "enrolled", request.AgentURL, envelope, createdAt, createdAt, createdAt); err != nil {
		clear(credential)
		return hosts.EnrollmentResponse{}, fmt.Errorf("%w: host: %v", hosts.ErrEnrollmentConflict, err)
	}
	if err := createAnchoredSchedule(ctx, tx, hostID, currentTime, 60, 10, managementRandom{random}); err != nil {
		clear(credential)
		return hosts.EnrollmentResponse{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE enrollment_tokens SET consumed_at=?,replay_until=?,request_id=?,payload_hash=?,host_id=?,version=version+1 WHERE id=? AND consumed_at IS NULL`, createdAt, dbTime(currentTime.Add(hosts.EnrollmentReplayTTL)), request.RequestID, payloadHash[:], hostID, id); err != nil {
		clear(credential)
		return hosts.EnrollmentResponse{}, err
	}
	if err := tx.Commit(); err != nil {
		clear(credential)
		return hosts.EnrollmentResponse{}, err
	}
	return hosts.EnrollmentResponse{HostID: hostID, AgentCredential: credential, PollIntervalSeconds: 60}, nil
}

func enrollmentPayloadHash(request hosts.EnrollmentRequest) [32]byte {
	canonical := struct {
		Token           string `json:"enrollment_token"`
		RequestID       string `json:"request_id"`
		DisplayName     string `json:"display_name"`
		AgentURL        string `json:"agent_url"`
		AgentVersion    string `json:"agent_version"`
		ProtocolVersion int    `json:"protocol_version"`
	}{request.Token, request.RequestID, request.DisplayName, request.AgentURL, request.AgentVersion, request.ProtocolVersion}
	encoded, _ := json.Marshal(canonical)
	return sha256.Sum256(encoded)
}

func validateManagementText(value string, max int) error {
	if len(value) == 0 || len(value) > max {
		return hosts.ErrInvalidManagement
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return hosts.ErrInvalidManagement
		}
	}
	return nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func nullableStringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
func managementStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	v := value.String
	return &v
}
func timePointer(value sql.NullString) (*time.Time, error) {
	if !value.Valid {
		return nil, nil
	}
	v, err := parseDBTime(value.String)
	return &v, err
}
func mustParseDBTime(value string) time.Time { parsed, _ := parseDBTime(value); return parsed }

func (database *Database) PatchHost(ctx context.Context, id string, version int64, patch hosts.HostPatch, now time.Time, random io.Reader) error {
	if patch.DisplayName == nil && patch.IntervalSeconds == nil && patch.TimeoutSeconds == nil {
		return hosts.ErrInvalidManagement
	}
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := database.requireCurrentSchema(ctx, tx); err != nil {
		return err
	}
	var currentName, archived sql.NullString
	var currentInterval, currentTimeout, currentVersion int64
	var state string
	if err := tx.QueryRowContext(ctx, "SELECT display_name,interval_seconds,timeout_seconds,version,archived_at,administrative_state FROM hosts WHERE id=?", id).Scan(&currentName, &currentInterval, &currentTimeout, &currentVersion, &archived, &state); errors.Is(err, sql.ErrNoRows) {
		return hostquery.ErrNotFound
	} else if err != nil {
		return err
	}
	if archived.Valid {
		return hosts.ErrHostArchived
	}
	if currentVersion != version {
		return hosts.ErrHostVersionConflict
	}
	newName := currentName.String
	if patch.DisplayName != nil {
		if err := validateManagementText(*patch.DisplayName, 128); err != nil {
			return err
		}
		newName = *patch.DisplayName
	}
	newInterval, newTimeout := currentInterval, currentTimeout
	if patch.IntervalSeconds != nil {
		newInterval = int64(*patch.IntervalSeconds)
	}
	if patch.TimeoutSeconds != nil {
		newTimeout = int64(*patch.TimeoutSeconds)
	}
	if err := hosts.ValidateSchedule(int(newInterval), int(newTimeout)); err != nil {
		return err
	}
	changedSchedule := newInterval != currentInterval || newTimeout != currentTimeout
	if newName == currentName.String && !changedSchedule {
		return tx.Commit()
	}
	if newName != currentName.String {
		var duplicate int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM hosts WHERE display_name=? AND id<>?", newName, id).Scan(&duplicate); err != nil {
			return err
		}
		if duplicate != 0 {
			return hosts.ErrHostConflict
		}
	}
	stamp := dbTime(now)
	if _, err := tx.ExecContext(ctx, `UPDATE hosts SET display_name=?,interval_seconds=?,timeout_seconds=?,collection_generation=collection_generation+?,updated_at=?,version=version+1 WHERE id=? AND version=?`, newName, newInterval, newTimeout, boolInt(changedSchedule), stamp, id, version); err != nil {
		return err
	}
	if changedSchedule {
		if err := resetHostSchedule(ctx, tx, id, now, newInterval, newTimeout, managementRandom{random}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (database *Database) SetHostAdministrativeState(ctx context.Context, id string, version int64, action string, now time.Time, random io.Reader) error {
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := database.requireCurrentSchema(ctx, tx); err != nil {
		return err
	}
	var currentVersion, interval, timeout int64
	var state string
	var archived sql.NullString
	var lastSuccess sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT version,administrative_state,archived_at,last_success_received_at,interval_seconds,timeout_seconds FROM hosts WHERE id=?", id).Scan(&currentVersion, &state, &archived, &lastSuccess, &interval, &timeout); errors.Is(err, sql.ErrNoRows) {
		return hostquery.ErrNotFound
	} else if err != nil {
		return err
	}
	if currentVersion != version {
		return hosts.ErrHostVersionConflict
	}
	if action == "enable" {
		if archived.Valid {
			return hosts.ErrHostArchived
		}
		if state == "active" || state == "pending" {
			return tx.Commit()
		}
		newState := "pending"
		if lastSuccess.Valid {
			newState = "active"
		}
		stamp := dbTime(now)
		if _, err := tx.ExecContext(ctx, `UPDATE hosts SET administrative_state=?,observed_state='unknown',stale=0,collection_generation=collection_generation+1,updated_at=?,version=version+1 WHERE id=? AND version=?`, newState, stamp, id, version); err != nil {
			return err
		}
		if err := resetHostSchedule(ctx, tx, id, now, interval, timeout, managementRandom{random}); err != nil {
			return err
		}
		return tx.Commit()
	}
	if action != "disable" && action != "archive" {
		return hosts.ErrInvalidManagement
	}
	if action == "disable" && state == "disabled" && !archived.Valid {
		return tx.Commit()
	}
	stamp := dbTime(now)
	if action == "archive" {
		if _, err := tx.ExecContext(ctx, `UPDATE hosts SET administrative_state='disabled',observed_state='unknown',stale=0,archived_at=?,collection_generation=collection_generation+1,updated_at=?,version=version+1 WHERE id=? AND version=?`, stamp, stamp, id, version); err != nil {
			return err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE hosts SET administrative_state='disabled',observed_state='unknown',stale=0,collection_generation=collection_generation+1,updated_at=?,version=version+1 WHERE id=? AND version=?`, stamp, id, version); err != nil {
			return err
		}
	}
	reason := "target_disabled"
	if action == "archive" {
		reason = "target_archived"
	}
	if err := cancelHostSchedule(ctx, tx, id, now, reason); err != nil {
		return err
	}
	return tx.Commit()
}

func (database *Database) RestoreHost(ctx context.Context, id string, version int64, now time.Time) error {
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := database.requireCurrentSchema(ctx, tx); err != nil {
		return err
	}
	var current int64
	var archived sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT version,archived_at FROM hosts WHERE id=?", id).Scan(&current, &archived); errors.Is(err, sql.ErrNoRows) {
		return hostquery.ErrNotFound
	} else if err != nil {
		return err
	}
	if current != version {
		return hosts.ErrHostVersionConflict
	}
	if !archived.Valid {
		return tx.Commit()
	}
	stamp := dbTime(now)
	_, err = tx.ExecContext(ctx, `UPDATE hosts SET archived_at=NULL,administrative_state='disabled',observed_state='unknown',stale=0,collection_generation=collection_generation+1,updated_at=?,version=version+1 WHERE id=? AND version=?`, stamp, id, version)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (database *Database) SelectFilesystem(ctx context.Context, hostID, filesystemID string, version int64, selected bool, now time.Time) error {
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := database.requireCurrentSchema(ctx, tx); err != nil {
		return err
	}
	var hostArchived sql.NullString
	var fsVersion int64
	var existing int
	var hostVersion int64
	if err := tx.QueryRowContext(ctx, "SELECT h.archived_at,f.version,f.selected,h.version FROM filesystems f JOIN hosts h ON h.id=f.host_id WHERE f.id=? AND f.host_id=?", filesystemID, hostID).Scan(&hostArchived, &fsVersion, &existing, &hostVersion); errors.Is(err, sql.ErrNoRows) {
		return hostquery.ErrNotFound
	} else if err != nil {
		return err
	}
	if hostArchived.Valid {
		return hosts.ErrHostArchived
	}
	if fsVersion != version {
		return hosts.ErrHostVersionConflict
	}
	if boolInt(selected) == existing {
		return tx.Commit()
	}
	stamp := dbTime(now)
	if _, err := tx.ExecContext(ctx, "UPDATE filesystems SET selected=?,version=version+1 WHERE id=? AND version=?", boolInt(selected), filesystemID, version); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE hosts SET updated_at=?,version=version+1 WHERE id=? AND version=?", stamp, hostID, hostVersion)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func cancelHostSchedule(ctx context.Context, tx *sql.Tx, hostID string, now time.Time, reason string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE schedule_slots SET state='cancelled',lease_until=NULL,cancellation_reason=?,finished_at=? WHERE target_type='host' AND target_id=? AND state IN ('scheduled','running')`, reason, dbTime(now), hostID); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, "SELECT id FROM schedule_periods WHERE target_type='host' AND target_id=? AND ends_at IS NULL", hostID)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := closePeriod(ctx, tx, id, now); err != nil {
			return err
		}
	}
	return nil
}

func resetHostSchedule(ctx context.Context, tx *sql.Tx, hostID string, now time.Time, interval, timeout int64, random collection.Random) error {
	if err := cancelHostSchedule(ctx, tx, hostID, now, "target_changed"); err != nil {
		return err
	}
	return createAnchoredSchedule(ctx, tx, hostID, now, interval, timeout, random)
}
