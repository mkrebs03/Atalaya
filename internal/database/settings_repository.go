package database

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mkrebs03/atalaya/internal/checks"
	"github.com/mkrebs03/atalaya/internal/metrics"
	settingsdomain "github.com/mkrebs03/atalaya/internal/settings"
)

var _ settingsdomain.Repository = (*Database)(nil)

func (database *Database) GetSettings(ctx context.Context) (settingsdomain.Settings, error) {
	var result settingsdomain.Settings
	var updated, cidrs string
	if err := database.sql.QueryRowContext(ctx, `SELECT raw_retention_seconds,rollup_retention_seconds,updated_at,version FROM retention_settings WHERE id=1`).Scan(&result.RawRetentionSeconds, &result.RollupRetentionSeconds, &updated, &result.Version); err != nil {
		return result, err
	}
	if err := database.sql.QueryRowContext(ctx, `SELECT interval_seconds,timeout_seconds,allowed_private_cidrs FROM check_settings WHERE id=1`).Scan(&result.CheckIntervalSeconds, &result.CheckTimeoutSeconds, &cidrs); err != nil {
		return result, err
	}
	if err := json.Unmarshal([]byte(cidrs), &result.AllowedPrivateCIDRs); err != nil {
		return result, err
	}
	result.UpdatedAt, _ = parseDBTime(updated)
	return result, nil
}

func (database *Database) UpdateSettings(ctx context.Context, patch settingsdomain.Patch, version int64, now time.Time) (settingsdomain.Settings, error) {
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return settingsdomain.Settings{}, err
	}
	defer tx.Rollback()

	var result settingsdomain.Settings
	var updated, cidrs string
	if err := tx.QueryRowContext(ctx, `SELECT raw_retention_seconds,rollup_retention_seconds,updated_at,version FROM retention_settings WHERE id=1`).Scan(&result.RawRetentionSeconds, &result.RollupRetentionSeconds, &updated, &result.Version); err != nil {
		return settingsdomain.Settings{}, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT interval_seconds,timeout_seconds,allowed_private_cidrs FROM check_settings WHERE id=1`).Scan(&result.CheckIntervalSeconds, &result.CheckTimeoutSeconds, &cidrs); err != nil {
		return settingsdomain.Settings{}, err
	}
	if err := json.Unmarshal([]byte(cidrs), &result.AllowedPrivateCIDRs); err != nil {
		return settingsdomain.Settings{}, err
	}
	if result.Version != version {
		return settingsdomain.Settings{}, settingsdomain.ErrVersionConflict
	}

	if patch.RawRetentionSeconds != nil {
		result.RawRetentionSeconds = *patch.RawRetentionSeconds
	}
	if patch.RollupRetentionSeconds != nil {
		result.RollupRetentionSeconds = *patch.RollupRetentionSeconds
	}
	if patch.CheckIntervalSeconds != nil {
		result.CheckIntervalSeconds = *patch.CheckIntervalSeconds
	}
	if patch.CheckTimeoutSeconds != nil {
		result.CheckTimeoutSeconds = *patch.CheckTimeoutSeconds
	}
	if patch.AllowedPrivateCIDRs != nil {
		result.AllowedPrivateCIDRs = append([]string(nil), (*patch.AllowedPrivateCIDRs)...)
	}
	currentRetention := metrics.RetentionSettings{RawRetentionSeconds: result.RawRetentionSeconds, RollupRetentionSeconds: result.RollupRetentionSeconds, Version: result.Version}
	if err := metrics.ValidateSettings(metrics.SettingsPatch{RawRetentionSeconds: &result.RawRetentionSeconds, RollupRetentionSeconds: &result.RollupRetentionSeconds}, currentRetention); err != nil {
		return settingsdomain.Settings{}, settingsdomain.ErrInvalid
	}
	if result.CheckIntervalSeconds < checks.MinIntervalSeconds || result.CheckIntervalSeconds > checks.MaxIntervalSeconds || result.CheckTimeoutSeconds < checks.MinTimeoutSeconds || result.CheckTimeoutSeconds > checks.MaxTimeoutSeconds || result.CheckTimeoutSeconds >= result.CheckIntervalSeconds {
		return settingsdomain.Settings{}, settingsdomain.ErrInvalid
	}
	policy, err := checks.NewNetworkPolicy(result.AllowedPrivateCIDRs)
	if err != nil {
		return settingsdomain.Settings{}, settingsdomain.ErrInvalid
	}
	result.AllowedPrivateCIDRs = result.AllowedPrivateCIDRs[:0]
	for _, prefix := range policy.PrivateCIDRs {
		result.AllowedPrivateCIDRs = append(result.AllowedPrivateCIDRs, prefix.String())
	}
	encoded, err := json.Marshal(result.AllowedPrivateCIDRs)
	if err != nil {
		return settingsdomain.Settings{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE retention_settings SET raw_retention_seconds=?,rollup_retention_seconds=?,updated_at=?,version=version+1 WHERE id=1 AND version=?`, result.RawRetentionSeconds, result.RollupRetentionSeconds, dbTime(now), version); err != nil {
		return settingsdomain.Settings{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE check_settings SET interval_seconds=?,timeout_seconds=?,allowed_private_cidrs=?,updated_at=? WHERE id=1`, result.CheckIntervalSeconds, result.CheckTimeoutSeconds, string(encoded), dbTime(now)); err != nil {
		return settingsdomain.Settings{}, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT version FROM retention_settings WHERE id=1`).Scan(&result.Version); err != nil {
		return settingsdomain.Settings{}, err
	}
	result.UpdatedAt = now.UTC()
	if err := tx.Commit(); err != nil {
		return settingsdomain.Settings{}, err
	}
	return result, nil
}
