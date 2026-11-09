package config

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// applyFile reads an iroffer.config-style file into c. Parsing is tolerant:
// unknown directives are ignored, comments (# at the start of a line) and blank
// lines are skipped, and multiple `network { ... }` blocks are collected into
// c.Networks. Global directives apply to the shared config.
func applyFile(c *Config, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var nets []Network
	var cur *Network

	for sc.Scan() {
		line := strings.TrimSpace(stripComment(sc.Text()))
		if line == "" {
			continue
		}
		if line == "{" {
			continue
		}
		if line == "}" {
			cur = nil
			continue
		}
		if strings.HasPrefix(strings.ToLower(line), "network") && strings.HasSuffix(line, "{") {
			nets = append(nets, Network{Port: 6667})
			cur = &nets[len(nets)-1]
			continue
		}

		key, value := splitKV(line)
		if key == "" {
			continue
		}

		switch key {
		case "server":
			h, p := splitServerSpec(value, "", 6667)
			if cur != nil {
				cur.Server, cur.Port = h, p
			} else {
				c.Server, c.Port = h, p
			}
		case "connectionmethod":
			if cur != nil {
				cur.SSL = strings.EqualFold(value, "ssl")
			} else {
				c.SSL = strings.EqualFold(value, "ssl")
			}
		case "user_nick", "usernick":
			if cur != nil {
				cur.Nick = value
			} else {
				c.Nick = value
			}
		case "channel":
			ch, k, na := splitChannelSpec(value)
			if cur != nil {
				cur.Channel, cur.Key, cur.NoAnnounce = ch, k, na
			} else {
				c.Channel, c.ChannelKey = ch, k
			}
		case "sasl_username", "sasl_user":
			if cur != nil {
				cur.SASLUser = value
			}
		case "sasl_password", "sasl_pass":
			if cur != nil {
				cur.SASLPass = value
			}

		// Directives that were formerly env-only now live in the file.
		case "usenatip":
			c.DCCIP = value
		case "filedir", "autoadd_dir":
			c.FileDir = value
		case "autoadd_time":
			c.AutoAdd = atoiOr(value, c.AutoAdd)
		case "tcprangestart":
			c.PortStart = atoiOr(value, c.PortStart)
		case "tcprangelimit":
			c.PortLimit = atoiOr(value, c.PortLimit)
		case "control_port":
			c.ControlPort = atoiOr(value, c.ControlPort)
		case "passive_dcc":
			c.PassiveDCC = yesNo(value, c.PassiveDCC)

		case "slotsmax":
			c.SlotsMax = atoiOr(value, c.SlotsMax)
		case "queuesize":
			c.QueueSize = atoiOr(value, c.QueueSize)
		case "idlequeuesize":
			c.IdleQueueSize = atoiOr(value, c.IdleQueueSize)
		case "maxtransfersperperson":
			c.MaxTransfersPerPerson = atoiOr(value, c.MaxTransfersPerPerson)
		case "maxqueueitemsperperson":
			c.MaxQueuedPerPerson = atoiOr(value, c.MaxQueuedPerPerson)
		case "maxidlequeuedperperson":
			c.MaxIdleQueuedPerPerson = atoiOr(value, c.MaxIdleQueuedPerPerson)
		case "transfermaxspeed":
			c.TransferMaxSpeed = speedToBytes(value, c.TransferMaxSpeed)
		case "overallmaxspeed":
			c.OverallMaxSpeed = speedToBytes(value, c.OverallMaxSpeed)
		case "reconnect_delay":
			c.ReconnectDelay = atoiOr(value, c.ReconnectDelay)
		case "statefile":
			c.StateFile = value
		case "logfile":
			c.LogFile = value
		case "logrotate":
			c.LogRotate = value
		case "expire_logfiles":
			c.ExpireLogFiles = atoiOr(value, c.ExpireLogFiles)
		case "logmessages":
			c.LogMessages = yesNo(value, c.LogMessages)
		case "lognotices":
			c.LogNotices = yesNo(value, c.LogNotices)
		case "logfile_messages":
			c.LogFileMessages = value
		case "logfile_notices":
			c.LogFileNotices = value
		case "logstats":
			c.LogStats = yesNo(value, c.LogStats)
		case "http_port":
			c.HTTPPort = atoiOr(value, c.HTTPPort)
			c.HTTPEnable = c.HTTPPort > 0
		case "http_allow":
			c.HTTPAllow = fields(value)
		case "http_deny":
			c.HTTPDeny = fields(value)
		case "adminpass":
			c.AdminPass = value
		case "sxdcc_enable":
			c.SXDCCEnable = yesNo(value, c.SXDCCEnable)
		case "xdcc_enable":
			c.XDCCEnable = yesNo(value, c.XDCCEnable)
		case "sxdcc_cert":
			c.CertFile = value
		case "sxdcc_key":
			c.KeyFile = value
		case "headline":
			c.Headline = value
		case "periodicmsg":
			c.PeriodicMsg = value
		case "periodicmsg_time":
			c.PeriodicInterval = atoiOr(value, c.PeriodicInterval)
		case "uploaddir":
			c.UploadDir = value
		case "uploadmaxsize":
			if n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
				c.UploadMaxSize = n
			}
		case "xdcc_allow", "downloadhost":
			c.XDCCAllow = append(c.XDCCAllow, fields(value)...)
		case "xdcc_deny":
			c.XDCCDeny = append(c.XDCCDeny, fields(value)...)
		case "noduplicatefiles", "no_duplicate_filenames":
			c.NoDuplicateFiles = yesNo(value, c.NoDuplicateFiles)
		case "adddir_exclude":
			c.AddDirExclude = append(c.AddDirExclude, fields(value)...)
		case "adddir_match":
			c.AddDirMatch = append(c.AddDirMatch, fields(value)...)
		case "adddir_min_size":
			if n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
				c.AddDirMinSize = n
			}
		case "autoaddann":
			c.AutoAddAnn = yesNo(value, c.AutoAddAnn)
		case "nat_sort":
			c.NaturalSort = yesNo(value, c.NaturalSort)
		case "nocrc32":
			c.NoCRC32 = yesNo(value, c.NoCRC32)
		case "nomd5sum":
			c.NoMD5Sum = yesNo(value, c.NoMD5Sum)
		case "xdcclistfile":
			c.XDCCListFile = value
		case "xdccxmlfile":
			c.XDCCXMLFile = value
		case "tcp_nodelay":
			c.TCPNoDelay = yesNo(value, c.TCPNoDelay)
		case "tcp_buffer_size":
			c.TCPBufferSize = speedToBytes(value, c.TCPBufferSize)
		case "uploadhost":
			c.UploadAllow = append(c.UploadAllow, fields(value)...)
		case "uploadminspace":
			if n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
				c.UploadMinSpace = n * 1024 * 1024
			}
		case "restrictlist":
			c.RestrictList = yesNo(value, c.RestrictList)
		case "restrictsend":
			c.RestrictSend = yesNo(value, c.RestrictSend)
		case "need_voice":
			c.NeedVoice = yesNo(value, c.NeedVoice)
		case "need_level":
			c.NeedLevel = atoiOr(value, c.NeedLevel)
		case "announce_size":
			c.AnnounceSize = yesNo(value, c.AnnounceSize)
		case "announce_date":
			c.AnnounceDate = yesNo(value, c.AnnounceDate)
		case "status_time_dcc_chat":
			c.StatusInterval = atoiOr(value, c.StatusInterval)
		case "old_statefile":
			c.OldStateFile = value
		case "flood_protection_rate":
			c.FloodProtectionRate = atoiOr(value, c.FloodProtectionRate)
		case "autoignore_time":
			c.AutoIgnoreTime = atoiOr(value, c.AutoIgnoreTime)
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if len(nets) > 0 {
		c.Networks = nets
	}
	return nil
}

func stripComment(s string) string {
	// Comment lines begin with '#'. A '#' elsewhere is significant (e.g. a
	// channel name like "#frog"), so only whole-line comments are stripped.
	if strings.HasPrefix(strings.TrimSpace(s), "#") {
		return ""
	}
	return s
}

func splitKV(line string) (string, string) {
	parts := strings.SplitN(line, " ", 2)
	key := strings.TrimSpace(parts[0])
	if len(parts) == 1 {
		return key, ""
	}
	return key, strings.TrimSpace(parts[1])
}

func atoiOr(s string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return n
	}
	return def
}

// speedToBytes interprets a KB/s value and returns bytes/sec.
func speedToBytes(s string, def int64) int64 {
	if n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil {
		return n * 1024
	}
	return def
}

func fields(s string) []string {
	return strings.Fields(s)
}

func yesNo(s string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "yes", "1", "on", "true":
		return true
	case "no", "0", "off", "false":
		return false
	}
	return def
}
