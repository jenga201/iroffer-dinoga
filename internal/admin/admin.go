// Package admin implements the telnet administration interface: a
// password-protected command loop with a registry of commands that the
// integrator can extend.
package admin

import (
	"bufio"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// ErrQuit is returned by a command to request that the connection be closed.
var ErrQuit = errors.New("admin: quit")

// Config configures the telnet administration server.
type Config struct {
	// Password is the plaintext password to accept. Ignored when PasswordHash
	// is set.
	Password string
	// PasswordHash is the hex-encoded SHA-256 digest of the password. When set,
	// a candidate password is hashed with SHA-256 and the digests are compared
	// in constant time.
	PasswordHash string
	// PackCount, when non-nil, supplies the current number of packs for the
	// "status" command.
	PackCount func() int
	// PackNames, when non-nil, supplies the ordered pack names for the "list"
	// command.
	PackNames func() []string
	// OnBroadcast is optionally called with a message broadcast through the
	// administration interface.
	OnBroadcast func(string)
	// Motd is optional text written to the client before the password prompt.
	Motd string
}

// Command is a single administration command.
type Command struct {
	// Name is the command word, matched case-insensitively.
	Name string
	// Usage is a short usage string shown by "help".
	Usage string
	// Help is a one-line description.
	Help string
	// Run executes the command. args holds the whitespace-separated arguments
	// after the command name. Returning ErrQuit closes the connection.
	Run func(out io.Writer, args []string) error
}

// Registry holds the commands recognized by the administration interface.
type Registry struct {
	mu sync.RWMutex
	m  map[string]Command
}

// NewRegistry returns an empty command registry.
func NewRegistry() *Registry {
	return &Registry{m: make(map[string]Command)}
}

// Register adds or replaces a command. It returns an error if the command has
// no name.
func (r *Registry) Register(c Command) error {
	if strings.TrimSpace(c.Name) == "" {
		return errors.New("admin: command name is empty")
	}
	r.mu.Lock()
	r.m[nameKey(c.Name)] = c
	r.mu.Unlock()
	return nil
}

// Lookup finds a command by name, case-insensitively.
func (r *Registry) Lookup(name string) (Command, bool) {
	r.mu.RLock()
	c, ok := r.m[nameKey(name)]
	r.mu.RUnlock()
	return c, ok
}

// Commands returns the registered commands, sorted by name.
func (r *Registry) Commands() []Command {
	r.mu.RLock()
	out := make([]Command, 0, len(r.m))
	for _, c := range r.m {
		out = append(out, c)
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func nameKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// Server is the telnet administration server.
type Server struct {
	cfg Config
	reg *Registry
}

// New creates a Server with the built-in commands (status, list, quit, help)
// pre-registered. Additional commands can be added with Register.
func New(cfg Config) *Server {
	s := &Server{cfg: cfg, reg: NewRegistry()}
	_ = s.reg.Register(Command{
		Name:  "status",
		Usage: "status",
		Help:  "Show the current pack count.",
		Run:   s.cmdStatus,
	})
	_ = s.reg.Register(Command{
		Name:  "list",
		Usage: "list [n]",
		Help:  "List the first n pack names (default 10).",
		Run:   s.cmdList,
	})
	_ = s.reg.Register(Command{
		Name:  "quit",
		Usage: "quit",
		Help:  "Close the connection.",
		Run:   s.cmdQuit,
	})
	_ = s.reg.Register(Command{
		Name:  "help",
		Usage: "help",
		Help:  "List the available commands.",
		Run:   s.cmdHelp,
	})
	return s
}

// Register adds a command to the server's registry.
func (s *Server) Register(c Command) error {
	return s.reg.Register(c)
}

// Commands returns the server's registered commands, sorted by name.
func (s *Server) Commands() []Command {
	return s.reg.Commands()
}

// Registry exposes the underlying command registry.
func (s *Server) Registry() *Registry {
	return s.reg
}

// Serve accepts connections on ln until it fails. Each connection is handled
// in its own goroutine: prompt for the password, then run the command loop.
func (s *Server) Serve(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go s.handle(conn)
	}
}

// checkPassword verifies a candidate against the configured credential.
func (s *Server) checkPassword(candidate string) bool {
	if s.cfg.PasswordHash != "" {
		sum := sha256.Sum256([]byte(candidate))
		got := fmt.Sprintf("%x", sum)
		return subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.PasswordHash)) == 1
	}
	// No credential configured: refuse everyone rather than opening the door.
	if s.cfg.Password == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(s.cfg.Password)) == 1
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()

	if s.cfg.Motd != "" {
		s.writeMotd(conn)
	}

	br := bufio.NewReader(conn)
	fmt.Fprint(conn, "Password: \r\n")

	pwLine, err := br.ReadString('\n')
	if err != nil {
		return
	}
	if !s.CheckPassword(trimLine(pwLine)) {
		fmt.Fprint(conn, "Access denied\r\n")
		return
	}
	fmt.Fprint(conn, "Welcome\r\n")

	s.loop(br, conn)
}

func (s *Server) writeMotd(w io.Writer) {
	for _, line := range strings.Split(s.cfg.Motd, "\n") {
		fmt.Fprintf(w, "%s\r\n", strings.TrimRight(line, "\r"))
	}
}

// loop runs the interactive command loop — prompt, read, dispatch, repeat —
// until a command returns ErrQuit or the stream closes. It is shared by the
// telnet frontend (handle) and the SDCC chat frontend (ServeChat).
func (s *Server) loop(r *bufio.Reader, w io.Writer) {
	for {
		fmt.Fprint(w, "> \r\n")
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		fields := strings.Fields(trimLine(line))
		if len(fields) == 0 {
			continue
		}
		cmd, ok := s.reg.Lookup(fields[0])
		if !ok {
			fmt.Fprintf(w, "unknown command: %s\r\n", fields[0])
			continue
		}
		if cmd.Run == nil {
			fmt.Fprintf(w, "command %s has no handler\r\n", cmd.Name)
			continue
		}
		if err := cmd.Run(w, fields[1:]); err != nil {
			if err == ErrQuit {
				return
			}
			fmt.Fprintf(w, "error: %v\r\n", err)
		}
	}
}

// CheckPassword verifies a candidate against the configured credential in
// constant time. It is exported so the SXDCC server can gate SDCC chat on the
// same admin credential used by the telnet frontend.
func (s *Server) CheckPassword(candidate string) bool {
	return s.checkPassword(candidate)
}

// ServeChat runs the interactive admin console on an already-authenticated
// stream. It is the SDCC entry point: the caller has already verified the admin
// credential, so no password prompt is shown and the first line written to the
// client is WELCOME.
func (s *Server) ServeChat(r *bufio.Reader, w io.Writer) {
	if s.cfg.Motd != "" {
		s.writeMotd(w)
	}
	fmt.Fprint(w, "WELCOME\r\n")
	s.loop(r, w)
}

func trimLine(s string) string {
	return strings.TrimRight(s, "\r\n")
}

func (s *Server) cmdStatus(out io.Writer, args []string) error {
	n := 0
	if s.cfg.PackCount != nil {
		n = s.cfg.PackCount()
	}
	fmt.Fprintf(out, "packs: %d\r\n", n)
	return nil
}

func (s *Server) cmdList(out io.Writer, args []string) error {
	n := 10
	if len(args) > 0 {
		if v, err := strconv.Atoi(args[0]); err == nil && v >= 0 {
			n = v
		}
	}
	if s.cfg.PackNames == nil {
		return nil
	}
	names := s.cfg.PackNames()
	if n > len(names) {
		n = len(names)
	}
	for i := 0; i < n; i++ {
		fmt.Fprintf(out, "%d. %s\r\n", i+1, names[i])
	}
	return nil
}

func (s *Server) cmdQuit(out io.Writer, args []string) error {
	fmt.Fprint(out, "bye\r\n")
	return ErrQuit
}

func (s *Server) cmdHelp(out io.Writer, args []string) error {
	for _, c := range s.reg.Commands() {
		fmt.Fprintf(out, "%-8s %s\r\n", c.Name, c.Usage)
	}
	return nil
}
