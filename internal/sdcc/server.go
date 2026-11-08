// Package sdcc implements the Secure DCC Chat (SDCC) server: the bot-side
// administrator console served over the single shared TLS listener (SDCC.md).
package sdcc

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jenga201/dinoga/internal/token"
)

// maxLineLen mirrors SXDCC.md §9.3.
const maxLineLen = 8192

// AdminChatter validates an admin credential and runs the interactive admin
// console over an authenticated stream. internal/admin.Server implements it.
type AdminChatter interface {
	CheckPassword(candidate string) bool
	ServeChat(r *bufio.Reader, w io.Writer)
}

// Server is the bot-side SDCC chat server.
type Server struct {
	adm    AdminChatter
	tokens *token.Store

	addrMu        sync.Mutex
	ipInt         uint32
	advertisePort int
	pin           string
}

// New builds an SDCC server. ipInt is the advertised IPv4 as a DCC 32-bit
// integer and pin is the bot's SPKI pin — both shared with the SXDCC identity
// that owns the single listener.
func New(adm AdminChatter, ipInt uint32, pin string) *Server {
	s := &Server{
		adm:    adm,
		tokens: token.New(120 * time.Second),
		ipInt:  ipInt,
		pin:    pin,
	}
	go s.pruneLoop()
	return s
}

func (s *Server) pruneLoop() {
	t := time.NewTicker(time.Minute)
	for range t.C {
		s.tokens.Prune()
	}
}

// BuildSDCCRendezvous renders the SDCC bot → client invitation:
//
//	SDCC CONNECT <ip> <port> <token> <pin>
func BuildSDCCRendezvous(ipInt uint32, port int, token, pin string) string {
	return fmt.Sprintf("SDCC CONNECT %d %d %s %s", ipInt, port, token, pin)
}

// ParseSDCCRendezvous parses an SDCC invitation line back into its fields.
func ParseSDCCRendezvous(line string) (ipInt uint32, port int, token, pin string, err error) {
	f := strings.Fields(line)
	if len(f) != 6 || !strings.EqualFold(f[0], "SDCC") || !strings.EqualFold(f[1], "CONNECT") {
		return 0, 0, "", "", fmt.Errorf("sdcc: malformed rendezvous %q", line)
	}
	v, err := strconv.ParseUint(f[2], 10, 32)
	if err != nil {
		return 0, 0, "", "", fmt.Errorf("sdcc: bad ip field %q", f[2])
	}
	p, err := strconv.Atoi(f[3])
	if err != nil {
		return 0, 0, "", "", fmt.Errorf("sdcc: bad port field %q", f[3])
	}
	return uint32(v), p, f[4], f[5], nil
}

// SetAdvertise updates the address (and SPKI pin) advertised in SDCC
// invitations, mirroring the SXDCC identity that owns the shared listener.
func (s *Server) SetAdvertise(ipInt uint32, port int, pin string) {
	s.addrMu.Lock()
	s.ipInt = ipInt
	s.advertisePort = port
	s.pin = pin
	s.addrMu.Unlock()
}

// Rendezvous mints a rendezvous token and returns the SDCC invitation line and
// token, which the ADMIN first line must present.
func (s *Server) Rendezvous() (line, token string, err error) {
	token, err = s.tokens.Mint()
	if err != nil {
		return "", "", err
	}
	s.tokens.Add(token)
	s.addrMu.Lock()
	ip, port := s.ipInt, s.advertisePort
	s.addrMu.Unlock()
	return BuildSDCCRendezvous(ip, port, token, s.pin), token, nil
}

// HandleConn handles an SDCC chat connection whose first line was
// "ADMIN <token>". conn is the already-TLS'd connection accepted on the shared
// listener; the TLS handshake and first-line dispatch belong to the caller.
func (s *Server) HandleConn(conn net.Conn, r *bufio.Reader, firstLine string) {
	f := strings.Fields(firstLine)
	if len(f) != 2 || !strings.EqualFold(f[0], "ADMIN") {
		fmt.Fprintf(conn, "ERR 400 bad sdcc binding\r\n")
		return
	}
	if !s.tokens.Consume(f[1]) {
		fmt.Fprintf(conn, "ERR 401 unauthorized\r\n")
		return
	}
	if s.adm == nil {
		fmt.Fprintf(conn, "ERR 501 admin console not configured\r\n")
		return
	}

	fmt.Fprintf(conn, "PASS\r\n")
	line, err := readLine(r)
	if err != nil {
		return
	}
	password := strings.TrimSpace(line)
	if pw := strings.ToUpper(password); pw == "PASS" {
		password = ""
	} else if strings.HasPrefix(pw, "PASS ") {
		password = strings.TrimSpace(password[len("PASS "):])
	}
	if !s.adm.CheckPassword(password) {
		fmt.Fprintf(conn, "ERR 401 unauthorized\r\n")
		return
	}
	s.adm.ServeChat(r, conn)
}

func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	if len(line) > maxLineLen {
		return "", errors.New("sdcc: control line too long")
	}
	return strings.TrimRight(line, "\r\n"), nil
}
