package database

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/mkrebs03/atalaya/internal/metrics"
)

var _ metrics.CheckRepository = (*Database)(nil)

type checkMetricDefinition struct {
	checkID string
	key     string
	unit    string
}

func (database *Database) QueryCheckMetrics(ctx context.Context, checkID string, query metrics.Query) (metrics.Response, error) {
	return database.queryCheckMetricsAt(ctx, checkID, query, time.Now().UTC())
}

func (database *Database) queryCheckMetricsAt(ctx context.Context, checkID string, query metrics.Query, now time.Time) (metrics.Response, error) {
	if err := metrics.ValidateQuery(query); err != nil {
		return metrics.Response{}, err
	}
	tx, err := database.sql.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return metrics.Response{}, err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, "SELECT 1 FROM service_checks WHERE id=?", checkID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return metrics.Response{}, metrics.ErrCheckNotFound
	} else if err != nil {
		return metrics.Response{}, err
	}
	var rawRetention int64
	if err := tx.QueryRowContext(ctx, "SELECT raw_retention_seconds FROM retention_settings WHERE id=1").Scan(&rawRetention); err != nil {
		return metrics.Response{}, err
	}
	now = now.UTC()
	cutoff := now.Add(-time.Duration(rawRetention) * time.Second)
	if query.Resolution == metrics.ResolutionRaw && query.From.Before(cutoff) {
		return metrics.Response{}, metrics.ErrRawNotAvailable
	}
	definitions := []checkMetricDefinition{{checkID: checkID, key: "availability", unit: "ratio"}, {checkID: checkID, key: "latency_ms", unit: "milliseconds"}}
	if err := validateHourlyQueryWork(query, cutoff, len(definitions)); err != nil {
		return metrics.Response{}, err
	}
	response := metrics.Response{From: query.From.UTC(), To: query.To.UTC(), Series: make([]metrics.Series, 0, len(definitions))}
	for _, definition := range definitions {
		var points []metrics.Point
		switch query.Resolution {
		case metrics.ResolutionRaw:
			points, err = rawCheckPoints(ctx, tx, definition, query.From.UTC(), query.To.UTC())
		case metrics.ResolutionHourly:
			points, err = hourlyCheckPoints(ctx, tx, definition, query.From.UTC(), query.To.UTC(), cutoff, now)
		default:
			points, err = autoCheckPoints(ctx, tx, definition, query.From.UTC(), query.To.UTC(), cutoff, now)
		}
		if err != nil {
			return metrics.Response{}, err
		}
		if len(points) > metrics.MaxPointsPerSeries {
			return metrics.Response{}, metrics.ErrPointLimit
		}
		response.Series = append(response.Series, metrics.Series{Key: definition.key, Unit: definition.unit, Points: points})
	}
	response.Resolution = responseResolution(query, response.Series, cutoff)
	if err := tx.Commit(); err != nil {
		return metrics.Response{}, err
	}
	return response, nil
}

func rawCheckPoints(ctx context.Context, tx *sql.Tx, definition checkMetricDefinition, from, to time.Time) ([]metrics.Point, error) {
	query := `SELECT received_at,status,latency_ms FROM check_results WHERE check_id=? AND received_at>=? AND received_at<?`
	args := []any{definition.checkID, dbTime(from), dbTime(to)}
	if definition.key == "latency_ms" {
		query += " AND status='up' AND latency_ms IS NOT NULL"
	}
	query += " ORDER BY received_at,slot_id LIMIT ?"
	args = append(args, metrics.MaxPointsPerSeries+1)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	points := make([]metrics.Point, 0)
	for rows.Next() {
		var received, status string
		var latency sql.NullInt64
		if err := rows.Scan(&received, &status, &latency); err != nil {
			return nil, err
		}
		at, err := parseDBTime(received)
		if err != nil {
			return nil, err
		}
		value := float64(0)
		successful := int64(0)
		if status == "up" {
			value = 1
			successful = 1
		}
		if definition.key == "latency_ms" {
			value = float64(latency.Int64)
		}
		point := metrics.Point{Timestamp: at, Resolution: metrics.ResolutionRaw, Value: value, Min: value, Max: value, SampleCount: 1}
		if definition.key == "availability" {
			point.SuccessfulCount = &successful
		}
		points = append(points, point)
		if len(points) > metrics.MaxPointsPerSeries {
			return nil, metrics.ErrPointLimit
		}
	}
	return points, rows.Err()
}

func autoCheckPoints(ctx context.Context, tx *sql.Tx, definition checkMetricDefinition, from, to, cutoff, now time.Time) ([]metrics.Point, error) {
	if !from.Before(cutoff) {
		return rawCheckPoints(ctx, tx, definition, from, to)
	}
	if !to.After(cutoff) {
		return hourlyCheckPoints(ctx, tx, definition, from, to, cutoff, now)
	}
	splitAt := cutoff.Truncate(time.Hour)
	if !cutoff.Equal(splitAt) {
		splitAt = splitAt.Add(time.Hour)
	}
	if splitAt.After(to) {
		splitAt = to
	}
	old, err := hourlyCheckPoints(ctx, tx, definition, from, splitAt, cutoff, now)
	if err != nil {
		return nil, err
	}
	if !splitAt.Before(to) {
		return old, nil
	}
	recent, err := rawCheckPoints(ctx, tx, definition, splitAt, to)
	return append(old, recent...), err
}

func hourlyCheckPoints(ctx context.Context, tx *sql.Tx, definition checkMetricDefinition, from, to, cutoff, now time.Time) ([]metrics.Point, error) {
	if hourlyBucketCount(from, to) > int64(metrics.MaxPointsPerSeries) {
		return nil, metrics.ErrPointLimit
	}
	points := make([]metrics.Point, 0)
	for bucket := from.UTC().Truncate(time.Hour); bucket.Before(to); bucket = bucket.Add(time.Hour) {
		bucketEnd := bucket.Add(time.Hour)
		point, found, err := readCheckRollup(ctx, tx, definition, bucket)
		if err != nil {
			return nil, err
		}
		if !found && bucketEnd.After(cutoff) {
			point, found, err = aggregateCheckRaw(ctx, tx, definition, bucket, bucketEnd, now)
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

func readCheckRollup(ctx context.Context, tx *sql.Tx, definition checkMetricDefinition, bucket time.Time) (metrics.Point, bool, error) {
	var point metrics.Point
	var start string
	var successful, expected, covered sql.NullInt64
	var complete bool
	err := tx.QueryRowContext(ctx, `SELECT bucket_start,min_value,max_value,average_value,sample_count,successful_count,expected_sample_count,covered_slot_count,complete FROM check_hourly_rollups WHERE check_id=? AND metric_key=? AND bucket_start=?`, definition.checkID, definition.key, dbTime(bucket)).Scan(&start, &point.Min, &point.Max, &point.Value, &point.SampleCount, &successful, &expected, &covered, &complete)
	if errors.Is(err, sql.ErrNoRows) {
		return metrics.Point{}, false, nil
	}
	if err != nil {
		return metrics.Point{}, false, err
	}
	point.Timestamp, err = parseDBTime(start)
	if err != nil {
		return metrics.Point{}, false, err
	}
	point.Resolution = metrics.ResolutionHourly
	point.Complete = complete
	if successful.Valid {
		point.SuccessfulCount = &successful.Int64
	}
	if expected.Valid {
		point.ExpectedSampleCount = &expected.Int64
	}
	if covered.Valid {
		point.CoveredSlotCount = &covered.Int64
	}
	return point, true, nil
}

func aggregateCheckRaw(ctx context.Context, tx *sql.Tx, definition checkMetricDefinition, bucket, bucketEnd, now time.Time) (metrics.Point, bool, error) {
	var point metrics.Point
	var minValue, maxValue sql.NullFloat64
	var successful sql.NullInt64
	if definition.key == "availability" {
		if err := tx.QueryRowContext(ctx, `SELECT MIN(CASE WHEN status='up' THEN 1.0 ELSE 0.0 END),MAX(CASE WHEN status='up' THEN 1.0 ELSE 0.0 END),AVG(CASE WHEN status='up' THEN 1.0 ELSE 0.0 END),COUNT(*),SUM(CASE WHEN status='up' THEN 1 ELSE 0 END) FROM check_results WHERE check_id=? AND received_at>=? AND received_at<?`, definition.checkID, dbTime(bucket), dbTime(bucketEnd)).Scan(&minValue, &maxValue, &point.Value, &point.SampleCount, &successful); err != nil {
			return metrics.Point{}, false, err
		}
	} else {
		if err := tx.QueryRowContext(ctx, `SELECT MIN(latency_ms),MAX(latency_ms),AVG(latency_ms),COUNT(*) FROM check_results WHERE check_id=? AND status='up' AND latency_ms IS NOT NULL AND received_at>=? AND received_at<?`, definition.checkID, dbTime(bucket), dbTime(bucketEnd)).Scan(&minValue, &maxValue, &point.Value, &point.SampleCount); err != nil {
			return metrics.Point{}, false, err
		}
	}
	if point.SampleCount == 0 {
		return metrics.Point{}, false, nil
	}
	point.Timestamp = bucket
	point.Resolution = metrics.ResolutionHourly
	point.Min, point.Max = minValue.Float64, maxValue.Float64
	point.Complete = false
	if definition.key == "availability" {
		point.SuccessfulCount = &successful.Int64
		if err := fillCheckCoverage(ctx, tx, definition.checkID, bucket, bucketEnd, &point); err != nil {
			return metrics.Point{}, false, err
		}
		point.Complete = bucketEnd.Before(now) && point.ExpectedSampleCount != nil && point.CoveredSlotCount != nil && *point.CoveredSlotCount >= *point.ExpectedSampleCount
	}
	return point, true, nil
}

func fillCheckCoverage(ctx context.Context, tx *sql.Tx, checkID string, bucket, bucketEnd time.Time, point *metrics.Point) error {
	var expected, covered int64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM schedule_slots s JOIN schedule_periods p ON p.id=s.schedule_period_id WHERE s.target_type='check' AND s.target_id=? AND s.nominal_at>=? AND s.nominal_at<? AND s.nominal_at>=p.starts_at AND (p.ends_at IS NULL OR s.nominal_at<p.ends_at)`, checkID, dbTime(bucket), dbTime(bucketEnd)).Scan(&expected); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(DISTINCT r.slot_id) FROM check_results r JOIN schedule_slots s ON s.id=r.slot_id JOIN schedule_periods p ON p.id=s.schedule_period_id WHERE r.check_id=? AND s.nominal_at>=? AND s.nominal_at<? AND s.nominal_at>=p.starts_at AND (p.ends_at IS NULL OR s.nominal_at<p.ends_at)`, checkID, dbTime(bucket), dbTime(bucketEnd)).Scan(&covered); err != nil {
		return err
	}
	point.ExpectedSampleCount = &expected
	point.CoveredSlotCount = &covered
	return nil
}
