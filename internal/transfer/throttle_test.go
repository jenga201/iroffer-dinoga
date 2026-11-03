package transfer

import (
	"bytes"
	"io"
	"testing"
	"time"
)

func TestThrottledReaderBoundsRate(t *testing.T) {
	// 100 KiB/s with burst: copying 200 KiB must take close to 2 seconds.
	rate := int64(100 * 1024)
	src := bytes.Repeat([]byte("x"), 200*1024)

	b := NewTokenBucket(rate)
	r := NewThrottledReader(bytes.NewReader(src), b)

	start := time.Now()
	n, err := io.Copy(io.Discard, r)
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)

	if int(n) != len(src) {
		t.Fatalf("copied %d, want %d", n, len(src))
	}
	// With burst = max(rate/4, 4KiB) = 25KiB, effective time should be very
	// roughly (200KiB - 25KiB)/100KiB/s ≈ 1.7s. Assert it is not faster than
	// ~1.2s (i.e. it did throttle) and not absurdly slow (< 5s).
	if elapsed < 1200*time.Millisecond {
		t.Fatalf("transfer too fast (%v) for %d bytes at %d B/s", elapsed, n, rate)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("transfer too slow (%v)", elapsed)
	}
}

func TestUnlimitedRate(t *testing.T) {
	src := bytes.Repeat([]byte("y"), 1<<20)
	b := NewTokenBucket(0)
	r := NewThrottledReader(bytes.NewReader(src), b)
	start := time.Now()
	n, err := io.Copy(io.Discard, r)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("unlimited rate too slow: %v", elapsed)
	}
	if int(n) != len(src) {
		t.Fatalf("copied %d, want %d", n, len(src))
	}
}

func TestLimitNoop(t *testing.T) {
	rr, ww := Limit(nil, nil, 0)
	if rr != nil || ww != nil {
		t.Fatal("Limit with zero rate should be a no-op")
	}
	rr, ww = Limit(bytes.NewReader(nil), io.Discard, 1024)
	if rr == nil || ww == nil {
		t.Fatal("Limit with a rate should wrap both")
	}
}
