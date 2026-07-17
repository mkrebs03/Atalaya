package auth

import (
	"math"
	"sync"
	"time"
)

const (
	globalRatePerMinute = 30
	globalBurst         = 10
	maximumRateEntries  = 1024
	rateEntryRetention  = 15 * time.Minute
)

type loginRateEntry struct {
	failures               int
	blockedUntil, lastSeen time.Time
	sequence               uint64
}

type LoginRateLimiter struct {
	mu         sync.Mutex
	clock      func() time.Time
	tokens     float64
	lastRefill time.Time
	entries    map[string]loginRateEntry
	sequence   uint64
}

func NewLoginRateLimiter(clock func() time.Time) *LoginRateLimiter {
	if clock == nil {
		clock = time.Now
	}
	now := clock()
	return &LoginRateLimiter{clock: clock, tokens: globalBurst, lastRefill: now, entries: make(map[string]loginRateEntry)}
}

func (limiter *LoginRateLimiter) Allow(username string) (bool, time.Duration) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	now := limiter.clock()
	limiter.purge(now)
	limiter.refill(now)
	entry := limiter.entries[username]
	if now.Before(entry.blockedUntil) {
		return false, entry.blockedUntil.Sub(now)
	}
	if limiter.tokens < 1 {
		return false, time.Duration(math.Ceil((1 - limiter.tokens) * float64(2*time.Second)))
	}
	limiter.tokens--
	entry = limiter.boundedEntry(username, now)
	entry.lastSeen = now
	entry.sequence = limiter.nextSequence()
	limiter.entries[username] = entry
	return true, 0
}

func (limiter *LoginRateLimiter) Failure(username string) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	now := limiter.clock()
	limiter.purge(now)
	entry := limiter.boundedEntry(username, now)
	entry.failures++
	entry.lastSeen = now
	entry.sequence = limiter.nextSequence()
	if entry.failures >= 5 {
		seconds := time.Second * time.Duration(1<<min(entry.failures-5, 6))
		if seconds > 60*time.Second {
			seconds = 60 * time.Second
		}
		entry.blockedUntil = now.Add(seconds)
	}
	limiter.entries[username] = entry
}

func (limiter *LoginRateLimiter) boundedEntry(username string, now time.Time) loginRateEntry {
	if entry, exists := limiter.entries[username]; exists {
		return entry
	}
	if len(limiter.entries) >= maximumRateEntries {
		var oldestKey string
		var oldest loginRateEntry
		for key, candidate := range limiter.entries {
			if oldestKey == "" || candidate.lastSeen.Before(oldest.lastSeen) || (candidate.lastSeen.Equal(oldest.lastSeen) && candidate.sequence < oldest.sequence) {
				oldestKey, oldest = key, candidate
			}
		}
		delete(limiter.entries, oldestKey)
	}
	return loginRateEntry{lastSeen: now}
}

func (limiter *LoginRateLimiter) nextSequence() uint64 {
	limiter.sequence++
	return limiter.sequence
}

func (limiter *LoginRateLimiter) Success(username string) {
	limiter.mu.Lock()
	delete(limiter.entries, username)
	limiter.mu.Unlock()
}

func (limiter *LoginRateLimiter) purge(now time.Time) {
	for key, entry := range limiter.entries {
		if now.Sub(entry.lastSeen) >= rateEntryRetention {
			delete(limiter.entries, key)
		}
	}
}
func (limiter *LoginRateLimiter) refill(now time.Time) {
	elapsed := now.Sub(limiter.lastRefill)
	if elapsed <= 0 {
		return
	}
	limiter.tokens = math.Min(globalBurst, limiter.tokens+elapsed.Minutes()*globalRatePerMinute)
	limiter.lastRefill = now
}
