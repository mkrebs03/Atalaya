package database

import (
	"context"
	cryptorand "crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mkrebs03/atalaya/internal/alerts"
	"github.com/mkrebs03/atalaya/internal/notifications"
)

type hostAlertEvent struct {
	Success            bool
	OperationalFailure bool
}

func (database *Database) evaluateHostAlertsTx(ctx context.Context, tx *sql.Tx, hostID string, event hostAlertEvent, at time.Time, random alerts.IDSource) error {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM alert_rules WHERE enabled=1 AND archived_at IS NULL AND host_id=?`, hostID)
	if err != nil {
		return err
	}
	var ruleIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ruleIDs = append(ruleIDs, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, ruleID := range ruleIDs {
		rule, err := readAlertRule(ctx, tx, ruleID)
		if err != nil {
			return err
		}
		observation, ok, err := hostRuleObservation(ctx, tx, rule, event, at)
		if err != nil {
			return err
		}
		if ok {
			if err := database.applyAlertObservationTx(ctx, tx, rule, observation, at, random); err != nil {
				return err
			}
		}
	}
	return nil
}

func (database *Database) evaluateCheckAlertsTx(ctx context.Context, tx *sql.Tx, checkID, status string, at time.Time, random alerts.IDSource) error {
	rows, err := tx.QueryContext(ctx, "SELECT id FROM alert_rules WHERE enabled=1 AND archived_at IS NULL AND type='check_failed' AND check_id=? ORDER BY id", checkID)
	if err != nil {
		return err
	}
	var ruleIDs []string
	for rows.Next() {
		var ruleID string
		if err := rows.Scan(&ruleID); err != nil {
			rows.Close()
			return err
		}
		ruleIDs = append(ruleIDs, ruleID)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, ruleID := range ruleIDs {
		rule, err := readAlertRule(ctx, tx, ruleID)
		if err != nil {
			return err
		}
		observation := alerts.Observation{At: at}
		switch status {
		case "up":
			observation.Status = alerts.ConditionFalse
		case "down":
			failures, firstFailure, err := currentConsecutiveCheckFailures(ctx, tx, checkID, at)
			if err != nil {
				return err
			}
			if rule.CheckMode == alerts.CheckAttempts {
				if failures < rule.ConsecutiveFailures {
					observation.Status = alerts.ConditionFalse
				} else {
					observation.Status = alerts.ConditionTrue
				}
			} else {
				observation.Status = alerts.ConditionTrue
				observation.Since = &firstFailure
			}
		default:
			observation.Status = alerts.ConditionUnknown
		}
		if err := database.applyAlertObservationTx(ctx, tx, rule, observation, at, random); err != nil {
			return err
		}
	}
	return nil
}

func (database *Database) evaluateFilesystemAlertTx(ctx context.Context, tx *sql.Tx, filesystemID string, event hostAlertEvent, at time.Time, random alerts.IDSource) error {
	rows, err := tx.QueryContext(ctx, "SELECT id FROM alert_rules WHERE enabled=1 AND archived_at IS NULL AND type='filesystem_threshold' AND filesystem_id=? ORDER BY id", filesystemID)
	if err != nil {
		return err
	}
	var ruleIDs []string
	for rows.Next() {
		var ruleID string
		if err := rows.Scan(&ruleID); err != nil {
			rows.Close()
			return err
		}
		ruleIDs = append(ruleIDs, ruleID)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, ruleID := range ruleIDs {
		rule, err := readAlertRule(ctx, tx, ruleID)
		if err != nil {
			return err
		}
		observation, _, err := filesystemRuleObservation(ctx, tx, rule, event, at)
		if err != nil {
			return err
		}
		if err := database.applyAlertObservationTx(ctx, tx, rule, observation, at, random); err != nil {
			return err
		}
	}
	return nil
}

func (database *Database) evaluateFilesystemAlertsForHostTx(ctx context.Context, tx *sql.Tx, hostID string, event hostAlertEvent, at time.Time, random alerts.IDSource) error {
	rows, err := tx.QueryContext(ctx, "SELECT id FROM filesystems WHERE host_id=? ORDER BY id", hostID)
	if err != nil {
		return err
	}
	var filesystemIDs []string
	for rows.Next() {
		var filesystemID string
		if err := rows.Scan(&filesystemID); err != nil {
			rows.Close()
			return err
		}
		filesystemIDs = append(filesystemIDs, filesystemID)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, filesystemID := range filesystemIDs {
		if err := database.evaluateFilesystemAlertTx(ctx, tx, filesystemID, event, at, random); err != nil {
			return err
		}
	}
	return nil
}

func hostRuleObservation(ctx context.Context, tx *sql.Tx, rule alerts.Rule, event hostAlertEvent, at time.Time) (alerts.Observation, bool, error) {
	if rule.Type == alerts.RuleHostUnreachable {
		if event.Success {
			return alerts.Observation{At: at, Status: alerts.ConditionFalse}, true, nil
		}
		if !event.OperationalFailure {
			return alerts.Observation{}, false, nil
		}
		var lastSuccess, created sql.NullString
		if err := tx.QueryRowContext(ctx, "SELECT last_success_received_at,created_at FROM hosts WHERE id=?", *rule.HostID).Scan(&lastSuccess, &created); err != nil {
			return alerts.Observation{}, false, err
		}
		observation := alerts.Observation{At: at, Status: alerts.ConditionTrue}
		base := created.String
		if lastSuccess.Valid {
			base = lastSuccess.String
		}
		parsed, err := parseDBTime(base)
		if err != nil {
			return alerts.Observation{}, false, err
		}
		observation.ThresholdSince = &parsed
		return observation, true, nil
	}
	if !event.Success {
		return alerts.Observation{At: at, Status: alerts.ConditionUnknown}, true, nil
	}
	var cpu sql.NullFloat64
	var memoryUsed, memoryTotal sql.NullInt64
	if err := tx.QueryRowContext(ctx, "SELECT cpu_used_percent,memory_used_bytes,memory_total_bytes FROM host_snapshots WHERE host_id=?", *rule.HostID).Scan(&cpu, &memoryUsed, &memoryTotal); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return alerts.Observation{At: at, Status: alerts.ConditionUnknown}, true, nil
		}
		return alerts.Observation{}, false, err
	}
	var value float64
	if rule.Metric == alerts.MetricCPUUsed {
		if !cpu.Valid {
			return alerts.Observation{At: at, Status: alerts.ConditionUnknown}, true, nil
		}
		value = cpu.Float64
	} else {
		if !memoryUsed.Valid || !memoryTotal.Valid || memoryTotal.Int64 <= 0 {
			return alerts.Observation{At: at, Status: alerts.ConditionUnknown}, true, nil
		}
		value = float64(memoryUsed.Int64) / float64(memoryTotal.Int64) * 100
	}
	return thresholdObservation(rule, at, value), true, nil
}

func filesystemRuleObservation(ctx context.Context, tx *sql.Tx, rule alerts.Rule, event hostAlertEvent, at time.Time) (alerts.Observation, bool, error) {
	if !event.Success {
		return alerts.Observation{At: at, Status: alerts.ConditionUnknown}, true, nil
	}
	var selected, present int
	var used, total int64
	var observed string
	if err := tx.QueryRowContext(ctx, "SELECT selected,present,used_bytes,total_bytes,last_observed_at FROM filesystems WHERE id=?", *rule.FilesystemID).Scan(&selected, &present, &used, &total, &observed); errors.Is(err, sql.ErrNoRows) {
		return alerts.Observation{At: at, Status: alerts.ConditionUnknown}, true, nil
	} else if err != nil {
		return alerts.Observation{}, false, err
	}
	observedAt, err := parseDBTime(observed)
	if err != nil {
		return alerts.Observation{}, false, err
	}
	if selected == 0 || present == 0 || !observedAt.Equal(at.UTC()) || total <= 0 || used < 0 || used > total {
		return alerts.Observation{At: at, Status: alerts.ConditionUnknown}, true, nil
	}
	return thresholdObservation(rule, at, float64(used)/float64(total)*100), true, nil
}

func thresholdObservation(rule alerts.Rule, at time.Time, value float64) alerts.Observation {
	trueValue := value > rule.Threshold
	if rule.Operator == alerts.OperatorGTE {
		trueValue = value >= rule.Threshold
	}
	status := alerts.ConditionFalse
	if trueValue {
		status = alerts.ConditionTrue
	}
	return alerts.Observation{At: at, Status: status, Value: &value}
}

func currentConsecutiveCheckFailures(ctx context.Context, tx *sql.Tx, checkID string, at time.Time) (int, time.Time, error) {
	// The active schedule period is the stable sequence boundary. Looking back
	// from the evaluation timestamp (or LastEvaluatedAt) would move the lower
	// bound after every sub-threshold failure and make attempts=3 impossible to
	// reach. A terminal slot without a result is an explicit gap and breaks the
	// run just like an up result does.
	rows, err := tx.QueryContext(ctx, `SELECT s.nominal_at,r.received_at,r.status
		FROM schedule_slots s
		JOIN schedule_periods p ON p.id=s.schedule_period_id
		LEFT JOIN check_results r ON r.slot_id=s.id AND r.check_id=?
		WHERE s.target_type='check' AND s.target_id=? AND p.target_type='check' AND p.ends_at IS NULL
			AND s.finished_at IS NOT NULL AND s.finished_at<=?
		ORDER BY s.nominal_at DESC,s.id DESC`, checkID, checkID, dbTime(at))
	if err != nil {
		return 0, time.Time{}, err
	}
	defer rows.Close()
	var first time.Time
	count := 0
	for rows.Next() {
		var nominal string
		var received, status sql.NullString
		if err := rows.Scan(&nominal, &received, &status); err != nil {
			return 0, time.Time{}, err
		}
		if !status.Valid || !received.Valid || status.String != "down" {
			break
		}
		parsed, err := parseDBTime(received.String)
		if err != nil {
			return 0, time.Time{}, err
		}
		first = parsed
		count++
	}
	if err := rows.Err(); err != nil {
		return 0, time.Time{}, err
	}
	if count == 0 {
		return 0, time.Time{}, fmt.Errorf("no consecutive check failure for %s", checkID)
	}
	return count, first, nil
}

func (database *Database) applyAlertObservationTx(ctx context.Context, tx *sql.Tx, rule alerts.Rule, observation alerts.Observation, at time.Time, random alerts.IDSource) error {
	current, err := readAlertEvaluation(ctx, tx, rule.ID)
	if err != nil {
		return err
	}
	oldIncidentID := current.IncidentID
	transition, err := alerts.Evaluate(rule, current, observation)
	if err != nil {
		return err
	}
	if transition.Opened {
		if random == nil {
			return fmt.Errorf("alert ID source is required")
		}
		incidentID, err := database.openIncidentTx(ctx, tx, rule, observation, transition.Evaluation.FirstTrueAt, at, random)
		if err != nil {
			return err
		}
		transition.Evaluation.IncidentID = &incidentID
	}
	if transition.Resolved && oldIncidentID != nil {
		if err := database.resolveIncidentTx(ctx, tx, *oldIncidentID, at, "condition_false", observation.Value, random); err != nil {
			return err
		}
	}
	if transition.Evaluation.State == alerts.EvaluationFiring && transition.Evaluation.IncidentID != nil && !transition.Opened {
		if _, err := tx.ExecContext(ctx, "UPDATE incidents SET condition_status=?,last_value=? WHERE id=? AND state='open'", string(observation.Status), nullableFloatValue(observation.Value), *transition.Evaluation.IncidentID); err != nil {
			return err
		}
	}
	return writeAlertEvaluationTx(ctx, tx, transition.Evaluation, at)
}

func readAlertEvaluation(ctx context.Context, query queryer, ruleID string) (alerts.Evaluation, error) {
	var evaluation alerts.Evaluation
	var state, status string
	var first, last, incident, updated sql.NullString
	var value sql.NullFloat64
	if err := query.QueryRowContext(ctx, "SELECT rule_id,state,first_true_at,last_evaluated_at,last_value,last_condition_status,incident_id,updated_at FROM alert_evaluations WHERE rule_id=?", ruleID).Scan(&evaluation.RuleID, &state, &first, &last, &value, &status, &incident, &updated); errors.Is(err, sql.ErrNoRows) {
		evaluation.RuleID = ruleID
		evaluation.State = alerts.EvaluationInactive
		evaluation.LastConditionStatus = alerts.ConditionUnknown
		return evaluation, nil
	} else if err != nil {
		return alerts.Evaluation{}, err
	}
	evaluation.State = alerts.EvaluationState(state)
	evaluation.LastConditionStatus = alerts.ConditionStatus(status)
	if first.Valid {
		parsed, err := parseDBTime(first.String)
		if err != nil {
			return alerts.Evaluation{}, err
		}
		evaluation.FirstTrueAt = &parsed
	}
	if last.Valid {
		parsed, err := parseDBTime(last.String)
		if err != nil {
			return alerts.Evaluation{}, err
		}
		evaluation.LastEvaluatedAt = &parsed
	}
	if updated.Valid {
		parsed, err := parseDBTime(updated.String)
		if err != nil {
			return alerts.Evaluation{}, err
		}
		evaluation.UpdatedAt = &parsed
	}
	if value.Valid {
		evaluation.LastValue = &value.Float64
	}
	if incident.Valid {
		evaluation.IncidentID = &incident.String
	}
	return evaluation, nil
}

func writeAlertEvaluationTx(ctx context.Context, tx *sql.Tx, evaluation alerts.Evaluation, at time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO alert_evaluations(rule_id,state,first_true_at,last_evaluated_at,last_value,last_condition_status,incident_id,updated_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(rule_id) DO UPDATE SET state=excluded.state,first_true_at=excluded.first_true_at,last_evaluated_at=excluded.last_evaluated_at,last_value=excluded.last_value,last_condition_status=excluded.last_condition_status,incident_id=excluded.incident_id,updated_at=excluded.updated_at`, evaluation.RuleID, evaluation.State, nullableTime(evaluation.FirstTrueAt), nullableTime(evaluation.LastEvaluatedAt), nullableFloatValue(evaluation.LastValue), evaluation.LastConditionStatus, nullableStringPointerValue(evaluation.IncidentID), dbTime(at))
	return err
}

func (database *Database) openIncidentTx(ctx context.Context, tx *sql.Tx, rule alerts.Rule, observation alerts.Observation, startedAt *time.Time, at time.Time, random alerts.IDSource) (string, error) {
	incidentID, err := random.ID()
	if err != nil {
		return "", err
	}
	targetID, targetName, err := targetNameTx(ctx, tx, rule)
	if err != nil {
		return "", err
	}
	started := at.UTC()
	if startedAt != nil && !startedAt.IsZero() {
		started = startedAt.UTC()
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO incidents(id,rule_id,rule_name,rule_type,target_id,target_name,severity,state,condition_status,started_at,fired_at,initial_value,last_value) VALUES(?,?,?,?,?,?,?,'open','true',?,?,?,?)`, incidentID, rule.ID, rule.Name, rule.Type, targetID, targetName, rule.Severity, dbTime(started), dbTime(at), nullableFloatValue(observation.Value), nullableFloatValue(observation.Value)); err != nil {
		return "", err
	}
	channels, err := queryRuleChannels(ctx, tx, rule.ID)
	if err != nil {
		return "", err
	}
	for _, channelID := range channels {
		if _, err := tx.ExecContext(ctx, "INSERT INTO incident_channel_snapshots(incident_id,channel_id) VALUES(?,?)", incidentID, channelID); err != nil {
			return "", err
		}
		state, reason, err := channelDeliveryState(ctx, tx, channelID)
		if err != nil {
			return "", err
		}
		payload := notifications.IncidentPayload("firing", string(rule.Severity), rule.Name, targetName, observation.Value, at)
		if err := database.insertIncidentDeliveryTx(ctx, tx, incidentID, channelID, notifications.TransitionFiring, payload, state, reason, at, random); err != nil {
			return "", err
		}
	}
	return incidentID, nil
}

func channelDeliveryState(ctx context.Context, tx *sql.Tx, channelID string) (string, string, error) {
	var enabled int
	var archived sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT enabled,archived_at FROM notification_channels WHERE id=?", channelID).Scan(&enabled, &archived); errors.Is(err, sql.ErrNoRows) {
		return "skipped", "channel_missing", nil
	} else if err != nil {
		return "", "", err
	}
	if archived.Valid {
		return "skipped", "channel_archived", nil
	}
	if enabled == 0 {
		return "skipped", "channel_disabled", nil
	}
	return "pending", "", nil
}

func (database *Database) insertIncidentDeliveryTx(ctx context.Context, tx *sql.Tx, incidentID, channelID string, transition notifications.Transition, payload []byte, state, reason string, at time.Time, random alerts.IDSource) error {
	id, err := random.ID()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO notification_deliveries(id,kind,channel_id,incident_id,transition,payload,state,attempts,next_attempt_at,skipped_reason,created_at,updated_at) VALUES(?,'incident',?,?,?,?,?,0,?,?,?,?) ON CONFLICT(incident_id,transition,channel_id) WHERE kind='incident' DO NOTHING`, id, channelID, incidentID, string(transition), string(payload), state, dbTime(at), nullableStringValue(reason), dbTime(at), dbTime(at))
	return err
}

func (database *Database) resolveIncidentTx(ctx context.Context, tx *sql.Tx, incidentID string, at time.Time, reason string, value *float64, random alerts.IDSource) error {
	var state, ruleName, targetName, severity string
	if err := tx.QueryRowContext(ctx, "SELECT state,rule_name,target_name,severity FROM incidents WHERE id=?", incidentID).Scan(&state, &ruleName, &targetName, &severity); errors.Is(err, sql.ErrNoRows) {
		return alerts.ErrNotFound
	} else if err != nil {
		return err
	}
	if state == "resolved" {
		return nil
	}
	if _, err := tx.ExecContext(ctx, "UPDATE incidents SET state='resolved',condition_status='false',resolved_at=?,last_value=?,resolution_reason=? WHERE id=? AND state='open'", dbTime(at), nullableFloatValue(value), reason, incidentID); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, "SELECT channel_id FROM incident_channel_snapshots WHERE incident_id=? ORDER BY channel_id", incidentID)
	if err != nil {
		return err
	}
	var channels []string
	for rows.Next() {
		var channelID string
		if err := rows.Scan(&channelID); err != nil {
			rows.Close()
			return err
		}
		channels = append(channels, channelID)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, channelID := range channels {
		var openingState string
		if err := tx.QueryRowContext(ctx, "SELECT state FROM notification_deliveries WHERE incident_id=? AND transition='firing' AND channel_id=?", incidentID, channelID).Scan(&openingState); err != nil {
			return err
		}
		state, skipReason, err := channelDeliveryState(ctx, tx, channelID)
		if err != nil {
			return err
		}
		if openingState == string(notifications.DeliverySkipped) {
			state, skipReason = "skipped", "opening_skipped"
		}
		payload := notifications.IncidentPayload("resolved", severity, ruleName, targetName, value, at)
		if err := database.insertIncidentDeliveryTx(ctx, tx, incidentID, channelID, notifications.TransitionResolved, payload, state, skipReason, at, random); err != nil {
			return err
		}
	}
	return nil
}

func resolveRuleTx(ctx context.Context, tx *sql.Tx, ruleID string, now time.Time, reason string, random alerts.IDSource) error {
	var incident sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT incident_id FROM alert_evaluations WHERE rule_id=?", ruleID).Scan(&incident); errors.Is(err, sql.ErrNoRows) {
		return resetAlertEvaluationTx(ctx, tx, ruleID, now)
	} else if err != nil {
		return err
	}
	if incident.Valid {
		if err := (&Database{}).resolveIncidentTx(ctx, tx, incident.String, now, reason, nil, random); err != nil {
			return err
		}
	}
	return resetAlertEvaluationTx(ctx, tx, ruleID, now)
}

func nullableFloatValue(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableStringPointerValue(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func alertRandomSource() alerts.IDSource {
	return alerts.NewReaderIDSource(cryptorand.Reader)
}

func (database *Database) resolveTargetRulesTx(ctx context.Context, tx *sql.Tx, targetType, targetID string, now time.Time, reason string, random alerts.IDSource) error {
	column := "host_id"
	if targetType == "check" {
		column = "check_id"
	} else if targetType == "filesystem" {
		column = "filesystem_id"
	}
	rows, err := tx.QueryContext(ctx, "SELECT id FROM alert_rules WHERE "+column+"=?", targetID)
	if err != nil {
		return err
	}
	var ruleIDs []string
	for rows.Next() {
		var ruleID string
		if err := rows.Scan(&ruleID); err != nil {
			rows.Close()
			return err
		}
		ruleIDs = append(ruleIDs, ruleID)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, ruleID := range ruleIDs {
		if err := resolveRuleTx(ctx, tx, ruleID, now, reason, random); err != nil {
			return err
		}
	}
	return nil
}
