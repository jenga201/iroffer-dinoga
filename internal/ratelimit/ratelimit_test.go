package ratelimit

import (
	"testing"
	"time"
)

func TestAllowUnderRate(t *testing.T) {
	l := New(3, time.Second, time.Second)
	cur := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return cur }
	for i := 0; i < 3; i++ {
		if !l.Allow("u") {
			t.Fatalf("event %d rejected within rate", i+1)
		}
	}
}

func TestIgnoreAfterExceedingRate(t *testing.T) {
	l := New(2, time.Second, 10*time.Second)
	cur := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return cur }

	if !l.Allow("u") || !l.Allow("u") {
		t.Fatal("first two events should be allowed")
	}
	if l.Allow("u") {
		t.Fatal("third event should exceed the rate")
	}
	if l.Allow("u") {
		t.Fatal("expected rejection while ignored")
	}

	cur = cur.Add(11 * time.Second)
	if !l.Allow("u") {
		t.Fatal("expected recovery after the ignore window")
	}
}

func TestKeysAreIndependent(t *testing.T) {
	l := New(1, time.Second, time.Second)
	cur := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return cur }
	if !l.Allow("a") {
		t.Fatal("a should be allowed")
	}
	if !l.Allow("b") {
		t.Fatal("b should be allowed (independent key)")
	}
}

func TestWindowSlides(t *testing.T) {
	l := New(2, time.Second, time.Second)
	cur := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return cur }

	if !l.Allow("u") {
		t.Fatal("event 1")
	}
	cur = cur.Add(500 * time.Millisecond)
	if !l.Allow("u") {
		t.Fatal("event 2")
	}
	cur = cur.Add(600 * time.Millisecond) // now = 1.1s; the t=0 event expired
	if !l.Allow("u") {
		t.Fatal("event 3 should fit after window slide")
	}
	if l.Allow("u") {
		t.Fatal("event 4 should exceed the rate")
	}
}
