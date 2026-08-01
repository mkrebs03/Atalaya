// Package settings defines the singleton settings resource shared by the API
// and the background workers.
package settings

import (
	"context"
	"errors"
	"time"
)

var (
	ErrVersionConflict = errors.New("settings version conflict")
	ErrInvalid         = errors.New("invalid settings")
)

type Settings struct {
	RawRetentionSeconds    int64
	RollupRetentionSeconds int64
	CheckIntervalSeconds   int
	CheckTimeoutSeconds    int
	AllowedPrivateCIDRs    []string
	UpdatedAt              time.Time
	Version                int64
}

type Patch struct {
	RawRetentionSeconds    *int64    `json:"raw_retention_seconds"`
	RollupRetentionSeconds *int64    `json:"rollup_retention_seconds"`
	CheckIntervalSeconds   *int      `json:"check_interval_seconds"`
	CheckTimeoutSeconds    *int      `json:"check_timeout_seconds"`
	AllowedPrivateCIDRs    *[]string `json:"allowed_private_cidrs"`
}

type Repository interface {
	GetSettings(context.Context) (Settings, error)
	UpdateSettings(context.Context, Patch, int64, time.Time) (Settings, error)
}
