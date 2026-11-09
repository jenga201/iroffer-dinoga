package irc

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/jenga201/dinoga/internal/config"
	"github.com/jenga201/dinoga/internal/packs"
)

func gateNC(cfg config.Config) *netConn {
	return &netConn{
		c:     &Client{cfg: cfg, resume: map[string]int64{}},
		net:   config.Network{Channel: "#frog"},
		modes: newChannelModes(),
	}
}

func TestRestrictListGate(t *testing.T) {
	nc := gateNC(config.Config{RestrictList: true})
	if nc.canList("bob") {
		t.Fatal("non-member should be denied under restrictlist")
	}
	nc.modes.add("bob")
	if !nc.canList("bob") {
		t.Fatal("member should be allowed under restrictlist")
	}

	if !gateNC(config.Config{}).canList("bob") {
		t.Fatal("restrictlist off should allow everyone")
	}
}

func TestRestrictSendWithNeedVoice(t *testing.T) {
	nc := gateNC(config.Config{RestrictSend: true, NeedVoice: true})
	nc.modes.add("plain")
	nc.modes.set("muted", modeVoice, true)
	if nc.canSend("plain") {
		t.Fatal("need_voice should deny an unvoiced member")
	}
	if !nc.canSend("muted") {
		t.Fatal("need_voice should allow a voiced member")
	}
}

func TestRestrictSendWithNeedLevel(t *testing.T) {
	nc := gateNC(config.Config{RestrictSend: true, NeedLevel: 3})
	nc.modes.put("op", modeOp)
	nc.modes.put("voiced", modeVoice)
	if !nc.canSend("op") {
		t.Fatal("op should satisfy need_level 3")
	}
	if nc.canSend("voiced") {
		t.Fatal("voice must not satisfy need_level 3")
	}
}

func TestRestrictSendMembership(t *testing.T) {
	nc := gateNC(config.Config{RestrictSend: true})
	if nc.canSend("ghost") {
		t.Fatal("non-member should be denied under restrictsend")
	}
	nc.modes.add("ghost")
	if !nc.canSend("ghost") {
		t.Fatal("member should be allowed under restrictsend")
	}
}

func TestAnnounceExtras(t *testing.T) {
	c := &Client{cfg: config.Config{}}
	p := packs.Pack{Name: "a.bin", Size: 10, ModTime: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}
	if got := c.announceExtras(p); got != "" {
		t.Fatalf("default announce extras = %q, want empty", got)
	}
	c.cfg.AnnounceSize = true
	c.cfg.AnnounceDate = true
	if got := c.announceExtras(p); got != " (10 bytes, 2026-01-02)" {
		t.Fatalf("announce extras = %q", got)
	}
}

func TestResumePersistenceRoundTrip(t *testing.T) {
	cfg := config.Config{StateFile: filepath.Join(t.TempDir(), "state.json")}
	c := &Client{cfg: cfg, resume: map[string]int64{}}
	c.setResume("/data/a.bin", 12345)
	c.persist()

	c2 := &Client{cfg: cfg, resume: map[string]int64{}}
	c2.loadState()
	if c2.resume["/data/a.bin"] != 12345 {
		t.Fatalf("resume offset not persisted: %v", c2.resume)
	}
	c2.clearResume("/data/a.bin")
	if _, ok := c2.resume["/data/a.bin"]; ok {
		t.Fatal("clearResume should remove the entry")
	}
}