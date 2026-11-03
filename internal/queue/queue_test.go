package queue

import (
	"sync"
	"testing"
	"time"
)

func TestSubmitPositions(t *testing.T) {
	m := New(Config{Slots: 1, QueueSize: 10})
	if pos, err := m.Submit("a", "1", false); err != nil || pos != 1 {
		t.Fatalf("first pos=%d err=%v", pos, err)
	}
	if pos, err := m.Submit("b", "2", false); err != nil || pos != 2 {
		t.Fatalf("second pos=%d err=%v", pos, err)
	}
}

func TestFIFOOrder(t *testing.T) {
	m := New(Config{Slots: 1, QueueSize: 10})
	m.Submit("a", "first", false)
	for _, s := range []string{"a", "b", "c"} {
		m.Submit("x", s, false)
	}

	_, key, release := m.Wait()
	release()
	if key != "first" {
		t.Fatalf("first key = %q", key)
	}
	for _, want := range []string{"a", "b", "c"} {
		_, key, release := m.Wait()
		release()
		if key != want {
			t.Fatalf("FIFO key = %q, want %q", key, want)
		}
	}
}

func TestQueueCap(t *testing.T) {
	m := New(Config{Slots: 1, QueueSize: 2})
	m.Submit("b", "2", false)
	m.Submit("c", "3", false)
	if _, err := m.Submit("d", "4", false); err != ErrQueueFull {
		t.Fatalf("err = %v, want ErrQueueFull", err)
	}
}

func TestPerUserCaps(t *testing.T) {
	m := New(Config{Slots: 3, QueueSize: 100, MaxQueuedPerPerson: 2})
	m.Submit("a", "1", false)
	m.Submit("a", "2", false)
	if _, err := m.Submit("a", "3", false); err != ErrUserCapped {
		t.Fatalf("err = %v, want ErrUserCapped", err)
	}
}

func TestConcurrentCapSkipsUser(t *testing.T) {
	// Two slots, but per-user concurrent cap of 1: user "a" must not run two
	// at once even though a second slot is free.
	m := New(Config{Slots: 2, QueueSize: 10, MaxTransfersPerPerson: 1})
	m.Submit("a", "a1", false)
	m.Submit("a", "a2", false)
	m.Submit("b", "b1", false)

	_, k1, r1 := m.Wait()
	if k1 != "a1" {
		t.Fatalf("k1 = %q", k1)
	}
	// Second pop must skip a's capped entry and take b's.
	_, k2, r2 := m.Wait()
	if k2 != "b1" {
		t.Fatalf("k2 = %q, want b1 (a capped)", k2)
	}
	r1()
	_, k3, r3 := m.Wait()
	if k3 != "a2" {
		t.Fatalf("k3 = %q, want a2", k3)
	}
	r2()
	r3()
}

func TestIdleQueuePromotion(t *testing.T) {
	m := New(Config{Slots: 1, QueueSize: 10, IdleQueueSize: 10})
	m.Submit("main", "M", false)
	m.Submit("idle", "I", true)

	// Main queue has priority: first pop is M.
	_, k, rel := m.Wait()
	if k != "M" {
		t.Fatalf("main key = %q, want M", k)
	}
	rel()
	// Now the idle entry is promoted.
	_, k, rel = m.Wait()
	if k != "I" {
		t.Fatalf("idle key = %q, want I", k)
	}
	rel()
}

func TestTryWait(t *testing.T) {
	m := New(Config{Slots: 0, QueueSize: 10}) // normalize makes Slots 1
	m.Submit("a", "1", false)
	_, k, rel, ok := m.TryWait()
	if !ok || k != "1" {
		t.Fatalf("TryWait ok=%v k=%q", ok, k)
	}
	rel()
	if _, _, _, ok := m.TryWait(); ok {
		t.Fatal("TryWait should fail with empty queue")
	}
}

func TestReleaseIdempotent(t *testing.T) {
	m := New(Config{Slots: 1, QueueSize: 10})
	m.Submit("a", "1", false)
	_, _, rel := m.Wait()
	rel()
	rel()
	if s := m.Snapshot(); s.FreeSlots != 1 {
		t.Fatalf("free slots = %d, want 1", s.FreeSlots)
	}
}

func TestCancel(t *testing.T) {
	m := New(Config{Slots: 1, QueueSize: 10})
	m.Submit("b", "2", false)
	if !m.Cancel("b", "2") {
		t.Fatal("cancel failed")
	}
	if m.Cancel("b", "2") {
		t.Fatal("cancel of absent item returned true")
	}
}

func TestConcurrencyRace(t *testing.T) {
	m := New(Config{Slots: 8, QueueSize: 10000})
	var wg sync.WaitGroup
	stop := make(chan struct{})
	var once sync.Once

	// Worker pool mimicking the bot: N workers each Wait() and "do work".
	const workers = 4
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				_, k, rel, ok := m.TryWait()
				if !ok {
					select {
					case <-stop:
						return
					default:
						time.Sleep(time.Millisecond)
						continue
					}
				}
				_ = k
				rel()
			}
		}()
	}

	// Submitters.
	const n = 400
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			user := "u" + string(rune('a'+i%26))
			key := string(rune('0' + i%10))
			idle := i%4 == 0
			m.Submit(user, key, idle)
			m.Cancel(user, key)
		}(i)
	}

	// Drain: wait for submitters to finish by a rough join.
	// (Race test ends once workers exit; give them a moment.)
	time.Sleep(50 * time.Millisecond)
	once.Do(func() { close(stop) })

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("deadlock: workers did not exit")
	}
	_ = m.Snapshot()
}
