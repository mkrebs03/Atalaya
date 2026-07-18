package collection

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mkrebs03/atalaya/internal/agent"
)

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

type fixedRandom struct {
	mu   sync.Mutex
	next int
}

func (r *fixedRandom) Intn(int) int { return 0 }
func (r *fixedRandom) ID() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next++
	return fmt.Sprintf("id-%d", r.next), nil
}

type schedulerRepo struct {
	mu        sync.Mutex
	claims    []Claim
	maintain  func(context.Context) error
	finishErr error
	released  int
	results   []Result
}

func (r *schedulerRepo) Maintain(ctx context.Context, _ time.Time, _ Random) error {
	if r.maintain != nil {
		return r.maintain(ctx)
	}
	return nil
}
func (r *schedulerRepo) ClaimDue(context.Context, time.Time) (Claim, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.claims) == 0 {
		return Claim{}, ErrNoDueSlot
	}
	c := r.claims[0]
	r.claims = r.claims[1:]
	return c, nil
}
func (r *schedulerRepo) Finish(_ context.Context, _ Claim, result Result, _ Random) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.results = append(r.results, result)
	return r.finishErr
}
func (r *schedulerRepo) Release(context.Context, Claim, time.Time) error {
	r.mu.Lock()
	r.released++
	r.mu.Unlock()
	return nil
}

type pullFunction func(context.Context, Claim) (agent.Snapshot, error)

func (f pullFunction) Pull(ctx context.Context, c Claim) (agent.Snapshot, error) { return f(ctx, c) }

func TestSchedulerCancellationIsGraceful(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	repo := &schedulerRepo{maintain: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }}
	done := make(chan error, 1)
	go func() {
		done <- (Scheduler{Repository: repo, Puller: pullFunction(func(context.Context, Claim) (agent.Snapshot, error) { return agent.Snapshot{}, nil }), Clock: fixedClock{}, Random: &fixedRandom{}, PollInterval: time.Hour}).Run(ctx)
	}()
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("cancel returned %v", err)
	}
}

func TestSchedulerCancellationMasksTransactionTeardownOnlyAfterShutdown(t *testing.T) {
	for range 50 {
		ctx, cancel := context.WithCancel(context.Background())
		repo := &schedulerRepo{maintain: func(ctx context.Context) error {
			cancel()
			<-ctx.Done()
			return sql.ErrTxDone
		}}
		if err := (Scheduler{Repository: repo, Puller: pullFunction(func(context.Context, Claim) (agent.Snapshot, error) { return agent.Snapshot{}, nil }), Clock: fixedClock{}, Random: &fixedRandom{}}).Run(ctx); err != nil {
			t.Fatalf("shutdown transaction race returned %v", err)
		}
	}
	repo := &schedulerRepo{maintain: func(context.Context) error { return sql.ErrTxDone }}
	err := (Scheduler{Repository: repo, Puller: pullFunction(func(context.Context, Claim) (agent.Snapshot, error) { return agent.Snapshot{}, nil }), Clock: fixedClock{}, Random: &fixedRandom{}}).Run(context.Background())
	if !errors.Is(err, sql.ErrTxDone) {
		t.Fatalf("live-context error hidden: %v", err)
	}
}

func TestSchedulerPropagatesFatalRepositoryError(t *testing.T) {
	fatal := errors.New("database invariant")
	repo := &schedulerRepo{maintain: func(context.Context) error { return fatal }}
	err := (Scheduler{Repository: repo, Puller: pullFunction(func(context.Context, Claim) (agent.Snapshot, error) { return agent.Snapshot{}, nil }), Clock: fixedClock{}, Random: &fixedRandom{}}).Run(context.Background())
	if !errors.Is(err, fatal) {
		t.Fatalf("fatal=%v", err)
	}
}

func TestSchedulerLimitsConcurrencyAndReleasesShutdownClaims(t *testing.T) {
	claims := make([]Claim, 6)
	for i := range claims {
		claims[i] = Claim{SlotID: fmt.Sprint(i), Credential: make([]byte, 32)}
	}
	repo := &schedulerRepo{claims: claims}
	started := make(chan struct{}, 6)
	ctx, cancel := context.WithCancel(context.Background())
	pull := pullFunction(func(ctx context.Context, _ Claim) (agent.Snapshot, error) {
		started <- struct{}{}
		<-ctx.Done()
		return agent.Snapshot{}, ctx.Err()
	})
	done := make(chan error, 1)
	go func() {
		done <- (Scheduler{Repository: repo, Puller: pull, Clock: fixedClock{}, Random: &fixedRandom{}, Concurrency: 4, PollInterval: time.Hour}).Run(ctx)
	}()
	for range 4 {
		<-started
	}
	select {
	case <-started:
		t.Fatal("more than four pulls started")
	default:
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	repo.mu.Lock()
	released := repo.released
	repo.mu.Unlock()
	if released != 4 {
		t.Fatalf("released=%d", released)
	}
}

func TestSchedulerTreatsFinishFailureAsFatal(t *testing.T) {
	fatal := errors.New("commit failed")
	repo := &schedulerRepo{claims: []Claim{{Credential: make([]byte, 32)}}, finishErr: fatal}
	err := (Scheduler{Repository: repo, Puller: pullFunction(func(context.Context, Claim) (agent.Snapshot, error) { return minimalWireSnapshot(), nil }), Clock: fixedClock{}, Random: &fixedRandom{}, PollInterval: time.Hour}).Run(context.Background())
	if !errors.Is(err, fatal) {
		t.Fatalf("fatal=%v", err)
	}
}

func TestSchedulerRecordsProtocolFailureWithoutStopping(t *testing.T) {
	repo := &schedulerRepo{}
	scheduler := Scheduler{Repository: repo, Puller: pullFunction(func(context.Context, Claim) (agent.Snapshot, error) {
		return agent.Snapshot{}, &PullError{Kind: FailureProtocol}
	}), Clock: fixedClock{at: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, Random: &fixedRandom{}}
	if err := scheduler.execute(context.Background(), Claim{Credential: make([]byte, 32)}); err != nil {
		t.Fatalf("protocol failure stopped scheduler: %v", err)
	}
	if len(repo.results) != 1 || repo.results[0].Failure != FailureProtocol || repo.results[0].Snapshot != nil {
		t.Fatalf("results=%+v", repo.results)
	}
}
func minimalWireSnapshot() agent.Snapshot {
	return agent.Snapshot{ProtocolVersion: 1, AgentVersion: "dev", Host: agent.Host{Hostname: "host", ObservedAt: "2026-01-01T00:00:00Z"}, Tailscale: agent.Tailscale{State: "connected", Addresses: []string{}}, Filesystems: []agent.Filesystem{}, CollectionWarnings: []string{}}
}
