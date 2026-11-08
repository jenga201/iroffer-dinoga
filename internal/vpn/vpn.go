// Package vpn discovers the public address a dinoga bot should advertise when
// running behind a hotio/base wireguard VPN with a single forwarded port.
//
// The forwarded port is read from the file hotio's service-pia/service-forwarder
// write (/config/wireguard/forwarded_port). The public IP is taken from the
// wireguard peer Endpoint (the VPN gateway's public address, which is what
// clients actually reach the forwarded port on) with an IP-echo fallback.
package vpn

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// DefaultEchoURL is the IP-echo service used as a fallback when the wireguard
// peer endpoint cannot be read.
const DefaultEchoURL = "https://api.ipify.org"

// IsEnabled reports whether the VPN is enabled (hotio sets VPN_ENABLED).
func IsEnabled() bool {
	return strings.EqualFold(os.Getenv("VPN_ENABLED"), "true")
}

// PortFile returns the path hotio writes the forwarded port to.
func PortFile() string {
	return "/config/wireguard/forwarded_port"
}

// WireguardConf returns the active wireguard config path, derived from VPN_CONF
// (default wg0). hotio writes it under /dev/shm.
func WireguardConf() string {
	name := os.Getenv("VPN_CONF")
	if name == "" {
		name = "wg0"
	}
	return "/dev/shm/" + name + ".conf"
}

// Address is the bot's public address behind the VPN.
type Address struct {
	IP   string
	Port int
}

// ReadForwardedPort reads the first line of path as an integer port.
func ReadForwardedPort(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	line := strings.TrimSpace(strings.SplitN(string(b), "\n", 2)[0])
	if line == "" {
		return 0, fmt.Errorf("vpn: empty forwarded port")
	}
	port, err := strconv.Atoi(line)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("vpn: invalid forwarded port %q", line)
	}
	return port, nil
}

// PeerEndpointIP parses the [Peer] Endpoint address (public IPv4/IPv6) from a
// wireguard config. The peer endpoint is the VPN gateway's public address — the
// address clients use to reach the forwarded port.
func PeerEndpointIP(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	inPeer := false
	for _, line := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") {
			inPeer = t == "[Peer]"
			continue
		}
		if !inPeer || !strings.HasPrefix(t, "Endpoint") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(t, "Endpoint"))
		rest = strings.TrimSpace(strings.TrimPrefix(rest, "="))
		host, _, err := net.SplitHostPort(rest)
		if err != nil {
			host = rest
		}
		host = strings.Trim(host, "[]")
		if net.ParseIP(host) != nil {
			return host, nil
		}
	}
	return "", fmt.Errorf("vpn: no peer endpoint in %s", path)
}

// EchoIP fetches the public IP from an IP-echo URL over the tunnel.
func EchoIP(url string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return "", err
	}
	ip := strings.TrimSpace(string(b))
	if net.ParseIP(ip) == nil {
		return "", fmt.Errorf("vpn: echo returned non-IP %q", ip)
	}
	return ip, nil
}

// DiscoverIP returns the public VPN IP via the wireguard peer endpoint,
// falling back to an IP echo when that fails.
func DiscoverIP(wgConf, echoURL string) (string, error) {
	if ip, err := PeerEndpointIP(wgConf); err == nil {
		return ip, nil
	}
	return EchoIP(echoURL)
}

// Watcher polls the forwarded port and public IP, calling onChange whenever the
// (IP, port) pair changes. It runs until ctx is cancelled.
type Watcher struct {
	PortFile string
	WgConf   string
	EchoURL  string
	Interval time.Duration
}

// Run blocks until ctx is cancelled.
func (w Watcher) Run(ctx context.Context, onChange func(Address)) {
	if w.Interval <= 0 {
		w.Interval = 30 * time.Second
	}
	if w.EchoURL == "" {
		w.EchoURL = DefaultEchoURL
	}

	var current Address
	var have bool

	poll := func() {
		port, err := ReadForwardedPort(w.PortFile)
		if err != nil {
			return
		}
		ip, err := DiscoverIP(w.WgConf, w.EchoURL)
		if err != nil {
			return
		}
		next := Address{IP: ip, Port: port}
		if !have || next != current {
			current, have = next, true
			onChange(next)
		}
	}

	poll()
	t := time.NewTicker(w.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			poll()
		}
	}
}

// MustAddr validates an address and returns a canonical Address, erroring on a
// bad IP or port. It is used by the bot to normalise a discovered address.
func MustAddr(ip string, port int) (Address, error) {
	if net.ParseIP(ip) == nil {
		return Address{}, fmt.Errorf("vpn: invalid IP %q", ip)
	}
	if port < 1 || port > 65535 {
		return Address{}, fmt.Errorf("vpn: invalid port %d", port)
	}
	return Address{IP: ip, Port: port}, nil
}
