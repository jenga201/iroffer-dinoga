package irc

import (
	"fmt"
	"strings"
	"time"

	"github.com/jenga201/dinoga/internal/packs"
)

// floodWindow is how far back flood protection counts a user's XDCC commands.
const floodWindow = 2 * time.Second

// isXDCCCommand reports whether a private message is an XDCC command (the
// messages that flood protection gates).
func isXDCCCommand(message string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(message)), "xdcc")
}

// addNet registers a network connection for broadcasts.
func (c *Client) addNet(nc *netConn) {
	c.netMu.Lock()
	c.nets = append(c.nets, nc)
	c.netMu.Unlock()
}

// broadcast sends msg to every registered network's channel (skipping channels
// marked -noannounce). It is safe to call before a network connects: writef
// no-ops while its connection is nil.
func (c *Client) broadcast(msg string) {
	c.netMu.Lock()
	nets := make([]*netConn, len(c.nets))
	copy(nets, c.nets)
	c.netMu.Unlock()
	for _, nc := range nets {
		if nc.net.NoAnnounce {
			continue
		}
		nc.writef("PRIVMSG %s :%s", nc.net.Channel, msg)
	}
}

// announceNew broadcasts a notice for each pack in next that is not in prev
// (autoaddann). announce_size/announce_date add size/date to the line.
func (c *Client) announceNew(prev, next []packs.Pack) {
	verb := "XDCC SEND"
	if !c.cfg.XDCCEnable {
		verb = "SXDCC SEND"
	}
	for _, p := range newPacks(prev, next) {
		c.broadcast(fmt.Sprintf("** New pack #%d: %s%s - /MSG %s %s #%d **",
			p.Number, p.Name, c.announceExtras(p), c.cfg.Nick, verb, p.Number))
	}
}

// announceExtras returns the optional " (size, date)" suffix for an announce.
func (c *Client) announceExtras(p packs.Pack) string {
	var extras []string
	if c.cfg.AnnounceSize {
		extras = append(extras, fmt.Sprintf("%d bytes", p.Size))
	}
	if c.cfg.AnnounceDate {
		extras = append(extras, p.ModTime.Format("2006-01-02"))
	}
	if len(extras) == 0 {
		return ""
	}
	return " (" + strings.Join(extras, ", ") + ")"
}

// newPacks returns the packs in next whose path is not present in prev.
func newPacks(prev, next []packs.Pack) []packs.Pack {
	seen := make(map[string]bool, len(prev))
	for _, p := range prev {
		seen[p.Path] = true
	}
	var out []packs.Pack
	for _, p := range next {
		if !seen[p.Path] {
			out = append(out, p)
		}
	}
	return out
}
