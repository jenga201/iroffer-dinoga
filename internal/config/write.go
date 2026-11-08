package config

import (
	"fmt"
	"io"
	"strings"
)

// Write serializes c to a dinoga.config file. The header documents every
// supported directive; network blocks and active values follow. The output is
// round-trip safe: Load(Write(c)) yields a config equivalent to c.
func Write(w io.Writer, c Config) error {
	var b strings.Builder
	b.WriteString(configHeader)

	for _, n := range c.Networks {
		b.WriteString("network {\n")
		fmt.Fprintf(&b, "  server %s %d\n", n.Server, n.Port)
		fmt.Fprintf(&b, "  user_nick %s\n", n.Nick)
		fmt.Fprintf(&b, "  channel %s\n", n.Channel)
		if n.SSL {
			b.WriteString("  connectionmethod ssl\n")
		}
		if n.SASLUser != "" {
			fmt.Fprintf(&b, "  sasl_username %s\n", n.SASLUser)
		}
		if n.SASLPass != "" {
			fmt.Fprintf(&b, "  sasl_password %s\n", n.SASLPass)
		}
		b.WriteString("}\n")
	}
	if len(c.Networks) == 0 {
		b.WriteString("network {\n  server irc.rizon.net 6667\n  user_nick dinoga\n  channel #frog\n}\n")
	}

	fmt.Fprintf(&b, "\nfiledir %s\n", orDefault(c.FileDir, "/data"))
	fmt.Fprintf(&b, "control_port %d\n", orInt(c.ControlPort, c.PortStart))
	fmt.Fprintf(&b, "tcprangestart %d\n", c.PortStart)
	fmt.Fprintf(&b, "tcprangelimit %d\n", c.PortLimit)
	fmt.Fprintf(&b, "usenatip %s\n", c.DCCIP)
	b.WriteString(boolLine("passive_dcc", c.PassiveDCC))
	fmt.Fprintf(&b, "autoadd_time %d\n", c.AutoAdd)
	fmt.Fprintf(&b, "slotsmax %d\n", c.SlotsMax)
	fmt.Fprintf(&b, "queuesize %d\n", c.QueueSize)
	fmt.Fprintf(&b, "idlequeuesize %d\n", c.IdleQueueSize)
	fmt.Fprintf(&b, "maxtransfersperperson %d\n", c.MaxTransfersPerPerson)
	fmt.Fprintf(&b, "maxqueueitemsperperson %d\n", c.MaxQueuedPerPerson)
	fmt.Fprintf(&b, "maxidlequeuedperperson %d\n", c.MaxIdleQueuedPerPerson)
	if c.TransferMaxSpeed > 0 {
		fmt.Fprintf(&b, "transfermaxspeed %d\n", c.TransferMaxSpeed/1024)
	}
	if c.OverallMaxSpeed > 0 {
		fmt.Fprintf(&b, "overallmaxspeed %d\n", c.OverallMaxSpeed/1024)
	}
	fmt.Fprintf(&b, "reconnect_delay %d\n", c.ReconnectDelay)
	for _, d := range c.XDCCAllow {
		fmt.Fprintf(&b, "xdcc_allow %s\n", d)
	}
	for _, d := range c.XDCCDeny {
		fmt.Fprintf(&b, "xdcc_deny %s\n", d)
	}
	b.WriteString(boolLine("noduplicatefiles", c.NoDuplicateFiles))
	for _, d := range c.AddDirExclude {
		fmt.Fprintf(&b, "adddir_exclude %s\n", d)
	}
	for _, d := range c.AddDirMatch {
		fmt.Fprintf(&b, "adddir_match %s\n", d)
	}
	if c.AddDirMinSize > 0 {
		fmt.Fprintf(&b, "adddir_min_size %d\n", c.AddDirMinSize)
	}

	b.WriteString(boolLine("sxdcc_enable", c.SXDCCEnable))
	if c.CertFile != "" {
		fmt.Fprintf(&b, "sxdcc_cert %s\n", c.CertFile)
	}
	if c.KeyFile != "" {
		fmt.Fprintf(&b, "sxdcc_key %s\n", c.KeyFile)
	}

	fmt.Fprintf(&b, "telnet_port %d\n", c.TelnetPort)
	if c.AdminPass != "" {
		fmt.Fprintf(&b, "adminpass %s\n", c.AdminPass)
	}
	fmt.Fprintf(&b, "http_port %d\n", c.HTTPPort)
	if c.UploadDir != "" {
		fmt.Fprintf(&b, "uploaddir %s\n", c.UploadDir)
	}
	if c.UploadMaxSize > 0 {
		fmt.Fprintf(&b, "uploadmaxsize %d\n", c.UploadMaxSize)
	}
	if c.Headline != "" {
		fmt.Fprintf(&b, "headline %s\n", c.Headline)
	}
	if c.PeriodicMsg != "" {
		fmt.Fprintf(&b, "periodicmsg %s\n", c.PeriodicMsg)
	}
	if c.PeriodicInterval > 0 {
		fmt.Fprintf(&b, "periodicmsg_time %d\n", c.PeriodicInterval)
	}
	if c.StateFile != "" {
		fmt.Fprintf(&b, "statefile %s\n", c.StateFile)
	}
	if c.LogFile != "" {
		fmt.Fprintf(&b, "logfile %s\n", c.LogFile)
	}
	if c.LogRotate != "" && c.LogRotate != "never" {
		fmt.Fprintf(&b, "logrotate %s\n", c.LogRotate)
	}
	if c.LogFileMessages != "" {
		fmt.Fprintf(&b, "logfile_messages %s\n", c.LogFileMessages)
	}
	if c.LogFileNotices != "" {
		fmt.Fprintf(&b, "logfile_notices %s\n", c.LogFileNotices)
	}
	b.WriteString(boolLine("logmessages", c.LogMessages))
	b.WriteString(boolLine("lognotices", c.LogNotices))
	b.WriteString(boolLine("logstats", c.LogStats))
	if c.ExpireLogFiles > 0 {
		fmt.Fprintf(&b, "expire_logfiles %d\n", c.ExpireLogFiles)
	}

	_, err := io.WriteString(w, b.String())
	return err
}

func boolLine(key string, v bool) string {
	return key + " " + yesNoText(v) + "\n"
}

func yesNoText(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func orInt(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// configHeader documents every directive the bot understands. It is emitted at
// the top of every generated config so operators can see the full surface.
const configHeader = `# dinoga config (generated)
# Dinoga is the Go rewrite of iroffer-dinoex. Directives mirror the original
# iroffer.config surface; unknown directives are ignored. Lines starting with
# '#' are comments. Each "network { ... }" block adds one IRC network.

# --- files served (XDCC/DCC/SXDCC) ---
# filedir <dir>           directory scanned into the packlist
# autoadd_time <sec>      rescan interval (0 = off)
# slotsmax <n>            max concurrent transfers
# queuesize <n>           max queued transfers
# idlequeuesize <n>       max idle queued transfers
# maxtransfersperperson <n>
# maxqueueitemsperperson <n>
# maxidlequeuedperperson <n>
# transfermaxspeed <KB/s> per-transfer cap (0 = unlimited)
# overallmaxspeed <KB/s>  global cap (0 = unlimited)
# noduplicatefiles yes    drop duplicate name+size
# adddir_exclude <glob>   skip matching files (repeatable)
# adddir_match <glob>     only include matching files (repeatable)
# adddir_min_size <bytes> skip smaller files

# --- access control (hostmasks nick!user@host) ---
# xdcc_allow <mask>       allow-list (empty = allow all except deny)
# xdcc_deny <mask>        deny-list (wins over allow)

# --- DCC / SXDCC ---
# usenatip <ip>           public IP advertised for DCC/SXDCC
# control_port <port>     single shared control port (TLS + web packlist)
# tcprangestart <port>    first DCC/TLS port (used when control_port is 0)
# tcprangelimit <port>    last DCC/TLS port
# passive_dcc yes         reverse DCC (bot connects out)
# sxdcc_enable yes        serve XDCC over TLS 1.3
# sxdcc_cert <path>       TLS certificate (self-signed generated if missing)
# sxdcc_key <path>        TLS private key

# --- admin / web ---
# adminpass <secret>      admin password (SDCC/telnet)
# telnet_port <port>      0 = disabled
# http_port <port>        separate HTTP interface (0 = disabled); the packlist
#                         is also served on the control port as /packlist.txt

# --- uploads / misc ---
# uploaddir <dir>         receive incoming DCC SEND uploads
# uploadmaxsize <bytes>   incoming upload cap (0 = unlimited)
# headline <msg>          announced on channel join
# periodicmsg <msg>       periodic channel message
# periodicmsg_time <sec>  seconds between periodic messages
# statefile <path>
# logfile <path>
# logrotate <v>           rotation: never/daily/weekly/monthly/<N> hours
# expire_logfiles <days>  delete rotated logs older than N days (0 = keep)
# logfile_messages <path> separate PRIVMSG transcript (requires logmessages)
# logfile_notices <path>  separate NOTICE transcript (requires lognotices)
# logmessages yes         log incoming private messages (fall back to logfile)
# lognotices yes          log incoming notices (fall back to logfile)
# logstats yes            periodic status line into the logfile

`
