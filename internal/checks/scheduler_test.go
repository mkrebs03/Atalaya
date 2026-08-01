package checks

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type schedulerTestRepository struct {
	mu       sync.Mutex
	claimed  bool
	finished chan Result
}

func (repository *schedulerTestRepository) MaintainScheduled(context.Context, time.Time, IDSource) error {
	return nil
}

func (repository *schedulerTestRepository) ClaimScheduled(context.Context, time.Time) (ScheduledClaim, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if repository.claimed {
		return ScheduledClaim{}, ErrNoDueSlot
	}
	repository.claimed = true
	return ScheduledClaim{SlotID: "slot", CheckID: "check", Generation: 1, Config: Config{Type: TypeTCP, TCP: &TCPConfig{Host: "service", Port: 443, IntervalSeconds: 60, TimeoutSeconds: 5}}, NominalAt: time.Now(), DeadlineAt: time.Now().Add(time.Minute), Interval: time.Minute, Timeout: time.Second}, nil
}

func (repository *schedulerTestRepository) FinishScheduled(_ context.Context, _ ScheduledClaim, result Result, _ IDSource) error {
	repository.finished <- result
	return nil
}

func (repository *schedulerTestRepository) ReleaseScheduled(context.Context, ScheduledClaim, time.Time) error {
	return nil
}

type schedulerTestExecutor struct{}

func (schedulerTestExecutor) Probe(context.Context, Config) Result {
	return Result{Status: StateUp, ReceivedAt: time.Now().UTC()}
}

type schedulerTestClock struct{}

func (schedulerTestClock) Now() time.Time { return time.Now().UTC() }

func TestSchedulerClaimsExecutesAndFinishesScheduledCheck(t *testing.T) {
	repository := &schedulerTestRepository{finished: make(chan Result, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- (Scheduler{Repository: repository, Probe: schedulerTestExecutor{}, Clock: schedulerTestClock{}, PollInterval: time.Millisecond}).Run(ctx)
	}()
	select {
	case result := <-repository.finished:
		if result.Status != StateUp {
			t.Fatalf("finished result=%+v", result)
		}
		cancel()
	case <-time.After(time.Second):
		t.Fatal("scheduler did not finish claim")
	}
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop")
	}
}
