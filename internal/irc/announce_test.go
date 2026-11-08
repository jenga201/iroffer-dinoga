package irc

import (
	"bufio"
	"net"
	"testing"

	"github.com/jenga201/dinoga/internal/config"
	"github.com/jenga201/dinoga/internal/packs"
)

func TestNewPacks(t *testing.T) {
	prev := []packs.Pack{{Path: "/d/a.bin"}, {Path: "/d/b.bin"}}
	next := []packs.Pack{{Path: "/d/a.bin"}, {Path: "/d/c.bin"}, {Path: "/d/d.bin"}}
	got := newPacks(prev, next)
	if len(got) != 2 || got[0].Path != "/d/c.bin" || got[1].Path != "/d/d.bin" {
		t.Fatalf("newPacks = %+v", got)
	}
}

func TestIsXDCCCommand(t *testing.T) {
	for _, c := range []struct {
		in   string
		want bool
	}{
		{"xdcc list", true},
		{"XDCC SEND #1", true},
		{"  xdcc info #2", true},
		{"sdcc help", false},
		{"hello", false},
	} {
		if got := isXDCCCommand(c.in); got != c.want {
			t.Errorf("isXDCCCommand(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestBroadcast(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	c := New(config.Config{Channel: "#frog"}, nil)
	nc := &netConn{c: c, net: config.Network{Channel: "#frog"}, conn: server}
	c.addNet(nc)

	go c.broadcast("hello there")

	br := bufio.NewReader(client)
	line, err := br.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if want := "PRIVMSG #frog :hello there\r\n"; line != want {
		t.Fatalf("broadcast = %q, want %q", line, want)
	}
}
