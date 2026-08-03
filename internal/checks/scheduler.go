package checks

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	mathrand "math/rand/v2"
	"sync"
	"time"
)

type Clock interface{ Now() time.Time }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

type systemIDs struct{}

func (systemIDs) ID() (string, error) {
	var value [16]byte
	if _, err := cryptorand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = value[6]&15 | 64
	value[8] = value[8]&63 | 128
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:]), nil
}

func (systemIDs) Intn(max int) int {
	if max <= 1 {
		return 0
	}
	return mathrand.IntN(max) //nolint:gosec // scheduling jitter, not security-sensitive
}

type Scheduler struct {
	Repository   SchedulerRepository
	Probe        Executor
	Clock        Clock
	Random       IDSource
	Concurrency  int
	PollInterval time.Duration
}

func (scheduler Scheduler) Run(ctx context.Context) error {
	if scheduler.Repository == nil || scheduler.Probe == nil {
		return fmt.Errorf("check scheduler dependencies are required")
	}
	if scheduler.Clock == nil {
		scheduler.Clock = systemClock{}
	}
	if scheduler.Random == nil {
		scheduler.Random = systemIDs{}
	}
	if scheduler.Concurrency == 0 {
		scheduler.Concurrency = 4
	}
	if scheduler.PollInterval == 0 {
		scheduler.PollInterval = 250 * time.Millisecond
	}
	if scheduler.Concurrency < 1 || scheduler.PollInterval <= 0 {
		return fmt.Errorf("invalid check scheduler settings")
	}
	semaphore := make(chan struct{}, scheduler.Concurrency)
	errorsChannel := make(chan error, scheduler.Concurrency)
	ticker := time.NewTicker(scheduler.PollInterval)
	defer ticker.Stop()
	var workers sync.WaitGroup
	defer workers.Wait()
	for {
		if ctx.Err() != nil {
			return nil
		}
		if err := scheduler.Repository.MaintainScheduled(ctx, scheduler.Clock.Now().UTC(), scheduler.Random); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("maintain check schedule: %w", err)
		}
		for len(semaphore) < cap(semaphore) {
			claim, err := scheduler.Repository.ClaimScheduled(ctx, scheduler.Clock.Now().UTC())
			if errors.Is(err, ErrNoDueSlot) {
				break
			}
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("claim check slot: %w", err)
			}
			semaphore <- struct{}{}
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer func() { <-semaphore }()
				if err := scheduler.execute(ctx, claim); err != nil {
					errorsChannel <- err
				}
			}()
		}
		select {
		case <-ctx.Done():
			return nil
		case err := <-errorsChannel:
			return err
		case <-ticker.C:
		}
	}
}

func (scheduler Scheduler) execute(ctx context.Context, claim ScheduledClaim) error {
	result := scheduler.Probe.Probe(ctx, claim.Config)
	if result.ReceivedAt.IsZero() {
		result.ReceivedAt = scheduler.Clock.Now().UTC()
	}
	if ctx.Err() != nil {
		releaseContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		_ = scheduler.Repository.ReleaseScheduled(releaseContext, claim, scheduler.Clock.Now().UTC())
		return nil
	}
	if err := scheduler.Repository.FinishScheduled(ctx, claim, result, scheduler.Random); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("finish check slot: %w", err)
	}
	return nil
}

type ManualWorker struct {
	Repository   ManualRepository
	Probe        Executor
	Clock        Clock
	Concurrency  int
	PollInterval time.Duration
}

func (worker ManualWorker) Run(ctx context.Context) error {
	if worker.Repository == nil || worker.Probe == nil {
		return fmt.Errorf("manual check worker dependencies are required")
	}
	if worker.Clock == nil {
		worker.Clock = systemClock{}
	}
	if worker.Concurrency == 0 {
		worker.Concurrency = 4
	}
	if worker.PollInterval == 0 {
		worker.PollInterval = 250 * time.Millisecond
	}
	if worker.Concurrency < 1 || worker.PollInterval <= 0 {
		return fmt.Errorf("invalid manual check worker settings")
	}
	semaphore := make(chan struct{}, worker.Concurrency)
	errorsChannel := make(chan error, worker.Concurrency)
	ticker := time.NewTicker(worker.PollInterval)
	defer ticker.Stop()
	var workers sync.WaitGroup
	defer workers.Wait()
	for {
		if ctx.Err() != nil {
			return nil
		}
		for len(semaphore) < cap(semaphore) {
			claim, err := worker.Repository.ClaimManualRun(ctx, worker.Clock.Now().UTC())
			if errors.Is(err, ErrNoDueSlot) {
				break
			}
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("claim manual check run: %w", err)
			}
			semaphore <- struct{}{}
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer func() { <-semaphore }()
				if err := worker.execute(ctx, claim); err != nil {
					errorsChannel <- err
				}
			}()
		}
		select {
		case <-ctx.Done():
			return nil
		case err := <-errorsChannel:
			return err
		case <-ticker.C:
		}
	}
}

func (worker ManualWorker) execute(ctx context.Context, claim ManualClaim) error {
	result := worker.Probe.Probe(ctx, claim.Config)
	if result.ReceivedAt.IsZero() {
		result.ReceivedAt = worker.Clock.Now().UTC()
	}
	if ctx.Err() != nil {
		releaseContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		_ = worker.Repository.ReleaseManualRun(releaseContext, claim, worker.Clock.Now().UTC())
		return nil
	}
	failureCode := ""
	if result.Status != StateUp && result.ErrorCode != nil && *result.ErrorCode == ErrorInternal {
		failureCode = "internal"
	}
	if err := worker.Repository.FinishManualRun(ctx, claim, result, failureCode, worker.Clock.Now().UTC()); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("finish manual check run: %w", err)
	}
	return nil
}
