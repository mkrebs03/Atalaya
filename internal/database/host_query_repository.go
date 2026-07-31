package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/mkrebs03/atalaya/internal/hostquery"
)

var _ hostquery.Repository = (*Database)(nil)

const hostProjection = `SELECT h.id,h.display_name,h.administrative_state,h.observed_state,h.stale,
	h.last_attempt_at,h.last_success_received_at,h.freshness_deadline_at,h.consecutive_failures,h.interval_seconds,h.timeout_seconds,h.archived_at,h.version,h.agent_url,
	s.received_at,s.observed_at,s.hostname,s.boot_id,s.uptime_seconds,s.cpu_used_percent,s.memory_used_bytes,s.memory_total_bytes,
	s.tailscale_state,s.tailscale_hostname,s.agent_version,s.protocol_version
	FROM hosts h LEFT JOIN host_snapshots s ON s.host_id=h.id`

func (database *Database) ListHosts(ctx context.Context, limit int, cursor *hostquery.HostCursor) ([]hostquery.HostSummary, error) {
	return database.listHosts(ctx, limit, cursor, false)
}

// ListHostsIncludingArchived is used by the administrative UI. The public
// summary deliberately keeps archived resources hidden by default.
func (database *Database) ListHostsIncludingArchived(ctx context.Context, limit int, cursor *hostquery.HostCursor) ([]hostquery.HostSummary, error) {
	return database.listHosts(ctx, limit, cursor, true)
}

func (database *Database) listHosts(ctx context.Context, limit int, cursor *hostquery.HostCursor, includeArchived bool) ([]hostquery.HostSummary, error) {
	tx, err := database.sql.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin host list snapshot: %w", err)
	}
	defer tx.Rollback()
	args := []any{}
	where := ` WHERE h.bootstrap_completed_at IS NOT NULL`
	if !includeArchived {
		where += ` AND h.archived_at IS NULL`
	}
	if cursor != nil {
		where += ` AND (lower(h.display_name)>? OR (lower(h.display_name)=? AND h.id>?))`
		args = append(args, cursor.Name, cursor.Name, cursor.ID)
	}
	args = append(args, limit)
	rows, err := tx.QueryContext(ctx, hostProjection+where+` ORDER BY h.display_name COLLATE NOCASE,h.id LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("list hosts: %w", err)
	}
	details := make([]hostquery.HostDetail, 0, limit)
	for rows.Next() {
		detail, err := scanHostBase(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		details = append(details, detail)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("list hosts: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := enrichHosts(ctx, tx, details); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit host list snapshot: %w", err)
	}
	items := make([]hostquery.HostSummary, len(details))
	for i := range details {
		items[i] = details[i].HostSummary
	}
	return items, nil
}

type scanner interface{ Scan(...any) error }

func (database *Database) GetHost(ctx context.Context, id string) (hostquery.HostDetail, error) {
	tx, err := database.sql.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return hostquery.HostDetail{}, err
	}
	defer tx.Rollback()
	row := tx.QueryRowContext(ctx, hostProjection+` WHERE h.id=? AND h.archived_at IS NULL AND h.bootstrap_completed_at IS NOT NULL`, id)
	detail, err := scanHostBase(row)
	if errors.Is(err, sql.ErrNoRows) {
		return hostquery.HostDetail{}, hostquery.ErrNotFound
	}
	if err != nil {
		return detail, err
	}
	items := []hostquery.HostDetail{detail}
	if err := enrichHosts(ctx, tx, items); err != nil {
		return detail, err
	}
	detail = items[0]
	if err := tx.Commit(); err != nil {
		return detail, fmt.Errorf("commit host detail snapshot: %w", err)
	}
	return detail, nil
}

func scanHostBase(row scanner) (hostquery.HostDetail, error) {
	var d hostquery.HostDetail
	var stale int
	var lastAttempt, lastSuccess, freshness, archived, received, observed, hostname, bootID, tsState, tsHostname, agentVersion sql.NullString
	var uptime, memUsed, memTotal, protocol sql.NullInt64
	var cpu sql.NullFloat64
	if err := row.Scan(&d.ID, &d.DisplayName, &d.AdministrativeState, &d.ObservedState, &stale, &lastAttempt, &lastSuccess, &freshness, &d.ConsecutiveFailures, &d.IntervalSeconds, &d.TimeoutSeconds, &archived, &d.Version, &d.AgentURL, &received, &observed, &hostname, &bootID, &uptime, &cpu, &memUsed, &memTotal, &tsState, &tsHostname, &agentVersion, &protocol); err != nil {
		return d, err
	}
	d.Stale = stale != 0
	d.LastAttemptAt = stringPointer(lastAttempt)
	d.LastSuccessReceivedAt = stringPointer(lastSuccess)
	d.FreshnessDeadlineAt = stringPointer(freshness)
	d.Archived = archived.Valid
	d.UptimeSeconds = intPointer(uptime)
	d.CPUUsedPercent = floatPointer(cpu)
	d.MemoryUsedBytes = intPointer(memUsed)
	d.MemoryTotalBytes = intPointer(memTotal)
	d.AgentVersion = stringPointer(agentVersion)
	d.ProtocolVersion = intPointer(protocol)
	d.Warnings = []string{}
	if received.Valid {
		ts := hostquery.Tailscale{State: tsState.String, Hostname: stringPointer(tsHostname), Addresses: []string{}}
		d.Tailscale = &ts
		d.Snapshot = &hostquery.Snapshot{ReceivedAt: received.String, ObservedAt: observed.String, Hostname: hostname.String, BootID: stringPointer(bootID), UptimeSeconds: intPointer(uptime), CPUUsedPercent: floatPointer(cpu), MemoryUsedBytes: intPointer(memUsed), MemoryTotalBytes: intPointer(memTotal), Tailscale: ts, CollectionWarnings: []string{}}
	}
	return d, nil
}

func enrichHosts(ctx context.Context, tx *sql.Tx, items []hostquery.HostDetail) error {
	if len(items) == 0 {
		return nil
	}
	index := make(map[string]int, len(items))
	placeholders := make([]string, len(items))
	args := make([]any, len(items))
	for i := range items {
		index[items[i].ID] = i
		placeholders[i] = "?"
		args[i] = items[i].ID
	}
	in := strings.Join(placeholders, ",")
	rows, err := tx.QueryContext(ctx, `SELECT host_id,address FROM host_snapshot_addresses WHERE host_id IN (`+in+`) ORDER BY host_id,address`, args...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, value string
		if err := rows.Scan(&id, &value); err != nil {
			rows.Close()
			return err
		}
		i := index[id]
		if items[i].Tailscale != nil {
			items[i].Tailscale.Addresses = append(items[i].Tailscale.Addresses, value)
			items[i].Snapshot.Tailscale.Addresses = items[i].Tailscale.Addresses
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	rows, err = tx.QueryContext(ctx, `SELECT host_id,code FROM host_snapshot_warnings WHERE host_id IN (`+in+`) ORDER BY host_id,code`, args...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, value string
		if err := rows.Scan(&id, &value); err != nil {
			rows.Close()
			return err
		}
		i := index[id]
		items[i].Warnings = append(items[i].Warnings, value)
		if value != "clock_skew" && items[i].Snapshot != nil {
			items[i].Snapshot.CollectionWarnings = append(items[i].Snapshot.CollectionWarnings, value)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	rows, err = tx.QueryContext(ctx, `SELECT host_id,id,mount_point,used_bytes,total_bytes,used_percent FROM (SELECT host_id,id,mount_point,used_bytes,total_bytes,(100.0*used_bytes/total_bytes) used_percent,row_number() OVER(PARTITION BY host_id ORDER BY (1.0*used_bytes/total_bytes) DESC,mount_point,id) rank FROM filesystems WHERE selected=1 AND present=1 AND total_bytes>0 AND host_id IN (`+in+`)) WHERE rank=1 ORDER BY host_id`, args...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		var value hostquery.ConstrainedFilesystem
		if err := rows.Scan(&id, &value.ID, &value.MountPoint, &value.UsedBytes, &value.TotalBytes, &value.UsedPercent); err != nil {
			_ = rows.Close()
			return err
		}
		i := index[id]
		items[i].MostConstrainedFilesystem = &value
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	return rows.Close()
}

func (database *Database) ListFilesystems(ctx context.Context, hostID string, limit int, cursor *hostquery.FilesystemCursor) ([]hostquery.Filesystem, error) {
	tx, err := database.sql.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM hosts WHERE id=? AND archived_at IS NULL AND bootstrap_completed_at IS NOT NULL`, hostID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return nil, hostquery.ErrNotFound
	} else if err != nil {
		return nil, err
	}
	where := ` WHERE host_id=?`
	args := []any{hostID}
	if cursor != nil {
		where += ` AND (mount_point>? OR (mount_point=? AND id>?))`
		args = append(args, cursor.MountPoint, cursor.MountPoint, cursor.ID)
	}
	args = append(args, limit)
	rows, err := tx.QueryContext(ctx, `SELECT id,identity_kind,mount_point,device,mount_root,major_minor,fs_type,selected,present,used_bytes,total_bytes,available_bytes,last_observed_at,version FROM filesystems`+where+` ORDER BY mount_point,id LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	items := make([]hostquery.Filesystem, 0, limit)
	for rows.Next() {
		var f hostquery.Filesystem
		var selected, present int
		var used, total, available sql.NullInt64
		var observed sql.NullString
		if err := rows.Scan(&f.ID, &f.IdentityKind, &f.MountPoint, &f.Device, &f.MountRoot, &f.MajorMinor, &f.FSType, &selected, &present, &used, &total, &available, &observed, &f.Version); err != nil {
			_ = rows.Close()
			return nil, err
		}
		f.Selected = selected != 0
		f.Present = present != 0
		f.UsedBytes = intPointer(used)
		f.TotalBytes = intPointer(total)
		f.AvailableBytes = intPointer(available)
		f.LastObservedAt = stringPointer(observed)
		items = append(items, f)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return items, nil
}

func stringPointer(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	x := v.String
	return &x
}
func intPointer(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	x := v.Int64
	return &x
}
func floatPointer(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	x := v.Float64
	return &x
}
