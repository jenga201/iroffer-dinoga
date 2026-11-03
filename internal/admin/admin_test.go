package admin_test

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/jenga201/dinoga/internal/admin"
)

// start listens on an ephemeral port, serves in the background, and dials back
// in. prepare runs (with the server in hand, before Serve) so tests can
// register extra commands without racing the accept loop.
func start(t *testing.T, cfg admin.Config, prepare func(*admin.Server)) (net.Conn, *bufio.Reader) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	srv := admin.New(cfg)
	if prepare != nil {
		prepare(srv)
	}
	go srv.Serve(ln)

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	return conn, bufio.NewReader(conn)
}

func readLine(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	line, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return strings.TrimRight(line, "\r\n")
}

func write(t *testing.T, conn net.Conn, s string) {
	t.Helper()
	if _, err := conn.Write([]byte(s)); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// expect reads the next non-prompt line and requires it to equal want.
func expect(t *testing.T, r *bufio.Reader, want string) {
	t.Helper()
	line := readLine(t, r)
	for line == "> " {
		line = readLine(t, r)
	}
	if line != want {
		t.Fatalf("got %q, want %q", line, want)
	}
}

func TestAuthSuccess(t *testing.T) {
	conn, r := start(t, admin.Config{
		Password:  "hunter2",
		PackCount: func() int { return 3 },
	}, nil)

	if got := readLine(t, r); got != "Password: " {
		t.Fatalf("prompt = %q", got)
	}
	write(t, conn, "hunter2\r\n")
	expect(t, r, "Welcome")

	write(t, conn, "status\r\n")
	expect(t, r, "packs: 3")

	write(t, conn, "quit\r\n")
	expect(t, r, "bye")
}

func TestAuthFailure(t *testing.T) {
	conn, r := start(t, admin.Config{Password: "hunter2"}, nil)

	if got := readLine(t, r); got != "Password: " {
		t.Fatalf("prompt = %q", got)
	}
	write(t, conn, "wrong\r\n")
	if got := readLine(t, r); got != "Access denied" {
		t.Fatalf("denied = %q", got)
	}
	// The server closes the connection after a failed authentication.
	if _, err := r.ReadString('\n'); err == nil {
		t.Fatal("expected connection close after failed auth")
	}
}

func TestHashAuth(t *testing.T) {
	sum := sha256.Sum256([]byte("secret"))
	hash := hex.EncodeToString(sum[:])

	conn, r := start(t, admin.Config{
		PasswordHash: hash,
		PackCount:    func() int { return 1 },
	}, nil)

	if got := readLine(t, r); got != "Password: " {
		t.Fatalf("prompt = %q", got)
	}
	write(t, conn, "secret\r\n")
	expect(t, r, "Welcome")
}

func TestListCommand(t *testing.T) {
	conn, r := start(t, admin.Config{
		Password:  "pw",
		PackNames: func() []string { return []string{"a.txt", "b.txt", "c.txt"} },
	}, nil)

	readLine(t, r) // Password:
	write(t, conn, "pw\r\n")
	expect(t, r, "Welcome")

	write(t, conn, "list 2\r\n")
	expect(t, r, "1. a.txt")
	expect(t, r, "2. b.txt")

	write(t, conn, "quit\r\n")
	expect(t, r, "bye")
}

func TestCustomCommand(t *testing.T) {
	conn, r := start(t, admin.Config{Password: "pw"}, func(srv *admin.Server) {
		if err := srv.Register(admin.Command{
			Name:  "ping",
			Usage: "ping",
			Help:  "Reply pong.",
			Run: func(out io.Writer, args []string) error {
				fmt.Fprint(out, "pong\r\n")
				return nil
			},
		}); err != nil {
			t.Fatal(err)
		}
	})

	readLine(t, r) // Password:
	write(t, conn, "pw\r\n")
	expect(t, r, "Welcome")

	write(t, conn, "ping\r\n")
	expect(t, r, "pong")

	write(t, conn, "quit\r\n")
	expect(t, r, "bye")
}
