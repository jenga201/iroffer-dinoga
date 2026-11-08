// Package ratelimit provides a small per-key fixed-window rate limiter with a
// post-exceed "ignore" cooldown, used for IRC flood protection.
package ratelimit

import (
	"sync"
	"time"
)

// Limiter permits up to rate events per key within a sliding window. A key that
// exceeds the rate is rejected (ignored) for ignoreFor before it may proceed
// again.
type Limiter struct {
	mu        sync.Mutex
	rate      int
	window    time.Duration
	ignoreFor time.Duration

	windows map[string][]time.Time
	ignored map[string]time.Time
	now     func() time.Time
}

// New returns a Limiter. A rate below 1 is clamped to 1; a non-positive window
// defaults to one second.
func New(rate int, window, ignoreFor time.Duration) *Limiter {
	if rate < 1 {
		rate = 1
	}
	if window <= 0 {
		window = time.Second
	}
	if ignoreFor < 0 {
		ignoreFor = 0
	}
	return &Limiter{
		rate:      rate,
		window:    window,
		ignoreFor: ignoreFor,
		windows:   map[string][]time.Time{},
		ignored:   map[string]time.Time{},
		now:       time.Now,
	}
}

// Allow records an event for key and reports whether it is permitted. Once a
// key exceeds the rate it is rejected until the ignore window elapses.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()

	if until, ok := l.ignored[key]; ok {
		if now.Before(until) {
			return false
		}
		delete(l.ignored, key)
	}

	cutoff := now.Add(-l.window)
	events := l.windows[key]
	kept := events[:0]
	for _, t := range events {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.rate {
		delete(l.windows, key)
		l.ignored[key] = now.Add(l.ignoreFor)
		return false
	}
	kept = append(kept, now)
	l.windows[key] = kept
	return true
}
