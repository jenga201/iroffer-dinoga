package sxdcc

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

// tokenStore mints single-use, expiring, ≥128-bit CSPRNG tokens and validates
// each token at most once. It is safe for concurrent use.
type tokenStore struct {
	mu  sync.Mutex
	ttl time.Duration
	exp map[string]time.Time
}

func newTokenStore(ttl time.Duration) *tokenStore {
	return &tokenStore{ttl: ttl, exp: map[string]time.Time{}}
}

// mint returns a fresh base64url (no padding) token encoding 16 random bytes
// (128 bits).
func (s *tokenStore) mint() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// consume validates tok (well-formed, unexpired, unused) and consumes it.
// It returns true exactly once per valid token.
func (s *tokenStore) consume(tok string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.exp[tok]
	if !ok {
		return false
	}
	delete(s.exp, tok)
	return time.Now().Before(exp)
}

// add records a token minted externally, or is a no-op for empty tokens.
func (s *tokenStore) add(tok string) {
	if tok == "" {
		return
	}
	s.mu.Lock()
	s.exp[tok] = time.Now().Add(s.ttl)
	s.mu.Unlock()
}

// prune drops expired tokens; call periodically.
func (s *tokenStore) prune() {
	s.mu.Lock()
	now := time.Now()
	for k, exp := range s.exp {
		if now.After(exp) {
			delete(s.exp, k)
		}
	}
	s.mu.Unlock()
}
