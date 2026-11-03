package acl

import "testing"

func TestEmptyAllowsAll(t *testing.T) {
	m := New(nil, nil)
	if !m.Empty() {
		t.Fatal("empty matcher should be Empty")
	}
	if !m.Allowed("nick!user@example.com") {
		t.Fatal("empty matcher should allow")
	}
}

func TestAllowList(t *testing.T) {
	m := New([]string{"*!*@*.good.com"}, nil)
	if !m.Allowed("bob!bob@gw.good.com") {
		t.Fatal("allow match should pass")
	}
	if m.Allowed("bob!bob@evil.net") {
		t.Fatal("non-matching host should be denied when allow list is set")
	}
}

func TestDenyWins(t *testing.T) {
	m := New([]string{"*!*@*"}, []string{"*!*@banned.net"})
	if m.Allowed("x!y@banned.net") {
		t.Fatal("deny must win over allow")
	}
	if !m.Allowed("x!y@fine.net") {
		t.Fatal("allow should still pass for non-denied hosts")
	}
}

func TestGlobMatch(t *testing.T) {
	cases := []struct {
		pattern, s string
		want       bool
	}{
		{"*!*@host", "nick!user@host", true},
		{"*!*@*.com", "nick!user@sub.example.com", true},
		{"*!*@*.com", "nick!user@example.org", false},
		{"nick!*@*", "nick!user@host", true},
		{"other!*@*", "nick!user@host", false},
		{"*!u?er@*", "nick!user@host", true},
	}
	for _, c := range cases {
		if got := globMatch(c.pattern, c.s); got != c.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", c.pattern, c.s, got, c.want)
		}
	}
}

func TestCaseInsensitive(t *testing.T) {
	m := New([]string{"*!*@EXAMPLE.com"}, nil)
	if !m.Allowed("NICK!USER@example.com") {
		t.Fatal("hostmask match should be case-insensitive")
	}
}
