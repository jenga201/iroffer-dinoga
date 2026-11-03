package irc

import (
	"testing"
	"time"
)

func TestReconnectDelay(t *testing.T) {
	base := 10 * time.Second
	cases := map[int]time.Duration{
		1: 10 * time.Second,
		2: 20 * time.Second,
		3: 40 * time.Second,
		4: 80 * time.Second,
	}
	for failures, want := range cases {
		if got := reconnectDelay(base, failures); got != want {
			t.Fatalf("reconnectDelay(%d) = %v, want %v", failures, got, want)
		}
	}
	if got := reconnectDelay(base, 100); got != maxReconnectDelay {
		t.Fatalf("reconnectDelay(100) = %v, want cap %v", got, maxReconnectDelay)
	}
}
