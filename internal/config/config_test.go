package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "iroffer.config")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadDefaults(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if c.Server != "irc.rizon.net" || c.Nick != "dinoga" || c.Channel != "#frog" {
		t.Fatalf("defaults = %+v", c)
	}
	if len(c.Networks) != 1 || c.Networks[0].Server != "irc.rizon.net" {
		t.Fatalf("networks = %+v", c.Networks)
	}
}

func TestLoadMissingFile(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.config"))
	if err != nil {
		t.Fatalf("missing file should not error, got %v", err)
	}
	if c.Server != "irc.rizon.net" {
		t.Fatalf("missing file should use defaults, got %q", c.Server)
	}
}

func TestLoadFileDirectives(t *testing.T) {
	path := writeConfig(t, `
network {
  server irc.example.net 7000
  user_nick testbot
  channel #test
  connectionmethod ssl
}
usenatip 2.31.76.192
tcprangestart 30000
tcprangelimit 30010
passive_dcc yes
autoadd_time 30
filedir /data/files
slotsmax 5
queuesize 200
transfermaxspeed 100
sxdcc_enable yes
headline welcome to the bot
periodicmsg pack up
periodicmsg_time 3600
uploaddir /uploads
statefile /home/dinoga/state.json
`)
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	n := c.Networks[0]
	if n.Server != "irc.example.net" || n.Port != 7000 || !n.SSL || n.Nick != "testbot" || n.Channel != "#test" {
		t.Fatalf("network = %+v", n)
	}
	if c.DCCIP != "2.31.76.192" || c.PortStart != 30000 || c.PortLimit != 30010 || !c.PassiveDCC {
		t.Fatalf("dcc = ip %q %d-%d passive=%v", c.DCCIP, c.PortStart, c.PortLimit, c.PassiveDCC)
	}
	if c.FileDir != "/data/files" || c.AutoAdd != 30 {
		t.Fatalf("files = %q autoadd %d", c.FileDir, c.AutoAdd)
	}
	if c.SlotsMax != 5 || c.QueueSize != 200 || c.TransferMaxSpeed != 100*1024 {
		t.Fatalf("queue slots=%d size=%d speed=%d", c.SlotsMax, c.QueueSize, c.TransferMaxSpeed)
	}
	if !c.SXDCCEnable || c.Headline != "welcome to the bot" || c.PeriodicMsg != "pack up" || c.PeriodicInterval != 3600 || c.UploadDir != "/uploads" {
		t.Fatalf("misc = %+v", c)
	}
}

func TestLoadTwoNetworks(t *testing.T) {
	path := writeConfig(t, `
network {
  server irc.rizon.net 9999
  user_nick dinoga
  channel #frog
  connectionmethod ssl
}
network {
  server irc.freenode.net 6697
  user_nick dinoga-fn
  channel #frog
  connectionmethod ssl
}
`)
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Networks) != 2 {
		t.Fatalf("networks = %d, want 2", len(c.Networks))
	}
	if c.Networks[0].Server != "irc.rizon.net" || c.Networks[1].Server != "irc.freenode.net" || !c.Networks[1].SSL || c.Networks[1].Port != 6697 {
		t.Fatalf("networks = %+v", c.Networks)
	}
	// Mirror fields follow Networks[0].
	if c.Server != "irc.rizon.net" || c.Nick != "dinoga" {
		t.Fatalf("mirror = %q %q", c.Server, c.Nick)
	}
}

func TestSplitChannelSpec(t *testing.T) {
	ch, key := splitChannelSpec("#frog -key secret")
	if ch != "#frog" || key != "secret" {
		t.Fatalf("got (%q,%q), want (#frog,secret)", ch, key)
	}
	ch, key = splitChannelSpec("#frog")
	if ch != "#frog" || key != "" {
		t.Fatalf("got (%q,%q)", ch, key)
	}
}

func TestSplitServerSpec(t *testing.T) {
	h, p := splitServerSpec("irc.rizon.net 9999", "def", 6667)
	if h != "irc.rizon.net" || p != 9999 {
		t.Fatalf("got %s:%d, want irc.rizon.net:9999", h, p)
	}
}

func TestLoadSASL(t *testing.T) {
	path := writeConfig(t, `
network {
  server irc.freenode.net 6697
  user_nick bot
  channel #frog
  connectionmethod ssl
  sasl_username bot
  sasl_password hunter2
}
`)
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	n := c.Networks[0]
	if n.SASLUser != "bot" || n.SASLPass != "hunter2" {
		t.Fatalf("sasl = %q/%q, want bot/hunter2", n.SASLUser, n.SASLPass)
	}
}

func TestLoadAntiAbuseAndScanFilters(t *testing.T) {
	path := writeConfig(t, `
xdcc_allow *!*@*.good.com
downloadhost *!*@friend.net
xdcc_deny *!*@bad.net
noduplicatefiles yes
adddir_exclude *.tmp *.part
adddir_match *.mkv *.mp4
adddir_min_size 4096
`)
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.XDCCAllow) != 2 || c.XDCCAllow[0] != "*!*@*.good.com" || c.XDCCAllow[1] != "*!*@friend.net" {
		t.Fatalf("XDCCAllow = %v", c.XDCCAllow)
	}
	if len(c.XDCCDeny) != 1 || c.XDCCDeny[0] != "*!*@bad.net" {
		t.Fatalf("XDCCDeny = %v", c.XDCCDeny)
	}
	if !c.NoDuplicateFiles || c.AddDirMinSize != 4096 {
		t.Fatalf("filters = nodup %v minsize %d", c.NoDuplicateFiles, c.AddDirMinSize)
	}
	if len(c.AddDirExclude) != 2 || len(c.AddDirMatch) != 2 {
		t.Fatalf("exclude=%v match=%v", c.AddDirExclude, c.AddDirMatch)
	}
}

func TestLoadSpeedAndUploadLimits(t *testing.T) {
	path := writeConfig(t, `
overallmaxspeed 1000
transfermaxspeed 500
uploaddir /uploads
uploadmaxsize 1048576
`)
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.OverallMaxSpeed != 1000*1024 {
		t.Fatalf("OverallMaxSpeed = %d, want %d", c.OverallMaxSpeed, 1000*1024)
	}
	if c.TransferMaxSpeed != 500*1024 {
		t.Fatalf("TransferMaxSpeed = %d, want %d", c.TransferMaxSpeed, 500*1024)
	}
	if c.UploadDir != "/uploads" || c.UploadMaxSize != 1048576 {
		t.Fatalf("upload = %q maxsize %d", c.UploadDir, c.UploadMaxSize)
	}
}
