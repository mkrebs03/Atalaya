package auth

import (
	"fmt"
	"testing"
	"time"
)

func TestLoginRateLimiterBackoffAndSuccessReset(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	limiter := NewLoginRateLimiter(func() time.Time { return now })
	for index := 0; index < 5; index++ {
		if ok, _ := limiter.Allow("admin"); !ok {
			t.Fatalf("attempt %d unexpectedly blocked", index)
		}
		limiter.Failure("admin")
	}
	if ok, retry := limiter.Allow("admin"); ok || retry != time.Second {
		t.Fatalf("fifth failure = %v/%v", ok, retry)
	}
	now = now.Add(time.Second)
	limiter.Success("admin")
	if ok, _ := limiter.Allow("admin"); !ok {
		t.Fatal("success did not clear username backoff")
	}
}

func TestLoginRateLimiterGlobalBucketAndEntryCap(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	limiter := NewLoginRateLimiter(func() time.Time { return now })
	for index := 0; index < 10; index++ {
		if ok, _ := limiter.Allow(fmt.Sprint(index)); !ok {
			t.Fatalf("burst attempt %d blocked", index)
		}
	}
	if ok, retry := limiter.Allow("overflow"); ok || retry != 2*time.Second {
		t.Fatalf("empty bucket = %v/%v", ok, retry)
	}
	now = now.Add(2 * time.Second)
	if ok, _ := limiter.Allow("refilled"); !ok {
		t.Fatal("global bucket did not refill")
	}
	for index := 0; index < 1100; index++ {
		limiter.Failure(fmt.Sprint(index))
	}
	if len(limiter.entries) > maximumRateEntries {
		t.Fatalf("entries = %d", len(limiter.entries))
	}
	now = now.Add(15 * time.Minute)
	limiter.Allow("purge")
	if len(limiter.entries) > 1 {
		t.Fatalf("stale entries remain: %d", len(limiter.entries))
	}
}

func TestLoginRateLimiterBoundsEntriesDuringAllowFailureFlow(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	limiter := NewLoginRateLimiter(func() time.Time { return now })
	limiter.tokens = maximumRateEntries + 100

	for index := 0; index < maximumRateEntries+100; index++ {
		username := fmt.Sprintf("user-%04d", index)
		if allowed, _ := limiter.Allow(username); !allowed {
			t.Fatalf("Allow(%q) unexpectedly blocked", username)
		}
		limiter.Failure(username)
	}

	if len(limiter.entries) != maximumRateEntries {
		t.Fatalf("entries = %d, want %d", len(limiter.entries), maximumRateEntries)
	}
	if _, exists := limiter.entries["user-1123"]; !exists {
		t.Fatal("newest username was evicted")
	}
}
