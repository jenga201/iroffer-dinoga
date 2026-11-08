package token

import (
	"testing"
	"time"
)

func TestStore(t *testing.T) {
	s := New(50 * time.Millisecond)
	tok, err := s.Mint()
	if err != nil {
		t.Fatal(err)
	}
	s.Add(tok)
	if !s.Consume(tok) {
		t.Fatal("first consume should succeed")
	}
	if s.Consume(tok) {
		t.Fatal("replay should fail")
	}

	tok2, _ := s.Mint()
	s.Add(tok2)
	time.Sleep(60 * time.Millisecond)
	if s.Consume(tok2) {
		t.Fatal("expired token should fail")
	}
}