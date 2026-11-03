// Package queue models the XDCC slot / queue / per-user limit system.
//
// A Manager holds a fixed number of concurrent transfer slots, a bounded FIFO
// queue for requests, and a separate bounded "idle" queue. Per-user caps limit
// concurrent transfers and queued items. The flow is:
//
//	pos, err := m.Submit(user, key, idle)   // enqueue (or error)
//	key, release := m.Wait(user)             // claim a slot (blocks)
//	defer release()                          // return the slot when done
//
// Submit never claims a slot; only Wait does. This keeps slot ownership
// unambiguous and race-free.
package queue

import (
	"errors"
	"sync"
)

// Errors returned by Manager.
var (
	ErrQueueFull   = errors.New("queue: main queue is full")
	ErrIdleFull    = errors.New("queue: idle queue is full")
	ErrUserCapped  = errors.New("queue: per-user queued cap reached")
	ErrUserIdleCap = errors.New("queue: per-user idle queued cap reached")
)

// Config configures a Manager. Non-positive values select safe defaults
// (Slots >= 1; QueueSize/IdleQueueSize 0 = unlimited).
type Config struct {
	Slots                  int // concurrent transfers allowed
	QueueSize              int // max main queue length (0 = unlimited)
	IdleQueueSize          int // max idle queue length (0 = unlimited)
	MaxTransfersPerPerson  int // max concurrent transfers per user (0 = unlimited)
	MaxQueuedPerPerson     int // max main-queue entries per user (0 = unlimited)
	MaxIdleQueuedPerPerson int // max idle-queue entries per user (0 = unlimited)
}

type entry struct {
	user string
	key  string
}

type userState struct {
	transfers  int
	queued     int
	idleQueued int
}

// Manager is a concurrency-safe slot/queue coordinator.
type Manager struct {
	cfg Config

	mu   sync.Mutex
	cond *sync.Cond

	freeSlots int
	main      []entry
	idle      []entry
	users     map[string]*userState
}

// New returns a Manager with normalized configuration.
func New(cfg Config) *Manager {
	n := normalize(cfg)
	m := &Manager{
		cfg:       n,
		freeSlots: n.Slots,
		users:     map[string]*userState{},
	}
	m.cond = sync.NewCond(&m.mu)
	return m
}

func normalize(c Config) Config {
	if c.Slots < 1 {
		c.Slots = 1
	}
	if c.QueueSize < 0 {
		c.QueueSize = 0
	}
	if c.IdleQueueSize < 0 {
		c.IdleQueueSize = 0
	}
	return c
}

func (m *Manager) user(user string) *userState {
	u := m.users[user]
	if u == nil {
		u = &userState{}
		m.users[user] = u
	}
	return u
}

// Submit enqueues a request keyed by key for user and returns its 1-based
// position in the queue. idle requests use the separate idle queue. It returns
// an error when a queue or per-user cap is exceeded. Submit never claims a
// slot; call Wait to obtain one.
func (m *Manager) Submit(user, key string, idle bool) (position int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	u := m.user(user)
	if idle {
		if m.cfg.MaxIdleQueuedPerPerson > 0 && u.idleQueued >= m.cfg.MaxIdleQueuedPerPerson {
			return 0, ErrUserIdleCap
		}
		if m.cfg.IdleQueueSize > 0 && len(m.idle) >= m.cfg.IdleQueueSize {
			return 0, ErrIdleFull
		}
		m.idle = append(m.idle, entry{user: user, key: key})
		u.idleQueued++
		m.cond.Broadcast()
		return len(m.idle), nil
	}

	if m.cfg.MaxQueuedPerPerson > 0 && u.queued >= m.cfg.MaxQueuedPerPerson {
		return 0, ErrUserCapped
	}
	if m.cfg.QueueSize > 0 && len(m.main) >= m.cfg.QueueSize {
		return 0, ErrQueueFull
	}
	m.main = append(m.main, entry{user: user, key: key})
	u.queued++
	m.cond.Broadcast()
	return len(m.main), nil
}

// Wait blocks until a slot is free and an eligible entry is available, then
// claims the slot and returns the entry's user, key, and a release func.
// Entries are popped FIFO (main queue before idle), skipping users already at
// their concurrent-transfer cap. The returned release func is idempotent and
// always credits the popped entry's user, so ownership follows the entry
// regardless of which worker calls Wait.
func (m *Manager) Wait() (user, key string, release func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for {
		if e, ok := m.pop(); ok {
			return e.user, e.key, m.releaseFunc(e.user)
		}
		m.cond.Wait()
	}
}

// TryWait is the non-blocking form of Wait.
func (m *Manager) TryWait() (user, key string, release func(), ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.pop(); ok {
		return e.user, e.key, m.releaseFunc(e.user), true
	}
	return "", "", nil, false
}

func (m *Manager) pop() (entry, bool) {
	if m.freeSlots == 0 {
		return entry{}, false
	}
	for i, e := range m.main {
		u := m.user(e.user)
		if m.cfg.MaxTransfersPerPerson > 0 && u.transfers >= m.cfg.MaxTransfersPerPerson {
			continue
		}
		m.main = append(m.main[:i], m.main[i+1:]...)
		u.queued--
		m.freeSlots--
		u.transfers++
		return e, true
	}
	for i, e := range m.idle {
		u := m.user(e.user)
		if m.cfg.MaxTransfersPerPerson > 0 && u.transfers >= m.cfg.MaxTransfersPerPerson {
			continue
		}
		m.idle = append(m.idle[:i], m.idle[i+1:]...)
		u.idleQueued--
		m.freeSlots--
		u.transfers++
		return e, true
	}
	return entry{}, false
}

func (m *Manager) releaseFunc(user string) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			m.mu.Lock()
			u := m.user(user)
			if u.transfers > 0 {
				u.transfers--
			}
			m.freeSlots++
			m.cond.Broadcast()
			m.mu.Unlock()
		})
	}
}

// Cancel removes one queued request matching key for user (main queue first,
// then idle). It returns true if an entry was removed.
func (m *Manager) Cancel(user, key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, e := range m.main {
		if e.user == user && e.key == key {
			m.main = append(m.main[:i], m.main[i+1:]...)
			m.user(user).queued--
			return true
		}
	}
	for i, e := range m.idle {
		if e.user == user && e.key == key {
			m.idle = append(m.idle[:i], m.idle[i+1:]...)
			m.user(user).idleQueued--
			return true
		}
	}
	return false
}

// Snapshot reports current occupancy for status output.
type Snapshot struct {
	FreeSlots int
	QueueLen  int
	IdleLen   int
	Users     int
}

func (m *Manager) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Snapshot{
		FreeSlots: m.freeSlots,
		QueueLen:  len(m.main),
		IdleLen:   len(m.idle),
		Users:     len(m.users),
	}
}
