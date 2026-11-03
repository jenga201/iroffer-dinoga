package sxdcc

import (
	"fmt"
	"strconv"
	"strings"
)

// BuildRendezvous renders the bot → client IRC invitation:
//
//	SXDCC CONNECT <ip> <port> <token> <pin>
//
// ip is the DCC 32-bit network-order integer form of the IPv4 address, port is
// the bot's TLS listener, token is the single-use control-channel token, and
// pin is the bot's SPKI pin.
func BuildRendezvous(ipInt uint32, port int, token, pin string) string {
	return fmt.Sprintf("SXDCC CONNECT %d %d %s %s", ipInt, port, token, pin)
}

// ParseRendezvous parses a bot → client invitation line.
func ParseRendezvous(line string) (ipInt uint32, port int, token, pin string, err error) {
	f := strings.Fields(line)
	if len(f) != 6 || !strings.EqualFold(f[0], "SXDCC") || !strings.EqualFold(f[1], "CONNECT") {
		return 0, 0, "", "", fmt.Errorf("sxdcc: malformed rendezvous %q", line)
	}
	v, err := strconv.ParseUint(f[2], 10, 32)
	if err != nil {
		return 0, 0, "", "", fmt.Errorf("sxdcc: bad ip field %q", f[2])
	}
	p, err := strconv.Atoi(f[3])
	if err != nil {
		return 0, 0, "", "", fmt.Errorf("sxdcc: bad port field %q", f[3])
	}
	return uint32(v), p, f[4], f[5], nil
}

// ParseTrigger parses a client → bot `sxdcc <verb> [args]` IRC trigger line,
// returning the verb (lowercased) and any argument string.
func ParseTrigger(message string) (verb string, args string, ok bool) {
	msg := strings.TrimSpace(message)
	f := strings.Fields(msg)
	if len(f) < 1 || !strings.EqualFold(f[0], "sxdcc") {
		return "", "", false
	}
	if len(f) == 1 {
		return "sxdcc", "", true
	}
	return strings.ToLower(f[1]), strings.TrimSpace(msg[len(f[0])+1+len(f[1]):]), true
}
