// Package acl implements hostmask allow/deny lists for the bot's anti-abuse
// controls (iroffer's downloadhost / xdcc_allow / xdcc_deny). A hostmask is the
// full IRC `nick!user@host` string; patterns use `*` (any run) and `?` (one
// character) and are matched case-insensitively.
package acl

import "strings"

// Matcher checks hostmasks against allow and deny lists.
type Matcher struct {
	allow []string
	deny  []string
}

// New builds a Matcher. An empty allow list means "allow all" (subject to the
// deny list); a non-empty allow list means "allow only matches". Deny always
// wins over allow.
func New(allow, deny []string) *Matcher {
	return &Matcher{allow: clone(allow), deny: clone(deny)}
}

// Empty reports whether the matcher has no rules (everything is allowed).
func (m *Matcher) Empty() bool {
	return len(m.allow) == 0 && len(m.deny) == 0
}

// Allowed reports whether the hostmask may proceed.
func (m *Matcher) Allowed(hostmask string) bool {
	for _, p := range m.deny {
		if match(p, hostmask) {
			return false
		}
	}
	if len(m.allow) == 0 {
		return true
	}
	for _, p := range m.allow {
		if match(p, hostmask) {
			return true
		}
	}
	return false
}

func clone(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func match(pattern, hostmask string) bool {
	return globMatch(strings.ToLower(pattern), strings.ToLower(hostmask))
}

// globMatch performs a wildcard match with '*' (any run) and '?' (one byte).
func globMatch(pattern, s string) bool {
	pi, si := 0, 0
	star, mark := -1, 0
	for si < len(s) {
		if pi < len(pattern) && (pattern[pi] == '?' || pattern[pi] == s[si]) {
			pi++
			si++
		} else if pi < len(pattern) && pattern[pi] == '*' {
			star = pi
			pi++
			mark = si
		} else if star != -1 {
			pi = star + 1
			mark++
			si = mark
		} else {
			return false
		}
	}
	for pi < len(pattern) && pattern[pi] == '*' {
		pi++
	}
	return pi == len(pattern)
}
