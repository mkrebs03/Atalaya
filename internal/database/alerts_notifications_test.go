package database

import (
	"bytes"
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mkrebs03/atalaya/internal/alerts"
	"github.com/mkrebs03/atalaya/internal/checks"
	"github.com/mkrebs03/atalaya/internal/collection"
	"github.com/mkrebs03/atalaya/internal/notifications"
)

const testDiscordWebhook = "https://discord.com/api/webhooks/123456789/token-value"

func TestHostUnreachableOpensAndResolvesIncidentWithOutbox(t *testing.T) {
	database := openTestDatabase(t, filepath.Join(t.TempDir(), "atalaya.db"))
	ctx := context.Background()
	host, err := database.PrepareLocalHost(ctx, "Oracle", "https://oracle.example.ts.net:9474", testInstant, bytes.NewReader(bytes.Repeat([]byte{0x2a}, 512)))
	if err != nil {
		t.Fatal(err)
	}
	defer clear(host.Credential)
	if err := database.CompleteLocalHost(ctx, host.ID, testInstant, bytes.NewReader(bytes.Repeat([]byte{0x2a}, 512))); err != nil {
		t.Fatal(err)
	}
	random := &collectionRandom{}
	channel, err := database.CreateChannel(ctx, notifications.Channel{Name: "Ops", Provider: notifications.ProviderDiscord, Enabled: true}, testDiscordWebhook, testInstant, random)
	if err != nil {
		t.Fatal(err)
	}
	rule, err := database.CreateAlertRule(ctx, alerts.Rule{Name: "Oracle unreachable", Type: alerts.RuleHostUnreachable, Severity: alerts.SeverityCritical, HostID: &host.ID, ForSeconds: 60, ChannelIDs: []string{channel.ID}, Enabled: true}, testInstant, random)
	if err != nil {
		t.Fatal(err)
	}

	claim, err := database.ClaimDue(ctx, testInstant)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := minimalSnapshot(testInstant)
	if err := database.Finish(ctx, claim, collection.Result{Snapshot: &snapshot, ReceivedAt: testInstant}, random); err != nil {
		t.Fatal(err)
	}
	failureAt := testInstant.Add(time.Minute)
	claim, err = database.ClaimDue(ctx, failureAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Finish(ctx, claim, collection.Result{Failure: collection.FailureConnect, ReceivedAt: failureAt}, random); err != nil {
		t.Fatal(err)
	}

	incidents, err := database.ListIncidents(ctx, "open", "", 10, nil)
	if err != nil || len(incidents) != 1 || incidents[0].RuleID != rule.ID || incidents[0].ConditionStatus != alerts.ConditionTrue || !incidents[0].StartedAt.Equal(failureAt) || !incidents[0].FiredAt.Equal(failureAt) {
		t.Fatalf("open incidents=%+v err=%v", incidents, err)
	}
	deliveries, err := database.ListDeliveries(ctx, channel.ID, "", 10, nil)
	if err != nil || len(deliveries) != 1 || deliveries[0].Kind != notifications.KindIncident || deliveries[0].Transition == nil || *deliveries[0].Transition != notifications.TransitionFiring || deliveries[0].State != notifications.DeliveryPending {
		t.Fatalf("opening deliveries=%+v err=%v", deliveries, err)
	}
	deliveryClaim, err := database.ClaimDelivery(ctx, failureAt)
	if err != nil || deliveryClaim.Kind != notifications.KindIncident || deliveryClaim.DeliveryID != deliveries[0].ID {
		t.Fatalf("opening delivery claim=%+v err=%v", deliveryClaim, err)
	}
	if err := database.FinishDelivery(ctx, deliveryClaim, notifications.DeliveryOutcome{Delivered: true}, failureAt, random); err != nil {
		t.Fatal(err)
	}
	deliveryClaim.Clear()

	recoveryAt := testInstant.Add(2 * time.Minute)
	claim, err = database.ClaimDue(ctx, recoveryAt)
	if err != nil {
		t.Fatal(err)
	}
	snapshot = minimalSnapshot(recoveryAt)
	if err := database.Finish(ctx, claim, collection.Result{Snapshot: &snapshot, ReceivedAt: recoveryAt}, random); err != nil {
		t.Fatal(err)
	}
	resolved, err := database.ListIncidents(ctx, "resolved", "", 10, nil)
	if err != nil || len(resolved) != 1 || resolved[0].ResolutionReason != "condition_false" || resolved[0].ConditionStatus != alerts.ConditionFalse {
		t.Fatalf("resolved incidents=%+v err=%v", resolved, err)
	}
	deliveries, err = database.ListDeliveries(ctx, channel.ID, "", 10, nil)
	if err != nil || len(deliveries) != 2 {
		t.Fatalf("resolved deliveries=%+v err=%v", deliveries, err)
	}
}

func TestCheckAttemptsRuleFiresOnlyAfterScheduledFailures(t *testing.T) {
	database := openTestDatabase(t, filepath.Join(t.TempDir(), "atalaya.db"))
	ctx := context.Background()
	random := &collectionRandom{}
	check, err := database.Create(ctx, testHTTPCheck("API health"), testInstant, random)
	if err != nil {
		t.Fatal(err)
	}
	channel, err := database.CreateChannel(ctx, notifications.Channel{Name: "Ops", Provider: notifications.ProviderDiscord, Enabled: true}, testDiscordWebhook, testInstant, random)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateAlertRule(ctx, alerts.Rule{Name: "API failures", Type: alerts.RuleCheckFailed, Severity: alerts.SeverityWarning, CheckID: &check.ID, CheckMode: alerts.CheckAttempts, ConsecutiveFailures: 3, ChannelIDs: []string{channel.ID}, Enabled: true}, testInstant, random); err != nil {
		t.Fatal(err)
	}

	first, err := database.ClaimScheduled(ctx, testInstant)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.FinishScheduled(ctx, first, checks.Result{Status: checks.StateDown, ReceivedAt: testInstant.Add(time.Second)}, random); err != nil {
		t.Fatal(err)
	}
	open, err := database.ListIncidents(ctx, "open", "", 10, nil)
	if err != nil || len(open) != 0 {
		t.Fatalf("incidents after first failure=%+v err=%v", open, err)
	}

	second, err := database.ClaimScheduled(ctx, testInstant.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.FinishScheduled(ctx, second, checks.Result{Status: checks.StateDown, ReceivedAt: testInstant.Add(time.Minute + time.Second)}, random); err != nil {
		t.Fatal(err)
	}
	open, err = database.ListIncidents(ctx, "open", "", 10, nil)
	if err != nil || len(open) != 0 {
		t.Fatalf("incidents after second failure=%+v err=%v", open, err)
	}

	third, err := database.ClaimScheduled(ctx, testInstant.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.FinishScheduled(ctx, third, checks.Result{Status: checks.StateDown, ReceivedAt: testInstant.Add(2*time.Minute + time.Second)}, random); err != nil {
		t.Fatal(err)
	}
	open, err = database.ListIncidents(ctx, "open", "", 10, nil)
	if err != nil || len(open) != 1 {
		t.Fatalf("incidents after third failure=%+v err=%v", open, err)
	}
}

func TestCheckFailureGapBreaksContinuity(t *testing.T) {
	database := openTestDatabase(t, filepath.Join(t.TempDir(), "atalaya.db"))
	ctx := context.Background()
	random := &collectionRandom{}
	check, err := database.Create(ctx, testHTTPCheck("API health"), testInstant, random)
	if err != nil {
		t.Fatal(err)
	}
	channel, err := database.CreateChannel(ctx, notifications.Channel{Name: "Ops", Provider: notifications.ProviderDiscord, Enabled: true}, testDiscordWebhook, testInstant, random)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateAlertRule(ctx, alerts.Rule{Name: "API failures", Type: alerts.RuleCheckFailed, Severity: alerts.SeverityWarning, CheckID: &check.ID, CheckMode: alerts.CheckAttempts, ConsecutiveFailures: 2, ChannelIDs: []string{channel.ID}, Enabled: true}, testInstant, random); err != nil {
		t.Fatal(err)
	}

	first, err := database.ClaimScheduled(ctx, testInstant)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.FinishScheduled(ctx, first, checks.Result{Status: checks.StateDown, ReceivedAt: testInstant.Add(time.Second)}, random); err != nil {
		t.Fatal(err)
	}

	second, err := database.ClaimScheduled(ctx, testInstant.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	internal := checks.ErrorInternal
	if err := database.FinishScheduled(ctx, second, checks.Result{ErrorCode: &internal, ReceivedAt: testInstant.Add(time.Minute + time.Second)}, random); err != nil {
		t.Fatal(err)
	}

	third, err := database.ClaimScheduled(ctx, testInstant.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.FinishScheduled(ctx, third, checks.Result{Status: checks.StateDown, ReceivedAt: testInstant.Add(2*time.Minute + time.Second)}, random); err != nil {
		t.Fatal(err)
	}
	open, err := database.ListIncidents(ctx, "open", "", 10, nil)
	if err != nil || len(open) != 0 {
		t.Fatalf("incidents across missing slot=%+v err=%v", open, err)
	}
}

func TestNotificationTestReplayIsIdempotentUnderConcurrency(t *testing.T) {
	database := openTestDatabase(t, filepath.Join(t.TempDir(), "atalaya.db"))
	defer database.Close()
	ctx := context.Background()
	random := &collectionRandom{}
	channel, err := database.CreateChannel(ctx, notifications.Channel{Name: "Ops", Provider: notifications.ProviderDiscord, Enabled: true}, testDiscordWebhook, testInstant, random)
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	type result struct {
		delivery notifications.Delivery
		replay   bool
		err      error
	}
	results := make(chan result, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			item, replay, err := database.EnqueueTest(ctx, channel.ID, "same-request", testInstant, &collectionRandom{})
			results <- result{delivery: item, replay: replay, err: err}
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	var firstID string
	replays := 0
	for item := range results {
		if item.err != nil {
			t.Fatal(item.err)
		}
		if firstID == "" {
			firstID = item.delivery.ID
		} else if item.delivery.ID != firstID {
			t.Fatalf("concurrent replay IDs=%q and %q", firstID, item.delivery.ID)
		}
		if item.replay {
			replays++
		}
	}
	if replays != 1 {
		t.Fatalf("replay count=%d, want one replay", replays)
	}
	var count int
	if err := database.sql.QueryRow("SELECT count(*) FROM notification_deliveries WHERE channel_id=? AND test_request_id=?", channel.ID, "same-request").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("delivery count=%d, want one", count)
	}
}

func TestNotificationDeliveriesFollowRetention(t *testing.T) {
	database := openTestDatabase(t, filepath.Join(t.TempDir(), "atalaya.db"))
	defer database.Close()
	ctx := context.Background()
	random := &collectionRandom{}
	channel, err := database.CreateChannel(ctx, notifications.Channel{Name: "Ops", Provider: notifications.ProviderDiscord, Enabled: true}, testDiscordWebhook, testInstant, random)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := database.EnqueueTest(ctx, channel.ID, "old-test", testInstant, random); err != nil {
		t.Fatal(err)
	}
	if _, err := database.sql.Exec("UPDATE notification_deliveries SET state='skipped',skipped_reason='test' WHERE channel_id=?", channel.ID); err != nil {
		t.Fatal(err)
	}
	if err := database.RunRetention(ctx, testInstant.Add(91*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.sql.QueryRow("SELECT count(*) FROM notification_deliveries WHERE channel_id=?", channel.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expired deliveries=%d, want 0", count)
	}
}

func TestChangingRuleChannelsDoesNotResetEvaluation(t *testing.T) {
	hostID := "host-1"
	channelA := "channel-a"
	channelB := "channel-b"
	base := alerts.Rule{
		Name: "host rule", Type: alerts.RuleHostUnreachable, Severity: alerts.SeverityWarning,
		HostID: &hostID, ForSeconds: 60, ChannelIDs: []string{channelA}, Enabled: true,
	}
	changedChannels := base
	changedChannels.ChannelIDs = []string{channelB}
	if materialRuleChange(base, changedChannels) {
		t.Fatal("changing channels must not reset an active evaluation")
	}
	changedCondition := base
	changedCondition.ForSeconds = 120
	if !materialRuleChange(base, changedCondition) {
		t.Fatal("changing duration must reset an active evaluation")
	}
}
