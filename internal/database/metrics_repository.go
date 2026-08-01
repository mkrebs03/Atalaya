package database

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/mkrebs03/atalaya/internal/metrics"
)

var _ metrics.Repository = (*Database)(nil)
var _ metrics.RetentionRepository = (*Database)(nil)

type metricSeriesDefinition struct {
	hostID       string
	scopeID      string
	filesystemID *string
	mountPoint   *string
	key          string
	unit         string
}

func (database *Database) QueryHostMetrics(ctx context.Context, hostID string, query metrics.Query) (metrics.Response, error) {
	return database.queryHostMetricsAt(ctx, hostID, query, time.Now().UTC())
}

func (database *Database) queryHostMetricsAt(ctx context.Context, hostID string, query metrics.Query, now time.Time) (metrics.Response, error) {
	if err := metrics.ValidateQuery(query); err != nil {
		return metrics.Response{}, err
	}
	now = now.UTC()
	tx, err := database.sql.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return metrics.Response{}, err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM hosts WHERE id=? AND archived_at IS NULL AND bootstrap_completed_at IS NOT NULL`, hostID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return metrics.Response{}, metrics.ErrNotFound
	} else if err != nil {
		return metrics.Response{}, err
	}
	var rawRetention int64
	if err := tx.QueryRowContext(ctx, `SELECT raw_retention_seconds FROM retention_settings WHERE id=1`).Scan(&rawRetention); err != nil {
		return metrics.Response{}, err
	}
	cutoff := now.Add(-time.Duration(rawRetention) * time.Second)
	if query.Resolution == metrics.ResolutionRaw && query.From.Before(cutoff) {
		return metrics.Response{}, metrics.ErrRawNotAvailable
	}

	definitions := []metricSeriesDefinition{
		{hostID: hostID, scopeID: hostID, key: "cpu_used_percent", unit: "percent"},
		{hostID: hostID, scopeID: hostID, key: "memory_used_bytes", unit: "bytes"},
		{hostID: hostID, scopeID: hostID, key: "memory_total_bytes", unit: "bytes"},
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,mount_point FROM filesystems WHERE host_id=? AND selected=1 ORDER BY mount_point,id`, hostID)
	if err != nil {
		return metrics.Response{}, err
	}
	for rows.Next() {
		var id, mountPoint string
		if err := rows.Scan(&id, &mountPoint); err != nil {
			rows.Close()
			return metrics.Response{}, err
		}
		idCopy, mountCopy := id, mountPoint
		definitions = append(definitions,
			metricSeriesDefinition{hostID: hostID, scopeID: id, filesystemID: &idCopy, mountPoint: &mountCopy, key: "filesystem_used_bytes", unit: "bytes"},
			metricSeriesDefinition{hostID: hostID, scopeID: id, filesystemID: &idCopy, mountPoint: &mountCopy, key: "filesystem_total_bytes", unit: "bytes"},
		)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return metrics.Response{}, err
	}
	if err := rows.Close(); err != nil {
		return metrics.Response{}, err
	}
	if err := validateHourlyQueryWork(query, cutoff, len(definitions)); err != nil {
		return metrics.Response{}, err
	}

	response := metrics.Response{From: query.From.UTC(), To: query.To.UTC(), Series: make([]metrics.Series, 0, len(definitions))}
	for _, definition := range definitions {
		var points []metrics.Point
		switch query.Resolution {
		case metrics.ResolutionRaw:
			points, err = rawPoints(ctx, tx, definition, query.From.UTC(), query.To.UTC())
		case metrics.ResolutionHourly:
			points, err = hourlyPoints(ctx, tx, definition, query.From.UTC(), query.To.UTC(), cutoff, now)
		default:
			points, err = autoPoints(ctx, tx, definition, query.From.UTC(), query.To.UTC(), cutoff, now)
		}
		if err != nil {
			return metrics.Response{}, err
		}
		if len(points) > metrics.MaxPointsPerSeries {
			return metrics.Response{}, metrics.ErrPointLimit
		}
		response.Series = append(response.Series, metrics.Series{Key: definition.key, Unit: definition.unit, FilesystemID: definition.filesystemID, MountPoint: definition.mountPoint, Points: points})
	}
	response.Resolution = responseResolution(query, response.Series, cutoff)
	if err := tx.Commit(); err != nil {
		return metrics.Response{}, err
	}
	return response, nil
}

func responseResolution(query metrics.Query, series []metrics.Series, cutoff time.Time) metrics.Resolution {
	if query.Resolution != metrics.ResolutionAuto {
		return query.Resolution
	}
	sawRaw, sawHourly := false, false
	for _, item := range series {
		for _, point := range item.Points {
			switch point.Resolution {
			case metrics.ResolutionRaw:
				sawRaw = true
			case metrics.ResolutionHourly:
				sawHourly = true
			}
		}
	}
	if sawRaw && sawHourly {
		return metrics.ResolutionMixed
	}
	if sawRaw {
		return metrics.ResolutionRaw
	}
	if sawHourly {
		return metrics.ResolutionHourly
	}
	return resolvedResolution(query, cutoff)
}

func resolvedResolution(query metrics.Query, cutoff time.Time) metrics.Resolution {
	if query.Resolution != metrics.ResolutionAuto {
		return query.Resolution
	}
	if query.To.Before(cutoff) || query.To.Equal(cutoff) {
		return metrics.ResolutionHourly
	}
	if query.From.After(cutoff) || query.From.Equal(cutoff) {
		return metrics.ResolutionRaw
	}
	return metrics.ResolutionMixed
}

func rawPoints(ctx context.Context, tx *sql.Tx, definition metricSeriesDefinition, from, to time.Time) ([]metrics.Point, error) {
	rows, err := tx.QueryContext(ctx, `SELECT received_at,COALESCE(value_real,CAST(value_integer AS REAL)) FROM metric_samples WHERE scope_id=? AND metric_key=? AND received_at>=? AND received_at<? ORDER BY received_at,slot_id LIMIT ?`, definition.scopeID, definition.key, dbTime(from), dbTime(to), metrics.MaxPointsPerSeries+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	points := []metrics.Point{}
	for rows.Next() {
		var rawTime string
		var value float64
		if err := rows.Scan(&rawTime, &value); err != nil {
			return nil, err
		}
		timestamp, err := parseDBTime(rawTime)
		if err != nil {
			return nil, err
		}
		if len(points) == metrics.MaxPointsPerSeries {
			return nil, metrics.ErrPointLimit
		}
		points = append(points, metrics.Point{Timestamp: timestamp, Resolution: metrics.ResolutionRaw, Value: value, Min: value, Max: value, SampleCount: 1})
	}
	return points, rows.Err()
}

func validateHourlyQueryWork(query metrics.Query, cutoff time.Time, seriesCount int) error {
	from, to, ok := hourlyQueryRange(query, cutoff)
	if !ok {
		return nil
	}
	buckets := hourlyBucketCount(from, to)
	if buckets > int64(metrics.MaxPointsPerSeries) {
		return metrics.ErrPointLimit
	}
	if seriesCount <= 0 || buckets == 0 {
		return nil
	}
	lookupsPerBucket := int64(4)
	if buckets > metrics.MaxHourlyQueryWork/lookupsPerBucket/int64(seriesCount) {
		return metrics.ErrPointLimit
	}
	return nil
}

func hourlyQueryRange(query metrics.Query, cutoff time.Time) (time.Time, time.Time, bool) {
	from, to := query.From.UTC(), query.To.UTC()
	if query.Resolution == metrics.ResolutionHourly {
		return from, to, true
	}
	if query.Resolution != metrics.ResolutionAuto || !from.Before(cutoff.UTC()) {
		return time.Time{}, time.Time{}, false
	}
	if !to.After(cutoff.UTC()) {
		return from, to, true
	}
	splitAt := cutoff.UTC().Truncate(time.Hour)
	if !cutoff.UTC().Equal(splitAt) {
		splitAt = splitAt.Add(time.Hour)
	}
	if splitAt.After(to) {
		splitAt = to
	}
	return from, splitAt, true
}

func hourlyBucketCount(from, to time.Time) int64 {
	start := from.UTC().Truncate(time.Hour)
	to = to.UTC()
	if !start.Before(to) {
		return 0
	}
	maxBucketsEnd := start.Add(time.Duration(metrics.MaxPointsPerSeries) * time.Hour)
	if to.After(maxBucketsEnd) {
		return int64(metrics.MaxPointsPerSeries) + 1
	}
	return int64((to.Sub(start) + time.Hour - 1) / time.Hour)
}

func autoPoints(ctx context.Context, tx *sql.Tx, definition metricSeriesDefinition, from, to, cutoff, now time.Time) ([]metrics.Point, error) {
	points := []metrics.Point{}
	cutoff = cutoff.UTC()
	if from.Before(cutoff) {
		if !to.After(cutoff) {
			return hourlyPoints(ctx, tx, definition, from, to, cutoff, now)
		}
		// A bucket crossing the cutoff must come from one source only. Prefer the
		// hourly representation for the whole bucket, then resume raw points at
		// the following bucket boundary.
		splitAt := cutoff.Truncate(time.Hour)
		if !cutoff.Equal(splitAt) {
			splitAt = splitAt.Add(time.Hour)
		}
		oldTo := to
		if oldTo.After(splitAt) {
			oldTo = splitAt
		}
		old, err := hourlyPoints(ctx, tx, definition, from, oldTo, cutoff, now)
		if err != nil {
			return nil, err
		}
		points = append(points, old...)
		if splitAt.Before(to) {
			recent, err := rawPoints(ctx, tx, definition, splitAt, to)
			if err != nil {
				return nil, err
			}
			points = append(points, recent...)
		}
		return points, nil
	}
	return rawPoints(ctx, tx, definition, from, to)
}

func hourlyPoints(ctx context.Context, tx *sql.Tx, definition metricSeriesDefinition, from, to, cutoff, now time.Time) ([]metrics.Point, error) {
	start := from.UTC().Truncate(time.Hour)
	if hourlyBucketCount(from, to) > int64(metrics.MaxPointsPerSeries) {
		return nil, metrics.ErrPointLimit
	}
	points := []metrics.Point{}
	for bucket := start; bucket.Before(to); bucket = bucket.Add(time.Hour) {
		bucketEnd := bucket.Add(time.Hour)
		point, found, err := readRollup(ctx, tx, definition, bucket)
		if err != nil {
			return nil, err
		}
		if !found && bucketEnd.After(cutoff) {
			point, found, err = aggregateRawPoint(ctx, tx, definition, bucket, bucketEnd, now)
			if err != nil {
				return nil, err
			}
		}
		if found {
			points = append(points, point)
		}
	}
	return points, nil
}

func readRollup(ctx context.Context, tx *sql.Tx, definition metricSeriesDefinition, bucket time.Time) (metrics.Point, bool, error) {
	var point metrics.Point
	var bucketStart, resolution string
	var covered, expected sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT bucket_start,min_value,max_value,average_value,sample_count,covered_slot_count,expected_sample_count,complete FROM hourly_rollups WHERE scope_id=? AND metric_key=? AND bucket_start=?`, definition.scopeID, definition.key, dbTime(bucket)).Scan(&bucketStart, &point.Min, &point.Max, &point.Value, &point.SampleCount, &covered, &expected, &point.Complete)
	if errors.Is(err, sql.ErrNoRows) {
		return metrics.Point{}, false, nil
	}
	if err != nil {
		return metrics.Point{}, false, err
	}
	resolution = string(metrics.ResolutionHourly)
	point.Timestamp, err = parseDBTime(bucketStart)
	if err != nil {
		return metrics.Point{}, false, err
	}
	point.Resolution = metrics.Resolution(resolution)
	if covered.Valid {
		point.CoveredSlotCount = &covered.Int64
	}
	if expected.Valid {
		point.ExpectedSampleCount = &expected.Int64
	}
	return point, true, nil
}

func aggregateRawPoint(ctx context.Context, tx *sql.Tx, definition metricSeriesDefinition, bucket, bucketEnd, now time.Time) (metrics.Point, bool, error) {
	var point metrics.Point
	var minValue, maxValue, average sql.NullFloat64
	err := tx.QueryRowContext(ctx, `SELECT MIN(COALESCE(value_real,CAST(value_integer AS REAL))),MAX(COALESCE(value_real,CAST(value_integer AS REAL))),AVG(COALESCE(value_real,CAST(value_integer AS REAL))),COUNT(*) FROM metric_samples WHERE scope_id=? AND metric_key=? AND received_at>=? AND received_at<?`, definition.scopeID, definition.key, dbTime(bucket), dbTime(bucketEnd)).Scan(&minValue, &maxValue, &average, &point.SampleCount)
	if err != nil {
		return metrics.Point{}, false, err
	}
	if point.SampleCount == 0 {
		return metrics.Point{}, false, nil
	}
	point.Min = minValue.Float64
	point.Max = maxValue.Float64
	point.Value = average.Float64
	point.Timestamp = bucket
	point.Resolution = metrics.ResolutionHourly
	point.Complete = false
	if err := fillCoverage(ctx, tx, definition, bucket, bucketEnd, &point); err != nil {
		return metrics.Point{}, false, err
	}
	point.Complete = bucketEnd.Before(now) && point.ExpectedSampleCount != nil && point.CoveredSlotCount != nil && *point.CoveredSlotCount >= *point.ExpectedSampleCount
	return point, true, nil
}

func fillCoverage(ctx context.Context, tx *sql.Tx, definition metricSeriesDefinition, bucket, bucketEnd time.Time, point *metrics.Point) error {
	var expected, covered int64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM schedule_slots s JOIN schedule_periods p ON p.id=s.schedule_period_id WHERE s.target_type='host' AND s.target_id=? AND s.nominal_at>=? AND s.nominal_at<? AND s.nominal_at>=p.starts_at AND (p.ends_at IS NULL OR s.nominal_at<p.ends_at)`, definition.hostID, dbTime(bucket), dbTime(bucketEnd)).Scan(&expected); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(DISTINCT m.slot_id) FROM metric_samples m JOIN schedule_slots s ON s.id=m.slot_id JOIN schedule_periods p ON p.id=s.schedule_period_id WHERE m.scope_id=? AND m.metric_key=? AND s.target_type='host' AND s.target_id=? AND s.nominal_at>=? AND s.nominal_at<? AND s.nominal_at>=p.starts_at AND (p.ends_at IS NULL OR s.nominal_at<p.ends_at)`, definition.scopeID, definition.key, definition.hostID, dbTime(bucket), dbTime(bucketEnd)).Scan(&covered); err != nil {
		return err
	}
	point.ExpectedSampleCount = &expected
	point.CoveredSlotCount = &covered
	point.Complete = covered >= expected
	return nil
}

func (database *Database) GetRetentionSettings(ctx context.Context) (metrics.RetentionSettings, error) {
	var result metrics.RetentionSettings
	var updated string
	err := database.sql.QueryRowContext(ctx, `SELECT raw_retention_seconds,rollup_retention_seconds,updated_at,version FROM retention_settings WHERE id=1`).Scan(&result.RawRetentionSeconds, &result.RollupRetentionSeconds, &updated, &result.Version)
	if err != nil {
		return result, err
	}
	result.UpdatedAt, err = parseDBTime(updated)
	return result, err
}

func (database *Database) UpdateRetentionSettings(ctx context.Context, patch metrics.SettingsPatch, version int64, now time.Time) (metrics.RetentionSettings, error) {
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return metrics.RetentionSettings{}, err
	}
	defer tx.Rollback()
	var current metrics.RetentionSettings
	var updated string
	if err := tx.QueryRowContext(ctx, `SELECT raw_retention_seconds,rollup_retention_seconds,updated_at,version FROM retention_settings WHERE id=1`).Scan(&current.RawRetentionSeconds, &current.RollupRetentionSeconds, &updated, &current.Version); err != nil {
		return metrics.RetentionSettings{}, err
	}
	if current.Version != version {
		return metrics.RetentionSettings{}, metrics.ErrSettingsConflict
	}
	current.UpdatedAt, err = parseDBTime(updated)
	if err != nil {
		return metrics.RetentionSettings{}, err
	}
	if err := metrics.ValidateSettings(patch, current); err != nil {
		return metrics.RetentionSettings{}, err
	}
	raw := current.RawRetentionSeconds
	rollup := current.RollupRetentionSeconds
	if patch.RawRetentionSeconds != nil {
		raw = *patch.RawRetentionSeconds
	}
	if patch.RollupRetentionSeconds != nil {
		rollup = *patch.RollupRetentionSeconds
	}
	if _, err := tx.ExecContext(ctx, `UPDATE retention_settings SET raw_retention_seconds=?,rollup_retention_seconds=?,updated_at=?,version=version+1 WHERE id=1 AND version=?`, raw, rollup, dbTime(now), version); err != nil {
		return metrics.RetentionSettings{}, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT raw_retention_seconds,rollup_retention_seconds,updated_at,version FROM retention_settings WHERE id=1`).Scan(&current.RawRetentionSeconds, &current.RollupRetentionSeconds, &updated, &current.Version); err != nil {
		return metrics.RetentionSettings{}, err
	}
	current.UpdatedAt, err = parseDBTime(updated)
	if err != nil {
		return metrics.RetentionSettings{}, err
	}
	if err := tx.Commit(); err != nil {
		return metrics.RetentionSettings{}, err
	}
	return current, nil
}

func (database *Database) RunRetention(ctx context.Context, now time.Time) error {
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var rawSeconds, rollupSeconds int64
	var lastRollup sql.NullString
	var lastRawRetention, lastRollupRetention sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT raw_retention_seconds,rollup_retention_seconds,last_rollup_bucket_start,last_raw_retention_seconds,last_rollup_retention_seconds FROM retention_settings JOIN retention_progress ON retention_progress.id=1 WHERE retention_settings.id=1`).Scan(&rawSeconds, &rollupSeconds, &lastRollup, &lastRawRetention, &lastRollupRetention); err != nil {
		return err
	}
	openBucket := now.UTC().Truncate(time.Hour)
	rollupCutoff := now.UTC().Add(-time.Duration(rollupSeconds) * time.Second).Truncate(time.Hour)
	start := rollupCutoff
	if lastRollup.Valid && lastRawRetention.Valid && lastRollupRetention.Valid && lastRawRetention.Int64 == rawSeconds && lastRollupRetention.Int64 == rollupSeconds {
		previous, parseErr := parseDBTime(lastRollup.String)
		if parseErr != nil {
			return parseErr
		}
		retryFrom := previous.Add(-time.Hour)
		if retryFrom.After(start) {
			start = retryFrom
		}
	}
	for bucket := start; bucket.Before(openBucket); bucket = bucket.Add(time.Hour) {
		if err := persistRollupBucket(ctx, tx, bucket, bucket.Add(time.Hour), now.UTC()); err != nil {
			return err
		}
		if err := persistCheckRollupBucket(ctx, tx, bucket, bucket.Add(time.Hour), now.UTC()); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE retention_progress SET last_rollup_bucket_start=?,updated_at=? WHERE id=1`, dbTime(bucket), dbTime(now)); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE retention_progress SET last_raw_retention_seconds=?,last_rollup_retention_seconds=?,updated_at=? WHERE id=1`, rawSeconds, rollupSeconds, dbTime(now)); err != nil {
		return err
	}
	cutoff := now.UTC().Add(-time.Duration(rawSeconds) * time.Second)
	for {
		result, err := tx.ExecContext(ctx, `DELETE FROM metric_samples WHERE rowid IN (SELECT m.rowid FROM metric_samples m WHERE m.received_at<? AND EXISTS (SELECT 1 FROM hourly_rollups r WHERE r.scope_id=m.scope_id AND r.metric_key=m.metric_key AND r.bucket_start=substr(m.received_at,1,13)||':00:00Z') LIMIT 1000)`, dbTime(cutoff))
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil || count == 0 {
			if err != nil {
				return err
			}
			break
		}
	}
	for {
		result, err := tx.ExecContext(ctx, `DELETE FROM check_results WHERE rowid IN (SELECT r.rowid FROM check_results r WHERE r.received_at<? AND EXISTS (SELECT 1 FROM check_hourly_rollups h WHERE h.check_id=r.check_id AND h.metric_key='availability' AND h.bucket_start=substr(r.received_at,1,13)||':00:00Z') LIMIT 1000)`, dbTime(cutoff))
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count == 0 {
			break
		}
	}
	for {
		result, err := tx.ExecContext(ctx, `DELETE FROM hourly_rollups WHERE rowid IN (SELECT rowid FROM hourly_rollups WHERE bucket_end<=? LIMIT 1000)`, dbTime(rollupCutoff))
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil || count == 0 {
			if err != nil {
				return err
			}
			break
		}
	}
	for {
		result, err := tx.ExecContext(ctx, `DELETE FROM check_hourly_rollups WHERE rowid IN (SELECT rowid FROM check_hourly_rollups WHERE bucket_end<=? LIMIT 1000)`, dbTime(rollupCutoff))
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count == 0 {
			break
		}
	}
	manualCutoff := now.UTC().Add(-90 * 24 * time.Hour)
	for {
		result, err := tx.ExecContext(ctx, `DELETE FROM check_runs WHERE rowid IN (SELECT rowid FROM check_runs WHERE completed_at IS NOT NULL AND completed_at<? LIMIT 1000)`, dbTime(manualCutoff))
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count == 0 {
			break
		}
	}
	// Notification payloads and delivery attempts are operational history too;
	// retain every terminal kind (incident, test, and skipped) for the same
	// documented 90-day default instead of allowing the outbox to grow forever.
	for {
		result, err := tx.ExecContext(ctx, `DELETE FROM notification_deliveries WHERE rowid IN (SELECT rowid FROM notification_deliveries WHERE created_at<? LIMIT 1000)`, dbTime(manualCutoff))
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count == 0 {
			break
		}
	}
	for {
		result, err := tx.ExecContext(ctx, `DELETE FROM schedule_slots WHERE rowid IN (SELECT rowid FROM schedule_slots WHERE target_type='check' AND state IN ('succeeded','failed','cancelled') AND finished_at<? LIMIT 1000)`, dbTime(cutoff))
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count == 0 {
			break
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE retention_progress SET last_raw_cleanup_at=?,updated_at=? WHERE id=1`, dbTime(cutoff), dbTime(now)); err != nil {
		return err
	}
	return tx.Commit()
}

func persistRollupBucket(ctx context.Context, tx *sql.Tx, bucket, bucketEnd, now time.Time) error {
	rows, err := tx.QueryContext(ctx, `SELECT host_id,scope_id,filesystem_id,metric_key,MIN(COALESCE(value_real,CAST(value_integer AS REAL))),MAX(COALESCE(value_real,CAST(value_integer AS REAL))),AVG(COALESCE(value_real,CAST(value_integer AS REAL))),COUNT(*) FROM metric_samples WHERE received_at>=? AND received_at<? GROUP BY scope_id,metric_key`, dbTime(bucket), dbTime(bucketEnd))
	if err != nil {
		return err
	}
	for rows.Next() {
		var hostID, scopeID, metricKey string
		var filesystemID sql.NullString
		var minValue, maxValue, average float64
		var count int64
		if err := rows.Scan(&hostID, &scopeID, &filesystemID, &metricKey, &minValue, &maxValue, &average, &count); err != nil {
			return err
		}
		var hostScope metricSeriesDefinition
		hostScope = metricSeriesDefinition{hostID: hostID, scopeID: scopeID, key: metricKey}
		if filesystemID.Valid {
			hostScope.filesystemID = &filesystemID.String
		}
		point := metrics.Point{ExpectedSampleCount: nil, CoveredSlotCount: nil}
		if err := fillCoverage(ctx, tx, hostScope, bucket, bucketEnd, &point); err != nil {
			return err
		}
		complete := bucketEnd.Before(now) && point.ExpectedSampleCount != nil && point.CoveredSlotCount != nil && *point.CoveredSlotCount >= *point.ExpectedSampleCount
		filesystemValue := ""
		if filesystemID.Valid {
			filesystemValue = filesystemID.String
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO hourly_rollups(host_id,scope_id,filesystem_id,metric_key,bucket_start,bucket_end,min_value,max_value,average_value,sample_count,expected_sample_count,covered_slot_count,complete,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(scope_id,metric_key,bucket_start) DO UPDATE SET host_id=excluded.host_id,filesystem_id=excluded.filesystem_id,bucket_end=excluded.bucket_end,min_value=excluded.min_value,max_value=excluded.max_value,average_value=excluded.average_value,sample_count=excluded.sample_count,expected_sample_count=excluded.expected_sample_count,covered_slot_count=excluded.covered_slot_count,complete=excluded.complete,updated_at=excluded.updated_at`, hostID, scopeID, nullableString(filesystemValue), metricKey, dbTime(bucket), dbTime(bucketEnd), minValue, maxValue, average, count, nullableInt(point.ExpectedSampleCount), nullableInt(point.CoveredSlotCount), boolInt(complete), dbTime(now)); err != nil {
			return err
		}
	}
	return rows.Err()
}

func persistCheckRollupBucket(ctx context.Context, tx *sql.Tx, bucket, bucketEnd, now time.Time) error {
	rows, err := tx.QueryContext(ctx, `SELECT check_id,MIN(CASE WHEN status='up' THEN 1.0 ELSE 0.0 END),MAX(CASE WHEN status='up' THEN 1.0 ELSE 0.0 END),AVG(CASE WHEN status='up' THEN 1.0 ELSE 0.0 END),COUNT(*),SUM(CASE WHEN status='up' THEN 1 ELSE 0 END) FROM check_results WHERE received_at>=? AND received_at<? GROUP BY check_id`, dbTime(bucket), dbTime(bucketEnd))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var checkID string
		var minValue, maxValue, average float64
		var count, successful int64
		if err := rows.Scan(&checkID, &minValue, &maxValue, &average, &count, &successful); err != nil {
			return err
		}
		var point metrics.Point
		point.ExpectedSampleCount = nil
		point.CoveredSlotCount = nil
		if err := fillCheckCoverage(ctx, tx, checkID, bucket, bucketEnd, &point); err != nil {
			return err
		}
		complete := bucketEnd.Before(now) && point.ExpectedSampleCount != nil && point.CoveredSlotCount != nil && *point.ExpectedSampleCount <= *point.CoveredSlotCount
		if _, err := tx.ExecContext(ctx, `INSERT INTO check_hourly_rollups(check_id,metric_key,bucket_start,bucket_end,min_value,max_value,average_value,sample_count,successful_count,expected_sample_count,covered_slot_count,complete,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(check_id,metric_key,bucket_start) DO UPDATE SET bucket_end=excluded.bucket_end,min_value=excluded.min_value,max_value=excluded.max_value,average_value=excluded.average_value,sample_count=excluded.sample_count,successful_count=excluded.successful_count,expected_sample_count=excluded.expected_sample_count,covered_slot_count=excluded.covered_slot_count,complete=excluded.complete,updated_at=excluded.updated_at`, checkID, "availability", dbTime(bucket), dbTime(bucketEnd), minValue, maxValue, average, count, successful, nullableInt(point.ExpectedSampleCount), nullableInt(point.CoveredSlotCount), boolInt(complete), dbTime(now)); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	latencyRows, err := tx.QueryContext(ctx, `SELECT check_id,MIN(latency_ms),MAX(latency_ms),AVG(latency_ms),COUNT(*) FROM check_results WHERE status='up' AND latency_ms IS NOT NULL AND received_at>=? AND received_at<? GROUP BY check_id`, dbTime(bucket), dbTime(bucketEnd))
	if err != nil {
		return err
	}
	defer latencyRows.Close()
	for latencyRows.Next() {
		var checkID string
		var minValue, maxValue, average float64
		var count int64
		if err := latencyRows.Scan(&checkID, &minValue, &maxValue, &average, &count); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO check_hourly_rollups(check_id,metric_key,bucket_start,bucket_end,min_value,max_value,average_value,sample_count,complete,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(check_id,metric_key,bucket_start) DO UPDATE SET bucket_end=excluded.bucket_end,min_value=excluded.min_value,max_value=excluded.max_value,average_value=excluded.average_value,sample_count=excluded.sample_count,complete=excluded.complete,updated_at=excluded.updated_at`, checkID, "latency_ms", dbTime(bucket), dbTime(bucketEnd), minValue, maxValue, average, count, boolInt(bucketEnd.Before(now)), dbTime(now)); err != nil {
			return err
		}
	}
	return latencyRows.Err()
}

func nullableInt(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}
