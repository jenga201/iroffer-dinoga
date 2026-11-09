package sxdcc

import (
	"bufio"
	"crypto"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
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
	"github.com/jenga201/dinoga/internal/token"
)

// Host is the interface the surrounding bot implements so the SXDCC server can
// list packs, look them up, open their files, log, and record transfer stats.
type Host interface {
	Packlist() []packs.Pack
	PackByNumber(n int) (packs.Pack, bool)
	OpenFile(path string) (io.ReadCloser, error)
	Logf(format string, args ...any)
	// RecordSent adds a completed transfer's byte count to the aggregate and
	// per-pack statistics (persisted by the host).
	RecordSent(path string, bytes int64)
	// ListHeader returns the "N transfers open" header for the LIST reply.
	ListHeader() string
	// ListFooter returns the totals footer for the LIST reply.
	ListFooter() string
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
	Owner     string        // owner/contact answered by OWNER (default "dinoga")
}

func (c Config) ttl() time.Duration {
	if c.TokenTTL <= 0 {
		return 120 * time.Second
	}
	return c.TokenTTL
}

// transfer is one pending (queued) or in-flight data-channel transfer.
type transfer struct {
	pack    packs.Pack
	passive bool     // passive: the bot dials the client after ACCEPT
	ses     *session // control session that issued the transfer (for DONE/ERR)

	cancel     chan struct{} // closed once on CANCEL
	cancelOnce sync.Once

	mu          sync.Mutex
	release     func() // queue slot release, set once granted (nil while queued)
	releaseOnce sync.Once
	claimed     bool      // data connection arrived
	conn        net.Conn  // data connection (set once data arrives; closed on cancel)
}

// markCanceled closes the cancel channel exactly once.
func (tr *transfer) markCanceled() { tr.cancelOnce.Do(func() { close(tr.cancel) }) }

// canceled reports whether the transfer has been cancelled.
func (tr *transfer) canceled() bool {
	select {
	case <-tr.cancel:
		return true
	default:
		return false
	}
}

func (tr *transfer) setRelease(f func()) { tr.mu.Lock(); tr.release = f; tr.mu.Unlock() }
func (tr *transfer) setConn(c net.Conn)  { tr.mu.Lock(); tr.conn = c; tr.mu.Unlock() }
func (tr *transfer) setClaimed()         { tr.mu.Lock(); tr.claimed = true; tr.mu.Unlock() }
func (tr *transfer) isClaimed() bool {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return tr.claimed
}

// closeConn closes the data connection (if any) to abort an in-flight stream.
func (tr *transfer) closeConn() {
	tr.mu.Lock()
	c := tr.conn
	tr.mu.Unlock()
	if c != nil {
		_ = c.Close()
	}
}

// complete releases the transfer's queue slot exactly once, so concurrent
// teardown paths cannot double-release.
func (tr *transfer) complete() {
	tr.releaseOnce.Do(func() {
		tr.mu.Lock()
		f := tr.release
		tr.mu.Unlock()
		if f != nil {
			f()
		}
	})
}

// Server is the bot-side SXDCC server.
type Server struct {
	host Host
	cfg  Config

	idMu sync.Mutex // guards id (regenerated on VPN IP change)
	id   *Identity

	tmu        sync.Mutex
	transfers  map[string]*transfer // datatoken -> transfer
	tokens     *token.Store         // rendezvous control tokens
	dataTokens *token.Store         // per-pack data tokens
	pending    map[string]string    // rendezvous token -> trigger command ("SEND 1")

	addrMu        sync.Mutex // guards advertiseIP + advertisePort
	advertiseIP   uint32
	advertisePort int // public port advertised to clients (forwarded port when VPN)
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
	// Seed the advertised IP from the loaded identity's IP SAN when present,
	// so a restart behind a stable VPN egress (where cfg.IP is only a
	// placeholder) does not count as an address change and re-issue the cert.
	if san, ok := id.ipSAN(); ok {
		if ip, ierr := dcc.IPToInt(san); ierr == nil {
			advertiseIP = ip
		}
	}
	s := &Server{
		host:        host,
		cfg:         cfg,
		id:          id,
		transfers:   map[string]*transfer{},
		tokens:      token.New(cfg.ttl()),
		dataTokens:  token.New(cfg.ttl()),
		pending:     map[string]string{},
		advertiseIP: advertiseIP,
	}
	go s.pruneLoop()
	return s, nil
}

func (s *Server) pruneLoop() {
	t := time.NewTicker(time.Minute)
	for range t.C {
		s.tokens.Prune()
		s.dataTokens.Prune()
	}
}

// Identity returns the loaded identity.
func (s *Server) Identity() *Identity {
	s.idMu.Lock()
	defer s.idMu.Unlock()
	return s.id
}

// Pin returns the server's SPKI pin.
func (s *Server) Pin() string {
	s.idMu.Lock()
	defer s.idMu.Unlock()
	return s.id.Pin
}

// RegenerateKey replaces the server identity with a fresh self-signed Ed25519
// leaf for the current advertised IP, persists it to the configured cert/key
// files, and swaps it into use (new connections verify against the new pin).
// It returns the new SPKI pin.
func (s *Server) RegenerateKey() (string, error) {
	s.addrMu.Lock()
	ipStr := dcc.IPFromInt(s.advertiseIP)
	s.addrMu.Unlock()

	id, err := GenerateIdentity(ipStr)
	if err != nil {
		return "", err
	}
	if err := writeIdentity(s.cfg.CertFile, s.cfg.KeyFile, id.Cert); err != nil {
		return "", err
	}
	s.idMu.Lock()
	s.id = id
	s.idMu.Unlock()
	return id.Pin, nil
}

// SetControlPort records the bot's single shared control port, which (without a
// VPN) is also the address advertised for active data channels.
func (s *Server) SetControlPort(port int) {
	s.addrMu.Lock()
	s.advertisePort = port
	s.addrMu.Unlock()
}

// SetAdvertise updates the public address advertised to clients. When ipStr
// differs from the current advertised IP, the self-signed leaf is re-issued for
// the new IP subjectAltName around the SAME key, so the advertised IP always has
// a matching cert while the SPKI pin stays put (SXDCC.md §7.5). It reports
// whether the advertised IP changed (i.e. the cert was renewed).
func (s *Server) SetAdvertise(ipStr string, port int) (bool, error) {
	ipInt, err := dcc.IPToInt(ipStr)
	if err != nil {
		return false, err
	}

	s.addrMu.Lock()
	defer s.addrMu.Unlock()

	ipChanged := ipInt != s.advertiseIP
	if ipChanged {
		// Renew the certificate around the SAME key so the SPKI pin stays put
		// across advertised-IP changes (SXDCC.md §7.5); regenerating the key
		// would churn the pin and break client TOFU.
		priv, ok := s.id.Cert.PrivateKey.(crypto.Signer)
		if !ok {
			return false, errors.New("sxdcc: loaded key is not a signer")
		}
		id, err := RenewIdentity(priv, ipStr)
		if err != nil {
			return false, err
		}
		if err := writeIdentity(s.cfg.CertFile, s.cfg.KeyFile, id.Cert); err != nil {
			return false, err
		}
		s.idMu.Lock()
		s.id = id
		s.idMu.Unlock()
		s.advertiseIP = ipInt
	}
	s.advertisePort = port
	return ipChanged, nil
}

// AdvertisePort returns the current public port advertised to clients.
func (s *Server) AdvertisePort() int {
	s.addrMu.Lock()
	defer s.addrMu.Unlock()
	return s.advertisePort
}

// advertiseAddr returns the current advertised IP (DCC network order) and port.
func (s *Server) advertiseAddr() (uint32, int) {
	s.addrMu.Lock()
	defer s.addrMu.Unlock()
	return s.advertiseIP, s.advertisePort
}

// expireTransfer removes a transfer whose data connection never arrived, so a
// client that never dials cannot leak a queue slot. It wakes early if the
// transfer is cancelled instead of waiting the full timeout.
func (s *Server) expireTransfer(tr *transfer, datatoken string, after time.Duration) {
	timedOut := false
	select {
	case <-tr.cancel:
	case <-time.After(after):
		timedOut = true
	}
	s.tmu.Lock()
	cur, ok := s.transfers[datatoken]
	if !ok || cur != tr || tr.isClaimed() {
		s.tmu.Unlock()
		return
	}
	delete(s.transfers, datatoken)
	s.tmu.Unlock()
	if timedOut && tr.ses != nil {
		tr.ses.write("ERR 408 transfer timed out")
	}
	tr.complete()
}

// SetPendingCommand associates the IRC trigger's command (e.g. "SEND 1") with a
// rendezvous token so the bot auto-executes it once the client authenticates
// (SXDCC.md §9.4). It is a no-op for an empty token or command.
func (s *Server) SetPendingCommand(token, command string) {
	if token == "" || command == "" {
		return
	}
	s.tmu.Lock()
	s.pending[token] = command
	s.tmu.Unlock()
}

// takePendingCommand atomically takes and removes the command for token.
func (s *Server) takePendingCommand(token string) string {
	s.tmu.Lock()
	defer s.tmu.Unlock()
	cmd := s.pending[token]
	delete(s.pending, token)
	return cmd
}

// TLSConfig returns the TLS 1.3 server config.
func (s *Server) TLSConfig() *tls.Config {
	s.idMu.Lock()
	id := s.id
	s.idMu.Unlock()
	return id.TLSConfig()
}

// Rendezvous mints a control token and returns the IRC invitation line and the
// token. The token is what the AUTH command must present.
func (s *Server) Rendezvous() (line, token string, err error) {
	token, err = s.tokens.Mint()
	if err != nil {
		return "", "", err
	}
	s.tokens.Add(token)
	ip, port := s.advertiseAddr()
	return BuildRendezvous(ip, port, token, s.Pin()), token, nil
}

// Listen opens a raw TCP listener on the first free port in [PortStart, PortLimit].
func (s *Server) Listen() (net.Listener, int, error) {
	return dcc.Listen(s.cfg.PortStart, s.cfg.PortLimit)
}

// Serve accepts TLS connections on ln and handles each on its own goroutine.
// It is the standalone SXDCC listener; the shared-listener deployment in
// internal/irc performs the same first-line read and then dispatches to
// HandleConn (SXDCC) or sdcc.HandleConn (SDCC).
func (s *Server) Serve(ln net.Listener) error {
	tlsLn := tls.NewListener(ln, s.id.TLSConfig())
	for {
		conn, err := tlsLn.Accept()
		if err != nil {
			return err
		}
		go func() {
			defer conn.Close()
			r := bufio.NewReader(conn)
			line, err := readFirstLine(r)
			if err != nil {
				return
			}
			s.HandleConn(conn, r, line)
		}()
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

// HandleConn serves one accepted TLS connection for SXDCC (file transfer). The
// caller has already read firstLine (and must pass r, the buffered reader it
// used, so buffered bytes are not lost). It demultiplexes the SXDCC data
// fallback (DATA) from the control session (AUTH).
func (s *Server) HandleConn(conn net.Conn, r *bufio.Reader, firstLine string) {
	ses := &session{s: s, conn: conn, r: r, remote: conn.RemoteAddr().String()}

	f := strings.Fields(firstLine)
	if len(f) == 2 && strings.EqualFold(f[0], "DATA") {
		s.serveDataOverControl(ses, f[1])
		return
	}
	if firstLine != "" && !ses.handle(firstLine) {
		return
	}
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

// readFirstLine reads one CRLF line enforcing the control length bound.
func readFirstLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	if len(line) > maxLineLen {
		return "", errors.New("sxdcc: control line too long")
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// serveDataOverControl serves the active data channel: the client dials the
// shared control port and sends DATA <datatoken> as its first line, and the
// pack is streamed over that connection. Completion/errors are signalled on
// the original control session (tr.ses), not on this data connection.
func (s *Server) serveDataOverControl(ses *session, datatoken string) {
	s.tmu.Lock()
	tr := s.transfers[datatoken]
	s.tmu.Unlock()
	if tr == nil {
		_, _ = fmt.Fprintf(ses.conn, "ERR 404 unknown data token\r\n")
		return
	}

	if !s.dataTokens.Consume(datatoken) {
		if tr.ses != nil {
			tr.ses.write("ERR 401 bad data token")
		}
		return
	}

	tr.setClaimed()
	tr.setConn(ses.conn)

	defer func() {
		s.tmu.Lock()
		delete(s.transfers, datatoken)
		s.tmu.Unlock()
		tr.complete()
	}()

	rc, err := s.host.OpenFile(tr.pack.Path)
	if err != nil {
		if tr.ses != nil {
			tr.ses.write("ERR 500 %v", err)
		}
		return
	}
	defer rc.Close()

	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(ses.conn, h), rc)
	if err != nil {
		if tr.ses != nil && !tr.canceled() {
			tr.ses.write("ERR 500 %v", err)
		}
		return
	}
	s.host.RecordSent(tr.pack.Path, n)
	if tr.ses != nil {
		tr.ses.write("DONE #%d %d sha256:%s", tr.pack.Number, n,
			base64.RawURLEncoding.EncodeToString(h.Sum(nil)))
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
		if len(f) != 2 || !ses.s.tokens.Consume(f[1]) {
			ses.write("ERR 401 unauthorized")
			return false
		}
		ses.auth = true
		ses.write("OK %d", time.Now().UnixNano())
		// Auto-execute the command named by the IRC trigger (SXDCC.md §9.4).
		if cmd := ses.s.takePendingCommand(f[1]); cmd != "" {
			return ses.handle(cmd)
		}
		return true
	}

	switch verb {
	case "LIST":
		ses.write("%s", ses.s.host.ListHeader())
		for _, p := range ses.s.host.Packlist() {
			ses.write("#%d [%s] %s", p.Number, humanSize(p.Size), p.Name)
		}
		ses.write("%s", ses.s.host.ListFooter())
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
		owner := ses.s.cfg.Owner
		if owner == "" {
			owner = "dinoga"
		}
		ses.write("OWNER %s", owner)
	case "CANCEL":
		ses.s.cancelSession(ses, cancelPackArg(f))
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

	datatoken, err := ses.s.dataTokens.Mint()
	if err != nil {
		ses.write("ERR 500 token mint failed")
		return
	}
	ses.s.dataTokens.Add(datatoken)

	// Register the transfer up front (queued state) so CANCEL can reach it, and
	// hand the slot wait + offer to a goroutine so the control loop stays
	// responsive while the request is queued.
	tr := &transfer{pack: p, passive: ses.s.cfg.Passive, ses: ses, cancel: make(chan struct{})}
	ses.s.tmu.Lock()
	ses.s.transfers[datatoken] = tr
	ses.s.tmu.Unlock()

	go ses.s.fulfill(tr, datatoken)
}

// fulfill waits for a transfer slot (when gated), then offers the pack to the
// client. If the transfer is cancelled while queued, it releases the slot and
// does not offer.
func (s *Server) fulfill(tr *transfer, datatoken string) {
	if s.cfg.Slot != nil {
		pos, err := s.cfg.Slot.Submit(tr.ses.remote, datatoken, false)
		if err != nil {
			tr.ses.write("ERR 503 queue full")
			s.dropTransfer(datatoken)
			return
		}
		if pos > 1 {
			tr.ses.write("QUEUE %d", pos)
		}
		_, _, release := s.cfg.Slot.Wait()
		if pos > 1 {
			tr.ses.write("READY")
		}
		if tr.canceled() {
			release()
			s.dropTransfer(datatoken)
			return
		}
		tr.setRelease(release)
	}

	if tr.passive {
		// SXDCC.md §9.5 passive form has no IP: SEND #<p> <size> "<name>" 0 <token>.
		tr.ses.write("SEND #%d %d %q 0 %s", tr.pack.Number, tr.pack.Size, tr.pack.Name, datatoken)
		return
	}

	// Active: data rides the shared control port. The client dials the control
	// address and sends DATA <datatoken>; serveDataOverControl streams it.
	ip, port := s.advertiseAddr()
	tr.ses.write("SEND #%d %d %q %d %d %s", tr.pack.Number, tr.pack.Size, tr.pack.Name, ip, port, datatoken)
	go s.expireTransfer(tr, datatoken, 2*time.Minute)
}

// dropTransfer removes the transfer entry for datatoken (no slot release).
func (s *Server) dropTransfer(datatoken string) {
	s.tmu.Lock()
	delete(s.transfers, datatoken)
	s.tmu.Unlock()
}

// cancelPackArg parses an optional "#<pack>" argument from a CANCEL command,
// returning 0 when absent or malformed (cancel-all).
func cancelPackArg(f []string) int {
	if len(f) < 2 {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimPrefix(f[1], "#"))
	if err != nil || n < 1 {
		return 0
	}
	return n
}

// cancelSession cancels the session's transfers matching pack (0 = all): queued
// transfers are marked so fulfill releases the slot without offering; in-flight
// transfers have their data connection closed to abort the stream.
func (s *Server) cancelSession(ses *session, pack int) {
	s.tmu.Lock()
	var targets []*transfer
	for datatoken, tr := range s.transfers {
		if tr.ses == ses && (pack == 0 || tr.pack.Number == pack) {
			delete(s.transfers, datatoken)
			targets = append(targets, tr)
		}
	}
	s.tmu.Unlock()

	for _, tr := range targets {
		tr.markCanceled()
		tr.closeConn()
		tr.complete()
	}
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
	if tr == nil || !tr.passive || tr.canceled() {
		ses.s.tmu.Unlock()
		ses.write("ERR 404 unknown or non-passive transfer")
		return
	}
	ses.s.tmu.Unlock()

	// The datatoken is single-use (REQ-7); consume it here so a replayed ACCEPT
	// using the same token is rejected even before the transfer map entry goes.
	if !ses.s.dataTokens.Consume(datatoken) {
		ses.write("ERR 401 bad data token")
		return
	}

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
		tr.complete()
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
	tr.setConn(tlsConn)

	if _, err := fmt.Fprintf(tlsConn, "DATA %s\r\n", datatoken); err != nil {
		return
	}

	rc, err := s.host.OpenFile(tr.pack.Path)
	if err != nil {
		ses.write("ERR 500 %v", err)
		return
	}
	defer rc.Close()

	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tlsConn, h), rc)
	if err != nil {
		if !tr.canceled() {
			ses.write("ERR 500 %v", err)
		}
		return
	}
	s.host.RecordSent(tr.pack.Path, n)
	ses.write("DONE #%d %d sha256:%s", tr.pack.Number, n,
		base64.RawURLEncoding.EncodeToString(h.Sum(nil)))
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
