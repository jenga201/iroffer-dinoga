package irc

import "testing"

func TestPrefixModes(t *testing.T) {
	cases := []struct {
		in     string
		nick   string
		level  int
	}{
		{"nick", "nick", 0},
		{"+nick", "nick", 1},
		{"%nick", "nick", 2},
		{"@nick", "nick", 3},
		{"&nick", "nick", 3},
		{"~nick", "nick", 3},
		{"@+nick", "nick", 3},
	}
	for _, c := range cases {
		nick, m := prefixModes(c.in)
		if nick != c.nick || levelOf(m) != c.level {
			t.Fatalf("prefixModes(%q) = (%q, level %d), want (%q, %d)", c.in, nick, levelOf(m), c.nick, c.level)
		}
	}
}

func TestApplyNAMES(t *testing.T) {
	m := newChannelModes()
	m.applyNAMES("dinoga = #frog :@op +leaf %half plain")

	if !m.member("op") || !m.member("leaf") || !m.member("half") || !m.member("plain") {
		t.Fatal("all NAMES members should be tracked")
	}
	if m.level("op") != 3 || m.level("half") != 2 || m.level("leaf") != 1 || m.level("plain") != 0 {
		t.Fatalf("levels = op %d half %d leaf %d plain %d", m.level("op"), m.level("half"), m.level("leaf"), m.level("plain"))
	}
}

func TestApplyMode(t *testing.T) {
	m := newChannelModes()
	m.applyNAMES("dinoga = #frog :@op +leaf plain")

	// Promote leaf to op (+o), demote op to plain (-o).
	m.applyMode("#frog +o leaf")
	m.applyMode("#frog -o op")
	if m.level("leaf") != 3 {
		t.Fatalf("leaf should be op, level = %d", m.level("leaf"))
	}
	if m.level("op") != 0 {
		t.Fatalf("op should be demoted, level = %d", m.level("op"))
	}

	// Voice (+v) removes on -v.
	m.applyMode("#frog +v plain")
	m.applyMode("#frog -v plain")
	if m.level("plain") != 0 {
		t.Fatalf("plain voice should be cleared, level = %d", m.level("plain"))
	}
}

func TestMembershipChanges(t *testing.T) {
	m := newChannelModes()
	m.add("alice")
	if !m.member("alice") {
		t.Fatal("alice should be a member")
	}
	m.remove("alice")
	if m.member("alice") {
		t.Fatal("alice should be removed")
	}
	if m.level("alice") != 0 {
		t.Fatalf("removed member level = %d, want 0", m.level("alice"))
	}
}