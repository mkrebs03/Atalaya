package database

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/mkrebs03/atalaya/internal/checks"
	"github.com/mkrebs03/atalaya/internal/metrics"
	settingsdomain "github.com/mkrebs03/atalaya/internal/settings"
)

func testHTTPCheck(name string) checks.Check {
	return checks.Check{Name: name, Config: checks.Config{Type: checks.TypeHTTP, HTTP: &checks.HTTPConfig{URL: "http://service.tailnet.ts.net/health", IntervalSeconds: 60, TimeoutSeconds: 10, ExpectedStatusMin: 200, ExpectedStatusMax: 299, MaxRedirects: 3}}}
}

func TestCheckRepositoryPersistsScheduleResultsAndManualRunsSeparately(t *testing.T) {
	database := openTestDatabase(t, filepath.Join(t.TempDir(), "atalaya.db"))
	ctx := context.Background()
	random := &collectionRandom{}
	created, err := database.Create(ctx, testHTTPCheck("API health"), testInstant, random)
	if err != nil {
		t.Fatal(err)
	}
	if created.Version != 1 || created.ObservedState != checks.StateUnknown || !created.Enabled {
		t.Fatalf("created check=%+v", created)
	}
	items, err := database.List(ctx, 10, nil, false)
	if err != nil || len(items) != 1 || items[0].Name != "API health" {
		t.Fatalf("list items=%+v err=%v", items, err)
	}

	claim, err := database.ClaimScheduled(ctx, testInstant)
	if err != nil {
		t.Fatal(err)
	}
	latency := int64(24)
	if err := database.FinishScheduled(ctx, claim, checks.Result{Status: checks.StateUp, LatencyMS: &latency, ReceivedAt: testInstant.Add(time.Second)}, random); err != nil {
		t.Fatal(err)
	}
	current, err := database.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.ObservedState != checks.StateUp || current.LastLatencyMS == nil || *current.LastLatencyMS != latency || current.LastResultAt == nil {
		t.Fatalf("current after scheduled result=%+v", current)
	}
	var resultCount int
	if err := database.sql.QueryRow("SELECT count(*) FROM check_results WHERE check_id=?", created.ID).Scan(&resultCount); err != nil || resultCount != 1 {
		t.Fatalf("scheduled result count=%d err=%v", resultCount, err)
	}
	if err := database.RunRetention(ctx, testInstant.Add(2*time.Hour)); err != nil {
		t.Fatalf("check rollup retention: %v", err)
	}
	hourly, err := database.queryCheckMetricsAt(ctx, created.ID, metrics.Query{From: testInstant.Truncate(time.Hour), To: testInstant.Truncate(time.Hour).Add(time.Hour), Resolution: metrics.ResolutionHourly}, testInstant.Add(3*time.Hour))
	if err != nil {
		t.Fatalf("hourly check metrics: %v", err)
	}
	if len(hourly.Series) != 2 || len(hourly.Series[0].Points) != 1 || hourly.Series[0].Points[0].Value != 1 || hourly.Series[0].Points[0].SuccessfulCount == nil || *hourly.Series[0].Points[0].SuccessfulCount != 1 {
		t.Fatalf("hourly check availability=%+v", hourly)
	}
	if len(hourly.Series[1].Points) != 1 || hourly.Series[1].Points[0].Value != float64(latency) || hourly.Series[1].Points[0].ExpectedSampleCount != nil {
		t.Fatalf("hourly check latency=%+v", hourly.Series[1])
	}

	run, replay, err := database.EnqueueRun(ctx, created.ID, "diagnostic-1", testInstant.Add(2*time.Second), random)
	if err != nil || replay || run.State != "queued" {
		t.Fatalf("enqueue run=%+v replay=%v err=%v", run, replay, err)
	}
	replayed, replay, err := database.EnqueueRun(ctx, created.ID, "diagnostic-1", testInstant.Add(3*time.Second), random)
	if err != nil || !replay || replayed.ID != run.ID {
		t.Fatalf("replay=%+v replayed=%v err=%v", replayed, replay, err)
	}
	manualClaim, err := database.ClaimManualRun(ctx, testInstant.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	downCode := checks.ErrorTimeout
	if err := database.FinishManualRun(ctx, manualClaim, checks.Result{Status: checks.StateDown, ErrorCode: &downCode, ReceivedAt: testInstant.Add(4 * time.Second)}, "", testInstant.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	completed, err := database.GetRun(ctx, created.ID, run.ID)
	if err != nil || completed.State != "completed" || completed.Result == nil || completed.Result.Status != checks.StateDown {
		t.Fatalf("completed run=%+v err=%v", completed, err)
	}
	if _, err := database.queryCheckMetricsAt(ctx, created.ID, metrics.Query{From: testInstant, To: testInstant.Add(time.Minute), Resolution: metrics.ResolutionRaw}, testInstant.Add(time.Hour)); err != nil {
		t.Fatalf("check metrics: %v", err)
	}
	if _, err := database.GetRun(ctx, created.ID, "missing"); !errors.Is(err, checks.ErrRunNotFound) {
		t.Fatalf("missing run error=%v", err)
	}
}

func TestCheckSettingsUseSharedVersionAndValidateNetworkPolicy(t *testing.T) {
	database := openTestDatabase(t, filepath.Join(t.TempDir(), "atalaya.db"))
	ctx := context.Background()
	settings, err := database.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cidr := []string{"192.168.50.0/24"}
	interval, timeout := 120, 15
	updated, err := database.UpdateSettings(ctx, settingsdomain.Patch{CheckIntervalSeconds: &interval, CheckTimeoutSeconds: &timeout, AllowedPrivateCIDRs: &cidr}, settings.Version, testInstant.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != settings.Version+1 || updated.CheckIntervalSeconds != interval || updated.AllowedPrivateCIDRs[0] != cidr[0] {
		t.Fatalf("updated settings=%+v", updated)
	}
	if _, err := database.UpdateSettings(ctx, settingsdomain.Patch{CheckTimeoutSeconds: &interval}, updated.Version, testInstant.Add(2*time.Minute)); !errors.Is(err, settingsdomain.ErrInvalid) {
		t.Fatalf("invalid timeout error=%v", err)
	}
	if _, err := database.UpdateSettings(ctx, settingsdomain.Patch{AllowedPrivateCIDRs: &[]string{"172.16.0.0/11"}}, updated.Version, testInstant.Add(2*time.Minute)); !errors.Is(err, settingsdomain.ErrInvalid) {
		t.Fatalf("invalid cidr error=%v", err)
	}
}

func TestCheckAdministrativeTransitionsResetObservedState(t *testing.T) {
	database := openTestDatabase(t, filepath.Join(t.TempDir(), "atalaya.db"))
	ctx := context.Background()
	random := &collectionRandom{}
	created, err := database.Create(ctx, testHTTPCheck("API health"), testInstant, random)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := database.ClaimScheduled(ctx, testInstant)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.FinishScheduled(ctx, claim, checks.Result{Status: checks.StateUp, ReceivedAt: testInstant.Add(time.Second)}, random); err != nil {
		t.Fatal(err)
	}
	current, err := database.Get(ctx, created.ID)
	if err != nil || current.ObservedState != checks.StateUp {
		t.Fatalf("observed state after success=%+v err=%v", current, err)
	}

	if err := database.SetEnabled(ctx, created.ID, current.Version, false, testInstant.Add(2*time.Second), random); err != nil {
		t.Fatal(err)
	}
	current, err = database.Get(ctx, created.ID)
	if err != nil || current.ObservedState != checks.StateUnknown {
		t.Fatalf("observed state after disable=%+v err=%v", current, err)
	}
	if err := database.SetEnabled(ctx, created.ID, current.Version, true, testInstant.Add(3*time.Second), random); err != nil {
		t.Fatal(err)
	}
	claim, err = database.ClaimScheduled(ctx, testInstant.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.FinishScheduled(ctx, claim, checks.Result{Status: checks.StateUp, ReceivedAt: testInstant.Add(4 * time.Second)}, random); err != nil {
		t.Fatal(err)
	}
	current, err = database.Get(ctx, created.ID)
	if err != nil || current.ObservedState != checks.StateUp {
		t.Fatalf("observed state after re-enable=%+v err=%v", current, err)
	}

	if err := database.Archive(ctx, created.ID, current.Version, testInstant.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	current, err = database.Get(ctx, created.ID)
	if err != nil || current.ObservedState != checks.StateUnknown {
		t.Fatalf("observed state after archive=%+v err=%v", current, err)
	}
	if err := database.Restore(ctx, created.ID, current.Version, testInstant.Add(6*time.Second)); err != nil {
		t.Fatal(err)
	}
	current, err = database.Get(ctx, created.ID)
	if err != nil || current.ObservedState != checks.StateUnknown {
		t.Fatalf("observed state after restore=%+v err=%v", current, err)
	}
}

func TestManualRunDoesNotExceedTwoProcessAttemptsAfterRelease(t *testing.T) {
	database := openTestDatabase(t, filepath.Join(t.TempDir(), "atalaya.db"))
	ctx := context.Background()
	random := &collectionRandom{}
	created, err := database.Create(ctx, testHTTPCheck("API health"), testInstant, random)
	if err != nil {
		t.Fatal(err)
	}
	run, _, err := database.EnqueueRun(ctx, created.ID, "retry-limit", testInstant, random)
	if err != nil {
		t.Fatal(err)
	}
	first, err := database.ClaimManualRun(ctx, testInstant)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.ReleaseManualRun(ctx, first, testInstant.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	second, err := database.ClaimManualRun(ctx, testInstant.Add(time.Second))
	if err != nil || second.Attempt != 2 {
		t.Fatalf("second manual claim=%+v err=%v", second, err)
	}
	if err := database.ReleaseManualRun(ctx, second, testInstant.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ClaimManualRun(ctx, testInstant.Add(2*time.Second)); !errors.Is(err, checks.ErrNoDueSlot) {
		t.Fatalf("claim after exhausted release error=%v", err)
	}
	finished, err := database.GetRun(ctx, created.ID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.State != "failed" || finished.FailureCode != "retry_exhausted" || finished.ProcessAttempts != 2 {
		t.Fatalf("exhausted manual run=%+v", finished)
	}
}

func TestManualRunLeaseFencesStaleWorker(t *testing.T) {
	database := openTestDatabase(t, filepath.Join(t.TempDir(), "atalaya.db"))
	ctx := context.Background()
	random := &collectionRandom{}
	created, err := database.Create(ctx, testHTTPCheck("API health"), testInstant, random)
	if err != nil {
		t.Fatal(err)
	}
	run, _, err := database.EnqueueRun(ctx, created.ID, "stale-worker", testInstant, random)
	if err != nil {
		t.Fatal(err)
	}
	first, err := database.ClaimManualRun(ctx, testInstant)
	if err != nil {
		t.Fatal(err)
	}
	retryAt := first.LeaseUntil.Add(time.Second)
	second, err := database.ClaimManualRun(ctx, retryAt)
	if err != nil || second.Attempt != 2 {
		t.Fatalf("reclaimed manual run=%+v err=%v", second, err)
	}

	if err := database.FinishManualRun(ctx, first, checks.Result{Status: checks.StateDown}, "", retryAt); err != nil {
		t.Fatal(err)
	}
	if err := database.ReleaseManualRun(ctx, first, retryAt); err != nil {
		t.Fatal(err)
	}
	running, err := database.GetRun(ctx, created.ID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if running.State != "running" || running.ProcessAttempts != 2 {
		t.Fatalf("stale worker changed current manual attempt=%+v", running)
	}
	if err := database.FinishManualRun(ctx, second, checks.Result{Status: checks.StateUp}, "", retryAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	finished, err := database.GetRun(ctx, created.ID, run.ID)
	if err != nil || finished.State != "completed" || finished.Result == nil || finished.Result.Status != checks.StateUp {
		t.Fatalf("current manual attempt=%+v err=%v", finished, err)
	}
}

func TestScheduledClaimLeaseFencesStaleWorker(t *testing.T) {
	database := openTestDatabase(t, filepath.Join(t.TempDir(), "atalaya.db"))
	ctx := context.Background()
	random := &collectionRandom{}
	created, err := database.Create(ctx, testHTTPCheck("API health"), testInstant, random)
	if err != nil {
		t.Fatal(err)
	}
	first, err := database.ClaimScheduled(ctx, testInstant)
	if err != nil {
		t.Fatal(err)
	}
	retryAt := testInstant.Add(time.Second)
	if err := database.ReleaseScheduled(ctx, first, retryAt); err != nil {
		t.Fatal(err)
	}
	second, err := database.ClaimScheduled(ctx, retryAt)
	if err != nil || second.Attempt != 2 {
		t.Fatalf("reclaimed scheduled slot=%+v err=%v", second, err)
	}
	if err := database.ReleaseScheduled(ctx, first, retryAt); err != nil {
		t.Fatal(err)
	}
	var state string
	var attempts int
	if err := database.sql.QueryRow("SELECT state,process_attempts FROM schedule_slots WHERE id=?", first.SlotID).Scan(&state, &attempts); err != nil {
		t.Fatal(err)
	}
	if state != "running" || attempts != 2 {
		t.Fatalf("stale release changed scheduled slot state=%s attempts=%d", state, attempts)
	}
	if err := database.FinishScheduled(ctx, first, checks.Result{Status: checks.StateDown, ReceivedAt: retryAt}, random); err != nil {
		t.Fatal(err)
	}
	if err := database.sql.QueryRow("SELECT state FROM schedule_slots WHERE id=?", first.SlotID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "running" {
		t.Fatalf("stale finish changed scheduled slot state=%s", state)
	}
	if err := database.FinishScheduled(ctx, second, checks.Result{Status: checks.StateUp, ReceivedAt: retryAt.Add(time.Second)}, random); err != nil {
		t.Fatal(err)
	}
	current, err := database.Get(ctx, created.ID)
	if err != nil || current.ObservedState != checks.StateUp {
		t.Fatalf("current scheduled result=%+v err=%v", current, err)
	}
}
