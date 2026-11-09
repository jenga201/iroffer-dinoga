// Package irc is the multi-network IRC front-end of the bot: it connects to
// one or more IRC networks, routes XDCC/SXDCC/CTCP commands, and drives DCC
// transfers on top of the shared packlist/queue/admin state.
package irc

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jenga201/dinoga/internal/acl"
	"github.com/jenga201/dinoga/internal/admin"
	"github.com/jenga201/dinoga/internal/config"
	"github.com/jenga201/dinoga/internal/dcc"
	"github.com/jenga201/dinoga/internal/logging"
	"github.com/jenga201/dinoga/internal/packs"
	"github.com/jenga201/dinoga/internal/queue"
	"github.com/jenga201/dinoga/internal/ratelimit"
	"github.com/jenga201/dinoga/internal/sdcc"
	"github.com/jenga201/dinoga/internal/sxdcc"
	throttle "github.com/jenga201/dinoga/internal/transfer"
	"github.com/jenga201/dinoga/internal/vpn"
	"github.com/jenga201/dinoga/internal/web"
	"github.com/jenga201/dinoga/internal/xdcc"
)

const (
	defaultReconnectDelay = 30 * time.Second
	maxReconnectDelay     = 30 * time.Minute
)

// transfer tracks one DCC send (active or passive) plus its queue release.
type transfer struct {
	nick    string
	name    string
	path    string
	size    int64
	port    int          // active listener port (0 for passive)
	token   int          // protocol token used for passive/resume matching
	ln      net.Listener // nil for passive
	passive bool
	offset  int64  // resume position (mutated under tmu)
	release func() // queue slot release (set for passive transfers)
	tb      *throttle.TokenBucket
}

// queueItem is a queued pack request awaiting a transfer slot.
type queueItem struct {
	nick string
	pack packs.Pack
	nc   *netConn // the network the requester is on
}

// Client holds the shared bot state across all networks.
type Client struct {
	cfg    config.Config
	logger *logging.Logger

	mu        sync.RWMutex // guards packlist + extras/descs/excluded + firstScan
	packlist  []packs.Pack
	firstScan bool

	extras   map[string]packs.Pack // runtime-added packs (addpack), keyed by path
	descs    map[string]string     // pack descriptions (chdesc), keyed by path
	excluded map[string]bool       // paths removed from the list (removepack)
	resume   map[string]int64      // per-pack resume offsets (path -> bytes)
	sent     map[string]int64      // per-pack transferred bytes (path -> bytes)
	totalSent int64                // aggregate bytes transferred

	tmu       sync.Mutex // guards transfers/nextToken
	transfers map[int]*transfer
	nextToken int

	q       *queue.Manager
	qmu     sync.Mutex
	pending map[string]*queueItem
	nextKey int64

	adminMu  sync.Mutex
	adminSrv *admin.Server

	sxdcc *sxdcc.Server
	sdcc  *sdcc.Server

	advMu   sync.Mutex
	advIP   string // current public IP advertised (VPN or usenatip)
	advPort int    // current public port advertised (forwarded or control)

	webMu           sync.Mutex
	webHandlerCache http.Handler

	acl *acl.Matcher

	uploadACL *acl.Matcher // uploadhost allow-list (empty = allow all)

	flood *ratelimit.Limiter // nil = flood protection disabled

	netMu sync.Mutex // guards nets
	nets  []*netConn

	globalTB *throttle.TokenBucket // overall transfer speed cap (nil = unlimited)
}

// New returns a Client configured by cfg.
func New(cfg config.Config, logger *logging.Logger) *Client {
	m := queue.New(queue.Config{
		Slots:                  cfg.SlotsMax,
		QueueSize:              cfg.QueueSize,
		IdleQueueSize:          cfg.IdleQueueSize,
		MaxTransfersPerPerson:  cfg.MaxTransfersPerPerson,
		MaxQueuedPerPerson:     cfg.MaxQueuedPerPerson,
		MaxIdleQueuedPerPerson: cfg.MaxIdleQueuedPerPerson,
	})
	c := &Client{
		cfg:       cfg,
		logger:    logger,
		transfers: map[int]*transfer{},
		pending:   map[string]*queueItem{},
		q:         m,
		acl:       acl.New(cfg.XDCCAllow, cfg.XDCCDeny),
		uploadACL: acl.New(cfg.UploadAllow, nil),
		resume:    map[string]int64{},
		firstScan: true,
	}
	if cfg.OverallMaxSpeed > 0 {
		c.globalTB = throttle.NewTokenBucket(cfg.OverallMaxSpeed)
	}
	if cfg.FloodProtectionRate > 0 {
		c.flood = ratelimit.New(cfg.FloodProtectionRate, floodWindow, time.Duration(cfg.AutoIgnoreTime)*time.Second)
	}
	return c
}

// Run starts the shared services and one connection goroutine per network,
// blocking until ctx is cancelled.
func (c *Client) Run(ctx context.Context) error {
	c.startWorkers()

	if err := c.startSXDCC(); err != nil {
		log.Printf("sxdcc: %v", err)
	}
	if err := c.startWeb(); err != nil {
		log.Printf("web: %v", err)
	}
	c.loadState()
	if err := c.scan(); err != nil {
		log.Printf("initial scan of %s: %v", c.cfg.FileDir, err)
	}
	if c.cfg.AutoAdd > 0 {
		go c.rescanLoop(ctx, time.Duration(c.cfg.AutoAdd)*time.Second)
	}

	for _, net := range c.cfg.Networks {
		nc := &netConn{c: c, net: net, modes: newChannelModes()}
		c.addNet(nc)
		go nc.run(ctx)
	}

	if c.cfg.LogStats && c.cfg.LogFile != "" {
		go c.statsLoop(ctx)
	}

	<-ctx.Done()
	return nil
}

// statusInterval returns the status/logstats tick interval (status_time_dcc_chat,
// default 120 s).
func (c *Client) statusInterval() time.Duration {
	if c.cfg.StatusInterval <= 0 {
		return 120 * time.Second
	}
	return time.Duration(c.cfg.StatusInterval) * time.Second
}

// statsLoop periodically writes a status line to the main log while logstats
// is enabled. It is only started when a logfile is configured, since that is
// where the line is written.
func (c *Client) statsLoop(ctx context.Context) {
	t := time.NewTicker(c.statusInterval())
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.logStats()
		}
	}
}

func (c *Client) logStats() {
	if c.logger == nil {
		return
	}
	s := c.q.Snapshot()
	used := c.cfg.SlotsMax - s.FreeSlots
	if used < 0 {
		used = 0
	}
	c.logger.Mainf("Stat: %d/%d slots, %d/%d queued, %d idle, %d packs",
		used, c.cfg.SlotsMax, s.QueueLen, c.cfg.QueueSize, s.IdleLen, len(c.Packlist()))
}

// netConn is a single IRC server connection. All XDCC/SXDCC/CTCP handling for
// that network runs through its read loop, writing responses to its own socket.
type netConn struct {
	c        *Client
	net      config.Network
	mu       sync.Mutex // guards conn
	conn     net.Conn
	failures int           // consecutive disconnects (reset on successful registration; run goroutine only)
	modes    *channelModes // channel members + per-user modes (restrict*/need_*)
}

func (nc *netConn) writef(format string, args ...any) {
	nc.mu.Lock()
	defer nc.mu.Unlock()
	if nc.conn == nil {
		return
	}
	fmt.Fprintf(nc.conn, format+"\r\n", args...)
}

func (nc *netConn) run(ctx context.Context) {
	if nc.c.cfg.PeriodicMsg != "" && nc.c.cfg.PeriodicInterval > 0 && !nc.net.NoAnnounce {
		go nc.periodicLoop(ctx, time.Duration(nc.c.cfg.PeriodicInterval)*time.Second)
	}

	base := time.Duration(nc.c.cfg.ReconnectDelay) * time.Second
	if base <= 0 {
		base = defaultReconnectDelay
	}

	for {
		err := nc.runOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		nc.failures++
		delay := reconnectDelay(base, nc.failures)
		log.Printf("network %s: disconnected (%v); reconnect #%d in %v", nc.net.Server, err, nc.failures, delay)
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

// reconnectDelay returns the exponential-backoff delay for a given number of
// consecutive failures, capped at maxReconnectDelay (30 minutes). A network
// that never reaches registration backs off quickly instead of being retried
// every base interval.
func reconnectDelay(base time.Duration, failures int) time.Duration {
	d := base
	for i := 2; i <= failures && d < maxReconnectDelay; i++ {
		d *= 2
		if d > maxReconnectDelay {
			d = maxReconnectDelay
		}
	}
	return d
}

func (nc *netConn) runOnce(ctx context.Context) error {
	addr := net.JoinHostPort(nc.net.Server, strconv.Itoa(nc.net.Port))
	var (
		conn net.Conn
		err  error
	)
	if nc.net.SSL {
		conn, err = tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true})
	} else {
		conn, err = net.Dial("tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("connect %s: %w", addr, err)
	}

	nc.mu.Lock()
	nc.conn = conn
	nc.mu.Unlock()
	defer func() {
		nc.mu.Lock()
		nc.conn = nil
		nc.mu.Unlock()
		conn.Close()
	}()

	if nc.net.SASLPass != "" {
		nc.writef("CAP REQ :sasl")
	}
	nc.writef("NICK %s", nc.net.Nick)
	nc.writef("USER %s 0 * :%s", nc.net.Nick, nc.net.Nick)
	return nc.readLoop(conn)
}

func (nc *netConn) readLoop(conn net.Conn) error {
	r := bufio.NewReader(conn)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return err
		}
		nc.handle(strings.TrimRight(line, "\r\n"))
	}
}

func (nc *netConn) handle(line string) {
	if strings.HasPrefix(line, "PING") {
		nc.writef("PONG %s", strings.TrimSpace(strings.TrimPrefix(line, "PING")))
		return
	}

	sender := ""
	rest := line
	if strings.HasPrefix(line, ":") {
		i := strings.IndexByte(line, ' ')
		if i < 0 {
			return
		}
		sender = line[1:i]
		rest = line[i+1:]
	}

	parts := strings.SplitN(rest, " ", 2)
	cmd := parts[0]
	args := ""
	if len(parts) == 2 {
		args = parts[1]
	}

	switch cmd {
	case "001":
		nc.failures = 0
		log.Printf("network %s: registered as %s", nc.net.Server, nc.net.Nick)
		nc.join()
	case "CAP":
		if strings.Contains(args, "sasl") && strings.Contains(args, "ACK") {
			nc.writef("AUTHENTICATE PLAIN")
		}
	case "AUTHENTICATE":
		if strings.TrimSpace(args) == "+" {
			nc.writef("AUTHENTICATE %s", saslPlain(nc.saslAccount(), nc.net.SASLPass))
		}
	case "900", "903":
		nc.writef("CAP END")
		log.Printf("network %s: SASL authenticated", nc.net.Server)
	case "904", "905", "906", "907":
		nc.writef("CAP END")
		log.Printf("network %s: SASL failed (%s)", nc.net.Server, cmd)
	case "PRIVMSG":
		if nc.c.logger != nil {
			nc.c.logger.Message(sender + " " + args)
		}
		nc.handlePrivmsg(sender, args)
	case "NOTICE":
		if nc.c.logger != nil {
			nc.c.logger.Notice(sender + " " + args)
		}
	case "353":
		nc.modes.applyNAMES(args)
	case "366":
		// end of NAMES (channel members are now tracked)
	case "MODE":
		nc.modes.applyMode(args)
	case "JOIN":
		if ch := strings.TrimPrefix(strings.TrimSpace(args), ":"); strings.EqualFold(ch, nc.net.Channel) {
			nc.modes.add(nick(sender))
		}
	case "PART":
		if f := strings.Fields(args); len(f) >= 1 && strings.EqualFold(strings.TrimPrefix(f[0], ":"), nc.net.Channel) {
			nc.modes.remove(nick(sender))
		}
	case "QUIT":
		nc.modes.remove(nick(sender))
	case "KICK":
		if f := strings.Fields(args); len(f) >= 2 && strings.EqualFold(strings.TrimPrefix(f[0], ":"), nc.net.Channel) {
			nc.modes.remove(f[1])
		}
	}
}

func (nc *netConn) join() {
	if nc.net.Key == "" {
		nc.writef("JOIN %s", nc.net.Channel)
	} else {
		nc.writef("JOIN %s %s", nc.net.Channel, nc.net.Key)
	}
	if h := nc.c.cfg.Headline; h != "" && !nc.net.NoAnnounce {
		nc.writef("PRIVMSG %s :%s", nc.net.Channel, h)
	}
}

func (nc *netConn) handlePrivmsg(sender, args string) {
	target := args
	message := ""
	if i := strings.Index(args, " :"); i >= 0 {
		target = args[:i]
		message = args[i+2:]
	}
	if !strings.EqualFold(target, nc.net.Nick) {
		return
	}

	from := nick(sender)
	if strings.HasPrefix(message, "\x01") && strings.HasSuffix(message, "\x01") && len(message) >= 2 {
		nc.handleCTCP(sender, from, message[1:len(message)-1])
		return
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(message)), "sdcc") {
		nc.handleSDCC(from, message)
		return
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(message)), "sxdcc") {
		nc.handleSXDCC(from, message)
		return
	}
	if isXDCCCommand(message) && !nc.c.cfg.XDCCEnable {
		nc.Notice(from, "** XDCC is disabled; use SXDCC (sxdcc list/send) **")
		return
	}
	if isDownload(message) && nc.c.acl != nil && !nc.c.acl.Empty() && !nc.c.acl.Allowed(sender) {
		nc.Notice(from, "** You are not authorized to download from this bot **")
		return
	}
	if isXDCCCommand(message) && nc.c.flood != nil && !nc.c.flood.Allow(sender) {
		return
	}
	switch xdccVerb(message) {
	case "list", "info":
		if !nc.canList(from) {
			nc.Notice(from, "** You are not allowed to list from this bot **")
			return
		}
	case "send", "batch":
		if !nc.canSend(from) {
			nc.Notice(from, "** You are not allowed to download from this bot **")
			return
		}
	}
	xdcc.Handle(nc, from, message)
}

// canList reports whether a user may xdcc list/info under restrictlist.
func (nc *netConn) canList(from string) bool {
	if !nc.c.cfg.RestrictList {
		return true
	}
	return nc.modes.member(from)
}

// canSend reports whether a user may xdcc send/batch under restrictsend, plus
// the optional need_voice/need_level refinement.
func (nc *netConn) canSend(from string) bool {
	if !nc.c.cfg.RestrictSend {
		return true
	}
	if nc.c.cfg.NeedVoice {
		return nc.modes.level(from) >= 1
	}
	if nc.c.cfg.NeedLevel > 0 {
		return nc.modes.level(from) >= nc.c.cfg.NeedLevel
	}
	return nc.modes.member(from)
}

// xdccVerb returns the lowercased XDCC command verb of a private message, or ""
// when it is not an "xdcc <verb>" command.
func xdccVerb(message string) string {
	f := strings.Fields(strings.TrimSpace(message))
	if len(f) < 2 || !strings.EqualFold(f[0], "xdcc") {
		return ""
	}
	return strings.ToLower(f[1])
}

// isDownload reports whether a private message is a download request (as
// opposed to list/info/owner), for hostmask access control.
func isDownload(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	return strings.HasPrefix(lower, "xdcc send") || strings.HasPrefix(lower, "xdcc batch")
}

func nick(sender string) string {
	if i := strings.IndexByte(sender, '!'); i >= 0 {
		return sender[:i]
	}
	if i := strings.IndexByte(sender, '@'); i >= 0 {
		return sender[:i]
	}
	return sender
}

// saslAccount returns the SASL authentication identity (defaults to the nick).
func (nc *netConn) saslAccount() string {
	if nc.net.SASLUser != "" {
		return nc.net.SASLUser
	}
	return nc.net.Nick
}

// saslPlain encodes the SASL PLAIN (RFC 4616) message authzid\0authcid\0passwd,
// where authzid and authcid are both the account (the common IRC/NickServ case).
func saslPlain(account, pass string) string {
	raw := account + "\x00" + account + "\x00" + pass
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

func (nc *netConn) periodicLoop(ctx context.Context, d time.Duration) {
	t := time.NewTicker(d)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			nc.writef("PRIVMSG %s :%s", nc.net.Channel, nc.c.cfg.PeriodicMsg)
		}
	}
}

// --- CTCP (DCC) handling ---

func (nc *netConn) handleCTCP(sender, from, ctcp string) {
	fields := strings.Fields(ctcp)
	if len(fields) < 2 || !strings.EqualFold(fields[0], "DCC") {
		return
	}
	switch strings.ToUpper(fields[1]) {
	case "SEND":
		nc.handleDCCSend(sender, from, fields)
	case "RESUME":
		nc.handleDCCResume(from, fields)
	case "CHAT":
		nc.handleDCCChat(from, fields)
	}
}

func (nc *netConn) handleDCCSend(sender, from string, f []string) {
	if len(f) < 6 {
		return
	}
	filename := f[2]
	port, err := strconv.Atoi(f[4])
	if err != nil {
		return
	}

	// Case 1: a passive reverse-send whose token matches a pending transfer.
	if len(f) >= 7 {
		if token, err := strconv.Atoi(f[6]); err == nil {
			c := nc.c
			c.tmu.Lock()
			tr := c.transfers[token]
			if tr != nil && tr.passive && strings.EqualFold(tr.nick, from) {
				delete(c.transfers, token)
				offset := tr.offset
				release := tr.release
				c.tmu.Unlock()
				ip := dccIP(f[3])
				tcpCfg := dcc.TCPConfig{NoDelay: nc.c.cfg.TCPNoDelay, BufferSize: nc.c.cfg.TCPBufferSize}
				go func() {
					defer func() {
						if release != nil {
							release()
						}
					}()
					if err := dcc.ConnectAndSend(ip, port, tr.path, offset, tcpCfg); err != nil {
						log.Printf("passive send to %s failed: %v", from, err)
					}
				}()
				return
			}
			c.tmu.Unlock()
		}
	}

	// Case 2: upload offer.
	if nc.c.cfg.UploadDir == "" {
		return
	}
	if nc.c.uploadACL != nil && !nc.c.uploadACL.Empty() && !nc.c.uploadACL.Allowed(sender) {
		nc.Notice(from, "** Upload rejected: you are not an upload host **")
		return
	}
	if nc.c.cfg.UploadMinSpace > 0 {
		if free, err := diskFreeBytes(nc.c.cfg.UploadDir); err == nil && free < nc.c.cfg.UploadMinSpace {
			nc.Notice(from, "** Upload rejected: not enough disk space **")
			return
		}
	}
	if len(f) >= 6 && nc.c.cfg.UploadMaxSize > 0 {
		if size, err := strconv.ParseInt(f[5], 10, 64); err == nil && size > nc.c.cfg.UploadMaxSize {
			nc.Notice(from, "** Upload rejected: file too large **")
			return
		}
	}
	go nc.c.receiveUpload(from, filename, dccIP(f[3]), port)
}

func (nc *netConn) handleDCCResume(from string, f []string) {
	if len(f) < 5 {
		return
	}
	filename := f[2]
	portStr := f[3]
	position, err := strconv.ParseInt(f[4], 10, 64)
	if err != nil {
		return
	}
	token := ""
	if len(f) >= 6 {
		token = f[5]
	}

	c := nc.c
	c.tmu.Lock()
	var tr *transfer
	if token != "" {
		if id, err := strconv.Atoi(token); err == nil {
			tr = c.transfers[id]
		}
	}
	if tr == nil {
		port, _ := strconv.Atoi(portStr)
		for _, t := range c.transfers {
			if strings.EqualFold(t.nick, from) && !t.passive && t.port == port &&
				(t.name == filename || t.name == filepath.Base(filename)) {
				tr = t
				break
			}
		}
	}
	if tr == nil || position >= tr.size {
		c.tmu.Unlock()
		nc.Notice(from, "DCC Resume Denied, unable to find transfer")
		return
	}
	tr.offset = position
	path := tr.path
	c.tmu.Unlock()

	c.setResume(path, position)
	nc.Privmsg(from, dcc.BuildDCCAccept(filename, portStr, position, token))
	log.Printf("resume accepted: %s @ %d (port %s)", filename, position, portStr)
}

func (nc *netConn) handleDCCChat(from string, f []string) {
	if len(f) < 5 {
		return
	}
	ip := dccIP(f[3])
	go nc.c.serveDCCChat(from, net.JoinHostPort(ip, f[4]))
}

func (nc *netConn) handleSXDCC(from, message string) {
	s := nc.c.sxdcc
	if s == nil {
		nc.Notice(from, "** SXDCC is not enabled **")
		return
	}

	// "sxdcc list" answers with the HTTP packlist URL instead of a TLS
	// rendezvous: only a URL crosses IRC (no filenames), so plain IRC clients
	// can open the packlist in a browser and SXDCC.md REQ-10 still holds. The
	// actual file data stays on TLS via "sxdcc send".
	if verb, _, ok := sxdcc.ParseTrigger(message); ok && verb == "list" {
		if url := nc.c.PacklistURL(); url != "" {
			nc.Notice(from, "** Packlist: "+url+" **")
		} else {
			nc.Notice(from, "** No packlist URL available yet **")
		}
		return
	}

	line, token, err := s.Rendezvous()
	if err != nil {
		nc.Notice(from, "** SXDCC error: "+err.Error()+" **")
		return
	}
	// Remember the trigger verb (e.g. "send 1") so the bot auto-executes it once
	// the client authenticates, instead of making the client repeat it
	// (SXDCC.md §9.4). A bare "sxdcc" carries no command to run.
	if verb, args, ok := sxdcc.ParseTrigger(message); ok && verb != "sxdcc" {
		verb = strings.ToUpper(verb)
		if args != "" {
			s.SetPendingCommand(token, verb+" "+args)
		} else {
			s.SetPendingCommand(token, verb)
		}
	}
	nc.Notice(from, line)
}

func (nc *netConn) handleSDCC(from, message string) {
	s := nc.c.sdcc
	if s == nil {
		nc.Notice(from, "** SDCC is not enabled **")
		return
	}
	line, _, err := s.Rendezvous()
	if err != nil {
		nc.Notice(from, "** SDCC error: "+err.Error()+" **")
		return
	}
	nc.Notice(from, line)
}

// --- xdcc.Client implementation (per network) ---

func (nc *netConn) Notice(target, msg string) {
	nc.writef("NOTICE %s :%s", target, msg)
}

func (nc *netConn) Privmsg(target, msg string) {
	nc.writef("PRIVMSG %s :%s", target, msg)
}

func (nc *netConn) Packlist() []packs.Pack {
	return nc.c.Packlist()
}

func (nc *netConn) PacklistURL() string {
	return nc.c.PacklistURL()
}

func (nc *netConn) SendPack(target string, p packs.Pack) error {
	return nc.c.SendPack(nc, target, p)
}

func (nc *netConn) CancelQueued(target string) bool {
	return nc.c.CancelQueued(target)
}

func (nc *netConn) Checksums() (bool, bool) {
	return !nc.c.cfg.NoMD5Sum, !nc.c.cfg.NoCRC32
}

func (nc *netConn) ListHeader() string { return nc.c.XDCCListHeader() }
func (nc *netConn) ListFooter() string { return nc.c.StatsFooter() }

// --- shared state ---

func (c *Client) scan() error {
	pl, err := packs.Scan(c.cfg.FileDir, c.scanOptions()...)
	if err != nil {
		return err
	}
	c.mu.Lock()
	prev := c.packlist
	merged := c.mergeLocked(pl)
	c.packlist = merged
	first := c.firstScan
	c.firstScan = false
	c.mu.Unlock()
	log.Printf("packlist: %d pack(s) from %s", len(merged), c.cfg.FileDir)
	if !first && c.cfg.AutoAddAnn {
		c.announceNew(prev, merged)
	}
	c.persist()
	return nil
}

// scanOptions builds the packs.Scan option list from the configured filters
// (noduplicatefiles, adddir_exclude/match, adddir_min_size, nat_sort).
func (c *Client) scanOptions() []packs.Option {
	var opts []packs.Option
	if c.cfg.NaturalSort {
		opts = append(opts, packs.WithNaturalSort())
	}
	if c.cfg.NoDuplicateFiles {
		opts = append(opts, packs.WithDedup())
	}
	if len(c.cfg.AddDirExclude) > 0 {
		opts = append(opts, packs.WithExclude(c.cfg.AddDirExclude...))
	}
	if len(c.cfg.AddDirMatch) > 0 {
		opts = append(opts, packs.WithMatch(c.cfg.AddDirMatch...))
	}
	if c.cfg.AddDirMinSize > 0 {
		opts = append(opts, packs.WithMinSize(c.cfg.AddDirMinSize))
	}
	return opts
}

func (c *Client) rescanLoop(ctx context.Context, d time.Duration) {
	t := time.NewTicker(d)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := c.scan(); err != nil {
				log.Printf("rescan: %v", err)
			}
		}
	}
}

func (c *Client) startWorkers() {
	n := c.cfg.SlotsMax
	if n < 1 {
		n = 1
	}
	for i := 0; i < n; i++ {
		go c.serveQueue()
	}
}

func (c *Client) serveQueue() {
	for {
		_, key, release := c.q.Wait()
		item := c.takePending(key)
		if item == nil {
			release()
			continue
		}
		c.ServeTransfer(item, release)
	}
}

func (c *Client) takePending(key string) *queueItem {
	c.qmu.Lock()
	defer c.qmu.Unlock()
	item := c.pending[key]
	delete(c.pending, key)
	return item
}

// CancelQueued removes the target's oldest queued (not yet started) transfer.
func (c *Client) CancelQueued(target string) bool {
	c.qmu.Lock()
	defer c.qmu.Unlock()
	for key, item := range c.pending {
		if item.nick == target {
			delete(c.pending, key)
			c.q.Cancel(target, key)
			return true
		}
	}
	return false
}

// SendPack enqueues a pack for delivery on the given network.
func (c *Client) SendPack(nc *netConn, target string, p packs.Pack) error {
	key := strconv.FormatInt(atomic.AddInt64(&c.nextKey, 1), 10)
	c.qmu.Lock()
	c.pending[key] = &queueItem{nick: target, pack: p, nc: nc}
	c.qmu.Unlock()

	pos, err := c.q.Submit(target, key, false)
	if err != nil {
		c.qmu.Lock()
		delete(c.pending, key)
		c.qmu.Unlock()
		return err
	}
	if pos > 1 {
		nc.Notice(target, fmt.Sprintf("** All slots full, queued as #%d **", pos))
	}
	return nil
}

// ServeTransfer performs the actual DCC transfer for a queued item.
func (c *Client) ServeTransfer(item *queueItem, release func()) {
	p := item.pack
	ipInt, err := dcc.IPToInt(c.cfg.DCCIP)
	if err != nil {
		release()
		item.nc.Notice(item.nick, "** Send failed: "+err.Error()+" **")
		return
	}

	tr := &transfer{nick: item.nick, name: p.Name, path: p.Path, size: p.Size, passive: c.cfg.PassiveDCC, release: release}
	if c.cfg.TransferMaxSpeed > 0 {
		tr.tb = throttle.NewTokenBucket(c.cfg.TransferMaxSpeed)
	}

	if tr.passive {
		c.tmu.Lock()
		c.nextToken++
		tr.token = c.nextToken
		c.transfers[tr.token] = tr
		c.tmu.Unlock()
		item.nc.Privmsg(item.nick, dcc.BuildPassiveDCCSend(p.Name, ipInt, p.Size, tr.token))
		return
	}

	ln, port, err := dcc.Listen(c.cfg.PortStart, c.cfg.PortLimit)
	if err != nil {
		release()
		item.nc.Notice(item.nick, "** Send failed: "+err.Error()+" **")
		return
	}
	tr.ln = ln
	tr.port = port

	c.tmu.Lock()
	c.nextToken++
	tr.token = c.nextToken
	c.transfers[tr.token] = tr
	c.tmu.Unlock()

	item.nc.Privmsg(item.nick, dcc.BuildDCCSend(p.Name, ipInt, port, p.Size))
	c.serveActive(tr)
	release()
}

func (c *Client) serveActive(tr *transfer) {
	defer func() {
		if tr.ln != nil {
			tr.ln.Close()
		}
		c.removeTransfer(tr.token)
	}()
	conn, err := tr.ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	dcc.ConfigureTCP(conn, dcc.TCPConfig{NoDelay: c.cfg.TCPNoDelay, BufferSize: c.cfg.TCPBufferSize})

	c.tmu.Lock()
	offset := tr.offset
	c.tmu.Unlock()

	w := io.Writer(conn)
	if tr.tb != nil {
		w = throttle.NewThrottledWriter(w, tr.tb)
	}
	if c.globalTB != nil {
		w = throttle.NewThrottledWriter(w, c.globalTB)
	}
	cw := &countingWriter{w: w}
	if err := dcc.StreamTo(cw, tr.path, offset); err != nil {
		log.Printf("send %s failed: %v", tr.name, err)
		c.setResume(tr.path, offset+cw.n)
		return
	}
	c.RecordSent(tr.path, cw.n)
	c.clearResume(tr.path)
}

// countingWriter counts the bytes written through it, for resume-point tracking.
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

func (c *Client) removeTransfer(token int) {
	c.tmu.Lock()
	delete(c.transfers, token)
	c.tmu.Unlock()
}

func dccIP(s string) string {
	if strings.Contains(s, ".") {
		return s
	}
	if v, err := strconv.ParseUint(s, 10, 32); err == nil {
		return dcc.IPFromInt(uint32(v))
	}
	return s
}

// setResume records a pack's resume offset, and clearResume drops it on a full
// send. Together they persist per-pack resume bookkeeping in the state file.
func (c *Client) setResume(path string, offset int64) {
	c.mu.Lock()
	if c.resume == nil {
		c.resume = map[string]int64{}
	}
	c.resume[path] = offset
	c.mu.Unlock()
}

func (c *Client) clearResume(path string) {
	c.mu.Lock()
	delete(c.resume, path)
	c.mu.Unlock()
}

// RecordSent adds the transferred byte count to the aggregate and per-pack
// stats (persisted in the state file). It is the sxdcc.Host.RecordSent impl.
func (c *Client) RecordSent(path string, bytes int64) {
	if bytes <= 0 {
		return
	}
	c.mu.Lock()
	if c.sent == nil {
		c.sent = map[string]int64{}
	}
	c.sent[path] += bytes
	c.totalSent += bytes
	for i := range c.packlist {
		if c.packlist[i].Path == path {
			c.packlist[i].Sent = c.sent[path]
			break
		}
	}
	c.mu.Unlock()
}

// TotalOffered returns the sum of all current pack sizes (bytes).
func (c *Client) TotalOffered() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var sum int64
	for _, p := range c.packlist {
		sum += p.Size
	}
	return sum
}

// TotalTransferred returns the aggregate bytes transferred.
func (c *Client) TotalTransferred() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.totalSent
}

// SlotStatus returns the classic XDCC slot header "used of total slots open".
func (c *Client) SlotStatus() string {
	s := c.q.Snapshot()
	used := c.cfg.SlotsMax - s.FreeSlots
	if used < 0 {
		used = 0
	}
	return fmt.Sprintf("%d of %d slots open", s.FreeSlots, c.cfg.SlotsMax)
}

// TransfersOpen returns the SXDCC "transfers open" count (free slots).
func (c *Client) TransfersOpen() int {
	return c.q.Snapshot().FreeSlots
}

// XDCCListHeader returns the classic list header "N of M slots open".
func (c *Client) XDCCListHeader() string { return c.SlotStatus() }

// SXDCCListHeader returns the secure list header "N transfers open".
func (c *Client) SXDCCListHeader() string {
	return fmt.Sprintf("%d transfers open", c.TransfersOpen())
}

// StatsFooter returns the iroffer-style totals line.
func (c *Client) StatsFooter() string {
	return fmt.Sprintf("Total Offered: %sB  Total Transferred: %sB",
		humanBytes(c.TotalOffered()), humanBytes(c.TotalTransferred()))
}

// ListHeader / ListFooter satisfy the sxdcc.Host interface (the "N transfers
// open" header + totals footer for the secure LIST reply).
func (c *Client) ListHeader() string { return c.SXDCCListHeader() }
func (c *Client) ListFooter() string { return c.StatsFooter() }

// humanBytes renders a byte count in the iroffer size style ("12.0M"), used by
// the totals footer (the "B" suffix is appended by the caller).
func humanBytes(n int64) string {
	const unit = int64(1024)
	if n < unit {
		return fmt.Sprintf("%d", n)
	}
	div := unit
	exp := 0
	for m := n / unit; m >= unit && exp < 5; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%c", float64(n)/float64(div), "KMGTP"[exp])
}

// diskFreeBytes returns the free disk space (bytes) available on the filesystem
// containing dir. Linux-only (syscall.Statfs); the bot only builds for Linux.
func diskFreeBytes(dir string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}

func (c *Client) receiveUpload(from, filename, ip string, port int) {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(ip, strconv.Itoa(port)), 15*time.Second)
	if err != nil {
		log.Printf("upload connect to %s failed: %v", from, err)
		return
	}
	defer conn.Close()
	dcc.ConfigureTCP(conn, dcc.TCPConfig{NoDelay: c.cfg.TCPNoDelay, BufferSize: c.cfg.TCPBufferSize})

	safe := filepath.Base(filename)
	if err := os.MkdirAll(c.cfg.UploadDir, 0o755); err != nil {
		log.Printf("upload mkdir: %v", err)
		return
	}
	f, err := os.Create(filepath.Join(c.cfg.UploadDir, safe))
	if err != nil {
		log.Printf("upload create: %v", err)
		return
	}
	defer f.Close()

	buf := make([]byte, 32*1024)
	var total int64
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				log.Printf("upload write: %v", werr)
				return
			}
			total += int64(n)
			var ack [4]byte
			t := uint32(total)
			ack[0] = byte(t >> 24)
			ack[1] = byte(t >> 16)
			ack[2] = byte(t >> 8)
			ack[3] = byte(t)
			if _, werr := conn.Write(ack[:]); werr != nil {
				return
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Printf("upload read: %v", err)
			return
		}
	}
	log.Printf("upload from %s: %s (%d bytes)", from, safe, total)
}

func (c *Client) serveDCCChat(from, addr string) {
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		log.Printf("dcc chat to %s failed: %v", from, err)
		return
	}
	defer conn.Close()
	dcc.ConfigureTCP(conn, dcc.TCPConfig{NoDelay: c.cfg.TCPNoDelay, BufferSize: c.cfg.TCPBufferSize})

	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	send := func(s string) { _, _ = rw.WriteString(s + "\r\n"); _ = rw.Flush() }

	send(fmt.Sprintf("** Dinoga DCC chat for %s **", from))
	send(fmt.Sprintf("** %d pack(s) **", len(c.Packlist())))
	for {
		_ = conn.SetReadDeadline(time.Now().Add(c.statusInterval()))
		line, err := rw.ReadString('\n')
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				c.writeChatStatus(send)
				continue
			}
			return
		}
		switch strings.ToLower(strings.TrimRight(line, "\r\n \t")) {
		case "status":
			c.writeChatStatus(send)
		case "list":
			pl := c.Packlist()
			for i, p := range pl {
				if i >= 20 {
					break
				}
				send(fmt.Sprintf("#%d [%d] %s", p.Number, p.Size, p.Name))
			}
		case "quit", "exit", "close":
			return
		}
	}
}

// writeChatStatus emits the DCC chat status screen (slots/queues/packs).
func (c *Client) writeChatStatus(send func(string)) {
	s := c.q.Snapshot()
	used := c.cfg.SlotsMax - s.FreeSlots
	if used < 0 {
		used = 0
	}
	send(fmt.Sprintf("packs: %d", len(c.Packlist())))
	send(fmt.Sprintf("slots: %d/%d in use", used, c.cfg.SlotsMax))
	send(fmt.Sprintf("queue: %d queued, %d idle", s.QueueLen, s.IdleLen))
}

// --- SXDCC / web / admin ---

func (c *Client) startSXDCC() error {
	if !c.cfg.SXDCCEnable {
		return nil
	}
	certFile := c.cfg.CertFile
	keyFile := c.cfg.KeyFile
	if certFile == "" {
		certFile = "/config/certs/sxdcc-cert.pem"
	}
	if keyFile == "" {
		keyFile = "/config/certs/sxdcc-key.pem"
	}
	advertiseIP := c.cfg.DCCIP
	if advertiseIP == "" {
		if vpn.IsEnabled() {
			// Behind a VPN the public IP is only known at runtime; start with a
			// placeholder and let the watcher replace it (and the cert).
			advertiseIP = "0.0.0.0"
			log.Printf("sxdcc: VPN enabled, advertising placeholder IP until the forwarded address is discovered")
		} else {
			return fmt.Errorf("sxdcc requires usenatip (public IP)")
		}
	}
	sxdccCfg := sxdcc.Config{
		CertFile:  certFile,
		KeyFile:   keyFile,
		IP:        advertiseIP,
		PortStart: c.cfg.PortStart,
		PortLimit: c.cfg.PortLimit,
		Passive:   c.cfg.PassiveDCC,
		Slot:      c.q,
		Owner:     c.cfg.OwnerNick,
	}
	s, err := sxdcc.New(c, sxdccCfg)
	if err != nil {
		return err
	}
	port := c.cfg.ControlPort
	if port <= 0 {
		port = c.cfg.PortStart
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return err
	}
	c.sxdcc = s
	s.SetControlPort(port)

	ipInt, err := dcc.IPToInt(advertiseIP)
	if err != nil {
		ln.Close()
		return err
	}
	c.sdcc = sdcc.New(c.adminServer(), ipInt, s.Pin())
	c.sdcc.SetAdvertise(ipInt, port, s.Pin())
	c.setAdvertise(advertiseIP, port)

	log.Printf("sxdcc: listening on control port %d, pin %s", port, s.Pin())
	go c.serveSecureDCC(ln, s, c.sdcc, c.webHandler())

	if vpn.IsEnabled() {
		go c.watchVPN()
	}
	return nil
}

// serveSecureDCC owns the single shared listener: plain HTTP for the packlist,
// TLS for SXDCC/SDCC. Each connection is demultiplexed by its first byte — a TLS
// ClientHello begins with 0x16, an HTTP request begins with an ASCII method
// letter — and handed to the matching handler. No port is "locked": the accept
// loop serves unlimited concurrent connections, one goroutine each.
func (c *Client) serveSecureDCC(ln net.Listener, s *sxdcc.Server, sdccSrv *sdcc.Server, webHandler http.Handler) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("sxdcc: %v", err)
			return
		}
		go c.handleSecureConn(conn, s, sdccSrv, webHandler)
	}
}

func (c *Client) handleSecureConn(conn net.Conn, s *sxdcc.Server, sdccSrv *sdcc.Server, webHandler http.Handler) {
	r := bufio.NewReader(conn)

	// Distinguish plain HTTP from TLS without consuming the first byte.
	first, err := r.Peek(1)
	if err != nil {
		conn.Close()
		return
	}
	if isHTTPMethodByte(first[0]) {
		c.serveHTTP(conn, r, webHandler)
		return
	}

	defer conn.Close()
	tlsConn := tls.Server(bufferedConn{Conn: conn, r: r}, s.TLSConfig())
	if err := tlsConn.Handshake(); err != nil {
		return
	}
	tr := bufio.NewReader(tlsConn)
	line, err := tr.ReadString('\n')
	if err != nil {
		return
	}
	if len(line) > 8192 {
		return
	}
	line = strings.TrimRight(line, "\r\n")
	if f := strings.Fields(line); len(f) == 2 && strings.EqualFold(f[0], "ADMIN") {
		sdccSrv.HandleConn(tlsConn, tr, line)
		return
	}
	s.HandleConn(tlsConn, tr, line)
}

// serveHTTP serves the web handler over a single accepted plain-HTTP
// connection (used when the shared port receives a browser/HTTP client).
func (c *Client) serveHTTP(conn net.Conn, r *bufio.Reader, handler http.Handler) {
	bconn := bufferedConn{Conn: conn, r: r}
	srv := &http.Server{Handler: handler}
	l := newOneConnListener(bconn)
	// Release Serve (and thus return) only after the connection is finished, so
	// the response body is flushed before this goroutine exits.
	srv.ConnState = func(_ net.Conn, st http.ConnState) {
		if st == http.StateClosed {
			_ = l.Close()
		}
	}
	_ = srv.Serve(l)
}

// webHandler lazily builds and caches the HTTP packlist handler.
func (c *Client) webHandler() http.Handler {
	c.webMu.Lock()
	defer c.webMu.Unlock()
	if c.webHandlerCache != nil {
		return c.webHandlerCache
	}
	allow, err := web.ParseCIDRs(c.cfg.HTTPAllow)
	if err != nil {
		log.Printf("web: http_allow: %v", err)
		allow = nil
	}
	deny, err := web.ParseCIDRs(c.cfg.HTTPDeny)
	if err != nil {
		log.Printf("web: http_deny: %v", err)
		deny = nil
	}
	c.webHandlerCache = web.New(c.Packlist, web.Config{
		Allow:  allow,
		Deny:   deny,
		Header: c.XDCCListHeader,
		Footer: c.StatsFooter,
	})
	return c.webHandlerCache
}

// PacklistURL returns the public HTTP URL of the packlist, or "" when no
// advertised address is known.
func (c *Client) PacklistURL() string {
	ip, port := c.advertise()
	if ip == "" || port <= 0 {
		return ""
	}
	return fmt.Sprintf("http://%s:%d/packlist.txt", ip, port)
}

// setAdvertise records the current advertised public address.
func (c *Client) setAdvertise(ip string, port int) {
	c.advMu.Lock()
	c.advIP, c.advPort = ip, port
	c.advMu.Unlock()
}

// advertise returns the current advertised public address.
func (c *Client) advertise() (string, int) {
	c.advMu.Lock()
	defer c.advMu.Unlock()
	return c.advIP, c.advPort
}

// watchVPN polls hotio's forwarded port + public IP and re-advertises (and re-
// generates the SXDCC identity when the IP changes) live, without dropping the
// listener.
func (c *Client) watchVPN() {
	w := vpn.Watcher{
		PortFile: vpn.PortFile(),
		WgConf:   vpn.WireguardConf(),
		EchoURL:  vpn.DefaultEchoURL,
		Interval: 30 * time.Second,
	}
	w.Run(context.Background(), c.applyVPNAddress)
}

// applyVPNAddress pushes a discovered VPN address into the SXDCC/SDCC servers
// and the client's advertised address.
func (c *Client) applyVPNAddress(addr vpn.Address) {
	if c.sxdcc == nil || c.sdcc == nil {
		return
	}
	pinChanged, err := c.sxdcc.SetAdvertise(addr.IP, addr.Port)
	if err != nil {
		log.Printf("vpn: SetAdvertise %v: %v", addr, err)
		return
	}
	if ipInt, err := dcc.IPToInt(addr.IP); err == nil {
		c.sdcc.SetAdvertise(ipInt, addr.Port, c.sxdcc.Pin())
	}
	c.setAdvertise(addr.IP, addr.Port)
	log.Printf("vpn: advertised address %s:%d (pin changed: %v)", addr.IP, addr.Port, pinChanged)
}

func (c *Client) startWeb() error {
	if !c.cfg.HTTPEnable {
		return nil
	}
	srv := &http.Server{
		Addr:    fmt.Sprintf("0.0.0.0:%d", c.cfg.HTTPPort),
		Handler: c.webHandler(),
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("web: %v", err)
		}
	}()
	log.Printf("web: listening on :%d", c.cfg.HTTPPort)
	return nil
}

func (c *Client) adminServer() *admin.Server {
	c.adminMu.Lock()
	defer c.adminMu.Unlock()
	if c.adminSrv == nil {
		c.adminSrv = admin.New(admin.Config{
			Password:  c.cfg.AdminPass,
			PackCount: func() int { return len(c.Packlist()) },
			PackNames: func() []string {
				pl := c.Packlist()
				out := make([]string, len(pl))
				for i, p := range pl {
					out[i] = p.Name
				}
				return out
			},
		})
		c.registerAdminCommands(c.adminSrv)
	}
	return c.adminSrv
}

// AdminRegistry exposes the bot's admin command registry for the TUI and other
// non-network frontends.
func (c *Client) AdminRegistry() *admin.Registry {
	return c.adminServer().Registry()
}

// --- sxdcc.Host implementation ---

func (c *Client) Packlist() []packs.Pack {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]packs.Pack, len(c.packlist))
	copy(out, c.packlist)
	return out
}

func (c *Client) PackByNumber(n int) (packs.Pack, bool) {
	pl := c.Packlist()
	if n < 1 || n > len(pl) {
		return packs.Pack{}, false
	}
	return pl[n-1], true
}

func (c *Client) OpenFile(path string) (io.ReadCloser, error) {
	return os.Open(path)
}

func (c *Client) Logf(format string, args ...any) {
	log.Printf(format, args...)
}
