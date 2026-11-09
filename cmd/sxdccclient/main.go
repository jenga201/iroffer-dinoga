// Command sxdccclient is a minimal SXDCC client for end-to-end testing. It
// connects to IRC, triggers a bot's rendezvous, verifies the bot's SPKI pin over
// TLS 1.3, and speaks the control protocol (AUTH/LIST/SEND) and the active TLS
// data channel, writing a downloaded pack to disk.
package main

import (
	"bufio"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jenga201/dinoga/internal/dcc"
	"github.com/jenga201/dinoga/internal/sxdcc"
)

func main() {
	server := flag.String("server", "127.0.0.1", "IRC server host")
	port := flag.Int("port", 6667, "IRC server port")
	ircSSL := flag.Bool("irc-ssl", false, "use TLS for the IRC connection")
	nick := flag.String("nick", "sxdccTester", "client nick")
	channel := flag.String("channel", "#frog", "channel to join")
	bot := flag.String("bot", "dinoga", "SXDCC bot nick")
	pack := flag.Int("pack", 0, "pack number to download (0 = LIST only)")
	downloadDir := flag.String("download", ".", "download directory")
	owner := flag.Bool("owner", false, "query OWNER and exit")
	cancel := flag.Bool("cancel", false, "SEND then CANCEL (needs -pack); verifies the transfer is removed")
	flag.Parse()

	rv, err := discover(*server, *port, *ircSSL, *nick, *channel, *bot)
	if err != nil {
		fatalf("rendezvous: %v", err)
	}
	fmt.Printf("rendezvous: %s:%d token=%s pin=%s\n", rv.ip, rv.port, rv.token, rv.pin)

	ctl, err := dialTLS(rv.addr(), rv.pin)
	if err != nil {
		fatalf("control dial: %v", err)
	}
	defer ctl.Close()

	if err := authed(ctl, rv.token); err != nil {
		fatalf("auth: %v", err)
	}

	if *owner {
		if err := ownerCmd(ctl); err != nil {
			fatalf("owner: %v", err)
		}
		return
	}
	if *cancel {
		if *pack == 0 {
			fatalf("--cancel requires --pack <n>")
		}
		if err := cancelCmd(ctl, rv.pin, *pack); err != nil {
			fatalf("cancel: %v", err)
		}
		return
	}

	if *pack == 0 {
		if err := list(ctl); err != nil {
			fatalf("list: %v", err)
		}
		return
	}

	if err := download(ctl, rv.pin, *pack, *downloadDir); err != nil {
		fatalf("download: %v", err)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "sxdccclient: "+format+"\n", args...)
	os.Exit(1)
}

// rendezvous is the parsed SXDCC CONNECT line.
type rendezvous struct {
	ip    string
	port  int
	token string
	pin   string
}

func (r rendezvous) addr() string {
	return net.JoinHostPort(r.ip, strconv.Itoa(r.port))
}

// lineConn is a CRLF line-framed connection.
type lineConn struct {
	conn net.Conn
	r    *bufio.Reader
	w    *bufio.Writer
}

func newLineConn(conn net.Conn) *lineConn {
	return &lineConn{conn: conn, r: bufio.NewReader(conn), w: bufio.NewWriter(conn)}
}

func (l *lineConn) write(line string) error {
	if _, err := l.w.WriteString(line + "\r\n"); err != nil {
		return err
	}
	return l.w.Flush()
}

func (l *lineConn) read() (string, error) {
	s, err := l.r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(s, "\r\n"), nil
}

func (l *lineConn) Close() error { return l.conn.Close() }

// discover connects to IRC, registers, joins, triggers the bot, and parses the
// SXDCC CONNECT rendezvous response.
func discover(server string, port int, ssl bool, nick, channel, bot string) (rendezvous, error) {
	var raw net.Conn
	var err error
	addr := net.JoinHostPort(server, strconv.Itoa(port))
	if ssl {
		raw, err = tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
	} else {
		raw, err = net.Dial("tcp", addr)
	}
	if err != nil {
		return rendezvous{}, err
	}
	c := newLineConn(raw)
	defer c.Close()

	if err := c.write("NICK " + nick); err != nil {
		return rendezvous{}, err
	}
	if err := c.write("USER " + nick + " 0 * :" + nick); err != nil {
		return rendezvous{}, err
	}

	joined := false
	deadline := time.Now().Add(45 * time.Second)
	for {
		// Short read poll so we can re-trigger until the bot is on IRC.
		_ = raw.SetReadDeadline(time.Now().Add(2 * time.Second))
		line, err := c.read()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				if time.Now().After(deadline) {
					return rendezvous{}, fmt.Errorf("timed out waiting for %s's rendezvous", bot)
				}
				if joined {
					if err := c.write("PRIVMSG " + bot + " :sxdcc"); err != nil {
						return rendezvous{}, err
					}
				}
				continue
			}
			return rendezvous{}, err
		}

		switch {
		case strings.HasPrefix(line, "PING "):
			if err := c.write("PONG " + strings.TrimPrefix(line, "PING ")); err != nil {
				return rendezvous{}, err
			}
		case strings.Contains(line, " 001 "), strings.Contains(line, " 376 "):
			if !joined {
				if err := c.write("JOIN " + channel); err != nil {
					return rendezvous{}, err
				}
				joined = true
			}
		}

		if i := strings.Index(line, "SXDCC CONNECT "); i >= 0 {
			f := strings.Fields(line[i:])
			if len(f) == 6 { // SXDCC CONNECT <ip> <port> <token> <pin>
				ipInt, err := strconv.ParseUint(f[2], 10, 32)
				if err != nil {
					return rendezvous{}, err
				}
				p, err := strconv.Atoi(f[3])
				if err != nil {
					return rendezvous{}, err
				}
				return rendezvous{ip: dcc.IPFromInt(uint32(ipInt)), port: p, token: f[4], pin: f[5]}, nil
			}
		}
	}
}

// dialTLS dials addr over TLS 1.3 and verifies the server's SPKI pin against pin.
func dialTLS(addr, pin string) (*lineConn, error) {
	raw, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	conn := tls.Client(raw, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13})
	if err := conn.Handshake(); err != nil {
		raw.Close()
		return nil, err
	}
	leaf := conn.ConnectionState().PeerCertificates[0]
	got := sxdcc.SPKIPin(leaf)
	if !sxdcc.PinEqual(got, pin) {
		conn.Close()
		return nil, fmt.Errorf("SPKI pin mismatch: got %s, want %s", got, pin)
	}
	return newLineConn(conn), nil
}

// authed sends AUTH <token> and expects OK.
func authed(ctl *lineConn, token string) error {
	if err := ctl.write("AUTH " + token); err != nil {
		return err
	}
	line, err := ctl.read()
	if err != nil {
		return err
	}
	if strings.HasPrefix(line, "ERR ") {
		return fmt.Errorf("auth rejected: %s", line)
	}
	if !strings.HasPrefix(line, "OK") {
		return fmt.Errorf("unexpected auth reply: %s", line)
	}
	fmt.Println("auth: OK")
	return nil
}

// list sends LIST and prints the pack list until OK.
func list(ctl *lineConn) error {
	if err := ctl.write("LIST"); err != nil {
		return err
	}
	for {
		line, err := ctl.read()
		if err != nil {
			return err
		}
		if strings.HasPrefix(line, "ERR ") {
			return fmt.Errorf("list: %s", line)
		}
		if line == "OK" || strings.HasPrefix(line, "OK ") {
			return nil
		}
		fmt.Println(line)
	}
}

type offer struct {
	pack      int
	size      int64
	name      string
	ip        string
	port      int
	datatoken string
}

// download sends SEND, parses the active offer, opens a TLS data channel, sends
// DATA <datatoken>, and streams the pack to dir.
func download(ctl *lineConn, pin string, pack int, dir string) error {
	if err := ctl.write(fmt.Sprintf("SEND %d", pack)); err != nil {
		return err
	}
	var off *offer
	for {
		line, err := ctl.read()
		if err != nil {
			return err
		}
		if strings.HasPrefix(line, "ERR ") {
			return fmt.Errorf("send: %s", line)
		}
		if strings.HasPrefix(line, "SEND ") {
			o, err := parseOffer(line)
			if err != nil {
				return err
			}
			off = &o
			break
		}
		// QUEUE / READY / OK: keep waiting.
	}

	dataConn, err := dialTLS(off.addr(), pin)
	if err != nil {
		return fmt.Errorf("data dial: %v", err)
	}
	defer dataConn.Close()

	if err := dataConn.write("DATA " + off.datatoken); err != nil {
		return err
	}

	outPath := filepath.Join(dir, off.name)
	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()

	n, err := io.Copy(out, dataConn.r)
	if err != nil {
		return fmt.Errorf("data read: %v", err)
	}
	fmt.Printf("downloaded %d bytes (%s) -> %s\n", n, off.name, outPath)

	// Expect DONE on the control channel.
	for {
		line, err := ctl.read()
		if err != nil {
			return err
		}
		if strings.HasPrefix(line, "DONE ") {
			fmt.Println(line)
			return nil
		}
		if strings.HasPrefix(line, "ERR ") {
			return fmt.Errorf("done: %s", line)
		}
	}
}

// parseOffer parses "SEND #<pack> <size> "<name>" <ip> <port> <datatoken>".
func parseOffer(line string) (offer, error) {
	f := strings.Fields(line)
	if len(f) != 7 {
		return offer{}, fmt.Errorf("malformed SEND offer: %s", line)
	}
	pack, err := strconv.Atoi(strings.TrimPrefix(f[1], "#"))
	if err != nil {
		return offer{}, err
	}
	size, err := strconv.ParseInt(f[2], 10, 64)
	if err != nil {
		return offer{}, err
	}
	name, err := strconv.Unquote(f[3])
	if err != nil {
		name = f[3]
	}
	ipInt, err := strconv.ParseUint(f[4], 10, 32)
	if err != nil {
		return offer{}, err
	}
	port, err := strconv.Atoi(f[5])
	if err != nil {
		return offer{}, err
	}
	return offer{pack: pack, size: size, name: name, ip: dcc.IPFromInt(uint32(ipInt)), port: port, datatoken: f[6]}, nil
}

// ownerCmd sends OWNER and prints the reply.
func ownerCmd(ctl *lineConn) error {
	if err := ctl.write("OWNER"); err != nil {
		return err
	}
	line, err := ctl.read()
	if err != nil {
		return err
	}
	fmt.Println(line)
	return nil
}

// cancelCmd sends SEND, parses the offer, sends CANCEL, and verifies a late DATA
// is rejected (proving the transfer was removed).
func cancelCmd(ctl *lineConn, pin string, pack int) error {
	if err := ctl.write(fmt.Sprintf("SEND %d", pack)); err != nil {
		return err
	}
	var off *offer
	for {
		line, err := ctl.read()
		if err != nil {
			return err
		}
		if strings.HasPrefix(line, "ERR ") {
			return fmt.Errorf("send: %s", line)
		}
		if strings.HasPrefix(line, "SEND ") {
			o, err := parseOffer(line)
			if err != nil {
				return err
			}
			off = &o
			break
		}
	}

	if err := ctl.write("CANCEL"); err != nil {
		return err
	}
	line, err := ctl.read()
	if err != nil {
		return err
	}
	if !strings.HasPrefix(line, "OK") {
		return fmt.Errorf("cancel reply: %s", line)
	}
	fmt.Printf("cancel: %s (offered #%d)\n", strings.TrimSpace(line), off.pack)

	// A late DATA on the offered token must now be rejected.
	dconn, err := dialTLS(off.addr(), pin)
	if err != nil {
		return fmt.Errorf("data dial after cancel: %v", err)
	}
	defer dconn.Close()
	if err := dconn.write("DATA " + off.datatoken); err != nil {
		return err
	}
	late, err := dconn.read()
	if err != nil {
		return err
	}
	fmt.Printf("data after cancel: %s\n", late)
	return nil
}

func (o offer) addr() string {
	return net.JoinHostPort(o.ip, strconv.Itoa(o.port))
}
