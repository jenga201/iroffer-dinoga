package irc

import (
	"testing"

	"github.com/jenga201/dinoga/internal/config"
)

func TestSASLPlain(t *testing.T) {
	// RFC 4616 / IRCv3 SASL reference vector: jilles / sesame.
	want := "amlsbGVzAGppbGxlcwBzZXNhbWU="
	if got := saslPlain("jilles", "sesame"); got != want {
		t.Fatalf("saslPlain = %q, want %q", got, want)
	}
}

func TestSASLAccountDefaultsToNick(t *testing.T) {
	nc := &netConn{net: config.Network{SASLUser: "", SASLPass: "pw", Nick: "bob"}}
	if nc.saslAccount() != "bob" {
		t.Fatalf("saslAccount = %q, want bob (default to nick)", nc.saslAccount())
	}
	nc2 := &netConn{net: config.Network{SASLUser: "acct", SASLPass: "pw", Nick: "bob"}}
	if nc2.saslAccount() != "acct" {
		t.Fatalf("saslAccount = %q, want acct", nc2.saslAccount())
	}
}
