// Package metrics defines the historical-series contract shared by SQLite and HTTP.
package metrics

import (
	"context"
	"errors"
	"time"
)

const (
	DefaultRawRetention    = 48 * time.Hour
	DefaultRollupRetention = 90 * 24 * time.Hour
	MinRawRetention        = 24 * time.Hour
	MaxRawRetention        = 30 * 24 * time.Hour
	MinRollupRetention     = 7 * 24 * time.Hour
	MaxRollupRetention     = 730 * 24 * time.Hour
	MaxPointsPerSeries     = 10000
	// MaxHourlyQueryWork bounds the estimated number of SQLite lookups a
	// historical query may perform before it starts walking hourly buckets.
	// The UI's largest supported range remains comfortably below this cap,
	// while unbounded API requests fail before turning into an N+1 query storm.
	MaxHourlyQueryWork int64 = 100000
)

var (
	ErrNotFound         = errors.New("host not found")
	ErrRawNotAvailable  = errors.New("raw metrics are not available for the requested range")
	ErrPointLimit       = errors.New("metric point limit exceeded")
	ErrSettingsConflict = errors.New("retention settings version conflict")
)

// ValidationError marks an error caused by user-supplied settings. Repository
// failures must not be presented as validation details by the HTTP layer.
type ValidationError struct {
	Message string
}

func (err *ValidationError) Error() string {
	return err.Message
}

type Resolution string

const (
	ResolutionAuto   Resolution = "auto"
	ResolutionRaw    Resolution = "raw"
	ResolutionHourly Resolution = "hourly"
	ResolutionMixed  Resolution = "mixed"
)

type Query struct {
	From       time.Time
	To         time.Time
	Resolution Resolution
}

type Point struct {
	Timestamp           time.Time  `json:"timestamp"`
	Resolution          Resolution `json:"resolution"`
	Value               float64    `json:"value"`
	Min                 float64    `json:"min"`
	Max                 float64    `json:"max"`
	SampleCount         int64      `json:"sample_count"`
	CoveredSlotCount    *int64     `json:"covered_slot_count,omitempty"`
	ExpectedSampleCount *int64     `json:"expected_sample_count,omitempty"`
	Complete            bool       `json:"complete"`
}

type Series struct {
	Key          string  `json:"key"`
	Unit         string  `json:"unit"`
	FilesystemID *string `json:"filesystem_id,omitempty"`
	MountPoint   *string `json:"mount_point,omitempty"`
	Points       []Point `json:"points"`
}

type Response struct {
	From       time.Time  `json:"from"`
	To         time.Time  `json:"to"`
	Resolution Resolution `json:"resolution"`
	Series     []Series   `json:"series"`
}

type RetentionSettings struct {
	RawRetentionSeconds    int64     `json:"raw_retention_seconds"`
	RollupRetentionSeconds int64     `json:"rollup_retention_seconds"`
	UpdatedAt              time.Time `json:"updated_at"`
	Version                int64     `json:"version"`
}

type SettingsPatch struct {
	RawRetentionSeconds    *int64 `json:"raw_retention_seconds"`
	RollupRetentionSeconds *int64 `json:"rollup_retention_seconds"`
}

type Repository interface {
	QueryHostMetrics(context.Context, string, Query) (Response, error)
	GetRetentionSettings(context.Context) (RetentionSettings, error)
	UpdateRetentionSettings(context.Context, SettingsPatch, int64, time.Time) (RetentionSettings, error)
}

type RetentionRepository interface {
	RunRetention(context.Context, time.Time) error
}

func ValidateQuery(query Query) error {
	if query.From.IsZero() || query.To.IsZero() || !query.From.Before(query.To) {
		return errors.New("metrics query must have a non-empty time range")
	}
	switch query.Resolution {
	case ResolutionAuto, ResolutionRaw, ResolutionHourly:
		return nil
	default:
		return errors.New("unsupported metrics resolution")
	}
}

func ValidateSettings(patch SettingsPatch, current RetentionSettings) error {
	raw := current.RawRetentionSeconds
	rollup := current.RollupRetentionSeconds
	if patch.RawRetentionSeconds != nil {
		raw = *patch.RawRetentionSeconds
	}
	if patch.RollupRetentionSeconds != nil {
		rollup = *patch.RollupRetentionSeconds
	}
	if raw < int64(MinRawRetention/time.Second) || raw > int64(MaxRawRetention/time.Second) {
		return &ValidationError{Message: "raw retention is outside the supported range"}
	}
	if rollup < int64(MinRollupRetention/time.Second) || rollup > int64(MaxRollupRetention/time.Second) {
		return &ValidationError{Message: "rollup retention is outside the supported range"}
	}
	if rollup < raw {
		return &ValidationError{Message: "rollup retention cannot be shorter than raw retention"}
	}
	return nil
}
