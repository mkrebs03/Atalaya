// Package retention periodically compacts historical metric samples.
package retention

import (
	"context"
	"fmt"
	"time"

	"github.com/mkrebs03/atalaya/internal/metrics"
)

type Clock interface{ Now() time.Time }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

type Worker struct {
	Repository metrics.RetentionRepository
	Clock      Clock
	Interval   time.Duration
}

func (worker Worker) Run(ctx context.Context) error {
	if worker.Repository == nil {
		return fmt.Errorf("retention repository is required")
	}
	if worker.Clock == nil {
		worker.Clock = systemClock{}
	}
	if worker.Interval == 0 {
		worker.Interval = time.Hour
	}
	if worker.Interval <= 0 {
		return fmt.Errorf("retention interval must be positive")
	}
	if err := worker.Repository.RunRetention(ctx, worker.Clock.Now().UTC()); err != nil {
		return err
	}
	ticker := time.NewTicker(worker.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := worker.Repository.RunRetention(ctx, worker.Clock.Now().UTC()); err != nil {
				return err
			}
		}
	}
}
