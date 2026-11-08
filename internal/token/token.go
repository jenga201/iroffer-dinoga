// Package token provides a single-use, expiring CSPRNG token store shared by
// the SXDCC (file transfer) and SDCC (chat) servers.
package token

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

// Store mints single-use, expiring, ≥128-bit CSPRNG tokens and validates each
// token at most once. It is safe for concurrent use.
type Store struct {
	mu  sync.Mutex
	ttl time.Duration
	exp map[string]time.Time
}

// New returns a Store whose tokens expire after ttl.
func New(ttl time.Duration) *Store {
	return &Store{ttl: ttl, exp: map[string]time.Time{}}
}

// Mint returns a fresh base64url (no padding) token encoding 16 random bytes
// (128 bits).
func (s *Store) Mint() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// Consume validates tok (well-formed, unexpired, unused) and consumes it.
// It returns true exactly once per valid token.
func (s *Store) Consume(tok string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.exp[tok]
	if !ok {
		return false
	}
	delete(s.exp, tok)
	return time.Now().Before(exp)
}

// Add records a token minted externally, or is a no-op for empty tokens.
func (s *Store) Add(tok string) {
	if tok == "" {
		return
	}
	s.mu.Lock()
	s.exp[tok] = time.Now().Add(s.ttl)
	s.mu.Unlock()
}

// Prune drops expired tokens; call periodically.
func (s *Store) Prune() {
	s.mu.Lock()
	now := time.Now()
	for k, exp := range s.exp {
		if now.After(exp) {
			delete(s.exp, k)
		}
	}
	s.mu.Unlock()
}