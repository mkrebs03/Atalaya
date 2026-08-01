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
