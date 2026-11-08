package config

import (
	"os"
	"path/filepath"
	"strings"
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

func TestApplyEnvOverrides(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	env := []string{
		"IROFFER_SLOTSMAX=9",
		"IROFFER_USENATIP=1.2.3.4",
		"IROFFER_PASSIVE_DCC=yes",
		"IROFFER_CONTROL_PORT=31337",
		"IROFFER_XDCC_ALLOW=*!*@a.com;;*!*@b.com",
	}
	applied := ApplyEnvOverrides(&c, env)
	if c.SlotsMax != 9 || c.DCCIP != "1.2.3.4" || !c.PassiveDCC || c.ControlPort != 31337 {
		t.Fatalf("env overrides not applied: %+v", c)
	}
	if len(c.XDCCAllow) != 2 || c.XDCCAllow[1] != "*!*@b.com" {
		t.Fatalf("multi-value allow = %v", c.XDCCAllow)
	}
	if len(applied) == 0 || !strings.Contains(applied[0], "control_port") {
		t.Fatalf("applied = %v", applied)
	}
}

func TestLoadWithEnvOverridesFile(t *testing.T) {
	path := writeConfig(t, "slotsmax 5\nusenatip 9.9.9.9\n")
	c, applied, err := LoadWithEnv(path, []string{"IROFFER_SLOTSMAX=42"})
	if err != nil {
		t.Fatal(err)
	}
	if c.SlotsMax != 42 || c.DCCIP != "9.9.9.9" {
		t.Fatalf("env should win only where set: %+v", c)
	}
	if len(applied) != 1 || !strings.Contains(applied[0], "slotsmax") {
		t.Fatalf("applied = %v", applied)
	}
}

func TestWriteRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "in.config")
	if err := os.WriteFile(path, []byte("network {\n  server irc.rizon.net 9999\n  user_nick bot\n  channel #frog\n  connectionmethod ssl\n}\nslotsmax 7\ncontrol_port 30000\nusenatip 2.31.76.192\nsxdcc_enable yes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	if err := Write(&sb, c); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "out.config")
	if err := os.WriteFile(out, []byte(sb.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	c2, err := Load(out)
	if err != nil {
		t.Fatal(err)
	}
	if c2.SlotsMax != 7 || c2.ControlPort != 30000 || c2.DCCIP != "2.31.76.192" {
		t.Fatalf("roundtrip = %+v", c2)
	}
	if c2.Networks[0].Server != "irc.rizon.net" || !c2.Networks[0].SSL {
		t.Fatalf("roundtrip network = %+v", c2.Networks[0])
	}
}

func TestResolveMigratesLegacyConfig(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "iroffer.config")
	if err := os.WriteFile(legacy, []byte("slotsmax 6\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	resolved, migrated, err := Resolve(dir, "dinoga.config")
	if err != nil {
		t.Fatal(err)
	}
	if !migrated || filepath.Base(resolved) != "dinoga.config" {
		t.Fatalf("resolved=%q migrated=%v", resolved, migrated)
	}
	if _, err := os.Stat(filepath.Join(dir, "dinoga.config")); err != nil {
		t.Fatalf("dinoga.config not written: %v", err)
	}
	if _, err := os.Stat(legacy + ".bak"); err != nil {
		t.Fatalf("legacy not renamed to .bak: %v", err)
	}

	// Second resolve uses the migrated file, no further migration.
	_, migrated2, err := Resolve(dir, "dinoga.config")
	if err != nil || migrated2 {
		t.Fatalf("second resolve: migrated=%v err=%v", migrated2, err)
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

func TestLoggingDefaults(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if !c.LogStats {
		t.Fatalf("logstats should default to true")
	}
	if c.LogRotate != "" || c.ExpireLogFiles != 0 || c.LogMessages || c.LogNotices {
		t.Fatalf("logging defaults = %+v", c)
	}
}

func TestLoadLoggingDirectives(t *testing.T) {
	path := writeConfig(t, `
logfile /var/log/dinoga.log
logrotate daily
expire_logfiles 14
logmessages yes
lognotices yes
logfile_messages /var/log/dinoga-msg.log
logfile_notices /var/log/dinoga-ntc.log
logstats no
`)
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.LogFile != "/var/log/dinoga.log" || c.LogRotate != "daily" || c.ExpireLogFiles != 14 {
		t.Fatalf("log config = %+v", c)
	}
	if !c.LogMessages || !c.LogNotices {
		t.Fatalf("message/notice flags = %v/%v", c.LogMessages, c.LogNotices)
	}
	if c.LogFileMessages != "/var/log/dinoga-msg.log" || c.LogFileNotices != "/var/log/dinoga-ntc.log" {
		t.Fatalf("msg/ntc files = %q/%q", c.LogFileMessages, c.LogFileNotices)
	}
	if c.LogStats {
		t.Fatalf("logstats should be disabled by file directive")
	}
}

func TestEnvLoggingOverrides(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	ApplyEnvOverrides(&c, []string{
		"IROFFER_LOGROTATE=weekly",
		"IROFFER_LOGSTATS=no",
		"IROFFER_LOGMESSAGES=yes",
		"IROFFER_EXPIRE_LOGFILES=7",
	})
	if c.LogRotate != "weekly" || c.LogStats || !c.LogMessages || c.ExpireLogFiles != 7 {
		t.Fatalf("logging env overrides = %+v", c)
	}
}
