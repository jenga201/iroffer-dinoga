package sxdcc

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jenga201/dinoga/internal/packs"
)

func TestIdentityAndPin(t *testing.T) {
	id, err := GenerateIdentity("127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	leaf := id.Cert.Leaf
	if leaf == nil {
		t.Fatal("generate identity leaves Leaf nil")
	}
	if len(leaf.IPAddresses) != 1 || leaf.IPAddresses[0].String() != "127.0.0.1" {
		t.Fatalf("IP SAN = %v", leaf.IPAddresses)
	}
	if !strings.HasPrefix(id.Pin, "sha256:") || len(id.Pin) != 7+43 {
		t.Fatalf("pin = %q (len %d)", id.Pin, len(id.Pin))
	}

	id2, _ := GenerateIdentity("127.0.0.1")
	if PinEqual(id.Pin, id2.Pin) {
		t.Fatal("different keys must yield different pins")
	}
}

func TestPinEqual(t *testing.T) {
	if !PinEqual("sha256:abcdefghijklmnopqrstuvwxyz0123456789-ABCD", "abcdefghijklmnopqrstuvwxyz0123456789-ABCD") {
		t.Fatal("PinEqual should ignore the sha256: prefix")
	}
	if PinEqual("sha256:aaaa", "sha256:bbbb") {
		t.Fatal("PinEqual should be false for different values")
	}
	if PinEqual("short", "longer-value") {
		t.Fatal("PinEqual should be false for different lengths")
	}
}

func TestTokenStore(t *testing.T) {
	s := newTokenStore(50 * time.Millisecond)
	tok, err := s.mint()
	if err != nil {
		t.Fatal(err)
	}
	s.add(tok)
	if !s.consume(tok) {
		t.Fatal("first consume should succeed")
	}
	if s.consume(tok) {
		t.Fatal("replay should fail")
	}

	tok2, _ := s.mint()
	s.add(tok2)
	time.Sleep(60 * time.Millisecond)
	if s.consume(tok2) {
		t.Fatal("expired token should fail")
	}
}

func TestParseRendezvousAndTrigger(t *testing.T) {
	line := BuildRendezvous(35605696, 40443, "tok", "sha256:abc")
	ip, port, tok, pin, err := ParseRendezvous(line)
	if err != nil {
		t.Fatal(err)
	}
	if ip != 35605696 || port != 40443 || tok != "tok" || pin != "sha256:abc" {
		t.Fatalf("parsed = %d %d %q %q", ip, port, tok, pin)
	}

	verb, args, ok := ParseTrigger("sxdcc send #1234")
	if !ok || verb != "send" || args != "#1234" {
		t.Fatalf("trigger = %q %q %v", verb, args, ok)
	}
	if _, _, ok := ParseTrigger("xdcc send #1"); ok {
		t.Fatal("should not parse xdcc as sxdcc")
	}
}

type stubHost struct {
	packs []packs.Pack
}

func (h *stubHost) Packlist() []packs.Pack {
	return h.packs
}
func (h *stubHost) PackByNumber(n int) (packs.Pack, bool) {
	if n < 1 || n > len(h.packs) {
		return packs.Pack{}, false
	}
	return h.packs[n-1], true
}
func (h *stubHost) OpenFile(p string) (io.ReadCloser, error) { return os.Open(p) }
func (h *stubHost) Logf(format string, args ...any)          {}

func TestControlAndDataRoundtrip(t *testing.T) {
	dir := t.TempDir()
	content := []byte("hello sxdcc data channel")
	file := filepath.Join(dir, "pack1.bin")
	if err := os.WriteFile(file, content, 0o600); err != nil {
		t.Fatal(err)
	}
	host := &stubHost{
		packs: []packs.Pack{
			{Number: 1, Name: "pack1.bin", Path: file, Size: int64(len(content))},
			{Number: 2, Name: "pack2.bin", Path: file, Size: int64(len(content))},
		},
	}
	s, err := New(host, Config{
		CertFile:  filepath.Join(dir, "cert.pem"),
		KeyFile:   filepath.Join(dir, "key.pem"),
		IP:        "127.0.0.1",
		PortStart: 45000,
		PortLimit: 45100,
	})
	if err != nil {
		t.Fatal(err)
	}

	rawLn, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer rawLn.Close()
	go s.Serve(rawLn)

	port := rawLn.Addr().(*net.TCPAddr).Port
	line, token, err := s.Rendezvous(port)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, s.Pin()) || !strings.Contains(line, token) {
		t.Fatalf("rendezvous missing pin/token: %q", line)
	}

	dialCfg := &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}
	conn, err := tls.Dial("tcp", rawLn.Addr().String(), dialCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Verify the presented cert matches the pin out-of-band.
	leafCert := conn.ConnectionState().PeerCertificates[0]
	if !PinEqual(SPKIPin(leafCert), s.Pin()) {
		t.Fatal("presented certificate SPKI does not match pin")
	}

	br := bufio.NewReader(conn)
	expectLine := func(write, wantPrefix string) string {
		t.Helper()
		if write != "" {
			if _, err := fmt.Fprintf(conn, write+"\r\n"); err != nil {
				t.Fatal(err)
			}
		}
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		line = strings.TrimRight(line, "\r\n")
		if !strings.HasPrefix(line, wantPrefix) {
			t.Fatalf("got %q, want prefix %q", line, wantPrefix)
		}
		return line
	}

	expectLine("AUTH "+token, "OK ")

	// LIST.
	if _, err := fmt.Fprintf(conn, "LIST\r\n"); err != nil {
		t.Fatal(err)
	}
	var listLines []string
	for {
		l, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("reading list: %v", err)
		}
		l = strings.TrimRight(l, "\r\n")
		if l == "OK" {
			break
		}
		listLines = append(listLines, l)
	}
	if len(listLines) != 2 || !strings.HasPrefix(listLines[0], "#1 ") {
		t.Fatalf("list lines = %v", listLines)
	}

	// SEND drives an active data transfer.
	fields := strings.Fields(expectLine("SEND 1", "SEND #1 "))
	if len(fields) != 7 {
		t.Fatalf("SEND fields = %v", fields)
	}
	dataPort, datatoken := fields[5], fields[6]

	dconn, err := tls.Dial("tcp", net.JoinHostPort("127.0.0.1", dataPort), dialCfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(dconn, "DATA %s\r\n", datatoken); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(dconn)
	dconn.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("data = %q, want %q", got, content)
	}

	// Control channel reports DONE.
	expectLine("", "DONE #1 ")
}

func TestPassiveDataRoundtrip(t *testing.T) {
	dir := t.TempDir()
	content := []byte("hello passive sxdcc")
	file := filepath.Join(dir, "p.bin")
	if err := os.WriteFile(file, content, 0o600); err != nil {
		t.Fatal(err)
	}
	host := &stubHost{packs: []packs.Pack{{Number: 1, Name: "p.bin", Path: file, Size: int64(len(content))}}}
	s, err := New(host, Config{
		CertFile:  filepath.Join(dir, "cert.pem"),
		KeyFile:   filepath.Join(dir, "key.pem"),
		IP:        "127.0.0.1",
		PortStart: 46000,
		PortLimit: 46100,
		Passive:   true,
	})
	if err != nil {
		t.Fatal(err)
	}

	rawLn, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer rawLn.Close()
	go s.Serve(rawLn)

	dialCfg := &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}
	conn, err := tls.Dial("tcp", rawLn.Addr().String(), dialCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	br := bufio.NewReader(conn)
	expectLine := func(write, wantPrefix string) string {
		t.Helper()
		if write != "" {
			if _, err := fmt.Fprintf(conn, write+"\r\n"); err != nil {
				t.Fatal(err)
			}
		}
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		line = strings.TrimRight(line, "\r\n")
		if !strings.HasPrefix(line, wantPrefix) {
			t.Fatalf("got %q, want prefix %q", line, wantPrefix)
		}
		return line
	}

	// Rendezvous + AUTH.
	port := rawLn.Addr().(*net.TCPAddr).Port
	line, token, err := s.Rendezvous(port)
	if err != nil {
		t.Fatal(err)
	}
	_ = line
	expectLine("AUTH "+token, "OK ")

	// SEND returns the passive (port 0) form.
	sendLine := expectLine("SEND 1", "SEND #1 ")
	fields := strings.Fields(sendLine)
	// SEND #1 <size> "<name>" <ip> 0 <datatoken>
	if len(fields) != 7 || fields[5] != "0" {
		t.Fatalf("passive SEND fields = %v", fields)
	}
	datatoken := fields[6]

	// Client listens; tell the bot to dial us.
	cLn, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer cLn.Close()
	cPort := cLn.Addr().(*net.TCPAddr).Port
	// Tell the bot to dial our passive listener (no sync reply expected yet).
	if _, err := fmt.Fprintf(conn, "ACCEPT %s 2130706433 %d\r\n", datatoken, cPort); err != nil {
		t.Fatal(err)
	}

	// Accept the bot's dial; the client is the TLS client (verifies pin).
	raw, err := cLn.Accept()
	if err != nil {
		t.Fatal(err)
	}
	tlsClient := tls.Client(raw, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13})
	if err := tlsClient.Handshake(); err != nil {
		t.Fatal(err)
	}
	// Verify the bot's presented cert matches the pin out-of-band.
	leaf := tlsClient.ConnectionState().PeerCertificates[0]
	if !PinEqual(SPKIPin(leaf), s.Pin()) {
		t.Fatal("passive data peer cert does not match bot pin")
	}

	dbr := bufio.NewReader(tlsClient)
	bindLine, err := dbr.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	bindLine = strings.TrimRight(bindLine, "\r\n")
	bf := strings.Fields(bindLine)
	if len(bf) != 2 || !strings.EqualFold(bf[0], "DATA") || bf[1] != datatoken {
		t.Fatalf("passive DATA binding = %q", bindLine)
	}

	got, err := io.ReadAll(dbr)
	if err != nil {
		t.Fatal(err)
	}
	tlsClient.Close()
	if string(got) != string(content) {
		t.Fatalf("passive data = %q, want %q", got, content)
	}

	// Control channel reports DONE.
	expectLine("", "DONE #1 ")
}
