// Package transfer provides per-transfer bandwidth throttling primitives for
// the DCC/SXDCC data channels (iroffer's transfermaxspeed / overallmaxspeed).
package transfer

import (
	"io"
	"sync"
	"time"
)

// TokenBucket is a concurrency-safe token bucket allowing Burst bytes to be
// transmitted immediately and replenishing at Rate bytes per second. A zero
// Rate disables throttling (reads/writes are unrestricted).
type TokenBucket struct {
	mu     sync.Mutex
	rate   int64 // bytes/sec; 0 = unlimited
	burst  int64 // bytes allowed as a burst (>= rate/10 by default)
	tokens float64
	last   time.Time
}

// NewTokenBucket returns a TokenBucket with the given rate (bytes/sec) and a
// burst equal to one second of traffic, or 1 KiB, whichever is larger.
func NewTokenBucket(rate int64) *TokenBucket {
	b := &TokenBucket{rate: rate, burst: bustSize(rate), tokens: 0}
	b.last = time.Now()
	if rate > 0 {
		b.tokens = float64(b.burst)
	}
	return b
}

func bustSize(rate int64) int64 {
	s := rate / 4 // 250ms of traffic is a reasonable burst
	if s < 0 || rate == 0 {
		return 0
	}
	if s < 4096 {
		s = 4096
	}
	return s
}

func (b *TokenBucket) refill(now time.Time) {
	if b.rate <= 0 {
		return
	}
	elapsed := now.Sub(b.last).Seconds()
	b.last = now
	b.tokens += elapsed * float64(b.rate)
	if b.tokens > float64(b.burst) {
		b.tokens = float64(b.burst)
	}
}

// take blocks the caller until n bytes are admitted at the configured rate.
func (b *TokenBucket) take(n int) {
	if b.rate <= 0 {
		return
	}
	for {
		b.mu.Lock()
		now := time.Now()
		b.refill(now)
		if b.tokens >= float64(n) {
			b.tokens -= float64(n)
			b.mu.Unlock()
			return
		}
		need := float64(n) - b.tokens
		b.mu.Unlock()

		d := time.Duration(need / float64(b.rate) * float64(time.Second))
		if d < time.Millisecond {
			d = time.Millisecond
		}
		time.Sleep(d)
	}
}

// ThrottledReader wraps an io.Reader and limits the aggregate read rate to the
// bucket's configured rate.
type ThrottledReader struct {
	r io.Reader
	b *TokenBucket
}

// NewThrottledReader returns a reader that throttles reads through b.
func NewThrottledReader(r io.Reader, b *TokenBucket) *ThrottledReader {
	return &ThrottledReader{r: r, b: b}
}

func (t *ThrottledReader) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	if n > 0 {
		t.b.take(n)
	}
	return n, err
}

// ThrottledWriter wraps an io.Writer and limits the aggregate write rate.
type ThrottledWriter struct {
	w io.Writer
	b *TokenBucket
}

// NewThrottledWriter returns a writer that throttles writes through b.
func NewThrottledWriter(w io.Writer, b *TokenBucket) *ThrottledWriter {
	return &ThrottledWriter{w: w, b: b}
}

func (t *ThrottledWriter) Write(p []byte) (int, error) {
	t.b.take(len(p))
	return t.w.Write(p)
}

// Limit is a shared convenience for building throttled streams.
func Limit(r io.Reader, w io.Writer, rate int64) (io.Reader, io.Writer) {
	if rate <= 0 {
		return r, w
	}
	b := NewTokenBucket(rate)
	return NewThrottledReader(r, b), NewThrottledWriter(w, b)
}
