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
		fmt.Fprintf(&b, "  channel %s%s%s\n", n.Channel,
			boolSuffix(" -key ", n.Key != "", n.Key),
			boolSuffix(" -noannounce", n.NoAnnounce, ""))
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
	b.WriteString(boolLine("autoaddann", c.AutoAddAnn))
	b.WriteString(boolLine("nat_sort", c.NaturalSort))
	b.WriteString(boolLine("nocrc32", c.NoCRC32))
	b.WriteString(boolLine("nomd5sum", c.NoMD5Sum))
	if c.XDCCListFile != "" {
		fmt.Fprintf(&b, "xdcclistfile %s\n", c.XDCCListFile)
	}
	if c.XDCCXMLFile != "" {
		fmt.Fprintf(&b, "xdccxmlfile %s\n", c.XDCCXMLFile)
	}
	if c.TCPNoDelay {
		b.WriteString(boolLine("tcp_nodelay", c.TCPNoDelay))
	}
	if c.TCPBufferSize > 0 {
		fmt.Fprintf(&b, "tcp_buffer_size %d\n", c.TCPBufferSize/1024)
	}
	for _, d := range c.UploadAllow {
		fmt.Fprintf(&b, "uploadhost %s\n", d)
	}
	if c.UploadMinSpace > 0 {
		fmt.Fprintf(&b, "uploadminspace %d\n", c.UploadMinSpace/(1024*1024))
	}
	b.WriteString(boolLine("restrictlist", c.RestrictList))
	b.WriteString(boolLine("restrictsend", c.RestrictSend))
	b.WriteString(boolLine("need_voice", c.NeedVoice))
	if c.NeedLevel > 0 {
		fmt.Fprintf(&b, "need_level %d\n", c.NeedLevel)
	}
	b.WriteString(boolLine("announce_size", c.AnnounceSize))
	b.WriteString(boolLine("announce_date", c.AnnounceDate))
	if c.StatusInterval > 0 {
		fmt.Fprintf(&b, "status_time_dcc_chat %d\n", c.StatusInterval)
	}
	if c.OldStateFile != "" {
		fmt.Fprintf(&b, "old_statefile %s\n", c.OldStateFile)
	}
	if c.FloodProtectionRate > 0 {
		fmt.Fprintf(&b, "flood_protection_rate %d\n", c.FloodProtectionRate)
	}
	if c.AutoIgnoreTime > 0 {
		fmt.Fprintf(&b, "autoignore_time %d\n", c.AutoIgnoreTime)
	}

	b.WriteString(boolLine("sxdcc_enable", c.SXDCCEnable))
	if !c.XDCCEnable {
		b.WriteString(boolLine("xdcc_enable", c.XDCCEnable))
	}
	if c.CertFile != "" {
		fmt.Fprintf(&b, "sxdcc_cert %s\n", c.CertFile)
	}
	if c.KeyFile != "" {
		fmt.Fprintf(&b, "sxdcc_key %s\n", c.KeyFile)
	}

	if c.AdminPass != "" {
		fmt.Fprintf(&b, "adminpass %s\n", c.AdminPass)
	}
	if c.OwnerNick != "" {
		fmt.Fprintf(&b, "owner_nick %s\n", c.OwnerNick)
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

// boolSuffix returns prefix+val when cond is true, otherwise "" (for optional
// inline channel flags such as `-key <k>` and `-noannounce`).
func boolSuffix(prefix string, cond bool, val string) string {
	if !cond {
		return ""
	}
	return prefix + val
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
# autoaddann yes          announce newly-added packs on rescan
# announce_size yes       include size in the autoaddann announce
# announce_date yes       include date in the autoaddann announce
# nat_sort yes            natural (strnatcmp-style) listing order
# nocrc32 yes             suppress CRC32 in "xdcc info"
# nomd5sum yes            suppress MD5 in "xdcc info"
# xdcclistfile <path>     write a plain-text pack list after each scan
# xdccxmlfile <path>      write an XML pack list after each scan

# --- access control (hostmasks nick!user@host) ---
# xdcc_allow <mask>       allow-list (empty = allow all except deny)
# xdcc_deny <mask>        deny-list (wins over allow)
# flood_protection_rate <n>  max XDCC commands per user per window (0 = off)
# autoignore_time <sec>      seconds a flooding user is ignored
# uploadhost <mask>       hostmask allowed to upload (empty = allow all)
# uploadminspace <MiB>    min free disk space to accept an upload (0 = none)
# restrictlist yes        only channel members may xdcc list/info
# restrictsend yes        only channel members may xdcc send/batch
# need_voice yes          + also require voice or higher (needs restrictsend)
# need_level <n>          + require level 1=voice 2=halfop 3=op (needs restrictsend)

# --- DCC / SXDCC ---
# usenatip <ip>           public IP advertised for DCC/SXDCC
# control_port <port>     single shared control port (TLS + web packlist)
# tcprangestart <port>    first DCC/TLS port (used when control_port is 0)
# tcprangelimit <port>    last DCC/TLS port
# passive_dcc yes         reverse DCC (bot connects out)
# tcp_nodelay yes         set TCP_NODELAY on transfer/chat sockets
# tcp_buffer_size <KB>    SO_SNDBUF/SO_RCVBUF (0 = OS default)
# sxdcc_enable yes        serve XDCC over TLS 1.3
# xdcc_enable no          disable classic cleartext XDCC/DCC (single-port/VPN)
# sxdcc_cert <path>       TLS certificate (self-signed generated if missing)
# sxdcc_key <path>        TLS private key

# --- admin / web ---
# adminpass <secret>      admin password (SDCC chat)
# owner_nick <nick>       answered by the SXDCC OWNER command
# http_port <port>        separate HTTP interface (0 = disabled); the packlist
#                         is also served on the control port as /packlist.txt

# --- uploads / misc ---
# uploaddir <dir>         receive incoming DCC SEND uploads
# uploadmaxsize <bytes>   incoming upload cap (0 = unlimited)
# headline <msg>          announced on channel join
# periodicmsg <msg>       periodic channel message
# periodicmsg_time <sec>  seconds between periodic messages
# statefile <path>
# old_statefile <path>    rolling backup of the previous statefile
# status_time_dcc_chat <sec>  status/logstats interval (default 120)
# logfile <path>
# logrotate <v>           rotation: never/daily/weekly/monthly/<N> hours
# expire_logfiles <days>  delete rotated logs older than N days (0 = keep)
# logfile_messages <path> separate PRIVMSG transcript (requires logmessages)
# logfile_notices <path>  separate NOTICE transcript (requires lognotices)
# logmessages yes         log incoming private messages (fall back to logfile)
# lognotices yes          log incoming notices (fall back to logfile)
# logstats yes            periodic status line into the logfile

`
