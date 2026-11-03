// Package bounce implements the SXDCC reverse-tunnel ("bounce") transport. A
// public Relay pairs inbound client connections with reverse-dialed connections
// from a bot behind NAT; the bot side is the Listener. The relay forwards bytes
// only and never terminates TLS, so the SXDCC TLS trust stays end-to-end.
package bounce

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// Listener is a bot-side net.Listener whose accepted connections arrive over an
// outbound tunnel to a Relay, registered on one public port.
type Listener struct {
	relay    string
	port     int
	ctrl     net.Conn
	br       *bufio.Reader
	acceptCh chan net.Conn
	closed   chan struct{}
	once     sync.Once
}

// New dials the relay's tunnel endpoint, registers this listener on relayPort
// (authenticated by secret), and returns a net.Listener. Each client that
// connects to relayPort on the relay is handed to Accept as an end-to-end
// connection.
func New(relay, secret string, relayPort int) (*Listener, error) {
	ctrl, err := net.DialTimeout("tcp", relay, 10*time.Second)
	if err != nil {
		return nil, err
	}
	if err := ctrl.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		ctrl.Close()
		return nil, err
	}
	if _, err := fmt.Fprintf(ctrl, "REGISTER %d %s\r\n", relayPort, secret); err != nil {
		ctrl.Close()
		return nil, err
	}
	br := bufio.NewReader(ctrl)
	line, err := br.ReadString('\n')
	if err != nil {
		ctrl.Close()
		return nil, err
	}
	if strings.TrimSpace(line) != "OK" {
		ctrl.Close()
		return nil, fmt.Errorf("bounce: relay refused registration on port %d", relayPort)
	}
	if err := ctrl.SetDeadline(time.Time{}); err != nil {
		ctrl.Close()
		return nil, err
	}

	l := &Listener{
		relay:    relay,
		port:     relayPort,
		ctrl:     ctrl,
		br:       br,
		acceptCh: make(chan net.Conn),
		closed:   make(chan struct{}),
	}
	go l.readLoop()
	return l, nil
}

func (l *Listener) readLoop() {
	for {
		line, err := l.br.ReadString('\n')
		if err != nil {
			l.shutdown()
			return
		}
		var sid string
		if _, err := fmt.Sscanf(line, "DIAL %s", &sid); err == nil && sid != "" {
			go l.join(sid)
		}
	}
}

func (l *Listener) join(sid string) {
	conn, err := net.DialTimeout("tcp", l.relay, 10*time.Second)
	if err != nil {
		return
	}
	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		conn.Close()
		return
	}
	if _, err := fmt.Fprintf(conn, "JOIN %s\r\n", sid); err != nil {
		conn.Close()
		return
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		conn.Close()
		return
	}
	select {
	case l.acceptCh <- conn:
	case <-l.closed:
		conn.Close()
	}
}

// Accept implements net.Listener.
func (l *Listener) Accept() (net.Conn, error) {
	select {
	case c := <-l.acceptCh:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

// Close implements net.Listener.
func (l *Listener) Close() error {
	l.once.Do(l.shutdown)
	return nil
}

// Addr implements net.Listener.
func (l *Listener) Addr() net.Addr { return l.ctrl.LocalAddr() }

func (l *Listener) shutdown() {
	select {
	case <-l.closed:
	default:
		close(l.closed)
		_ = l.ctrl.Close()
	}
}