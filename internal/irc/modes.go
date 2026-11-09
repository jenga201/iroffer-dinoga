package irc

import (
	"strings"
	"sync"
)

// userMode is a per-user channel mode bitmask (+v voice, +h halfop, +o op).
type userMode uint8

const (
	modeVoice userMode = 1 << iota // +v
	modeHalfOp                     // +h
	modeOp                         // +o
)

// levelOf returns the numeric level implied by a mode bitmask: 0 plain member,
// 1 voice, 2 halfop, 3 op (highest bit wins).
func levelOf(m userMode) int {
	switch {
	case m&modeOp != 0:
		return 3
	case m&modeHalfOp != 0:
		return 2
	case m&modeVoice != 0:
		return 1
	default:
		return 0
	}
}

// channelModes tracks membership and per-user modes for one channel. It backs
// the restrictlist/restrictsend/need_voice/need_level gates.
type channelModes struct {
	mu    sync.Mutex
	users map[string]userMode // presence = member
}

func newChannelModes() *channelModes {
	return &channelModes{users: map[string]userMode{}}
}

// add records a member (JOIN) without changing their mode.
func (m *channelModes) add(nick string) {
	m.mu.Lock()
	if _, ok := m.users[nick]; !ok {
		m.users[nick] = 0
	}
	m.mu.Unlock()
}

// remove drops a member (PART/QUIT/KICK).
func (m *channelModes) remove(nick string) {
	m.mu.Lock()
	delete(m.users, nick)
	m.mu.Unlock()
}

// put sets a member's exact mode (NAMES snapshot authority).
func (m *channelModes) put(nick string, mode userMode) {
	m.mu.Lock()
	m.users[nick] = mode
	m.mu.Unlock()
}

// set turns one mode bit on/off (incremental MODE change).
func (m *channelModes) set(nick string, mode userMode, on bool) {
	m.mu.Lock()
	cur, ok := m.users[nick]
	if !ok {
		cur = 0
	}
	if on {
		m.users[nick] = cur | mode
	} else {
		m.users[nick] = cur &^ mode
	}
	m.mu.Unlock()
}

func (m *channelModes) member(nick string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.users[nick]
	return ok
}

func (m *channelModes) level(nick string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return levelOf(m.users[nick])
}

// prefixModes strips the NAMES status prefixes from a nick and returns the
// nick plus the mode bits the prefixes imply (@/&/~ = op, % = halfop, + = voice).
func prefixModes(name string) (string, userMode) {
	var mode userMode
	for len(name) > 0 {
		switch name[0] {
		case '@', '&', '~':
			mode |= modeOp
			name = name[1:]
		case '%':
			mode |= modeHalfOp
			name = name[1:]
		case '+':
			mode |= modeVoice
			name = name[1:]
		default:
			return name, mode
		}
	}
	return name, mode
}

// applyNAMES parses a NAMES body (353 reply, `args` = "<me> <symbol> <chan> :<names>").
// It is the authoritative membership + mode snapshot for the channel.
func (m *channelModes) applyNAMES(args string) {
	i := strings.Index(args, " :")
	if i < 0 {
		return
	}
	for _, name := range strings.Fields(args[i+2:]) {
		nick, mode := prefixModes(name)
		if nick == "" {
			continue
		}
		m.put(nick, mode)
	}
}

// applyMode parses a MODE command body (`args` = "<channel> <modestring> [args…]`)
// and updates +v/+h/+o members.
func (m *channelModes) applyMode(args string) {
	fields := strings.Fields(args)
	if len(fields) < 2 {
		return
	}
	modeStr := fields[1]
	add := true
	argIdx := 2
	for i := 0; i < len(modeStr); i++ {
		switch modeStr[i] {
		case '+':
			add = true
		case '-':
			add = false
		case 'v', 'h', 'o':
			if argIdx >= len(fields) {
				continue
			}
			nick := fields[argIdx]
			argIdx++
			var mode userMode
			switch modeStr[i] {
			case 'v':
				mode = modeVoice
			case 'h':
				mode = modeHalfOp
			case 'o':
				mode = modeOp
			}
			m.set(nick, mode, add)
		}
	}
}