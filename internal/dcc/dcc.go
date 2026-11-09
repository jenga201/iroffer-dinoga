// Package dcc implements the DCC wire helpers for the bot: the 32-bit IPv4
// integer conversion, port-range listener, and the DCC SEND/ACCEPT/passive
// message builders plus the file streamer.
package dcc

import (
	"fmt"
	"io"
	"net"
	"os"
	"time"
)

// IPToInt converts an IPv4 string to the 32-bit integer used in a DCC SEND
// ctcp payload (e.g. 2.31.76.192 -> 35605696).
func IPToInt(s string) (uint32, error) {
	ip := net.ParseIP(s)
	if ip == nil {
		return 0, fmt.Errorf("invalid IP %q", s)
	}
	v4 := ip.To4()
	if v4 == nil {
		return 0, fmt.Errorf("not an IPv4 address: %q", s)
	}
	return uint32(v4[0])<<24 | uint32(v4[1])<<16 | uint32(v4[2])<<8 | uint32(v4[3]), nil
}

// IPFromInt converts a 32-bit IPv4 integer back to dotted-quad form
// (e.g. 35605696 -> "2.31.76.192").
func IPFromInt(n uint32) string {
	return net.IPv4(byte(n>>24), byte(n>>16), byte(n>>8), byte(n)).String()
}

// Listen opens the first free port in [start, limit].
func Listen(start, limit int) (net.Listener, int, error) {
	for p := start; p <= limit; p++ {
		ln, err := net.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", p))
		if err == nil {
			return ln, p, nil
		}
	}
	return nil, 0, fmt.Errorf("no free DCC port in %d-%d", start, limit)
}

// TCPConfig carries optional TCP socket tuning for DCC sockets.
type TCPConfig struct {
	NoDelay    bool  // set TCP_NODELAY
	BufferSize int64 // SO_SNDBUF/SO_RCVBUF in bytes (0 = OS default)
}

// ConfigureTCP applies nodelay and send/receive buffer sizes to a TCP
// connection. It is best-effort and no-ops on non-TCP connections.
func ConfigureTCP(conn net.Conn, cfg TCPConfig) {
	tc, ok := conn.(*net.TCPConn)
	if !ok {
		return
	}
	if cfg.NoDelay {
		_ = tc.SetNoDelay(true)
	}
	if cfg.BufferSize > 0 {
		n := int(cfg.BufferSize)
		_ = tc.SetReadBuffer(n)
		_ = tc.SetWriteBuffer(n)
	}
}

// BuildDCCSend returns the CTCP payload for an active DCC SEND (bot listens).
func BuildDCCSend(filename string, ipInt uint32, port int, size int64) string {
	return fmt.Sprintf("\x01DCC SEND %s %d %d %d\x01", filename, ipInt, port, size)
}

// BuildPassiveDCCSend returns the CTCP payload for a passive DCC SEND. The
// explicit zero port signals reverse DCC: the downloader replies with a
// reverse DCC SEND carrying its own address, and the bot connects out.
func BuildPassiveDCCSend(filename string, ipInt uint32, size int64, token int) string {
	return fmt.Sprintf("\x01DCC SEND %s %d 0 %d %d\x01", filename, ipInt, size, token)
}

// BuildDCCAccept returns the CTCP reply accepting a DCC RESUME.
func BuildDCCAccept(filename, port string, position int64, token string) string {
	if token == "" {
		return fmt.Sprintf("\x01DCC ACCEPT %s %s %d\x01", filename, port, position)
	}
	return fmt.Sprintf("\x01DCC ACCEPT %s %s %d %s\x01", filename, port, position, token)
}

// SendFile accepts one connection on ln and streams the file into it.
func SendFile(ln net.Listener, path string) error {
	return SendFileFrom(ln, path, 0)
}

// SendFileFrom is SendFile but seeks the file to offset first (DCC resume).
func SendFileFrom(ln net.Listener, path string, offset int64) error {
	defer ln.Close()
	conn, err := ln.Accept()
	if err != nil {
		return err
	}
	defer conn.Close()
	return StreamTo(conn, path, offset)
}

// ConnectAndSend dials ip:port and streams the file into the connection
// (reverse/passive DCC: the bot connects out to the downloader). cfg applies
// optional TCP socket tuning.
func ConnectAndSend(ip string, port int, path string, offset int64, cfg TCPConfig) error {
	addr := net.JoinHostPort(ip, fmt.Sprintf("%d", port))
	conn, err := net.DialTimeout("tcp", addr, 30*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	ConfigureTCP(conn, cfg)
	return StreamTo(conn, path, offset)
}

// StreamTo writes the file into w, optionally seeking to offset first.
func StreamTo(w io.Writer, path string, offset int64) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return err
		}
	}
	_, err = io.Copy(w, f)
	return err
}
