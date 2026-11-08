package sdcc

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
)

type fakeAdmin struct {
	pw     string
	lastPW string
	served bool
}

func (a *fakeAdmin) CheckPassword(candidate string) bool {
	a.lastPW = candidate
	return candidate == a.pw
}

func (a *fakeAdmin) ServeChat(r *bufio.Reader, w io.Writer) {
	a.served = true
	if _, err := fmt.Fprint(w, "WELCOME\r\n"); err != nil {
		return
	}
	if _, err := fmt.Fprint(w, "> \r\n"); err != nil {
		return
	}
	line, err := r.ReadString('\n')
	if err != nil {
		return
	}
	fmt.Fprintf(w, "echo: %s\r\n", strings.TrimRight(line, "\r\n"))
	fmt.Fprint(w, "> \r\n")
}

// startServer runs an SDCC server over plain TCP (TLS is the dispatcher's job).
func startServer(t *testing.T, adm AdminChatter) (*Server, net.Listener) {
	t.Helper()
	s := New(adm, 2130706433, "sha256:test")
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				r := bufio.NewReader(conn)
				line, err := r.ReadString('\n')
				if err != nil {
					return
				}
				s.HandleConn(conn, r, strings.TrimRight(line, "\r\n"))
			}()
		}
	}()
	return s, ln
}

func TestRendezvousAndChat(t *testing.T) {
	admin := &fakeAdmin{pw: "secret"}
	s, ln := startServer(t, admin)

	port := ln.Addr().(*net.TCPAddr).Port
	s.SetAdvertise(2130706433, port, "sha256:test")
	line, token, err := s.Rendezvous()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(line, "SDCC CONNECT 2130706433 ") ||
		!strings.Contains(line, token) ||
		!strings.Contains(line, "sha256:test") {
		t.Fatalf("rendezvous = %q", line)
	}

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	br := bufio.NewReader(conn)

	fmt.Fprintf(conn, "ADMIN %s\r\n", token)
	if l, _ := br.ReadString('\n'); strings.TrimRight(l, "\r\n") != "PASS" {
		t.Fatalf("want PASS, got %q", l)
	}

	fmt.Fprint(conn, "PASS secret\r\n")
	if l, _ := br.ReadString('\n'); strings.TrimRight(l, "\r\n") != "WELCOME" {
		t.Fatalf("want WELCOME, got %q", l)
	}
	if l, _ := br.ReadString('\n'); strings.TrimRight(l, "\r\n") != "> " {
		t.Fatalf("want prompt, got %q", l)
	}

	fmt.Fprint(conn, "status\r\n")
	if l, _ := br.ReadString('\n'); strings.TrimRight(l, "\r\n") != "echo: status" {
		t.Fatalf("want echo, got %q", l)
	}

	if admin.lastPW != "secret" || !admin.served {
		t.Fatalf("admin state = pw %q served %v", admin.lastPW, admin.served)
	}
}

func TestRejectsBadPassword(t *testing.T) {
	admin := &fakeAdmin{pw: "secret"}
	s, ln := startServer(t, admin)

	_, token, err := s.Rendezvous()
	if err != nil {
		t.Fatal(err)
	}

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	br := bufio.NewReader(conn)

	fmt.Fprintf(conn, "ADMIN %s\r\n", token)
	if l, _ := br.ReadString('\n'); strings.TrimRight(l, "\r\n") != "PASS" {
		t.Fatalf("want PASS, got %q", l)
	}
	fmt.Fprint(conn, "PASS wrong\r\n")
	l, _ := br.ReadString('\n')
	if !strings.HasPrefix(strings.TrimRight(l, "\r\n"), "ERR 401") {
		t.Fatalf("want ERR 401, got %q", l)
	}
	if admin.served {
		t.Fatal("admin console must not run on bad password")
	}
}
