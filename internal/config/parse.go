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
			ch, k := splitChannelSpec(value)
			if cur != nil {
				cur.Channel, cur.Key = ch, k
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
		case "http_port":
			c.HTTPPort = atoiOr(value, c.HTTPPort)
			c.HTTPEnable = c.HTTPPort > 0
		case "http_allow":
			c.HTTPAllow = fields(value)
		case "http_deny":
			c.HTTPDeny = fields(value)
		case "telnet_port":
			c.TelnetPort = atoiOr(value, c.TelnetPort)
			c.TelnetEnable = c.TelnetPort > 0
		case "adminpass":
			c.AdminPass = value
		case "adminhost":
			c.AdminHost = fields(value)
		case "sxdcc_enable":
			c.SXDCCEnable = yesNo(value, c.SXDCCEnable)
		case "sxdcc_cert":
			c.CertFile = value
		case "sxdcc_key":
			c.KeyFile = value
		case "sxdcc_bounce":
			c.SXDCCBounce = value
		case "sxdcc_tunnel":
			c.SXDCCTunnel = value
		case "sxdcc_tunnel_secret":
			c.SXDCCTunnelSecret = value
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
