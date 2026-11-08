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

// start runs srv.ServeChat (the chat/TUI admin console entry point) over an
// accepted TCP connection and dials back in. prepare runs before the accept so
// tests can register extra commands without racing the loop.
func start(t *testing.T, cfg admin.Config, prepare func(*admin.Server)) (net.Conn, *bufio.Reader) {
	t.Helper()

	srv := admin.New(cfg)
	if prepare != nil {
		prepare(srv)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		srv.ServeChat(bufio.NewReader(conn), conn)
	}()

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

func TestCheckPassword(t *testing.T) {
	srv := admin.New(admin.Config{Password: "hunter2"})
	if !srv.CheckPassword("hunter2") {
		t.Fatal("correct password rejected")
	}
	if srv.CheckPassword("wrong") {
		t.Fatal("wrong password accepted")
	}
}

func TestCheckPasswordHash(t *testing.T) {
	sum := sha256.Sum256([]byte("secret"))
	srv := admin.New(admin.Config{PasswordHash: hex.EncodeToString(sum[:])})
	if !srv.CheckPassword("secret") {
		t.Fatal("correct password rejected")
	}
	if srv.CheckPassword("wrong") {
		t.Fatal("wrong password accepted")
	}
}

func TestCheckPasswordNoCredential(t *testing.T) {
	// With no credential configured, everyone is refused.
	if admin.New(admin.Config{}).CheckPassword("anything") {
		t.Fatal("should refuse when no credential is configured")
	}
}

func TestChatStatusAndQuit(t *testing.T) {
	conn, r := start(t, admin.Config{PackCount: func() int { return 3 }}, nil)
	if got := readLine(t, r); got != "WELCOME" {
		t.Fatalf("welcome = %q", got)
	}
	write(t, conn, "status\r\n")
	expect(t, r, "packs: 3")
	write(t, conn, "quit\r\n")
	expect(t, r, "bye")
}

func TestChatList(t *testing.T) {
	conn, r := start(t, admin.Config{
		PackNames: func() []string { return []string{"a.txt", "b.txt", "c.txt"} },
	}, nil)
	readLine(t, r) // WELCOME
	write(t, conn, "list 2\r\n")
	expect(t, r, "1. a.txt")
	expect(t, r, "2. b.txt")
	write(t, conn, "quit\r\n")
	expect(t, r, "bye")
}

func TestChatCustomCommand(t *testing.T) {
	conn, r := start(t, admin.Config{}, func(srv *admin.Server) {
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
	readLine(t, r) // WELCOME
	write(t, conn, "ping\r\n")
	expect(t, r, "pong")
	write(t, conn, "quit\r\n")
	expect(t, r, "bye")
}
