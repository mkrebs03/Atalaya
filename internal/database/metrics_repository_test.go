package database

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/mkrebs03/atalaya/internal/agent"
	"github.com/mkrebs03/atalaya/internal/collection"
	"github.com/mkrebs03/atalaya/internal/metrics"
)

func TestHourlyMetricQueryWorkIsBoundedBeforeBucketIteration(t *testing.T) {
	cutoff := testInstant.Add(-48 * time.Hour)
	query := metrics.Query{
		From:       testInstant.Add(-365 * 24 * time.Hour),
		To:         testInstant,
		Resolution: metrics.ResolutionAuto,
	}
	if err := validateHourlyQueryWork(query, cutoff, 3); !errors.Is(err, metrics.ErrPointLimit) {
		t.Fatalf("auto query error = %v, want point limit", err)
	}

	query.Resolution = metrics.ResolutionHourly
	if err := validateHourlyQueryWork(query, cutoff, 3); !errors.Is(err, metrics.ErrPointLimit) {
		t.Fatalf("hourly query error = %v, want point limit", err)
	}
}

func TestMetricsPersistRawAndRollupIdempotently(t *testing.T) {
	database := openTestDatabase(t, filepath.Join(t.TempDir(), "atalaya.db"))
	defer database.Close()
	ctx := context.Background()
	seed := bytes.NewReader(bytes.Repeat([]byte{0x21}, 512))
	host, err := database.PrepareLocalHost(ctx, "Oracle", "https://oracle.example.ts.net:9474", testInstant, seed)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(host.Credential)
	if err := database.CompleteLocalHost(ctx, host.ID, testInstant, seed); err != nil {
		t.Fatal(err)
	}
	claimTime := testInstant.Add(5 * time.Second)
	claim, err := database.ClaimDue(ctx, claimTime)
	if err != nil {
		t.Fatal(err)
	}
	cpu := 12.5
	used, total := uint64(100), uint64(200)
	uuid := "deadbeef"
	snapshot := agent.Snapshot{
		ProtocolVersion: 1,
		AgentVersion:    "dev",
		Host:            agent.Host{Hostname: "oracle", ObservedAt: testInstant.Format(time.RFC3339Nano), CPUUsedPercent: &cpu, MemoryUsedBytes: &used, MemoryTotalBytes: &total},
		Tailscale:       agent.Tailscale{State: "connected", Addresses: []string{}},
		Filesystems:     []agent.Filesystem{{MountPoint: "/", MountRoot: "/", Device: "/dev/sda1", MajorMinor: "8:1", FSType: "ext4", FSUUID: &uuid, UsedBytes: 100, TotalBytes: 200, AvailableBytes: 100}},
	}
	if err := database.Finish(ctx, claim, collection.Result{Snapshot: &snapshot, ReceivedAt: claimTime}, &collectionRandom{}); err != nil {
		t.Fatal(err)
	}
	result, err := database.queryHostMetricsAt(ctx, host.ID, metrics.Query{From: claimTime, To: claimTime.Add(time.Hour), Resolution: metrics.ResolutionRaw}, claimTime.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if result.Resolution != metrics.ResolutionRaw || len(result.Series) != 5 {
		t.Fatalf("raw response = %+v", result)
	}
	if len(result.Series[0].Points) != 1 || result.Series[0].Points[0].Value != cpu {
		t.Fatalf("CPU raw series = %+v", result.Series[0])
	}
	if err := database.RunRetention(ctx, claimTime.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := database.RunRetention(ctx, claimTime.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var rollups int
	if err := database.sql.QueryRow(`SELECT count(*) FROM hourly_rollups WHERE host_id=?`, host.ID).Scan(&rollups); err != nil {
		t.Fatal(err)
	}
	if rollups != 5 {
		t.Fatalf("rollup count = %d, want 5", rollups)
	}
	hourly, err := database.queryHostMetricsAt(ctx, host.ID, metrics.Query{From: claimTime, To: claimTime.Add(time.Hour), Resolution: metrics.ResolutionHourly}, claimTime.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(hourly.Series[0].Points) != 1 || hourly.Series[0].Points[0].Resolution != metrics.ResolutionHourly {
		t.Fatalf("hourly CPU series = %+v", hourly.Series[0])
	}
	settings, err := database.GetRetentionSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rawRetention := int64(metrics.MinRawRetention / time.Second)
	rollupRetention := int64(metrics.MinRollupRetention / time.Second)
	if _, err := database.UpdateRetentionSettings(ctx, metrics.SettingsPatch{RawRetentionSeconds: &rawRetention, RollupRetentionSeconds: &rollupRetention}, settings.Version, claimTime.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := database.RunRetention(ctx, claimTime.Add(8*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := database.sql.QueryRow(`SELECT count(*) FROM hourly_rollups WHERE host_id=?`, host.ID).Scan(&rollups); err != nil {
		t.Fatal(err)
	}
	var samples int
	if err := database.sql.QueryRow(`SELECT count(*) FROM metric_samples WHERE host_id=?`, host.ID).Scan(&samples); err != nil {
		t.Fatal(err)
	}
	if rollups != 0 || samples != 0 {
		t.Fatalf("expired metrics remain: rollups=%d samples=%d", rollups, samples)
	}
}

func TestAutoMetricsDoesNotDuplicateCutoffBucket(t *testing.T) {
	database := openTestDatabase(t, filepath.Join(t.TempDir(), "atalaya.db"))
	ctx := context.Background()
	seed := bytes.NewReader(bytes.Repeat([]byte{0x22}, 512))
	host, err := database.PrepareLocalHost(ctx, "Oracle", "https://oracle.example.ts.net:9474", testInstant, seed)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(host.Credential)
	if err := database.CompleteLocalHost(ctx, host.ID, testInstant, seed); err != nil {
		t.Fatal(err)
	}
	var periodID, firstSlotID string
	if err := database.sql.QueryRow(`SELECT schedule_period_id,id FROM schedule_slots WHERE target_id=? AND state='scheduled'`, host.ID).Scan(&periodID, &firstSlotID); err != nil {
		t.Fatal(err)
	}
	firstSampleAt := testInstant
	secondSampleAt := firstSampleAt.Add(time.Minute)
	if _, err := database.sql.Exec(`UPDATE schedule_slots SET state='succeeded',process_attempts=1,finished_at=? WHERE id=?`, dbTime(firstSampleAt), firstSlotID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.sql.Exec(`INSERT INTO schedule_slots(id,target_type,target_id,schedule_period_id,nominal_at,not_before_at,deadline_at,effective_interval_seconds,effective_timeout_seconds,state,process_attempts,finished_at) VALUES('cutoff-slot-2','host',?,?,?,?,?,?,?,'succeeded',1,?)`, host.ID, periodID, dbTime(secondSampleAt), dbTime(secondSampleAt), dbTime(secondSampleAt.Add(15*time.Second)), 60, 10, dbTime(secondSampleAt)); err != nil {
		t.Fatal(err)
	}
	for _, sample := range []struct {
		slotID     string
		receivedAt time.Time
		value      float64
	}{{firstSlotID, firstSampleAt, 10}, {"cutoff-slot-2", secondSampleAt, 20}} {
		if _, err := database.sql.Exec(`INSERT INTO metric_samples(host_id,scope_id,metric_key,slot_id,received_at,value_real) VALUES(?,?,?,?,?,?)`, host.ID, host.ID, "cpu_used_percent", sample.slotID, dbTime(sample.receivedAt), sample.value); err != nil {
			t.Fatal(err)
		}
	}
	now := firstSampleAt.Add(48*time.Hour + 30*time.Second)
	if err := database.RunRetention(ctx, now); err != nil {
		t.Fatal(err)
	}
	result, err := database.queryHostMetricsAt(ctx, host.ID, metrics.Query{From: firstSampleAt.Add(-time.Hour), To: now, Resolution: metrics.ResolutionAuto}, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.Resolution != metrics.ResolutionHourly || len(result.Series[0].Points) != 1 || result.Series[0].Points[0].Resolution != metrics.ResolutionHourly {
		t.Fatalf("cutoff response = %+v", result)
	}
}

func TestRetentionBackfillsAfterRetentionChange(t *testing.T) {
	database := openTestDatabase(t, filepath.Join(t.TempDir(), "atalaya.db"))
	ctx := context.Background()
	seed := bytes.NewReader(bytes.Repeat([]byte{0x23}, 512))
	host, err := database.PrepareLocalHost(ctx, "Oracle", "https://oracle.example.ts.net:9474", testInstant, seed)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(host.Credential)
	if err := database.CompleteLocalHost(ctx, host.ID, testInstant, seed); err != nil {
		t.Fatal(err)
	}
	oldSampleAt := testInstant.Add(-8 * 24 * time.Hour)
	if _, err := database.sql.Exec(`INSERT INTO schedule_periods(id,target_type,target_id,starts_at,ends_at,effective_interval_seconds,effective_timeout_seconds) VALUES('historical-period','host',?,?,?,60,10)`, host.ID, dbTime(oldSampleAt), dbTime(oldSampleAt.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	if _, err := database.sql.Exec(`INSERT INTO schedule_slots(id,target_type,target_id,schedule_period_id,nominal_at,not_before_at,deadline_at,effective_interval_seconds,effective_timeout_seconds,state,process_attempts,finished_at) VALUES('historical-slot','host',?,?,?,?,?,?,?,'succeeded',1,?)`, host.ID, "historical-period", dbTime(oldSampleAt), dbTime(oldSampleAt), dbTime(oldSampleAt.Add(15*time.Second)), 60, 10, dbTime(oldSampleAt.Add(time.Second))); err != nil {
		t.Fatal(err)
	}
	if _, err := database.sql.Exec(`INSERT INTO metric_samples(host_id,scope_id,metric_key,slot_id,received_at,value_real) VALUES(?,?,?,?,?,?)`, host.ID, host.ID, "cpu_used_percent", "historical-slot", dbTime(oldSampleAt.Add(10*time.Second)), 42.0); err != nil {
		t.Fatal(err)
	}
	settings, err := database.GetRetentionSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sevenDays := int64((7 * 24 * time.Hour) / time.Second)
	if _, err := database.UpdateRetentionSettings(ctx, metrics.SettingsPatch{RawRetentionSeconds: &sevenDays, RollupRetentionSeconds: &sevenDays}, settings.Version, testInstant); err != nil {
		t.Fatal(err)
	}
	if err := database.RunRetention(ctx, testInstant); err != nil {
		t.Fatal(err)
	}
	var rollups int
	if err := database.sql.QueryRow(`SELECT count(*) FROM hourly_rollups WHERE host_id=?`, host.ID).Scan(&rollups); err != nil {
		t.Fatal(err)
	}
	if rollups != 0 {
		t.Fatalf("initial rollups = %d, want 0", rollups)
	}
	settings, err = database.GetRetentionSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	thirtyDays := int64((30 * 24 * time.Hour) / time.Second)
	if _, err := database.UpdateRetentionSettings(ctx, metrics.SettingsPatch{RawRetentionSeconds: &thirtyDays, RollupRetentionSeconds: &thirtyDays}, settings.Version, testInstant.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := database.RunRetention(ctx, testInstant.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := database.sql.QueryRow(`SELECT count(*) FROM hourly_rollups WHERE host_id=?`, host.ID).Scan(&rollups); err != nil {
		t.Fatal(err)
	}
	if rollups != 1 {
		t.Fatalf("backfilled rollups = %d, want 1", rollups)
	}
}

func TestMigration006AddsRetentionWatermarksToExistingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "atalaya.db")
	database, err := open(context.Background(), path, testMasterKey(), openOptions{
		clock:      func() time.Time { return testInstant },
		random:     bytes.NewReader(bytes.Repeat([]byte{0x24}, 512)),
		migrations: embeddedMigrations[:5],
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = Open(context.Background(), path, testMasterKey())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	var raw, rollup, expectedRaw, expectedRollup int64
	if err := database.sql.QueryRow(`SELECT p.last_raw_retention_seconds,p.last_rollup_retention_seconds,s.raw_retention_seconds,s.rollup_retention_seconds FROM retention_progress p JOIN retention_settings s ON s.id=1 WHERE p.id=1`).Scan(&raw, &rollup, &expectedRaw, &expectedRollup); err != nil {
		t.Fatal(err)
	}
	if raw != expectedRaw || rollup != expectedRollup {
		t.Fatalf("retention watermarks = (%d, %d), settings = (%d, %d)", raw, rollup, expectedRaw, expectedRollup)
	}
}
