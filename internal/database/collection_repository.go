package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/mkrebs03/atalaya/internal/agent"
	"github.com/mkrebs03/atalaya/internal/collection"
	atalayacrypto "github.com/mkrebs03/atalaya/internal/crypto"
	"github.com/mkrebs03/atalaya/internal/hosts"
)

var _ collection.Repository = (*Database)(nil)

func dbTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }
func parseDBTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}

func (database *Database) ClaimDue(ctx context.Context, now time.Time) (collection.Claim, error) {
	if database == nil || database.sql == nil {
		return collection.Claim{}, fmt.Errorf("database is closed")
	}
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return collection.Claim{}, err
	}
	defer tx.Rollback()
	var claim collection.Claim
	var credentialEnvelope []byte
	var nominal, deadline string
	var interval, timeout int64
	err = tx.QueryRowContext(ctx, `SELECT s.id,h.id,h.agent_url,h.agent_credential,s.nominal_at,s.deadline_at,s.effective_interval_seconds,s.effective_timeout_seconds,h.collection_generation
		FROM schedule_slots s JOIN hosts h ON h.id=s.target_id
		WHERE s.target_type='host' AND s.state='scheduled' AND s.process_attempts<2 AND s.not_before_at<=? AND s.deadline_at>=?
		AND h.bootstrap_completed_at IS NOT NULL AND h.administrative_state IN ('pending','active') AND h.archived_at IS NULL
		ORDER BY s.not_before_at,s.id LIMIT 1`, dbTime(now), dbTime(now)).Scan(&claim.SlotID, &claim.HostID, &claim.AgentURL, &credentialEnvelope, &nominal, &deadline, &interval, &timeout, &claim.CollectionGeneration)
	if errors.Is(err, sql.ErrNoRows) {
		return collection.Claim{}, collection.ErrNoDueSlot
	}
	if err != nil {
		return collection.Claim{}, fmt.Errorf("select due slot: %w", err)
	}
	result, err := tx.ExecContext(ctx, `UPDATE schedule_slots SET state='running',lease_until=?,started_at=COALESCE(started_at,?),process_attempts=process_attempts+1 WHERE id=? AND state='scheduled'`, dbTime(now.Add(time.Duration(timeout+5)*time.Second)), dbTime(now), claim.SlotID)
	if err != nil {
		return collection.Claim{}, fmt.Errorf("claim due slot: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return collection.Claim{}, collection.ErrNoDueSlot
	}
	claim.Credential, err = atalayacrypto.Open(database.key, agentSecretType, claim.HostID, credentialEnvelope)
	if err != nil {
		return collection.Claim{}, fmt.Errorf("open agent credential: %w", err)
	}
	claim.NominalAt, err = parseDBTime(nominal)
	if err != nil {
		claim.Clear()
		return collection.Claim{}, err
	}
	claim.DeadlineAt, err = parseDBTime(deadline)
	if err != nil {
		claim.Clear()
		return collection.Claim{}, err
	}
	claim.Interval = time.Duration(interval) * time.Second
	claim.Timeout = time.Duration(timeout) * time.Second
	if err = tx.Commit(); err != nil {
		claim.Clear()
		return collection.Claim{}, err
	}
	return claim, nil
}

func (database *Database) Release(ctx context.Context, claim collection.Claim, now time.Time) error {
	_, err := database.sql.ExecContext(ctx, `UPDATE schedule_slots SET state='scheduled',lease_until=NULL WHERE id=? AND state='running'`, claim.SlotID)
	if err != nil {
		return fmt.Errorf("release collection lease: %w", err)
	}
	return nil
}

func (database *Database) Finish(ctx context.Context, claim collection.Claim, result collection.Result, random collection.Random) error {
	if random == nil {
		return fmt.Errorf("random source is required")
	}
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var state, url, periodID string
	var archived, completed sql.NullString
	var interval, timeout, generation int64
	err = tx.QueryRowContext(ctx, `SELECT h.administrative_state,h.agent_url,h.archived_at,h.bootstrap_completed_at,h.interval_seconds,h.timeout_seconds,h.collection_generation,s.schedule_period_id FROM hosts h JOIN schedule_slots s ON s.target_id=h.id WHERE h.id=? AND s.id=? AND s.state='running'`, claim.HostID, claim.SlotID).Scan(&state, &url, &archived, &completed, &interval, &timeout, &generation, &periodID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("recheck collection target: %w", err)
	}
	if archived.Valid || !completed.Valid || state == "disabled" {
		reason := "target_disabled"
		if archived.Valid {
			reason = "target_archived"
		}
		if _, err = tx.ExecContext(ctx, `UPDATE schedule_slots SET state='cancelled',lease_until=NULL,cancellation_reason=?,finished_at=? WHERE id=? AND state='running'`, reason, dbTime(result.ReceivedAt), claim.SlotID); err != nil {
			return err
		}
		if err = closePeriod(ctx, tx, periodID, result.ReceivedAt); err != nil {
			return err
		}
		return tx.Commit()
	}
	if url != claim.AgentURL || generation != claim.CollectionGeneration {
		if _, err = tx.ExecContext(ctx, `UPDATE schedule_slots SET state='cancelled',lease_until=NULL,cancellation_reason='target_changed',finished_at=? WHERE id=? AND state='running'`, dbTime(result.ReceivedAt), claim.SlotID); err != nil {
			return err
		}
		if err = closePeriod(ctx, tx, periodID, result.ReceivedAt); err != nil {
			return err
		}
		if err = createAnchoredSchedule(ctx, tx, claim.HostID, result.ReceivedAt, interval, timeout, random); err != nil {
			return err
		}
		return tx.Commit()
	}
	nextNominal := claim.NominalAt.Add(claim.Interval)
	jitter := time.Duration(random.Intn(6)) * time.Second
	nextDeadline := nextNominal.Add(5*time.Second + time.Duration(timeout)*time.Second)
	nextID, err := random.ID()
	if err != nil {
		return err
	}
	finished := dbTime(result.ReceivedAt)
	terminalState := "failed"
	if result.Snapshot != nil {
		terminalState = "succeeded"
	}
	if _, err = tx.ExecContext(ctx, `UPDATE schedule_slots SET state=?,lease_until=NULL,finished_at=? WHERE id=? AND state='running'`, terminalState, finished, claim.SlotID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO schedule_slots(id,target_type,target_id,schedule_period_id,nominal_at,not_before_at,deadline_at,effective_interval_seconds,effective_timeout_seconds,state,process_attempts) VALUES(?,'host',?,?,?,?,?,?,?,'scheduled',0)`, nextID, claim.HostID, periodID, dbTime(nextNominal), dbTime(nextNominal.Add(jitter)), dbTime(nextDeadline), interval, timeout); err != nil {
		return fmt.Errorf("create next collection slot: %w", err)
	}
	if result.Snapshot == nil {
		if result.Failure == collection.FailureProtocol {
			return tx.Commit()
		}
		if _, err = tx.ExecContext(ctx, `UPDATE hosts SET last_attempt_at=?,consecutive_failures=consecutive_failures+1,observed_state=CASE WHEN observed_state='unreachable' THEN 'unreachable' WHEN last_success_received_at IS NULL THEN 'unknown' ELSE 'degraded' END,updated_at=?,version=version+1 WHERE id=?`, finished, finished, claim.HostID); err != nil {
			return err
		}
		return tx.Commit()
	}
	if err = persistSnapshot(ctx, tx, claim.HostID, *result.Snapshot, result.ReceivedAt, random); err != nil {
		return err
	}
	observed, _ := time.Parse(time.RFC3339Nano, result.Snapshot.Host.ObservedAt)
	degraded := len(result.Snapshot.CollectionWarnings) > 0 || result.Snapshot.Tailscale.State != "connected" || absDuration(observed.Sub(result.ReceivedAt)) > 5*time.Minute
	observedState := "healthy"
	if degraded {
		observedState = "degraded"
	}
	if _, err = tx.ExecContext(ctx, `UPDATE hosts SET administrative_state='active',observed_state=?,last_attempt_at=?,last_success_received_at=?,freshness_deadline_at=?,consecutive_failures=0,stale=0,updated_at=?,version=version+1 WHERE id=?`, observedState, finished, finished, dbTime(nextDeadline.Add(5*time.Second)), finished, claim.HostID); err != nil {
		return err
	}
	return tx.Commit()
}

func persistSnapshot(ctx context.Context, tx *sql.Tx, hostID string, snapshot agent.Snapshot, received time.Time, random collection.Random) error {
	observed, err := time.Parse(time.RFC3339Nano, snapshot.Host.ObservedAt)
	if err != nil {
		return err
	}
	integer := func(value *uint64) (any, error) {
		if value == nil {
			return nil, nil
		}
		if *value > math.MaxInt64 {
			return nil, fmt.Errorf("snapshot integer exceeds storage range")
		}
		return int64(*value), nil
	}
	uptime, err := integer(snapshot.Host.UptimeSeconds)
	if err != nil {
		return err
	}
	memoryUsed, err := integer(snapshot.Host.MemoryUsedBytes)
	if err != nil {
		return err
	}
	memoryTotal, err := integer(snapshot.Host.MemoryTotalBytes)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO host_snapshots(host_id,received_at,observed_at,hostname,boot_id,uptime_seconds,cpu_used_percent,memory_used_bytes,memory_total_bytes,tailscale_state,tailscale_hostname,agent_version,protocol_version) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(host_id) DO UPDATE SET received_at=excluded.received_at,observed_at=excluded.observed_at,hostname=excluded.hostname,boot_id=excluded.boot_id,uptime_seconds=excluded.uptime_seconds,cpu_used_percent=excluded.cpu_used_percent,memory_used_bytes=excluded.memory_used_bytes,memory_total_bytes=excluded.memory_total_bytes,tailscale_state=excluded.tailscale_state,tailscale_hostname=excluded.tailscale_hostname,agent_version=excluded.agent_version,protocol_version=excluded.protocol_version`, hostID, dbTime(received), dbTime(observed), snapshot.Host.Hostname, snapshot.Host.BootID, uptime, snapshot.Host.CPUUsedPercent, memoryUsed, memoryTotal, snapshot.Tailscale.State, snapshot.Tailscale.Hostname, snapshot.AgentVersion, snapshot.ProtocolVersion); err != nil {
		return fmt.Errorf("store host snapshot: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM host_snapshot_warnings WHERE host_id=?`, hostID); err != nil {
		return err
	}
	for _, warning := range snapshot.CollectionWarnings {
		if _, err = tx.ExecContext(ctx, `INSERT INTO host_snapshot_warnings(host_id,code) VALUES(?,?)`, hostID, warning); err != nil {
			return err
		}
	}
	if absDuration(observed.Sub(received)) > 5*time.Minute {
		if _, err = tx.ExecContext(ctx, `INSERT INTO host_snapshot_warnings(host_id,code) VALUES(?,'clock_skew')`, hostID); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM host_snapshot_addresses WHERE host_id=?`, hostID); err != nil {
		return err
	}
	for _, address := range snapshot.Tailscale.Addresses {
		if _, err = tx.ExecContext(ctx, `INSERT INTO host_snapshot_addresses(host_id,address) VALUES(?,?)`, hostID, address); err != nil {
			return err
		}
	}
	warnings := map[string]bool{}
	for _, warning := range snapshot.CollectionWarnings {
		warnings[warning] = true
	}
	authoritative := !warnings["filesystems_partial"] && !warnings["filesystems_unavailable"]
	if authoritative {
		if _, err = tx.ExecContext(ctx, `UPDATE filesystems SET present=0,version=version+1 WHERE host_id=? AND present=1`, hostID); err != nil {
			return err
		}
	}
	if warnings["filesystems_unavailable"] {
		return nil
	}
	for _, filesystem := range snapshot.Filesystems {
		kind := hosts.IdentityFallback
		first := filesystem.Device
		if filesystem.FSUUID != nil {
			kind = hosts.IdentityUUID
			first = *filesystem.FSUUID
		}
		identity, encodeErr := hosts.EncodeIdentityV1(kind, first, filesystem.MountRoot, filesystem.MountPoint)
		if encodeErr != nil {
			return encodeErr
		}
		if filesystem.TotalBytes > math.MaxInt64 || filesystem.UsedBytes > math.MaxInt64 || filesystem.AvailableBytes > math.MaxInt64 {
			return fmt.Errorf("filesystem integer exceeds storage range")
		}
		filesystemID, found, lookupErr := filesystemIdentityID(ctx, tx, hostID, string(kind), identity)
		if lookupErr != nil {
			return lookupErr
		}
		if !found {
			filesystemID, err = random.ID()
			if err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO filesystems(id,host_id,identity_kind,identity_key,fs_uuid,mount_root,mount_point,major_minor,fs_type,device,total_bytes,used_bytes,available_bytes,discovered_at,selected,present,last_observed_at,version) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,1,1,?,1) ON CONFLICT(host_id,identity_kind,identity_key) DO UPDATE SET fs_uuid=excluded.fs_uuid,mount_root=excluded.mount_root,mount_point=excluded.mount_point,major_minor=excluded.major_minor,fs_type=excluded.fs_type,device=excluded.device,total_bytes=excluded.total_bytes,used_bytes=excluded.used_bytes,available_bytes=excluded.available_bytes,present=1,last_observed_at=excluded.last_observed_at,version=filesystems.version+1`, filesystemID, hostID, string(kind), identity, filesystem.FSUUID, filesystem.MountRoot, filesystem.MountPoint, filesystem.MajorMinor, filesystem.FSType, filesystem.Device, int64(filesystem.TotalBytes), int64(filesystem.UsedBytes), int64(filesystem.AvailableBytes), dbTime(received), dbTime(received))
		if err != nil {
			return fmt.Errorf("store filesystem: %w", err)
		}
	}
	return nil
}

func filesystemIdentityID(ctx context.Context, tx *sql.Tx, hostID, kind string, key []byte) (string, bool, error) {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM filesystems WHERE host_id=? AND identity_kind=? AND identity_key=?`, hostID, kind, key).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return id, err == nil, err
}

func absDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}

func (database *Database) Maintain(ctx context.Context, now time.Time, random collection.Random) error {
	if random == nil {
		return fmt.Errorf("random source is required")
	}
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	timestamp := dbTime(now)
	if _, err = tx.ExecContext(ctx, `UPDATE schedule_slots SET state='cancelled',lease_until=NULL,cancellation_reason=CASE WHEN h.archived_at IS NULL THEN 'target_disabled' ELSE 'target_archived' END,finished_at=? FROM hosts h WHERE schedule_slots.target_type='host' AND schedule_slots.target_id=h.id AND schedule_slots.state IN ('scheduled','running') AND (h.administrative_state='disabled' OR h.archived_at IS NOT NULL)`, timestamp); err != nil {
		return err
	}
	periodRows, err := tx.QueryContext(ctx, `SELECT p.id FROM schedule_periods p JOIN hosts h ON h.id=p.target_id WHERE p.ends_at IS NULL AND p.target_type='host' AND (h.administrative_state='disabled' OR h.archived_at IS NOT NULL)`)
	if err != nil {
		return err
	}
	var administrativePeriods []string
	for periodRows.Next() {
		var id string
		if err = periodRows.Scan(&id); err != nil {
			periodRows.Close()
			return err
		}
		administrativePeriods = append(administrativePeriods, id)
	}
	if err = periodRows.Close(); err != nil {
		return err
	}
	for _, id := range administrativePeriods {
		if err = closePeriod(ctx, tx, id, now); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE schedule_slots SET state='scheduled',lease_until=NULL WHERE state='running' AND lease_until<=? AND deadline_at>=? AND process_attempts<2 AND EXISTS(SELECT 1 FROM hosts h WHERE h.id=schedule_slots.target_id AND h.administrative_state IN ('pending','active') AND h.archived_at IS NULL)`, timestamp, timestamp); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE hosts SET stale=1,updated_at=?,version=version+1 WHERE stale=0 AND freshness_deadline_at IS NOT NULL AND freshness_deadline_at<=? AND EXISTS(SELECT 1 FROM host_snapshots x WHERE x.host_id=hosts.id)`, timestamp, timestamp); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE hosts SET observed_state='unreachable',updated_at=?,version=version+1 WHERE consecutive_failures>=3 AND observed_state!='unreachable' AND (SELECT s.deadline_at FROM schedule_slots s WHERE s.target_id=hosts.id AND s.target_type='host' AND s.state='failed' ORDER BY s.finished_at DESC LIMIT 1)<=?`, timestamp, timestamp); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT s.id,s.target_id,s.schedule_period_id,s.nominal_at,s.effective_interval_seconds,h.interval_seconds,h.timeout_seconds FROM schedule_slots s JOIN hosts h ON h.id=s.target_id WHERE s.target_type='host' AND s.state IN ('scheduled','running') AND s.deadline_at<? AND h.administrative_state IN ('pending','active') AND h.archived_at IS NULL`, timestamp)
	if err != nil {
		return err
	}
	type expired struct {
		slot, host, period, nominal    string
		oldInterval, interval, timeout int64
	}
	var expiredSlots []expired
	for rows.Next() {
		var item expired
		if err = rows.Scan(&item.slot, &item.host, &item.period, &item.nominal, &item.oldInterval, &item.interval, &item.timeout); err != nil {
			rows.Close()
			return err
		}
		expiredSlots = append(expiredSlots, item)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, item := range expiredSlots {
		if err = reanchor(ctx, tx, item.slot, item.host, item.period, item.nominal, item.oldInterval, item.interval, item.timeout, now, random); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func closePeriod(ctx context.Context, tx *sql.Tx, periodID string, at time.Time) error {
	var starts string
	if err := tx.QueryRowContext(ctx, `SELECT starts_at FROM schedule_periods WHERE id=?`, periodID).Scan(&starts); err != nil {
		return err
	}
	start, err := parseDBTime(starts)
	if err != nil {
		return err
	}
	if !at.After(start) {
		at = start.Add(time.Nanosecond)
	}
	_, err = tx.ExecContext(ctx, `UPDATE schedule_periods SET ends_at=? WHERE id=? AND ends_at IS NULL`, dbTime(at), periodID)
	return err
}

func createAnchoredSchedule(ctx context.Context, tx *sql.Tx, hostID string, now time.Time, interval, timeout int64, random collection.Random) error {
	periodID, err := random.ID()
	if err != nil {
		return err
	}
	slotID, err := random.ID()
	if err != nil {
		return err
	}
	jitter := time.Duration(random.Intn(6)) * time.Second
	if _, err = tx.ExecContext(ctx, `INSERT INTO schedule_periods(id,target_type,target_id,starts_at,effective_interval_seconds,effective_timeout_seconds) VALUES(?,'host',?,?,?,?)`, periodID, hostID, dbTime(now), interval, timeout); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO schedule_slots(id,target_type,target_id,schedule_period_id,nominal_at,not_before_at,deadline_at,effective_interval_seconds,effective_timeout_seconds,state,process_attempts) VALUES(?,'host',?,?,?,?,?,?,?,'scheduled',0)`, slotID, hostID, periodID, dbTime(now), dbTime(now.Add(jitter)), dbTime(now.Add(5*time.Second+time.Duration(timeout)*time.Second)), interval, timeout)
	return err
}

func reanchor(ctx context.Context, tx *sql.Tx, slotID, hostID, periodID, nominalRaw string, oldInterval, interval, timeout int64, now time.Time, random collection.Random) error {
	nominal, err := parseDBTime(nominalRaw)
	if err != nil {
		return err
	}
	finished := dbTime(now)
	if _, err = tx.ExecContext(ctx, `UPDATE schedule_slots SET state='cancelled',lease_until=NULL,cancellation_reason='missed_deadline',finished_at=? WHERE id=?`, finished, slotID); err != nil {
		return err
	}
	end := nominal.Add(time.Duration(oldInterval) * time.Second)
	if now.Before(end) {
		end = now
	}
	if _, err = tx.ExecContext(ctx, `UPDATE schedule_periods SET ends_at=? WHERE id=? AND ends_at IS NULL`, dbTime(end), periodID); err != nil {
		return err
	}
	newPeriod, err := random.ID()
	if err != nil {
		return err
	}
	newSlot, err := random.ID()
	if err != nil {
		return err
	}
	jitter := time.Duration(random.Intn(6)) * time.Second
	if _, err = tx.ExecContext(ctx, `INSERT INTO schedule_periods(id,target_type,target_id,starts_at,effective_interval_seconds,effective_timeout_seconds) VALUES(?,'host',?,?,?,?)`, newPeriod, hostID, dbTime(now), interval, timeout); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO schedule_slots(id,target_type,target_id,schedule_period_id,nominal_at,not_before_at,deadline_at,effective_interval_seconds,effective_timeout_seconds,state,process_attempts) VALUES(?,'host',?,?,?,?,?,?,?,'scheduled',0)`, newSlot, hostID, newPeriod, dbTime(now), dbTime(now.Add(jitter)), dbTime(now.Add(5*time.Second+time.Duration(timeout)*time.Second)), interval, timeout)
	return err
}
