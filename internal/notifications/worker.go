package notifications

import (
	"context"
	"errors"
	"fmt"
	mathrand "math/rand/v2"
	"time"
)

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

type systemRandom struct{}

func (systemRandom) ID() (string, error) {
	return "", fmt.Errorf("worker random ID source is not available")
}
func (systemRandom) Intn(max int) int {
	if max <= 1 {
		return 0
	}
	return mathrand.IntN(max) //nolint:gosec // retry jitter, not security-sensitive
}

type Worker struct {
	Repository   Repository
	Sender       Sender
	Clock        Clock
	Random       IDSource
	PollInterval time.Duration
}

func (worker Worker) Run(ctx context.Context) error {
	if worker.Repository == nil || worker.Sender == nil {
		return fmt.Errorf("notification worker dependencies are required")
	}
	if worker.Clock == nil {
		worker.Clock = systemClock{}
	}
	if worker.Random == nil {
		worker.Random = systemRandom{}
	}
	if worker.PollInterval == 0 {
		worker.PollInterval = 250 * time.Millisecond
	}
	if worker.PollInterval <= 0 {
		return fmt.Errorf("invalid notification worker settings")
	}
	ticker := time.NewTicker(worker.PollInterval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return nil
		}
		for {
			claim, err := worker.Repository.ClaimDelivery(ctx, worker.Clock.Now().UTC())
			if errors.Is(err, ErrNoDueDelivery) {
				break
			}
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("claim notification delivery: %w", err)
			}
			result := worker.Sender.Send(ctx, claim)
			claimOutcome := DeliveryOutcome{Delivered: result.Err == nil && result.StatusCode >= 200 && result.StatusCode <= 299, RetryAfter: result.RetryAfter, ErrorCode: result.ErrorCode}
			if result.Err != nil {
				claimOutcome.Retryable = result.ErrorCode != "invalid_webhook"
			}
			if result.StatusCode == 429 || result.StatusCode >= 500 {
				claimOutcome.Retryable = true
			}
			if !claimOutcome.Delivered && claimOutcome.ErrorCode == "" {
				claimOutcome.ErrorCode = "delivery_failed"
			}
			finishErr := worker.Repository.FinishDelivery(ctx, claim, claimOutcome, worker.Clock.Now().UTC(), worker.Random)
			claim.Clear()
			if finishErr != nil {
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("finish notification delivery: %w", finishErr)
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func RetryDelay(attempt int) time.Duration {
	delays := [...]time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute}
	if attempt < 1 {
		return delays[0]
	}
	if attempt > len(delays) {
		return delays[len(delays)-1]
	}
	return delays[attempt-1]
}
