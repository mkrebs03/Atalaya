package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mkrebs03/atalaya/internal/alerts"
)

var _ alerts.Repository = (*Database)(nil)

const alertSecretType = "notification-channel"

func (database *Database) ListAlertRules(ctx context.Context, limit int, cursor *alerts.RuleCursor, includeArchived bool) ([]alerts.Rule, error) {
	if limit < 1 {
		return nil, alerts.ErrInvalid
	}
	query := `SELECT id FROM alert_rules WHERE (? OR archived_at IS NULL)`
	args := []any{includeArchived}
	if cursor != nil {
		query += ` AND (lower(name) > lower(?) OR (lower(name) = lower(?) AND id > ?))`
		args = append(args, cursor.Name, cursor.Name, cursor.ID)
	}
	query += ` ORDER BY lower(name), id LIMIT ?`
	args = append(args, limit)
	rows, err := database.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]alerts.Rule, 0, limit)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		item, err := database.GetAlertRule(ctx, id)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (database *Database) GetAlertRule(ctx context.Context, id string) (alerts.Rule, error) {
	return readAlertRule(ctx, database.sql, id)
}

func (database *Database) CreateAlertRule(ctx context.Context, rule alerts.Rule, now time.Time, random alerts.IDSource) (alerts.Rule, error) {
	if random == nil {
		return alerts.Rule{}, alerts.ErrInvalid
	}
	if err := alerts.ValidateRule(rule); err != nil {
		return alerts.Rule{}, err
	}
	id, err := random.ID()
	if err != nil {
		return alerts.Rule{}, err
	}
	now = now.UTC()
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return alerts.Rule{}, err
	}
	defer tx.Rollback()
	if err := validateAlertTarget(ctx, tx, rule); err != nil {
		return alerts.Rule{}, err
	}
	if err := validateAlertChannels(ctx, tx, rule.ChannelIDs); err != nil {
		return alerts.Rule{}, err
	}
	var duplicate int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM alert_rules WHERE lower(name)=lower(?) AND archived_at IS NULL", rule.Name).Scan(&duplicate); err != nil {
		return alerts.Rule{}, err
	}
	if duplicate != 0 {
		return alerts.Rule{}, fmt.Errorf("%w: rule name", alerts.ErrInvalid)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO alert_rules(id,name,type,severity,host_id,check_id,filesystem_id,check_mode,for_seconds,consecutive_failures,metric,operator,threshold,enabled,archived_at,created_at,updated_at,version) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,1)`, id, rule.Name, rule.Type, rule.Severity, nullableID(rule.HostID), nullableID(rule.CheckID), nullableID(rule.FilesystemID), nullableStringValue(string(rule.CheckMode)), rule.ForSeconds, nullablePositive(rule.ConsecutiveFailures), nullableStringValue(string(rule.Metric)), nullableStringValue(string(rule.Operator)), nullableFloat(rule.Threshold, rule.Type == alerts.RuleHostMetric || rule.Type == alerts.RuleFilesystem), boolInt(rule.Enabled), nil, dbTime(now), dbTime(now)); err != nil {
		return alerts.Rule{}, err
	}
	if err := replaceRuleChannels(ctx, tx, id, rule.ChannelIDs); err != nil {
		return alerts.Rule{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO alert_evaluations(rule_id,state,last_condition_status,updated_at) VALUES(?, 'inactive', 'unknown', ?)`, id, dbTime(now)); err != nil {
		return alerts.Rule{}, err
	}
	if err := tx.Commit(); err != nil {
		return alerts.Rule{}, err
	}
	return database.GetAlertRule(ctx, id)
}

func (database *Database) UpdateAlertRule(ctx context.Context, id string, version int64, rule alerts.Rule, now time.Time) error {
	if err := alerts.ValidateRule(rule); err != nil {
		return err
	}
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, err := readAlertRule(ctx, tx, id)
	if errors.Is(err, alerts.ErrNotFound) {
		return err
	}
	if err != nil {
		return err
	}
	if current.Version != version {
		return alerts.ErrVersionConflict
	}
	if current.ArchivedAt != nil {
		return alerts.ErrArchived
	}
	rule.ID = id
	rule.Enabled = current.Enabled
	rule.ArchivedAt = current.ArchivedAt
	if err := validateAlertTarget(ctx, tx, rule); err != nil {
		return err
	}
	if err := validateAlertChannels(ctx, tx, rule.ChannelIDs); err != nil {
		return err
	}
	now = now.UTC()
	material := materialRuleChange(current, rule)
	if _, err := tx.ExecContext(ctx, `UPDATE alert_rules SET name=?,type=?,severity=?,host_id=?,check_id=?,filesystem_id=?,check_mode=?,for_seconds=?,consecutive_failures=?,metric=?,operator=?,threshold=?,updated_at=?,version=version+1 WHERE id=? AND version=?`, rule.Name, rule.Type, rule.Severity, nullableID(rule.HostID), nullableID(rule.CheckID), nullableID(rule.FilesystemID), nullableStringValue(string(rule.CheckMode)), rule.ForSeconds, nullablePositive(rule.ConsecutiveFailures), nullableStringValue(string(rule.Metric)), nullableStringValue(string(rule.Operator)), nullableFloat(rule.Threshold, rule.Type == alerts.RuleHostMetric || rule.Type == alerts.RuleFilesystem), dbTime(now), id, version); err != nil {
		return err
	}
	if err := replaceRuleChannels(ctx, tx, id, rule.ChannelIDs); err != nil {
		return err
	}
	if material {
		if err := resolveRuleTx(ctx, tx, id, now, "rule_changed", alertRandomSource()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (database *Database) SetAlertRuleEnabled(ctx context.Context, id string, version int64, enabled bool, now time.Time) error {
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, err := readAlertRule(ctx, tx, id)
	if err != nil {
		return err
	}
	if current.Version != version {
		return alerts.ErrVersionConflict
	}
	if current.ArchivedAt != nil {
		return alerts.ErrArchived
	}
	if current.Enabled == enabled {
		return tx.Commit()
	}
	now = now.UTC()
	if _, err := tx.ExecContext(ctx, "UPDATE alert_rules SET enabled=?,updated_at=?,version=version+1 WHERE id=? AND version=?", boolInt(enabled), dbTime(now), id, version); err != nil {
		return err
	}
	if !enabled {
		if err := resolveRuleTx(ctx, tx, id, now, "rule_disabled", alertRandomSource()); err != nil {
			return err
		}
	} else if err := resetAlertEvaluationTx(ctx, tx, id, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (database *Database) ArchiveAlertRule(ctx context.Context, id string, version int64, now time.Time) error {
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, err := readAlertRule(ctx, tx, id)
	if err != nil {
		return err
	}
	if current.Version != version {
		return alerts.ErrVersionConflict
	}
	if current.ArchivedAt != nil {
		return tx.Commit()
	}
	now = now.UTC()
	if _, err := tx.ExecContext(ctx, "UPDATE alert_rules SET enabled=0,archived_at=?,updated_at=?,version=version+1 WHERE id=? AND version=?", dbTime(now), dbTime(now), id, version); err != nil {
		return err
	}
	if err := resolveRuleTx(ctx, tx, id, now, "rule_archived", alertRandomSource()); err != nil {
		return err
	}
	return tx.Commit()
}

func (database *Database) RestoreAlertRule(ctx context.Context, id string, version int64, now time.Time) error {
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current int64
	var archived sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT version,archived_at FROM alert_rules WHERE id=?", id).Scan(&current, &archived); errors.Is(err, sql.ErrNoRows) {
		return alerts.ErrNotFound
	} else if err != nil {
		return err
	}
	if current != version {
		return alerts.ErrVersionConflict
	}
	if !archived.Valid {
		return tx.Commit()
	}
	_, err = tx.ExecContext(ctx, "UPDATE alert_rules SET archived_at=NULL,enabled=0,updated_at=?,version=version+1 WHERE id=? AND version=?", dbTime(now), id, version)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (database *Database) ListIncidents(ctx context.Context, state, severity string, limit int, cursor *alerts.IncidentCursor) ([]alerts.Incident, error) {
	if limit < 1 {
		return nil, alerts.ErrInvalid
	}
	query := `SELECT id FROM incidents WHERE 1=1`
	args := make([]any, 0, 6)
	if state != "" {
		query += " AND state=?"
		args = append(args, state)
	}
	if severity != "" {
		query += " AND severity=?"
		args = append(args, severity)
	}
	if cursor != nil {
		query += " AND (fired_at<? OR (fired_at=? AND id<?))"
		value := dbTime(cursor.FiredAt)
		args = append(args, value, value, cursor.ID)
	}
	query += " ORDER BY fired_at DESC,id DESC LIMIT ?"
	args = append(args, limit)
	rows, err := database.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]alerts.Incident, 0, limit)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		item, err := database.GetIncident(ctx, id)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (database *Database) GetIncident(ctx context.Context, id string) (alerts.Incident, error) {
	return readIncident(ctx, database.sql, id)
}

func readAlertRule(ctx context.Context, query queryer, id string) (alerts.Rule, error) {
	var rule alerts.Rule
	var typeName, severity string
	var mode, metric, operator sql.NullString
	var hostID, checkID, filesystemID, archived sql.NullString
	var consecutive sql.NullInt64
	var threshold sql.NullFloat64
	var created, updated string
	if err := query.QueryRowContext(ctx, `SELECT id,name,type,severity,host_id,check_id,filesystem_id,check_mode,for_seconds,consecutive_failures,metric,operator,threshold,enabled,archived_at,created_at,updated_at,version FROM alert_rules WHERE id=?`, id).Scan(&rule.ID, &rule.Name, &typeName, &severity, &hostID, &checkID, &filesystemID, &mode, &rule.ForSeconds, &consecutive, &metric, &operator, &threshold, &rule.Enabled, &archived, &created, &updated, &rule.Version); errors.Is(err, sql.ErrNoRows) {
		return alerts.Rule{}, alerts.ErrNotFound
	} else if err != nil {
		return alerts.Rule{}, err
	}
	rule.Type = alerts.RuleType(typeName)
	rule.Severity = alerts.Severity(severity)
	rule.HostID = alertNullableStringPointer(hostID)
	rule.CheckID = alertNullableStringPointer(checkID)
	rule.FilesystemID = alertNullableStringPointer(filesystemID)
	if mode.Valid {
		rule.CheckMode = alerts.CheckMode(mode.String)
	}
	if metric.Valid {
		rule.Metric = alerts.Metric(metric.String)
	}
	if operator.Valid {
		rule.Operator = alerts.Operator(operator.String)
	}
	if consecutive.Valid {
		rule.ConsecutiveFailures = int(consecutive.Int64)
	}
	if threshold.Valid {
		rule.Threshold = threshold.Float64
	}
	if archived.Valid {
		value, err := parseDBTime(archived.String)
		if err != nil {
			return alerts.Rule{}, err
		}
		rule.ArchivedAt = &value
	}
	var err error
	if rule.CreatedAt, err = parseDBTime(created); err != nil {
		return alerts.Rule{}, err
	}
	if rule.UpdatedAt, err = parseDBTime(updated); err != nil {
		return alerts.Rule{}, err
	}
	channels, err := query.QueryContext(ctx, "SELECT channel_id FROM alert_rule_channels WHERE rule_id=? ORDER BY channel_id", id)
	if err != nil {
		return alerts.Rule{}, err
	}
	defer channels.Close()
	for channels.Next() {
		var channel string
		if err := channels.Scan(&channel); err != nil {
			return alerts.Rule{}, err
		}
		rule.ChannelIDs = append(rule.ChannelIDs, channel)
	}
	if err := channels.Err(); err != nil {
		return alerts.Rule{}, err
	}
	if rule.Evaluation, err = readAlertEvaluation(ctx, query, id); err != nil {
		return alerts.Rule{}, err
	}
	return rule, nil
}

func readIncident(ctx context.Context, query queryer, id string) (alerts.Incident, error) {
	var incident alerts.Incident
	var ruleType, severity, state, status string
	var started, fired string
	var resolved, reason sql.NullString
	var initial, last sql.NullFloat64
	if err := query.QueryRowContext(ctx, `SELECT id,rule_id,rule_name,rule_type,target_id,target_name,severity,state,condition_status,started_at,fired_at,resolved_at,initial_value,last_value,resolution_reason FROM incidents WHERE id=?`, id).Scan(&incident.ID, &incident.RuleID, &incident.RuleName, &ruleType, &incident.TargetID, &incident.TargetName, &severity, &state, &status, &started, &fired, &resolved, &initial, &last, &reason); errors.Is(err, sql.ErrNoRows) {
		return alerts.Incident{}, alerts.ErrNotFound
	} else if err != nil {
		return alerts.Incident{}, err
	}
	incident.RuleType = alerts.RuleType(ruleType)
	incident.Severity = alerts.Severity(severity)
	incident.State = state
	incident.ConditionStatus = alerts.ConditionStatus(status)
	var err error
	if incident.StartedAt, err = parseDBTime(started); err != nil {
		return alerts.Incident{}, err
	}
	if incident.FiredAt, err = parseDBTime(fired); err != nil {
		return alerts.Incident{}, err
	}
	if resolved.Valid {
		value, parseErr := parseDBTime(resolved.String)
		if parseErr != nil {
			return alerts.Incident{}, parseErr
		}
		incident.ResolvedAt = &value
	}
	if initial.Valid {
		incident.InitialValue = &initial.Float64
	}
	if last.Valid {
		incident.LastValue = &last.Float64
	}
	if reason.Valid {
		incident.ResolutionReason = reason.String
	}
	return incident, nil
}

func nullableID(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func alertNullableStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	copy := value.String
	return &copy
}

func nullableStringValue(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullablePositive(value int) any {
	if value <= 0 {
		return nil
	}
	return value
}

func nullableFloat(value float64, present bool) any {
	if !present {
		return nil
	}
	return value
}

func validateAlertTarget(ctx context.Context, tx *sql.Tx, rule alerts.Rule) error {
	var table, id string
	switch rule.Type {
	case alerts.RuleHostUnreachable, alerts.RuleHostMetric:
		table, id = "hosts", *rule.HostID
	case alerts.RuleCheckFailed:
		table, id = "service_checks", *rule.CheckID
	case alerts.RuleFilesystem:
		table, id = "filesystems", *rule.FilesystemID
	default:
		return alerts.ErrInvalid
	}
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE id=?", id).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return alerts.ErrTargetNotFound
	}
	return nil
}

func validateAlertChannels(ctx context.Context, tx *sql.Tx, ids []string) error {
	for _, id := range ids {
		var archived sql.NullString
		if err := tx.QueryRowContext(ctx, "SELECT archived_at FROM notification_channels WHERE id=?", id).Scan(&archived); errors.Is(err, sql.ErrNoRows) {
			return alerts.ErrChannelInvalid
		} else if err != nil {
			return err
		}
		if archived.Valid {
			return alerts.ErrChannelInvalid
		}
	}
	return nil
}

func replaceRuleChannels(ctx context.Context, tx *sql.Tx, ruleID string, channels []string) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM alert_rule_channels WHERE rule_id=?", ruleID); err != nil {
		return err
	}
	for _, channelID := range channels {
		if _, err := tx.ExecContext(ctx, "INSERT INTO alert_rule_channels(rule_id,channel_id) VALUES(?,?)", ruleID, channelID); err != nil {
			return err
		}
	}
	return nil
}

func materialRuleChange(old, next alerts.Rule) bool {
	if old.Type != next.Type || old.HostID == nil != (next.HostID == nil) || old.CheckID == nil != (next.CheckID == nil) || old.FilesystemID == nil != (next.FilesystemID == nil) || old.CheckMode != next.CheckMode || old.ForSeconds != next.ForSeconds || old.ConsecutiveFailures != next.ConsecutiveFailures || old.Metric != next.Metric || old.Operator != next.Operator || old.Threshold != next.Threshold {
		return true
	}
	if !sameStringPointer(old.HostID, next.HostID) || !sameStringPointer(old.CheckID, next.CheckID) || !sameStringPointer(old.FilesystemID, next.FilesystemID) {
		return true
	}
	return false
}

func sameStringPointer(left, right *string) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func resetAlertEvaluationTx(ctx context.Context, tx *sql.Tx, ruleID string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO alert_evaluations(rule_id,state,last_condition_status,updated_at) VALUES(?, 'inactive', 'unknown', ?) ON CONFLICT(rule_id) DO UPDATE SET state='inactive',first_true_at=NULL,last_evaluated_at=NULL,last_value=NULL,last_condition_status='unknown',incident_id=NULL,updated_at=excluded.updated_at`, ruleID, dbTime(now))
	return err
}

func targetNameTx(ctx context.Context, tx *sql.Tx, rule alerts.Rule) (string, string, error) {
	var name string
	var targetID string
	var err error
	switch rule.Type {
	case alerts.RuleHostUnreachable, alerts.RuleHostMetric:
		targetID = *rule.HostID
		err = tx.QueryRowContext(ctx, "SELECT display_name FROM hosts WHERE id=?", targetID).Scan(&name)
	case alerts.RuleCheckFailed:
		targetID = *rule.CheckID
		err = tx.QueryRowContext(ctx, "SELECT name FROM service_checks WHERE id=?", targetID).Scan(&name)
	case alerts.RuleFilesystem:
		targetID = *rule.FilesystemID
		err = tx.QueryRowContext(ctx, "SELECT mount_point FROM filesystems WHERE id=?", targetID).Scan(&name)
	default:
		return "", "", alerts.ErrInvalid
	}
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", alerts.ErrTargetNotFound
	}
	return targetID, name, err
}

func queryRuleChannels(ctx context.Context, tx *sql.Tx, ruleID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, "SELECT channel_id FROM alert_rule_channels WHERE rule_id=? ORDER BY channel_id", ruleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, rows.Err()
}
