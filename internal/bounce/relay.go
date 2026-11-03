package bounce

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Relay is the public bounce server. It binds a tunnel endpoint (which the bot
// dials out to) and a set of public client listeners. Each client connection is
// paired with a reverse "JOIN" connection from the bot and the bytes are
// forwarded bidirectionally without inspection.
type Relay struct {
	secret string

	mu      sync.Mutex
	tunnels map[int]*tunnel // public port -> registered bot tunnel
	join    map[string]net.Conn
}

type tunnel struct {
	conn net.Conn
	mu   sync.Mutex
	w    *bufio.Writer
}

// NewRelay returns an unstarted Relay.
func NewRelay(secret string) *Relay {
	return &Relay{secret: secret, tunnels: map[int]*tunnel{}, join: map[string]net.Conn{}}
}

// ServeTunnel binds the tunnel endpoint and serves the bot's REGISTER/JOIN
// connections in a goroutine. It returns the bound address.
func (r *Relay) ServeTunnel(addr string) (net.Addr, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	go r.acceptLoop(ln, r.handleTunnel)
	return ln.Addr(), nil
}

// ServeClient binds one public client listener and serves accepted connections
// in a goroutine. It returns the bound address (use port 0 for an ephemeral
// port).
func (r *Relay) ServeClient(addr string) (net.Addr, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	go r.acceptLoop(ln, r.handleClient)
	return ln.Addr(), nil
}

func (r *Relay) acceptLoop(ln net.Listener, fn func(net.Conn)) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go fn(c)
	}
}

func (r *Relay) handleTunnel(c net.Conn) {
	br := bufio.NewReader(c)
	line, err := br.ReadString('\n')
	if err != nil {
		c.Close()
		return
	}
	f := strings.Fields(line)
	switch {
	case len(f) >= 3 && f[0] == "REGISTER":
		port, perr := strconv.Atoi(f[1])
		if perr != nil || f[2] != r.secret {
			fmt.Fprintln(c, "ERR")
			c.Close()
			return
		}
		r.register(port, c)
		io.Copy(io.Discard, br) // block until the bot drops this tunnel
		r.unregister(port, c)
		c.Close()
	case len(f) == 2 && f[0] == "JOIN":
		r.attach(f[1], c)
	default:
		c.Close()
	}
}

func (r *Relay) register(port int, c net.Conn) {
	t := &tunnel{conn: c, w: bufio.NewWriter(c)}
	r.mu.Lock()
	r.tunnels[port] = t
	r.mu.Unlock()
	fmt.Fprintln(c, "OK")
}

func (r *Relay) unregister(port int, c net.Conn) {
	r.mu.Lock()
	if t := r.tunnels[port]; t != nil && t.conn == c {
		delete(r.tunnels, port)
	}
	r.mu.Unlock()
}

func (r *Relay) handleClient(c net.Conn) {
	port := c.LocalAddr().(*net.TCPAddr).Port

	r.mu.Lock()
	t := r.tunnels[port]
	r.mu.Unlock()
	if t == nil {
		c.Close()
		return
	}

	sid := newSID()
	if err := r.writeDial(t, sid); err != nil {
		c.Close()
		return
	}
	r.mu.Lock()
	r.join[sid] = c
	r.mu.Unlock()

	time.AfterFunc(30*time.Second, func() {
		r.mu.Lock()
		if r.join[sid] == c {
			delete(r.join, sid)
			c.Close()
		}
		r.mu.Unlock()
	})
}

func (r *Relay) writeDial(t *tunnel, sid string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, err := t.w.WriteString("DIAL " + sid + "\r\n"); err != nil {
		return err
	}
	return t.w.Flush()
}

func (r *Relay) attach(sid string, j net.Conn) {
	r.mu.Lock()
	c := r.join[sid]
	delete(r.join, sid)
	r.mu.Unlock()
	if c == nil {
		j.Close()
		return
	}
	pipe(c, j)
}

func pipe(a, b net.Conn) {
	var once sync.Once
	closeBoth := func() { once.Do(func() { a.Close(); b.Close() }) }
	go func() { io.Copy(a, b); closeBoth() }()
	io.Copy(b, a)
	closeBoth()
}

func newSID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}