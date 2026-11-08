// Connection helpers for serving plain HTTP and TLS on the same shared control
// port.
package irc

import (
	"bufio"
	"net"
	"sync"
)

// bufferedConn re-exposes a bufio.Reader's buffered bytes as the connection's
// Read stream, so a peeked first byte is not lost when the connection is handed
// onward (to tls.Server or http.Server).
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c bufferedConn) Read(p []byte) (int, error) { return c.r.Read(p) }

// oneConnListener adapts a single accepted net.Conn to net.Listener for
// http.Server.Serve. Its first Accept returns the conn; subsequent Accepts
// block until Close is called. Serve is then closed (via ConnState) once the
// connection is finished, so the response is fully flushed before Serve returns.
type oneConnListener struct {
	conn net.Conn
	mu   sync.Mutex
	done bool
	ch   chan struct{}
	once sync.Once
}

func newOneConnListener(conn net.Conn) *oneConnListener {
	return &oneConnListener{conn: conn, ch: make(chan struct{})}
}

func (l *oneConnListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	if !l.done {
		l.done = true
		l.mu.Unlock()
		return l.conn, nil
	}
	l.mu.Unlock()
	<-l.ch
	return nil, net.ErrClosed
}

func (l *oneConnListener) Close() error {
	l.once.Do(func() { close(l.ch) })
	return nil
}

func (l *oneConnListener) Addr() net.Addr { return l.conn.LocalAddr() }

// isHTTPMethodByte reports whether b is the first byte of an HTTP request line
// (an ASCII uppercase method letter such as "G"/"H"/"P"/"O"). A TLS ClientHello
// begins with the handshake record type byte 0x16, so the two never collide.
func isHTTPMethodByte(b byte) bool {
	return b >= 'A' && b <= 'Z'
}
