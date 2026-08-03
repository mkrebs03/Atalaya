package collection

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"
)

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

type systemRandom struct{}

func (systemRandom) Intn(n int) int { return rand.IntN(n) } //nolint:gosec // scheduling jitter, not security-sensitive; IDs use crypto/rand below
func (systemRandom) ID() (string, error) {
	var v [16]byte
	if _, e := cryptorand.Read(v[:]); e != nil {
		return "", e
	}
	v[6] = v[6]&15 | 64
	v[8] = v[8]&63 | 128
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", v[0:4], v[4:6], v[6:8], v[8:10], v[10:]), nil
}

type Scheduler struct {
	Repository   Repository
	Puller       Puller
	Clock        Clock
	Random       Random
	Concurrency  int
	PollInterval time.Duration
	OnPullError  func(FailureKind)
}

func (s Scheduler) Run(ctx context.Context) error {
	if s.Repository == nil || s.Puller == nil {
		return fmt.Errorf("collection dependencies are required")
	}
	if s.Clock == nil {
		s.Clock = systemClock{}
	}
	if s.Random == nil {
		s.Random = systemRandom{}
	}
	if s.Concurrency == 0 {
		s.Concurrency = DefaultConcurrency
	}
	if s.PollInterval == 0 {
		s.PollInterval = 250 * time.Millisecond
	}
	if s.Concurrency < 1 || s.PollInterval <= 0 {
		return fmt.Errorf("invalid collection scheduler settings")
	}
	sem := make(chan struct{}, s.Concurrency)
	fatal := make(chan error, s.Concurrency)
	ticker := time.NewTicker(s.PollInterval)
	defer ticker.Stop()
	var workers sync.WaitGroup
	defer func() { workers.Wait() }()
	for {
		if ctx.Err() != nil {
			return nil
		}
		if err := s.Repository.Maintain(ctx, s.Clock.Now().UTC(), s.Random); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("maintain collection schedule: %w", err)
		}
		if ctx.Err() != nil {
			return nil
		}
		for len(sem) < cap(sem) {
			claim, err := s.Repository.ClaimDue(ctx, s.Clock.Now().UTC())
			if err != nil && ctx.Err() != nil {
				return nil
			}
			if errors.Is(err, ErrNoDueSlot) {
				break
			}
			if err != nil {
				return fmt.Errorf("claim collection slot: %w", err)
			}
			sem <- struct{}{}
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer func() { <-sem }()
				if err := s.execute(ctx, claim); err != nil {
					fatal <- err
				}
			}()
		}
		select {
		case <-ctx.Done():
			return nil
		case err := <-fatal:
			return err
		case <-ticker.C:
		}
	}
}

func (s Scheduler) execute(ctx context.Context, claim Claim) error {
	defer claim.Clear()
	snapshot, err := s.Puller.Pull(ctx, claim)
	if ctx.Err() != nil {
		releaseContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		_ = s.Repository.Release(releaseContext, claim, s.Clock.Now().UTC())
		return nil
	}
	result := Result{ReceivedAt: s.Clock.Now().UTC()}
	if err == nil {
		result.Snapshot = &snapshot
	} else {
		var pull *PullError
		if errors.As(err, &pull) {
			result.Failure = pull.Kind
		} else {
			result.Failure = FailureInternal
		}
		if s.OnPullError != nil {
			s.OnPullError(result.Failure)
		}
	}
	if err := s.Repository.Finish(ctx, claim, result, s.Random); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("finish collection slot: %w", err)
	}
	return nil
}
