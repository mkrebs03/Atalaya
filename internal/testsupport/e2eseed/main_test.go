package main

import (
	"testing"
	"time"
)

func TestStableSeedTimeKeepsCollectionBeyondBrowserRun(t *testing.T) {
	now := time.Date(2042, 11, 9, 17, 43, 21, 0, time.FixedZone("test", -3*60*60))
	seed := stableSeedTime(now)
	if seed.Location() != time.UTC {
		t.Fatalf("location = %v, want UTC", seed.Location())
	}
	margin := seed.Sub(now.UTC())
	if margin < 23*time.Hour || margin > 24*time.Hour {
		t.Fatalf("future margin = %v, want 23h..24h", margin)
	}
	if !seed.Equal(seed.Truncate(time.Hour)) {
		t.Fatalf("seed = %v, want whole UTC hour", seed)
	}
}
