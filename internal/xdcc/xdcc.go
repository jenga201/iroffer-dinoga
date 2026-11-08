// Package xdcc implements the XDCC command vocabulary that users send to the
// bot over IRC (LIST, SEND, INFO, BATCH, REMOVE/CANCEL). It is transport
// agnostic: the IRC package supplies a per-network Client implementation.
package xdcc

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jenga201/dinoga/internal/packs"
)

// Client is the subset of the IRC client that XDCC command handling needs.
type Client interface {
	Notice(target, msg string)
	Privmsg(target, msg string)
	SendPack(target string, p packs.Pack) error
	Packlist() []packs.Pack
	// CancelQueued removes the caller's oldest queued (not yet started)
	// transfer, returning true if one was removed.
	CancelQueued(target string) bool
	// PacklistURL returns the public HTTP URL of the packlist ("" when not
	// available), advertised in the "xdcc list" reply.
	PacklistURL() string
}

// Handle routes one private message to the bot (e.g. "xdcc list", "xdcc send #1").
func Handle(c Client, target, message string) {
	msg := strings.TrimSpace(message)
	lower := strings.ToLower(msg)
	switch {
	case strings.HasPrefix(lower, "xdcc list"):
		sendList(c, target)
	case strings.HasPrefix(lower, "xdcc send"):
		send(c, target, msg)
	case strings.HasPrefix(lower, "xdcc info"):
		info(c, target, msg)
	case strings.HasPrefix(lower, "xdcc batch"):
		batch(c, target, msg)
	case strings.HasPrefix(lower, "xdcc remove"), strings.HasPrefix(lower, "xdcc cancel"):
		remove(c, target)
	}
}

func sendList(c Client, target string) {
	pl := c.Packlist()
	c.Notice(target, fmt.Sprintf("** %d pack(s) **  (use: xdcc send #<n>)", len(pl)))
	if url := c.PacklistURL(); url != "" {
		c.Notice(target, fmt.Sprintf("** Packlist: %s **", url))
	}
	for _, p := range pl {
		c.Notice(target, fmt.Sprintf("#%d %6d [%s]", p.Number, p.Size, p.Name))
	}
	c.Notice(target, "** To request a file, type \"/MSG <bot> XDCC SEND #<n>\" **")
}

func send(c Client, target, message string) {
	fields := strings.Fields(message) // ["xdcc", "send", "#1"]
	num := 0
	if len(fields) >= 3 {
		num, _ = strconv.Atoi(strings.TrimPrefix(fields[2], "#"))
	}

	pl := c.Packlist()
	if num < 1 || num > len(pl) {
		c.Notice(target, fmt.Sprintf("** No such pack: %d **", num))
		return
	}

	p := pl[num-1]
	c.Notice(target, fmt.Sprintf("** Sending You Pack #%d (%s) **", p.Number, p.Name))
	if err := c.SendPack(target, p); err != nil {
		c.Notice(target, fmt.Sprintf("** Send failed: %v **", err))
	}
}

// info replies with metadata for one pack: "xdcc info #<pack>".
func info(c Client, target, message string) {
	fields := strings.Fields(message) // ["xdcc", "info", "#1"]
	num := 0
	if len(fields) >= 3 {
		num, _ = strconv.Atoi(strings.TrimPrefix(fields[2], "#"))
	}
	pl := c.Packlist()
	p, ok := lookup(pl, num)
	if !ok {
		c.Notice(target, fmt.Sprintf("** No such pack: %d **", num))
		return
	}
	c.Notice(target, fmt.Sprintf("** Pack #%d: %s (%d bytes) **", p.Number, p.Name, p.Size))
	if p.Desc != "" {
		c.Notice(target, fmt.Sprintf("**   %s **", p.Desc))
	}
}

// batch sends a contiguous range: "xdcc batch #<start>-#<end>".
func batch(c Client, target, message string) {
	fields := strings.Fields(message)
	if len(fields) < 3 {
		c.Notice(target, "** Usage: xdcc batch #<start>-#<end> **")
		return
	}
	parts := strings.SplitN(strings.TrimPrefix(fields[2], "#"), "-", 2)
	if len(parts) != 2 {
		c.Notice(target, "** Usage: xdcc batch #<start>-#<end> **")
		return
	}
	start, err1 := strconv.Atoi(strings.TrimPrefix(parts[0], "#"))
	end, err2 := strconv.Atoi(strings.TrimPrefix(parts[1], "#"))
	if err1 != nil || err2 != nil || start < 1 || end < start {
		c.Notice(target, "** Invalid batch range **")
		return
	}

	pl := c.Packlist()
	for n := start; n <= end && n <= len(pl); n++ {
		p := pl[n-1]
		c.Notice(target, fmt.Sprintf("** Sending You Pack #%d (%s) **", p.Number, p.Name))
		if err := c.SendPack(target, p); err != nil {
			c.Notice(target, fmt.Sprintf("** Send failed: %v **", err))
		}
	}
}

func lookup(pl []packs.Pack, num int) (packs.Pack, bool) {
	if num < 1 || num > len(pl) {
		return packs.Pack{}, false
	}
	return pl[num-1], true
}

// remove cancels the caller's oldest queued transfer: "xdcc remove" / "xdcc cancel".
func remove(c Client, target string) {
	if c.CancelQueued(target) {
		c.Notice(target, "** Removed queued transfer **")
	} else {
		c.Notice(target, "** No queued transfer to remove **")
	}
}
