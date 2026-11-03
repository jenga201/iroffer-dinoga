package sxdcc

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jenga201/dinoga/internal/dcc"
	"github.com/jenga201/dinoga/internal/packs"
)

// Host is the interface the surrounding bot implements so the SXDCC server can
// list packs, look them up, open their files, and log.
type Host interface {
	Packlist() []packs.Pack
	PackByNumber(n int) (packs.Pack, bool)
	OpenFile(path string) (io.ReadCloser, error)
	Logf(format string, args ...any)
}

// SlotGate is an optional queue/slot controller, satisfied by *queue.Manager.
// When a Server has a SlotGate, each SEND is gated on a transfer slot.
type SlotGate interface {
	// Submit enqueues a request and returns its 1-based queue position.
	Submit(user, key string, idle bool) (int, error)
	// Wait blocks until a slot is available and returns its release func.
	Wait() (user, key string, release func())
}

// Config configures a Server.
type Config struct {
	CertFile  string        // PEM cert (created if missing)
	KeyFile   string        // PEM key (created if missing)
	IP        string        // advertised public IPv4 (also the IP SAN)
	PortStart int           // first TLS port to try
	PortLimit int           // last TLS port to try
	TokenTTL  time.Duration // rendezvous token lifetime
	Passive   bool          // passive/reverse data channel (client listens, bot dials)
	Slot      SlotGate      // optional: gate SEND on transfer slots (nil = unbounded)
	AdvertiseIP string      // optional public IP to advertise (defaults to IP)
	NewListener func() (net.Listener, int, error) // optional listener factory (bounce mode)
}

func (c Config) ttl() time.Duration {
	if c.TokenTTL <= 0 {
		return 120 * time.Second
	}
	return c.TokenTTL
}

// transfer is one pending or in-flight data-channel transfer.
type transfer struct {
	pack    packs.Pack
	ln      net.Listener // active: the bot listens here
	passive bool         // passive: the bot dials the client after ACCEPT
	release func()       // queue slot release, if gated
}

// Server is the bot-side SXDCC server.
type Server struct {
	host Host
	cfg  Config
	id   *Identity

	tmu        sync.Mutex
	transfers  map[string]*transfer // datatoken -> transfer
	tokens     *tokenStore          // rendezvous control tokens
	dataTokens *tokenStore          // per-pack data tokens

	advertiseIP uint32
}

// New builds a Server, loading or creating the identity and computing the pin.
func New(host Host, cfg Config) (*Server, error) {
	if host == nil {
		return nil, errors.New("sxdcc: nil host")
	}
	id, err := LoadOrCreateIdentity(cfg.CertFile, cfg.KeyFile, cfg.IP)
	if err != nil {
		return nil, err
	}
	advertiseIP, err := dcc.IPToInt(cfg.IP)
	if err != nil {
		return nil, err
	}
	if cfg.AdvertiseIP != "" {
		advertiseIP, err = dcc.IPToInt(cfg.AdvertiseIP)
		if err != nil {
			return nil, err
		}
	}
	s := &Server{
		host:        host,
		cfg:         cfg,
		id:          id,
		transfers:   map[string]*transfer{},
		tokens:      newTokenStore(cfg.ttl()),
		dataTokens:  newTokenStore(cfg.ttl()),
		advertiseIP: advertiseIP,
	}
	go s.pruneLoop()
	return s, nil
}

func (s *Server) pruneLoop() {
	t := time.NewTicker(time.Minute)
	for range t.C {
		s.tokens.prune()
		s.dataTokens.prune()
	}
}

// Identity returns the loaded identity.
func (s *Server) Identity() *Identity { return s.id }

// Pin returns the server's SPKI pin.
func (s *Server) Pin() string { return s.id.Pin }

// TLSConfig returns the TLS 1.3 server config.
func (s *Server) TLSConfig() *tls.Config { return s.id.TLSConfig() }

// Rendezvous mints a control token and returns the IRC invitation line and the
// token. The token is what the AUTH command must present.
func (s *Server) Rendezvous(port int) (line, token string, err error) {
	token, err = s.tokens.mint()
	if err != nil {
		return "", "", err
	}
	s.tokens.add(token)
	return BuildRendezvous(s.advertiseIP, port, token, s.id.Pin), token, nil
}

// Listen opens a raw TCP listener on the first free port in [PortStart, PortLimit].
func (s *Server) Listen() (net.Listener, int, error) {
	if s.cfg.NewListener != nil {
		return s.cfg.NewListener()
	}
	return dcc.Listen(s.cfg.PortStart, s.cfg.PortLimit)
}

// Serve accepts TLS connections on ln and handles each on its own goroutine.
func (s *Server) Serve(ln net.Listener) error {
	tlsLn := tls.NewListener(ln, s.id.TLSConfig())
	for {
		conn, err := tlsLn.Accept()
		if err != nil {
			return err
		}
		go s.handleControl(conn)
	}
}

// --- control protocol ---

const maxLineLen = 8192

type session struct {
	s      *Server
	conn   net.Conn
	r      *bufio.Reader
	wmu    sync.Mutex
	remote string // remote address, used as the queue "user" key
	auth   bool
}

func (s *Server) handleControl(conn net.Conn) {
	defer conn.Close()
	ses := &session{s: s, conn: conn, r: bufio.NewReader(conn), remote: conn.RemoteAddr().String()}
	for {
		line, err := ses.readLine()
		if err != nil {
			return
		}
		if line == "" {
			continue
		}
		if !ses.handle(line) {
			return
		}
	}
}

func (ses *session) readLine() (string, error) {
	line, err := ses.r.ReadString('\n')
	if err != nil {
		return "", err
	}
	if len(line) > maxLineLen {
		return "", errors.New("sxdcc: control line too long")
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func (ses *session) write(format string, args ...any) {
	ses.wmu.Lock()
	defer ses.wmu.Unlock()
	fmt.Fprintf(ses.conn, format+"\r\n", args...)
}

// handle processes one control line; returning false closes the session.
func (ses *session) handle(line string) bool {
	f := strings.Fields(line)
	if len(f) == 0 {
		return true
	}
	verb := strings.ToUpper(f[0])

	if !ses.auth {
		if verb != "AUTH" {
			ses.write("ERR 401 unauthorized")
			return false
		}
		if len(f) != 2 || !ses.s.tokens.consume(f[1]) {
			ses.write("ERR 401 unauthorized")
			return false
		}
		ses.auth = true
		ses.write("OK %d", time.Now().UnixNano())
		return true
	}

	switch verb {
	case "LIST":
		for _, p := range ses.s.host.Packlist() {
			ses.write("#%d [%s] %s", p.Number, humanSize(p.Size), p.Name)
		}
		ses.write("OK")
	case "SEND":
		ses.handleSend(f)
	case "ACCEPT":
		ses.handleAccept(f)
	case "INFO":
		if len(f) >= 2 {
			ses.handleInfo(f[1])
		}
	case "BATCH":
		if len(f) >= 2 {
			ses.handleBatch(f[1])
		}
	case "OWNER":
		ses.write("OWNER %s", "dinoga") // delegated to host later
	case "CANCEL":
		ses.write("OK")
	case "QUIT":
		ses.write("BYE")
		return false
	default:
		ses.write("ERR 400 unknown command %s", f[0])
	}
	return true
}

func (ses *session) handleSend(f []string) {
	if len(f) < 2 {
		ses.write("ERR 400 usage: SEND <pack>")
		return
	}
	n, err := strconv.Atoi(strings.TrimPrefix(f[1], "#"))
	if err != nil || n < 1 {
		ses.write("ERR 400 bad pack number")
		return
	}
	p, ok := ses.s.host.PackByNumber(n)
	if !ok {
		ses.write("ERR 404 no such pack %d", n)
		return
	}

	datatoken, err := ses.s.dataTokens.mint()
	if err != nil {
		ses.write("ERR 500 token mint failed")
		return
	}
	ses.s.dataTokens.add(datatoken)

	// Optional queue/slot gating.
	var release func()
	if ses.s.cfg.Slot != nil {
		pos, err := ses.s.cfg.Slot.Submit(ses.remote, datatoken, false)
		if err != nil {
			ses.write("ERR 503 queue full")
			return
		}
		if pos > 1 {
			ses.write("QUEUE %d", pos)
		}
		_, _, release = ses.s.cfg.Slot.Wait()
		if pos > 1 {
			ses.write("READY")
		}
	}

	if ses.s.cfg.Passive {
		ses.s.tmu.Lock()
		ses.s.transfers[datatoken] = &transfer{pack: p, passive: true, release: release}
		ses.s.tmu.Unlock()
		// The bot responds with port 0; the client ACCEPTs with its listener.
		ses.write("SEND #%d %d %q %d 0 %s", p.Number, p.Size, p.Name, ses.s.advertiseIP, datatoken)
		return
	}

	ln, port, err := ses.s.Listen()
	if err != nil {
		if release != nil {
			release()
		}
		ses.write("ERR 500 %v", err)
		return
	}
	ses.s.tmu.Lock()
	ses.s.transfers[datatoken] = &transfer{pack: p, ln: ln, release: release}
	ses.s.tmu.Unlock()

	ses.write("SEND #%d %d %q %d %d %s", p.Number, p.Size, p.Name, ses.s.advertiseIP, port, datatoken)
	go ses.s.serveData(ses, datatoken)
}

// handleAccept handles the client's passive-listen reply:
//
//	ACCEPT <datatoken> <ip> <port>
func (ses *session) handleAccept(f []string) {
	if len(f) != 4 {
		ses.write("ERR 400 usage: ACCEPT <datatoken> <ip> <port>")
		return
	}
	datatoken := f[1]

	ses.s.tmu.Lock()
	tr := ses.s.transfers[datatoken]
	if tr == nil || !tr.passive {
		ses.s.tmu.Unlock()
		ses.write("ERR 404 unknown or non-passive transfer")
		return
	}
	ses.s.tmu.Unlock()

	ip := f[2]
	if !strings.Contains(ip, ".") {
		if v, err := strconv.ParseUint(ip, 10, 32); err == nil {
			ip = dcc.IPFromInt(uint32(v))
		}
	}
	go ses.s.serveDataPassive(ses, datatoken, net.JoinHostPort(ip, f[3]))
}

func (ses *session) handleInfo(arg string) {
	n, err := strconv.Atoi(strings.TrimPrefix(arg, "#"))
	if err != nil || n < 1 {
		ses.write("ERR 400 bad pack number")
		return
	}
	p, ok := ses.s.host.PackByNumber(n)
	if !ok {
		ses.write("ERR 404 no such pack %d", n)
		return
	}
	ses.write("INFO #%d %d %q", p.Number, p.Size, p.Name)
}

func (ses *session) handleBatch(arg string) {
	parts := strings.SplitN(arg, "-", 2)
	if len(parts) != 2 {
		ses.write("ERR 400 usage: BATCH <start>-<end>")
		return
	}
	start, err1 := strconv.Atoi(strings.TrimPrefix(parts[0], "#"))
	end, err2 := strconv.Atoi(strings.TrimPrefix(parts[1], "#"))
	if err1 != nil || err2 != nil || start < 1 || end < start {
		ses.write("ERR 400 bad range")
		return
	}
	count := 0
	for n := start; n <= end; n++ {
		if _, ok := ses.s.host.PackByNumber(n); ok {
			count++
		}
	}
	ses.write("OK %d pack(s) in range", count)
}

// serveData streams one pack on an active data conn after validating its
// datatoken. The bot is the TLS server and the client is the origin (sends the
// DATA line).
func (s *Server) serveData(ses *session, datatoken string) {
	s.tmu.Lock()
	tr := s.transfers[datatoken]
	s.tmu.Unlock()
	if tr == nil {
		ses.write("ERR 500 lost transfer")
		return
	}
	defer func() {
		if tr.ln != nil {
			tr.ln.Close()
		}
		s.tmu.Lock()
		delete(s.transfers, datatoken)
		s.tmu.Unlock()
		if tr.release != nil {
			tr.release()
		}
	}()

	conn, err := tr.ln.Accept()
	if err != nil {
		return
	}

	tlsConn := tls.Server(conn, s.id.TLSConfig())
	if err := tlsConn.Handshake(); err != nil {
		conn.Close()
		return
	}
	defer tlsConn.Close()

	br := bufio.NewReader(tlsConn)
	line, err := br.ReadString('\n')
	if err != nil {
		return
	}
	line = strings.TrimRight(line, "\r\n")
	f := strings.Fields(line)
	if len(f) != 2 || !strings.EqualFold(f[0], "DATA") || !s.dataTokens.consume(f[1]) {
		ses.write("ERR 401 bad data token")
		return
	}

	rc, err := s.host.OpenFile(tr.pack.Path)
	if err != nil {
		ses.write("ERR 500 %v", err)
		return
	}
	defer rc.Close()

	n, err := io.Copy(tlsConn, rc)
	if err != nil {
		ses.write("ERR 500 %v", err)
		return
	}
	ses.write("DONE #%d %d", tr.pack.Number, n)
}

// serveDataPassive dials the client's listener, acts as the TLS server on the
// dialed connection, sends the DATA binding line (the bot is the origin), and
// streams the pack.
func (s *Server) serveDataPassive(ses *session, datatoken, addr string) {
	s.tmu.Lock()
	tr := s.transfers[datatoken]
	s.tmu.Unlock()
	if tr == nil {
		return
	}
	defer func() {
		s.tmu.Lock()
		delete(s.transfers, datatoken)
		s.tmu.Unlock()
		if tr.release != nil {
			tr.release()
		}
	}()

	conn, err := net.DialTimeout("tcp", addr, 30*time.Second)
	if err != nil {
		ses.write("ERR 500 %v", err)
		return
	}

	tlsConn := tls.Server(conn, s.id.TLSConfig())
	if err := tlsConn.Handshake(); err != nil {
		conn.Close()
		return
	}
	defer tlsConn.Close()

	if _, err := fmt.Fprintf(tlsConn, "DATA %s\r\n", datatoken); err != nil {
		return
	}

	rc, err := s.host.OpenFile(tr.pack.Path)
	if err != nil {
		ses.write("ERR 500 %v", err)
		return
	}
	defer rc.Close()

	n, err := io.Copy(tlsConn, rc)
	if err != nil {
		ses.write("ERR 500 %v", err)
		return
	}
	ses.write("DONE #%d %d", tr.pack.Number, n)
}

// humanSize renders a byte count in the XDCC "[1.2G]" style (SI, one decimal).
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 4; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%c", float64(n)/float64(div), "KMGTP"[exp])
}
