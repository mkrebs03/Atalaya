package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mkrebs03/atalaya/internal/checks"
)

var _ checks.Repository = (*Database)(nil)
var _ checks.SchedulerRepository = (*Database)(nil)
var _ checks.ManualRepository = (*Database)(nil)

const checkSelect = `SELECT id,name,related_host_id,type,target_url,target_host,target_port,interval_seconds,timeout_seconds,expected_status_min,expected_status_max,follow_redirects,max_redirects,enabled,observed_state,last_attempt_at,last_result_at,freshness_deadline_at,consecutive_failures,last_latency_ms,last_error_code,last_http_status,archived_at,created_at,updated_at,configuration_generation,version FROM service_checks`

type checkNullableInt struct {
	value int64
	valid bool
}

func (value *checkNullableInt) Scan(source any) error {
	switch item := source.(type) {
	case nil:
		value.valid = false
		return nil
	case int64:
		value.value, value.valid = item, true
		return nil
	default:
		return fmt.Errorf("unexpected nullable integer %T", source)
	}
}

type checkScanner interface {
	Scan(...any) error
}

func scanCheck(scanner checkScanner) (checks.Check, error) {
	var item checks.Check
	var relatedHost, targetURL, targetHost, lastAttempt, lastResult, freshness, lastError, archived, created, updated sql.NullString
	var targetPort, expectedMin, expectedMax, follow, redirects, lastLatency, lastStatus checkNullableInt
	var typeName string
	var interval, timeout, enabled, consecutive, generation, version int64
	var observed string
	if err := scanner.Scan(&item.ID, &item.Name, &relatedHost, &typeName, &targetURL, &targetHost, &targetPort, &interval, &timeout, &expectedMin, &expectedMax, &follow, &redirects, &enabled, &observed, &lastAttempt, &lastResult, &freshness, &consecutive, &lastLatency, &lastError, &lastStatus, &archived, &created, &updated, &generation, &version); err != nil {
		return checks.Check{}, err
	}
	item.RelatedHostID = stringPointer(relatedHost)
	item.ArchivedAt, _ = timePointer(archived)
	item.LastAttemptAt, _ = timePointer(lastAttempt)
	item.LastResultAt, _ = timePointer(lastResult)
	item.FreshnessDeadlineAt, _ = timePointer(freshness)
	item.CreatedAt, _ = parseDBTime(created.String)
	item.UpdatedAt, _ = parseDBTime(updated.String)
	item.Enabled = enabled != 0
	item.ObservedState = checks.State(observed)
	item.ConsecutiveFailures = consecutive
	item.LastLatencyMS = checkIntPointer(lastLatency)
	item.LastHTTPStatus = checkIntValue(lastStatus)
	if lastError.Valid {
		code := checks.ErrorCode(lastError.String)
		item.LastErrorCode = &code
	}
	item.ConfigurationGeneration = generation
	item.Version = version
	switch checks.Type(typeName) {
	case checks.TypeHTTP:
		item.Config = checks.Config{Type: checks.TypeHTTP, HTTP: &checks.HTTPConfig{
			URL: targetURL.String, IntervalSeconds: int(interval), TimeoutSeconds: int(timeout), ExpectedStatusMin: int(expectedMin.value), ExpectedStatusMax: int(expectedMax.value), FollowRedirects: follow.value != 0, MaxRedirects: int(redirects.value),
		}}
	case checks.TypeTCP:
		item.Config = checks.Config{Type: checks.TypeTCP, TCP: &checks.TCPConfig{Host: targetHost.String, Port: int(targetPort.value), IntervalSeconds: int(interval), TimeoutSeconds: int(timeout)}}
	default:
		return checks.Check{}, fmt.Errorf("unknown check type %q", typeName)
	}
	return item, nil
}

func checkIntPointer(value checkNullableInt) *int64 {
	if !value.valid {
		return nil
	}
	return &value.value
}

func checkIntValue(value checkNullableInt) *int {
	if !value.valid {
		return nil
	}
	converted := int(value.value)
	return &converted
}

func (database *Database) List(ctx context.Context, limit int, cursor *checks.Cursor, includeArchived bool) ([]checks.Check, error) {
	if limit < 1 || limit > 101 {
		return nil, fmt.Errorf("invalid check list limit")
	}
	query := checkSelect + " WHERE (? OR archived_at IS NULL)"
	args := []any{includeArchived}
	if cursor != nil {
		query += " AND (name COLLATE NOCASE > ? OR (name COLLATE NOCASE = ? AND id > ?))"
		args = append(args, cursor.Name, cursor.Name, cursor.ID)
	}
	query += " ORDER BY name COLLATE NOCASE ASC,id ASC LIMIT ?"
	args = append(args, limit)
	rows, err := database.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]checks.Check, 0, limit)
	for rows.Next() {
		item, err := scanCheck(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (database *Database) Get(ctx context.Context, id string) (checks.Check, error) {
	item, err := scanCheck(database.sql.QueryRowContext(ctx, checkSelect+" WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return checks.Check{}, checks.ErrNotFound
	}
	return item, err
}

func (database *Database) Create(ctx context.Context, item checks.Check, now time.Time, random checks.IDSource) (checks.Check, error) {
	if random == nil || checks.ValidateName(item.Name) != nil || checks.ValidateConfig(item.Config) != nil {
		return checks.Check{}, checks.ErrInvalid
	}
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return checks.Check{}, err
	}
	defer tx.Rollback()
	if item.RelatedHostID != nil {
		var exists int
		if err := tx.QueryRowContext(ctx, "SELECT 1 FROM hosts WHERE id=?", *item.RelatedHostID).Scan(&exists); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return checks.Check{}, checks.ErrInvalid
			}
			return checks.Check{}, err
		}
	}
	id, err := random.ID()
	if err != nil {
		return checks.Check{}, err
	}
	created := now.UTC()
	if err := insertCheck(ctx, tx, id, item, created, created, 1, 1); err != nil {
		return checks.Check{}, err
	}
	if err := createCheckSchedule(ctx, tx, id, item.Config, created, random); err != nil {
		return checks.Check{}, err
	}
	if err := tx.Commit(); err != nil {
		return checks.Check{}, err
	}
	return database.Get(ctx, id)
}

func insertCheck(ctx context.Context, tx *sql.Tx, id string, item checks.Check, created, updated time.Time, generation, version int64) error {
	values := checkConfigValues(item.Config)
	_, err := tx.ExecContext(ctx, `INSERT INTO service_checks(id,name,related_host_id,type,target_url,target_host,target_port,interval_seconds,timeout_seconds,expected_status_min,expected_status_max,follow_redirects,max_redirects,enabled,observed_state,consecutive_failures,created_at,updated_at,configuration_generation,version) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, item.Name, nullableDBStringPointer(item.RelatedHostID), values[0], values[1], values[2], values[3], values[4], values[5], values[6], values[7], values[8], values[9], 1, checks.StateUnknown, 0, dbTime(created), dbTime(updated), generation, version)
	return err
}

func checkConfigValues(config checks.Config) []any {
	if config.HTTP != nil {
		return []any{string(checks.TypeHTTP), stringValue(config.HTTP.URL), nil, nil, config.HTTP.IntervalSeconds, config.HTTP.TimeoutSeconds, config.HTTP.ExpectedStatusMin, config.HTTP.ExpectedStatusMax, boolInt(config.HTTP.FollowRedirects), config.HTTP.MaxRedirects}
	}
	return []any{string(checks.TypeTCP), nil, stringValue(config.TCP.Host), config.TCP.Port, config.TCP.IntervalSeconds, config.TCP.TimeoutSeconds, nil, nil, nil, nil}
}

func stringValue(value string) any { return value }

func nullableDBStringPointer(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func (database *Database) Update(ctx context.Context, id string, version int64, item checks.Check, now time.Time, random checks.IDSource) error {
	if random == nil || checks.ValidateName(item.Name) != nil || checks.ValidateConfig(item.Config) != nil {
		return checks.ErrInvalid
	}
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, err := scanCheck(tx.QueryRowContext(ctx, checkSelect+" WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return checks.ErrNotFound
	}
	if err != nil {
		return err
	}
	if current.Version != version {
		return checks.ErrVersionConflict
	}
	if current.ArchivedAt != nil {
		return checks.ErrArchived
	}
	if item.RelatedHostID != nil {
		var exists int
		if err := tx.QueryRowContext(ctx, "SELECT 1 FROM hosts WHERE id=?", *item.RelatedHostID).Scan(&exists); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return checks.ErrInvalid
			}
			return err
		}
	}
	changedConfig := !configsEqual(current.Config, item.Config)
	generation := current.ConfigurationGeneration
	if changedConfig {
		generation++
	}
	values := checkConfigValues(item.Config)
	_, err = tx.ExecContext(ctx, `UPDATE service_checks SET name=?,related_host_id=?,type=?,target_url=?,target_host=?,target_port=?,interval_seconds=?,timeout_seconds=?,expected_status_min=?,expected_status_max=?,follow_redirects=?,max_redirects=?,updated_at=?,configuration_generation=?,version=version+1 WHERE id=? AND version=?`, item.Name, nullableDBStringPointer(item.RelatedHostID), values[0], values[1], values[2], values[3], values[4], values[5], values[6], values[7], values[8], values[9], dbTime(now), generation, id, version)
	if err != nil {
		return err
	}
	if changedConfig {
		if err := cancelCheckSchedule(ctx, tx, id, "target_changed", now); err != nil {
			return err
		}
		if current.Enabled {
			if err := createCheckSchedule(ctx, tx, id, item.Config, now.UTC(), random); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func configsEqual(left, right checks.Config) bool {
	if left.Type != right.Type {
		return false
	}
	if left.HTTP != nil && right.HTTP != nil {
		return *left.HTTP == *right.HTTP
	}
	if left.TCP != nil && right.TCP != nil {
		return *left.TCP == *right.TCP
	}
	return left.HTTP == nil && left.TCP == nil && right.HTTP == nil && right.TCP == nil
}

func (database *Database) Archive(ctx context.Context, id string, version int64, now time.Time) error {
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current int64
	var archived sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT version,archived_at FROM service_checks WHERE id=?", id).Scan(&current, &archived); errors.Is(err, sql.ErrNoRows) {
		return checks.ErrNotFound
	} else if err != nil {
		return err
	}
	if current != version {
		return checks.ErrVersionConflict
	}
	if archived.Valid {
		return checks.ErrArchived
	}
	if _, err := tx.ExecContext(ctx, "UPDATE service_checks SET enabled=0,observed_state='unknown',archived_at=?,updated_at=?,version=version+1 WHERE id=? AND version=?", dbTime(now), dbTime(now), id, version); err != nil {
		return err
	}
	if err := cancelCheckSchedule(ctx, tx, id, "target_archived", now); err != nil {
		return err
	}
	if err := database.resolveTargetRulesTx(ctx, tx, "check", id, now, "target_archived", alertRandomSource()); err != nil {
		return err
	}
	return tx.Commit()
}

func (database *Database) SetEnabled(ctx context.Context, id string, version int64, enabled bool, now time.Time, random checks.IDSource) error {
	if random == nil {
		return checks.ErrInvalid
	}
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, err := scanCheck(tx.QueryRowContext(ctx, checkSelect+" WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return checks.ErrNotFound
	}
	if err != nil {
		return err
	}
	if current.Version != version {
		return checks.ErrVersionConflict
	}
	if current.ArchivedAt != nil {
		return checks.ErrArchived
	}
	if current.Enabled == enabled {
		return nil
	}
	if _, err := tx.ExecContext(ctx, "UPDATE service_checks SET enabled=?,observed_state='unknown',updated_at=?,version=version+1 WHERE id=? AND version=?", boolInt(enabled), dbTime(now), id, version); err != nil {
		return err
	}
	if !enabled {
		if err := cancelCheckSchedule(ctx, tx, id, "target_disabled", now); err != nil {
			return err
		}
		if err := database.resolveTargetRulesTx(ctx, tx, "check", id, now, "target_disabled", random); err != nil {
			return err
		}
	} else if err := createCheckSchedule(ctx, tx, id, current.Config, now.UTC(), random); err != nil {
		return err
	}
	return tx.Commit()
}

func (database *Database) Restore(ctx context.Context, id string, version int64, now time.Time) error {
	result, err := database.sql.ExecContext(ctx, "UPDATE service_checks SET archived_at=NULL,enabled=0,observed_state='unknown',updated_at=?,version=version+1 WHERE id=? AND version=? AND archived_at IS NOT NULL", dbTime(now), id, version)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 1 {
		return nil
	}
	var exists int
	if err := database.sql.QueryRowContext(ctx, "SELECT 1 FROM service_checks WHERE id=?", id).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return checks.ErrNotFound
	}
	return checks.ErrVersionConflict
}

func createCheckSchedule(ctx context.Context, tx *sql.Tx, checkID string, config checks.Config, now time.Time, random checks.IDSource) error {
	periodID, err := random.ID()
	if err != nil {
		return err
	}
	slotID, err := random.ID()
	if err != nil {
		return err
	}
	interval := int64(config.Interval() / time.Second)
	timeout := int64(config.Timeout() / time.Second)
	jitter := time.Duration(random.Intn(6)) * time.Second
	if _, err := tx.ExecContext(ctx, `INSERT INTO schedule_periods(id,target_type,target_id,starts_at,effective_interval_seconds,effective_timeout_seconds) VALUES(?,'check',?,?,?,?)`, periodID, checkID, dbTime(now), interval, timeout); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO schedule_slots(id,target_type,target_id,schedule_period_id,nominal_at,not_before_at,deadline_at,effective_interval_seconds,effective_timeout_seconds,state,process_attempts) VALUES(?,'check',?,?,?,?,?,?,?,'scheduled',0)`, slotID, checkID, periodID, dbTime(now), dbTime(now.Add(jitter)), dbTime(now.Add(5*time.Second+time.Duration(timeout)*time.Second)), interval, timeout)
	return err
}

func cancelCheckSchedule(ctx context.Context, tx *sql.Tx, checkID, reason string, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `UPDATE schedule_slots SET state='cancelled',lease_until=NULL,cancellation_reason=?,finished_at=? WHERE target_type='check' AND target_id=? AND state IN ('scheduled','running')`, reason, dbTime(now), checkID); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, "SELECT id FROM schedule_periods WHERE target_type='check' AND target_id=? AND ends_at IS NULL", checkID)
	if err != nil {
		return err
	}
	var periods []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		periods = append(periods, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, period := range periods {
		if err := closePeriod(ctx, tx, period, now); err != nil {
			return err
		}
	}
	return nil
}

func (database *Database) GetCheckSettings(ctx context.Context) (checks.Settings, error) {
	var result checks.Settings
	var raw, updated string
	if err := database.sql.QueryRowContext(ctx, "SELECT interval_seconds,timeout_seconds,allowed_private_cidrs,updated_at FROM check_settings WHERE id=1").Scan(&result.IntervalSeconds, &result.TimeoutSeconds, &raw, &updated); err != nil {
		return result, err
	}
	if err := json.Unmarshal([]byte(raw), &result.AllowedPrivateCIDRs); err != nil {
		return result, fmt.Errorf("decode check settings: %w", err)
	}
	result.UpdatedAt, _ = parseDBTime(updated)
	row := database.sql.QueryRowContext(ctx, "SELECT version FROM retention_settings WHERE id=1")
	if err := row.Scan(&result.Version); err != nil {
		return result, err
	}
	return result, nil
}

func (database *Database) UpdateCheckSettings(ctx context.Context, patch checks.SettingsPatch, version int64, now time.Time) (checks.Settings, error) {
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return checks.Settings{}, err
	}
	defer tx.Rollback()
	var current checks.Settings
	var raw, updated string
	if err := tx.QueryRowContext(ctx, "SELECT interval_seconds,timeout_seconds,allowed_private_cidrs,updated_at FROM check_settings WHERE id=1").Scan(&current.IntervalSeconds, &current.TimeoutSeconds, &raw, &updated); err != nil {
		return checks.Settings{}, err
	}
	if err := json.Unmarshal([]byte(raw), &current.AllowedPrivateCIDRs); err != nil {
		return checks.Settings{}, err
	}
	if err := tx.QueryRowContext(ctx, "SELECT version FROM retention_settings WHERE id=1").Scan(&current.Version); err != nil {
		return checks.Settings{}, err
	}
	if current.Version != version {
		return checks.Settings{}, checks.ErrVersionConflict
	}
	if patch.IntervalSeconds != nil {
		current.IntervalSeconds = *patch.IntervalSeconds
	}
	if patch.TimeoutSeconds != nil {
		current.TimeoutSeconds = *patch.TimeoutSeconds
	}
	if patch.AllowedPrivateCIDRs != nil {
		current.AllowedPrivateCIDRs = append([]string(nil), (*patch.AllowedPrivateCIDRs)...)
	}
	if current.IntervalSeconds < checks.MinIntervalSeconds || current.IntervalSeconds > checks.MaxIntervalSeconds || current.TimeoutSeconds < checks.MinTimeoutSeconds || current.TimeoutSeconds > checks.MaxTimeoutSeconds || current.TimeoutSeconds >= current.IntervalSeconds {
		return checks.Settings{}, checks.ErrInvalidSettings
	}
	policy, err := checks.NewNetworkPolicy(current.AllowedPrivateCIDRs)
	if err != nil {
		return checks.Settings{}, err
	}
	current.AllowedPrivateCIDRs = current.AllowedPrivateCIDRs[:0]
	for _, prefix := range policy.PrivateCIDRs {
		current.AllowedPrivateCIDRs = append(current.AllowedPrivateCIDRs, prefix.String())
	}
	encoded, err := json.Marshal(current.AllowedPrivateCIDRs)
	if err != nil {
		return checks.Settings{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE check_settings SET interval_seconds=?,timeout_seconds=?,allowed_private_cidrs=?,updated_at=? WHERE id=1", current.IntervalSeconds, current.TimeoutSeconds, string(encoded), dbTime(now)); err != nil {
		return checks.Settings{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE retention_settings SET version=version+1,updated_at=? WHERE id=1 AND version=?", dbTime(now), version); err != nil {
		return checks.Settings{}, err
	}
	if err := tx.QueryRowContext(ctx, "SELECT version FROM retention_settings WHERE id=1").Scan(&current.Version); err != nil {
		return checks.Settings{}, err
	}
	current.UpdatedAt = now.UTC()
	if err := tx.Commit(); err != nil {
		return checks.Settings{}, err
	}
	return current, nil
}

func (database *Database) ClaimScheduled(ctx context.Context, now time.Time) (checks.ScheduledClaim, error) {
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return checks.ScheduledClaim{}, err
	}
	defer tx.Rollback()
	var claim checks.ScheduledClaim
	var typeName string
	var targetURL, targetHost sql.NullString
	var targetPort, expectedMin, expectedMax, follow, redirects checkNullableInt
	var nominal, deadline string
	var interval, timeout, generation, effectiveInterval, effectiveTimeout, processAttempts int64
	err = tx.QueryRowContext(ctx, `SELECT s.id,c.id,c.type,c.target_url,c.target_host,c.target_port,c.interval_seconds,c.timeout_seconds,c.expected_status_min,c.expected_status_max,c.follow_redirects,c.max_redirects,c.configuration_generation,s.nominal_at,s.deadline_at,s.effective_interval_seconds,s.effective_timeout_seconds,s.process_attempts FROM schedule_slots s JOIN service_checks c ON c.id=s.target_id WHERE s.target_type='check' AND s.state='scheduled' AND s.process_attempts<2 AND s.not_before_at<=? AND s.deadline_at>=? AND c.enabled=1 AND c.archived_at IS NULL ORDER BY s.not_before_at,s.id LIMIT 1`, dbTime(now), dbTime(now)).Scan(&claim.SlotID, &claim.CheckID, &typeName, &targetURL, &targetHost, &targetPort, &interval, &timeout, &expectedMin, &expectedMax, &follow, &redirects, &generation, &nominal, &deadline, &effectiveInterval, &effectiveTimeout, &processAttempts)
	if errors.Is(err, sql.ErrNoRows) {
		return checks.ScheduledClaim{}, checks.ErrNoDueSlot
	}
	if err != nil {
		return checks.ScheduledClaim{}, err
	}
	claim.Config = configFromColumns(typeName, targetURL, targetHost, targetPort, interval, timeout, expectedMin, expectedMax, follow, redirects)
	claim.Generation = generation
	claim.NominalAt, err = parseDBTime(nominal)
	if err != nil {
		return checks.ScheduledClaim{}, err
	}
	claim.DeadlineAt, err = parseDBTime(deadline)
	if err != nil {
		return checks.ScheduledClaim{}, err
	}
	claim.Interval = time.Duration(effectiveInterval) * time.Second
	claim.Timeout = time.Duration(effectiveTimeout) * time.Second
	claim.Attempt = int(processAttempts) + 1
	claim.LeaseUntil = now.Add(claim.Timeout + 5*time.Second)
	result, err := tx.ExecContext(ctx, `UPDATE schedule_slots SET state='running',lease_until=?,started_at=COALESCE(started_at,?),process_attempts=process_attempts+1 WHERE id=? AND state='scheduled' AND process_attempts=?`, dbTime(claim.LeaseUntil), dbTime(now), claim.SlotID, processAttempts)
	if err != nil {
		return checks.ScheduledClaim{}, err
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return checks.ScheduledClaim{}, checks.ErrNoDueSlot
	}
	if err := tx.Commit(); err != nil {
		return checks.ScheduledClaim{}, err
	}
	return claim, nil
}

func configFromColumns(typeName string, targetURL, targetHost sql.NullString, targetPort checkNullableInt, interval, timeout int64, expectedMin, expectedMax, follow, redirects checkNullableInt) checks.Config {
	if checks.Type(typeName) == checks.TypeHTTP {
		return checks.Config{Type: checks.TypeHTTP, HTTP: &checks.HTTPConfig{URL: targetURL.String, IntervalSeconds: int(interval), TimeoutSeconds: int(timeout), ExpectedStatusMin: int(expectedMin.value), ExpectedStatusMax: int(expectedMax.value), FollowRedirects: follow.value != 0, MaxRedirects: int(redirects.value)}}
	}
	return checks.Config{Type: checks.TypeTCP, TCP: &checks.TCPConfig{Host: targetHost.String, Port: int(targetPort.value), IntervalSeconds: int(interval), TimeoutSeconds: int(timeout)}}
}

func (database *Database) FinishScheduled(ctx context.Context, claim checks.ScheduledClaim, result checks.Result, random checks.IDSource) error {
	if random == nil {
		return checks.ErrInvalid
	}
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var enabled int
	var archived sql.NullString
	var generation int64
	var periodID string
	var typeName string
	var targetURL, targetHost sql.NullString
	var targetPort, expectedMin, expectedMax, follow, redirects checkNullableInt
	var interval, timeout int64
	err = tx.QueryRowContext(ctx, `SELECT c.enabled,c.archived_at,c.configuration_generation,s.schedule_period_id,c.type,c.target_url,c.target_host,c.target_port,c.interval_seconds,c.timeout_seconds,c.expected_status_min,c.expected_status_max,c.follow_redirects,c.max_redirects FROM service_checks c JOIN schedule_slots s ON s.target_type='check' AND s.target_id=c.id WHERE c.id=? AND s.id=? AND s.state='running' AND s.process_attempts=? AND s.lease_until=?`, claim.CheckID, claim.SlotID, claim.Attempt, dbTime(claim.LeaseUntil)).Scan(&enabled, &archived, &generation, &periodID, &typeName, &targetURL, &targetHost, &targetPort, &interval, &timeout, &expectedMin, &expectedMax, &follow, &redirects)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	currentConfig := configFromColumns(typeName, targetURL, targetHost, targetPort, interval, timeout, expectedMin, expectedMax, follow, redirects)
	if archived.Valid || enabled == 0 || generation != claim.Generation {
		reason := "target_changed"
		if archived.Valid {
			reason = "target_archived"
		} else if enabled == 0 {
			reason = "target_disabled"
		}
		if _, err := tx.ExecContext(ctx, `UPDATE schedule_slots SET state='cancelled',lease_until=NULL,cancellation_reason=?,finished_at=? WHERE id=? AND state='running' AND process_attempts=? AND lease_until=?`, reason, dbTime(result.ReceivedAt), claim.SlotID, claim.Attempt, dbTime(claim.LeaseUntil)); err != nil {
			return err
		}
		if err := closePeriod(ctx, tx, periodID, result.ReceivedAt); err != nil {
			return err
		}
		if enabled != 0 && !archived.Valid && generation != claim.Generation {
			if err := createCheckSchedule(ctx, tx, claim.CheckID, currentConfig, result.ReceivedAt, random); err != nil {
				return err
			}
		}
		resolutionReason := reason
		if archived.Valid {
			resolutionReason = "target_archived"
		}
		if err := database.resolveTargetRulesTx(ctx, tx, "check", claim.CheckID, result.ReceivedAt, resolutionReason, random); err != nil {
			return err
		}
		return tx.Commit()
	}
	nextNominal := claim.NominalAt.Add(claim.Interval)
	nextID, err := random.ID()
	if err != nil {
		return err
	}
	jitter := time.Duration(random.Intn(6)) * time.Second
	terminalState := "succeeded"
	if result.ErrorCode != nil && *result.ErrorCode == checks.ErrorInternal {
		terminalState = "failed"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE schedule_slots SET state=?,lease_until=NULL,finished_at=? WHERE id=? AND state='running' AND process_attempts=? AND lease_until=?`, terminalState, dbTime(result.ReceivedAt), claim.SlotID, claim.Attempt, dbTime(claim.LeaseUntil)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schedule_slots(id,target_type,target_id,schedule_period_id,nominal_at,not_before_at,deadline_at,effective_interval_seconds,effective_timeout_seconds,state,process_attempts) VALUES(?,'check',?,?,?,?,?,?,?,'scheduled',0)`, nextID, claim.CheckID, periodID, dbTime(nextNominal), dbTime(nextNominal.Add(jitter)), dbTime(nextNominal.Add(5*time.Second+claim.Timeout)), int64(claim.Interval/time.Second), int64(claim.Timeout/time.Second)); err != nil {
		return err
	}
	if terminalState == "failed" {
		_, err = tx.ExecContext(ctx, "UPDATE service_checks SET last_attempt_at=?,updated_at=?,version=version+1 WHERE id=?", dbTime(result.ReceivedAt), dbTime(result.ReceivedAt), claim.CheckID)
		if err != nil {
			return err
		}
		// An internal scheduler failure closes the slot without a result row.
		// That missing evidence must break pending continuity and make a
		// firing incident unknown.
		if err := database.evaluateCheckAlertsTx(ctx, tx, claim.CheckID, "", result.ReceivedAt, random); err != nil {
			return err
		}
		return tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO check_results(check_id,slot_id,received_at,status,latency_ms,error_code,http_status) VALUES(?,?,?,?,?,?,?) ON CONFLICT(check_id,slot_id) DO NOTHING`, claim.CheckID, claim.SlotID, dbTime(result.ReceivedAt), result.Status, nullableIntPointer(result.LatencyMS), nullableError(result.ErrorCode), nullableCheckStatus(result.HTTPStatus)); err != nil {
		return err
	}
	freshness := nextNominal.Add(10*time.Second + claim.Timeout)
	if _, err := tx.ExecContext(ctx, `UPDATE service_checks SET observed_state=?,last_attempt_at=?,last_result_at=?,freshness_deadline_at=?,consecutive_failures=CASE WHEN ?='down' THEN consecutive_failures+1 ELSE 0 END,last_latency_ms=?,last_error_code=?,last_http_status=?,updated_at=?,version=version+1 WHERE id=?`, result.Status, dbTime(result.ReceivedAt), dbTime(result.ReceivedAt), dbTime(freshness), result.Status, nullableIntPointer(result.LatencyMS), nullableError(result.ErrorCode), nullableCheckStatus(result.HTTPStatus), dbTime(result.ReceivedAt), claim.CheckID); err != nil {
		return err
	}
	if err := database.evaluateCheckAlertsTx(ctx, tx, claim.CheckID, string(result.Status), result.ReceivedAt, random); err != nil {
		return err
	}
	return tx.Commit()
}

func errOrCommit(tx *sql.Tx, err error) error {
	if err != nil {
		return err
	}
	return tx.Commit()
}

func nullableIntPointer(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableCheckStatus(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableError(value *checks.ErrorCode) any {
	if value == nil {
		return nil
	}
	return string(*value)
}

func (database *Database) ReleaseScheduled(ctx context.Context, claim checks.ScheduledClaim, _ time.Time) error {
	_, err := database.sql.ExecContext(ctx, "UPDATE schedule_slots SET state='scheduled',lease_until=NULL WHERE id=? AND state='running' AND process_attempts=? AND lease_until=?", claim.SlotID, claim.Attempt, dbTime(claim.LeaseUntil))
	return err
}

func (database *Database) MaintainScheduled(ctx context.Context, now time.Time, random checks.IDSource) error {
	if random == nil {
		return checks.ErrInvalid
	}
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE schedule_slots SET state='cancelled',lease_until=NULL,cancellation_reason=CASE WHEN c.archived_at IS NULL THEN 'target_disabled' ELSE 'target_archived' END,finished_at=? FROM service_checks c WHERE schedule_slots.target_type='check' AND schedule_slots.target_id=c.id AND schedule_slots.state IN ('scheduled','running') AND (c.enabled=0 OR c.archived_at IS NOT NULL)`, dbTime(now)); err != nil {
		return err
	}
	periods, err := tx.QueryContext(ctx, "SELECT p.id FROM schedule_periods p JOIN service_checks c ON c.id=p.target_id WHERE p.target_type='check' AND p.ends_at IS NULL AND (c.enabled=0 OR c.archived_at IS NOT NULL)")
	if err != nil {
		return err
	}
	var closed []string
	for periods.Next() {
		var id string
		if err := periods.Scan(&id); err != nil {
			periods.Close()
			return err
		}
		closed = append(closed, id)
	}
	if err := periods.Close(); err != nil {
		return err
	}
	for _, id := range closed {
		if err := closePeriod(ctx, tx, id, now); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE schedule_slots SET state='scheduled',lease_until=NULL WHERE target_type='check' AND state='running' AND lease_until<=? AND deadline_at>=? AND process_attempts<2 AND EXISTS(SELECT 1 FROM service_checks c WHERE c.id=schedule_slots.target_id AND c.enabled=1 AND c.archived_at IS NULL)`, dbTime(now), dbTime(now)); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT s.id,s.target_id,s.schedule_period_id,s.nominal_at,s.effective_interval_seconds,c.interval_seconds,c.timeout_seconds FROM schedule_slots s JOIN service_checks c ON c.id=s.target_id WHERE s.target_type='check' AND s.state IN ('scheduled','running') AND s.deadline_at<? AND c.enabled=1 AND c.archived_at IS NULL`, dbTime(now))
	if err != nil {
		return err
	}
	type expiredCheck struct {
		slot, check, period, nominal   string
		oldInterval, interval, timeout int64
	}
	var expired []expiredCheck
	for rows.Next() {
		var item expiredCheck
		if err := rows.Scan(&item.slot, &item.check, &item.period, &item.nominal, &item.oldInterval, &item.interval, &item.timeout); err != nil {
			rows.Close()
			return err
		}
		expired = append(expired, item)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range expired {
		if err := database.reanchorCheck(ctx, tx, item, now, random); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (database *Database) reanchorCheck(ctx context.Context, tx *sql.Tx, item struct {
	slot, check, period, nominal   string
	oldInterval, interval, timeout int64
}, now time.Time, random checks.IDSource) error {
	if _, err := tx.ExecContext(ctx, "UPDATE schedule_slots SET state='cancelled',lease_until=NULL,cancellation_reason='missed_deadline',finished_at=? WHERE id=?", dbTime(now), item.slot); err != nil {
		return err
	}
	nominal, err := parseDBTime(item.nominal)
	if err != nil {
		return err
	}
	end := nominal.Add(time.Duration(item.oldInterval) * time.Second)
	if now.Before(end) {
		end = now
	}
	if _, err := tx.ExecContext(ctx, "UPDATE schedule_periods SET ends_at=? WHERE id=? AND ends_at IS NULL", dbTime(end), item.period); err != nil {
		return err
	}
	// A missed deadline is an explicit missing slot. Evaluate it before
	// opening the replacement period so a later failure cannot bridge the gap.
	if err := database.evaluateCheckAlertsTx(ctx, tx, item.check, "", now, random); err != nil {
		return err
	}
	config, err := readCheckConfig(ctx, tx, item.check)
	if err != nil {
		return err
	}
	return createCheckSchedule(ctx, tx, item.check, config, now, random)
}

func readCheckConfig(ctx context.Context, tx *sql.Tx, id string) (checks.Config, error) {
	var typeName string
	var targetURL, targetHost sql.NullString
	var targetPort, expectedMin, expectedMax, follow, redirects checkNullableInt
	var interval, timeout int64
	err := tx.QueryRowContext(ctx, "SELECT type,target_url,target_host,target_port,interval_seconds,timeout_seconds,expected_status_min,expected_status_max,follow_redirects,max_redirects FROM service_checks WHERE id=?", id).Scan(&typeName, &targetURL, &targetHost, &targetPort, &interval, &timeout, &expectedMin, &expectedMax, &follow, &redirects)
	if errors.Is(err, sql.ErrNoRows) {
		return checks.Config{}, checks.ErrNotFound
	}
	return configFromColumns(typeName, targetURL, targetHost, targetPort, interval, timeout, expectedMin, expectedMax, follow, redirects), err
}

func (database *Database) EnqueueRun(ctx context.Context, checkID, requestID string, now time.Time, random checks.IDSource) (checks.ManualRun, bool, error) {
	if random == nil || checks.ValidateRequestID(requestID) != nil {
		return checks.ManualRun{}, false, checks.ErrInvalid
	}
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return checks.ManualRun{}, false, err
	}
	defer tx.Rollback()
	var archived sql.NullString
	var exists int
	if err := tx.QueryRowContext(ctx, "SELECT 1,archived_at FROM service_checks WHERE id=?", checkID).Scan(&exists, &archived); errors.Is(err, sql.ErrNoRows) {
		return checks.ManualRun{}, false, checks.ErrNotFound
	} else if err != nil {
		return checks.ManualRun{}, false, err
	}
	if archived.Valid {
		return checks.ManualRun{}, false, checks.ErrArchived
	}
	var existingID string
	if err := tx.QueryRowContext(ctx, "SELECT id FROM check_runs WHERE check_id=? AND request_id=?", checkID, requestID).Scan(&existingID); err == nil {
		if err := tx.Commit(); err != nil {
			return checks.ManualRun{}, false, err
		}
		run, err := database.GetRun(ctx, checkID, existingID)
		return run, true, err
	} else if !errors.Is(err, sql.ErrNoRows) {
		return checks.ManualRun{}, false, err
	}
	config, err := readCheckConfig(ctx, tx, checkID)
	if err != nil {
		return checks.ManualRun{}, false, err
	}
	id, err := random.ID()
	if err != nil {
		return checks.ManualRun{}, false, err
	}
	values := checkConfigValues(config)
	if _, err := tx.ExecContext(ctx, `INSERT INTO check_runs(id,check_id,request_id,state,requested_at,process_attempts,snapshot_type,snapshot_url,snapshot_host,snapshot_port,snapshot_interval_seconds,snapshot_timeout_seconds,snapshot_expected_status_min,snapshot_expected_status_max,snapshot_follow_redirects,snapshot_max_redirects) VALUES(?,?,?,'queued',?,0,?,?,?,?,?,?,?,?,?,?)`, id, checkID, requestID, dbTime(now), values[0], values[1], values[2], values[3], values[4], values[5], values[6], values[7], values[8], values[9]); err != nil {
		return checks.ManualRun{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return checks.ManualRun{}, false, err
	}
	run, err := database.GetRun(ctx, checkID, id)
	return run, false, err
}

func scanManualRun(scanner checkScanner) (checks.ManualRun, error) {
	var run checks.ManualRun
	var requested, started, completed, lease sql.NullString
	var typeName string
	var targetURL, targetHost sql.NullString
	var targetPort, expectedMin, expectedMax, follow, redirects checkNullableInt
	var interval, timeout int64
	var resultStatus, resultError, failure sql.NullString
	var resultLatency, resultHTTP checkNullableInt
	var attempts int
	if err := scanner.Scan(&run.ID, &run.CheckID, &run.RequestID, &run.State, &requested, &started, &completed, &lease, &attempts, &typeName, &targetURL, &targetHost, &targetPort, &interval, &timeout, &expectedMin, &expectedMax, &follow, &redirects, &resultStatus, &resultLatency, &resultError, &resultHTTP, &failure); err != nil {
		return checks.ManualRun{}, err
	}
	run.RequestedAt, _ = parseDBTime(requested.String)
	run.StartedAt, _ = timePointer(started)
	run.CompletedAt, _ = timePointer(completed)
	run.LeaseUntil, _ = timePointer(lease)
	run.ProcessAttempts = attempts
	run.Config = configFromColumns(typeName, targetURL, targetHost, targetPort, interval, timeout, expectedMin, expectedMax, follow, redirects)
	if resultStatus.Valid {
		result := checks.Result{Status: checks.State(resultStatus.String)}
		result.LatencyMS = checkIntPointer(resultLatency)
		if resultError.Valid {
			code := checks.ErrorCode(resultError.String)
			result.ErrorCode = &code
		}
		result.HTTPStatus = checkIntValue(resultHTTP)
		run.Result = &result
	}
	run.FailureCode = failure.String
	return run, nil
}

const manualRunSelect = `SELECT id,check_id,request_id,state,requested_at,started_at,completed_at,lease_until,process_attempts,snapshot_type,snapshot_url,snapshot_host,snapshot_port,snapshot_interval_seconds,snapshot_timeout_seconds,snapshot_expected_status_min,snapshot_expected_status_max,snapshot_follow_redirects,snapshot_max_redirects,result_status,result_latency_ms,result_error_code,result_http_status,failure_code FROM check_runs`

func (database *Database) GetRun(ctx context.Context, checkID, runID string) (checks.ManualRun, error) {
	run, err := scanManualRun(database.sql.QueryRowContext(ctx, manualRunSelect+" WHERE check_id=? AND id=?", checkID, runID))
	if errors.Is(err, sql.ErrNoRows) {
		return checks.ManualRun{}, checks.ErrRunNotFound
	}
	return run, err
}

func (database *Database) ListRuns(ctx context.Context, checkID string, limit int, cursor string) ([]checks.ManualRun, error) {
	return database.ListRunsBetween(ctx, checkID, limit, cursor, nil, nil)
}

func (database *Database) ListRunsBetween(ctx context.Context, checkID string, limit int, cursor string, from, to *time.Time) ([]checks.ManualRun, error) {
	if limit < 1 || limit > 101 {
		return nil, fmt.Errorf("invalid run list limit")
	}
	query := manualRunSelect + " WHERE check_id=?"
	args := []any{checkID}
	var exists int
	if err := database.sql.QueryRowContext(ctx, "SELECT 1 FROM service_checks WHERE id=?", checkID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return nil, checks.ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if from != nil {
		query += " AND requested_at>=?"
		args = append(args, dbTime(from.UTC()))
	}
	if to != nil {
		query += " AND requested_at<?"
		args = append(args, dbTime(to.UTC()))
	}
	if cursor != "" {
		parts := strings.SplitN(cursor, "\x00", 2)
		if len(parts) == 2 {
			query += " AND (requested_at<? OR (requested_at=? AND id<?))"
			args = append(args, parts[0], parts[0], parts[1])
		}
	}
	query += " ORDER BY requested_at DESC,id DESC LIMIT ?"
	args = append(args, limit)
	rows, err := database.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]checks.ManualRun, 0, limit)
	for rows.Next() {
		item, err := scanManualRun(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (database *Database) ClaimManualRun(ctx context.Context, now time.Time) (checks.ManualClaim, error) {
	for {
		tx, err := database.sql.BeginTx(ctx, nil)
		if err != nil {
			return checks.ManualClaim{}, err
		}
		defer tx.Rollback()
		if _, err := tx.ExecContext(ctx, `UPDATE check_runs SET state='queued',lease_until=NULL WHERE state='running' AND lease_until<=? AND process_attempts<2`, dbTime(now)); err != nil {
			return checks.ManualClaim{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE check_runs SET state='failed',lease_until=NULL,completed_at=?,failure_code='retry_exhausted' WHERE state='running' AND lease_until<=? AND process_attempts>=2`, dbTime(now), dbTime(now)); err != nil {
			return checks.ManualClaim{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE check_runs SET state='failed',lease_until=NULL,completed_at=?,failure_code='retry_exhausted' WHERE state='queued' AND process_attempts>=2`, dbTime(now)); err != nil {
			return checks.ManualClaim{}, err
		}
		var claim checks.ManualClaim
		var archived sql.NullString
		var typeName string
		var targetURL, targetHost sql.NullString
		var targetPort, expectedMin, expectedMax, follow, redirects checkNullableInt
		var interval, timeout int64
		err = tx.QueryRowContext(ctx, `SELECT r.id,r.check_id,c.archived_at,c.type,r.snapshot_url,r.snapshot_host,r.snapshot_port,r.snapshot_interval_seconds,r.snapshot_timeout_seconds,r.snapshot_expected_status_min,r.snapshot_expected_status_max,r.snapshot_follow_redirects,r.snapshot_max_redirects,r.process_attempts FROM check_runs r JOIN service_checks c ON c.id=r.check_id WHERE r.state='queued' AND r.process_attempts<2 ORDER BY r.requested_at,r.id LIMIT 1`).Scan(&claim.RunID, &claim.CheckID, &archived, &typeName, &targetURL, &targetHost, &targetPort, &interval, &timeout, &expectedMin, &expectedMax, &follow, &redirects, &claim.Attempt)
		if errors.Is(err, sql.ErrNoRows) {
			if err := tx.Commit(); err != nil {
				return checks.ManualClaim{}, err
			}
			return checks.ManualClaim{}, checks.ErrNoDueSlot
		}
		if err != nil {
			return checks.ManualClaim{}, err
		}
		if archived.Valid {
			if _, err := tx.ExecContext(ctx, "UPDATE check_runs SET state='failed',completed_at=?,failure_code='check_archived' WHERE id=? AND state='queued'", dbTime(now), claim.RunID); err != nil {
				return checks.ManualClaim{}, err
			}
			if err := tx.Commit(); err != nil {
				return checks.ManualClaim{}, err
			}
			continue
		}
		claim.Config = configFromColumns(typeName, targetURL, targetHost, targetPort, interval, timeout, expectedMin, expectedMax, follow, redirects)
		claim.LeaseUntil = now.Add(claim.Config.Timeout() + 5*time.Second)
		result, err := tx.ExecContext(ctx, "UPDATE check_runs SET state='running',started_at=COALESCE(started_at,?),lease_until=?,process_attempts=process_attempts+1 WHERE id=? AND state='queued' AND process_attempts=?", dbTime(now), dbTime(claim.LeaseUntil), claim.RunID, claim.Attempt)
		if err != nil {
			return checks.ManualClaim{}, err
		}
		count, err := result.RowsAffected()
		if err != nil || count != 1 {
			tx.Rollback()
			continue
		}
		if err := tx.Commit(); err != nil {
			return checks.ManualClaim{}, err
		}
		claim.Attempt++
		return claim, nil
	}
}

func (database *Database) FinishManualRun(ctx context.Context, claim checks.ManualClaim, result checks.Result, failureCode string, now time.Time) error {
	state := "completed"
	if failureCode != "" {
		state = "failed"
	}
	_, err := database.sql.ExecContext(ctx, `UPDATE check_runs SET state=?,lease_until=NULL,completed_at=?,result_status=?,result_latency_ms=?,result_error_code=?,result_http_status=?,failure_code=? WHERE id=? AND state='running' AND process_attempts=? AND lease_until=?`, state, dbTime(now), nullableState(result, failureCode), nullableIntPointer(result.LatencyMS), nullableError(result.ErrorCode), nullableCheckStatus(result.HTTPStatus), nullableString(failureCode), claim.RunID, claim.Attempt, dbTime(claim.LeaseUntil))
	return err
}

func nullableState(result checks.Result, failure string) any {
	if failure != "" {
		return nil
	}
	return string(result.Status)
}

func (database *Database) ReleaseManualRun(ctx context.Context, claim checks.ManualClaim, _ time.Time) error {
	_, err := database.sql.ExecContext(ctx, "UPDATE check_runs SET state='queued',lease_until=NULL WHERE id=? AND state='running' AND process_attempts=? AND lease_until=?", claim.RunID, claim.Attempt, dbTime(claim.LeaseUntil))
	return err
}
