package config

import (
	"strconv"
	"strings"
)

// ApplyEnvOverrides overlays IROFFER_* environment variables onto c, replacing
// any file-provided value (env wins). It returns a description of each applied
// override ("<directive>=<value>") in a stable, sorted order, for startup
// INFO logging. Only globally-scoped directives are applied here; per-network
// IROFFER_NETWORK_<n>_* overrides are intentionally not handled (network blocks
// are authored in the config file).
func ApplyEnvOverrides(c *Config, environ []string) []string {
	env := environMap(environ)

	var applied []string
	apply := func(directive, value string) {
		applied = append(applied, directive+"="+value)
	}

	if v, ok := get(env, "control_port"); ok && v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			if c.ControlPort != n {
				c.ControlPort = n
				apply("control_port", v)
			}
		}
	}
	if v, ok := get(env, "usenatip"); ok && v != "" {
		c.DCCIP = strings.TrimSpace(v)
		apply("usenatip", v)
	}
	if v, ok := get(env, "filedir"); ok && v != "" {
		// filedir is a single directory in dinoga; take the first entry.
		c.FileDir = firstMulti(v)
		apply("filedir", v)
	}
	if v, ok := get(env, "autoadd_time"); ok && v != "" {
		c.AutoAdd = atoiOr(v, c.AutoAdd)
		apply("autoadd_time", v)
	}
	if v, ok := get(env, "tcprangestart"); ok && v != "" {
		c.PortStart = atoiOr(v, c.PortStart)
		apply("tcprangestart", v)
	}
	if v, ok := get(env, "tcprangelimit"); ok && v != "" {
		c.PortLimit = atoiOr(v, c.PortLimit)
		apply("tcprangelimit", v)
	}
	if v, ok := get(env, "passive_dcc"); ok && v != "" {
		c.PassiveDCC = yesNo(v, c.PassiveDCC)
		apply("passive_dcc", v)
	}

	if v, ok := get(env, "slotsmax"); ok && v != "" {
		c.SlotsMax = atoiOr(v, c.SlotsMax)
		apply("slotsmax", v)
	}
	if v, ok := get(env, "queuesize"); ok && v != "" {
		c.QueueSize = atoiOr(v, c.QueueSize)
		apply("queuesize", v)
	}
	if v, ok := get(env, "idlequeuesize"); ok && v != "" {
		c.IdleQueueSize = atoiOr(v, c.IdleQueueSize)
		apply("idlequeuesize", v)
	}
	if v, ok := get(env, "maxtransfersperperson"); ok && v != "" {
		c.MaxTransfersPerPerson = atoiOr(v, c.MaxTransfersPerPerson)
		apply("maxtransfersperperson", v)
	}
	if v, ok := get(env, "maxqueueitemsperperson"); ok && v != "" {
		c.MaxQueuedPerPerson = atoiOr(v, c.MaxQueuedPerPerson)
		apply("maxqueueitemsperperson", v)
	}
	if v, ok := get(env, "maxidlequeuedperperson"); ok && v != "" {
		c.MaxIdleQueuedPerPerson = atoiOr(v, c.MaxIdleQueuedPerPerson)
		apply("maxidlequeuedperperson", v)
	}
	if v, ok := get(env, "transfermaxspeed"); ok && v != "" {
		c.TransferMaxSpeed = speedToBytes(v, c.TransferMaxSpeed)
		apply("transfermaxspeed", v)
	}
	if v, ok := get(env, "overallmaxspeed"); ok && v != "" {
		c.OverallMaxSpeed = speedToBytes(v, c.OverallMaxSpeed)
		apply("overallmaxspeed", v)
	}
	if v, ok := get(env, "reconnect_delay"); ok && v != "" {
		c.ReconnectDelay = atoiOr(v, c.ReconnectDelay)
		apply("reconnect_delay", v)
	}
	if v, ok := get(env, "statefile"); ok && v != "" {
		c.StateFile = strings.TrimSpace(v)
		apply("statefile", v)
	}
	if v, ok := get(env, "logfile"); ok && v != "" {
		c.LogFile = strings.TrimSpace(v)
		apply("logfile", v)
	}
	if v, ok := get(env, "logrotate"); ok && v != "" {
		c.LogRotate = strings.TrimSpace(v)
		apply("logrotate", v)
	}
	if v, ok := get(env, "expire_logfiles"); ok && v != "" {
		c.ExpireLogFiles = atoiOr(v, c.ExpireLogFiles)
		apply("expire_logfiles", v)
	}
	if v, ok := get(env, "logmessages"); ok && v != "" {
		c.LogMessages = yesNo(v, c.LogMessages)
		apply("logmessages", v)
	}
	if v, ok := get(env, "lognotices"); ok && v != "" {
		c.LogNotices = yesNo(v, c.LogNotices)
		apply("lognotices", v)
	}
	if v, ok := get(env, "logfile_messages"); ok && v != "" {
		c.LogFileMessages = strings.TrimSpace(v)
		apply("logfile_messages", v)
	}
	if v, ok := get(env, "logfile_notices"); ok && v != "" {
		c.LogFileNotices = strings.TrimSpace(v)
		apply("logfile_notices", v)
	}
	if v, ok := get(env, "logstats"); ok && v != "" {
		c.LogStats = yesNo(v, c.LogStats)
		apply("logstats", v)
	}

	if v, ok := get(env, "http_port"); ok && v != "" {
		c.HTTPPort = atoiOr(v, c.HTTPPort)
		c.HTTPEnable = c.HTTPPort > 0
		apply("http_port", v)
	}
	if v, ok := get(env, "http_allow"); ok {
		c.HTTPAllow = splitMulti(v)
		apply("http_allow", v)
	}
	if v, ok := get(env, "http_deny"); ok {
		c.HTTPDeny = splitMulti(v)
		apply("http_deny", v)
	}

	if v, ok := get(env, "adminpass"); ok && v != "" {
		c.AdminPass = v
		apply("adminpass", "<redacted>")
	}

	if v, ok := get(env, "sxdcc_enable"); ok && v != "" {
		c.SXDCCEnable = yesNo(v, c.SXDCCEnable)
		apply("sxdcc_enable", v)
	}
	if v, ok := get(env, "sxdcc_cert"); ok && v != "" {
		c.CertFile = strings.TrimSpace(v)
		apply("sxdcc_cert", v)
	}
	if v, ok := get(env, "sxdcc_key"); ok && v != "" {
		c.KeyFile = strings.TrimSpace(v)
		apply("sxdcc_key", v)
	}

	if v, ok := get(env, "headline"); ok && v != "" {
		c.Headline = v
		apply("headline", v)
	}
	if v, ok := get(env, "periodicmsg"); ok && v != "" {
		c.PeriodicMsg = v
		apply("periodicmsg", v)
	}
	if v, ok := get(env, "periodicmsg_time"); ok && v != "" {
		c.PeriodicInterval = atoiOr(v, c.PeriodicInterval)
		apply("periodicmsg_time", v)
	}
	if v, ok := get(env, "uploaddir"); ok && v != "" {
		c.UploadDir = strings.TrimSpace(v)
		apply("uploaddir", v)
	}
	if v, ok := get(env, "uploadmaxsize"); ok && v != "" {
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			c.UploadMaxSize = n
			apply("uploadmaxsize", v)
		}
	}

	if v, ok := get(env, "xdcc_allow"); ok {
		c.XDCCAllow = splitMulti(v)
		apply("xdcc_allow", v)
	}
	if v, ok := get(env, "xdcc_deny"); ok {
		c.XDCCDeny = splitMulti(v)
		apply("xdcc_deny", v)
	}
	if v, ok := get(env, "noduplicatefiles"); ok && v != "" {
		c.NoDuplicateFiles = yesNo(v, c.NoDuplicateFiles)
		apply("noduplicatefiles", v)
	}
	if v, ok := get(env, "adddir_exclude"); ok {
		c.AddDirExclude = splitMulti(v)
		apply("adddir_exclude", v)
	}
	if v, ok := get(env, "adddir_match"); ok {
		c.AddDirMatch = splitMulti(v)
		apply("adddir_match", v)
	}
	if v, ok := get(env, "adddir_min_size"); ok && v != "" {
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			c.AddDirMinSize = n
			apply("adddir_min_size", v)
		}
	}
	if v, ok := get(env, "autoaddann"); ok && v != "" {
		c.AutoAddAnn = yesNo(v, c.AutoAddAnn)
		apply("autoaddann", v)
	}
	if v, ok := get(env, "nat_sort"); ok && v != "" {
		c.NaturalSort = yesNo(v, c.NaturalSort)
		apply("nat_sort", v)
	}
	if v, ok := get(env, "nocrc32"); ok && v != "" {
		c.NoCRC32 = yesNo(v, c.NoCRC32)
		apply("nocrc32", v)
	}
	if v, ok := get(env, "nomd5sum"); ok && v != "" {
		c.NoMD5Sum = yesNo(v, c.NoMD5Sum)
		apply("nomd5sum", v)
	}
	if v, ok := get(env, "xdcclistfile"); ok && v != "" {
		c.XDCCListFile = strings.TrimSpace(v)
		apply("xdcclistfile", v)
	}
	if v, ok := get(env, "xdccxmlfile"); ok && v != "" {
		c.XDCCXMLFile = strings.TrimSpace(v)
		apply("xdccxmlfile", v)
	}
	if v, ok := get(env, "tcp_nodelay"); ok && v != "" {
		c.TCPNoDelay = yesNo(v, c.TCPNoDelay)
		apply("tcp_nodelay", v)
	}
	if v, ok := get(env, "tcp_buffer_size"); ok && v != "" {
		c.TCPBufferSize = speedToBytes(v, c.TCPBufferSize)
		apply("tcp_buffer_size", v)
	}
	if v, ok := get(env, "uploadhost"); ok {
		c.UploadAllow = splitMulti(v)
		apply("uploadhost", v)
	}
	if v, ok := get(env, "uploadminspace"); ok && v != "" {
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			c.UploadMinSpace = n * 1024 * 1024
			apply("uploadminspace", v)
		}
	}
	if v, ok := get(env, "restrictlist"); ok && v != "" {
		c.RestrictList = yesNo(v, c.RestrictList)
		apply("restrictlist", v)
	}
	if v, ok := get(env, "restrictsend"); ok && v != "" {
		c.RestrictSend = yesNo(v, c.RestrictSend)
		apply("restrictsend", v)
	}
	if v, ok := get(env, "need_voice"); ok && v != "" {
		c.NeedVoice = yesNo(v, c.NeedVoice)
		apply("need_voice", v)
	}
	if v, ok := get(env, "need_level"); ok && v != "" {
		c.NeedLevel = atoiOr(v, c.NeedLevel)
		apply("need_level", v)
	}
	if v, ok := get(env, "announce_size"); ok && v != "" {
		c.AnnounceSize = yesNo(v, c.AnnounceSize)
		apply("announce_size", v)
	}
	if v, ok := get(env, "announce_date"); ok && v != "" {
		c.AnnounceDate = yesNo(v, c.AnnounceDate)
		apply("announce_date", v)
	}
	if v, ok := get(env, "status_time_dcc_chat"); ok && v != "" {
		c.StatusInterval = atoiOr(v, c.StatusInterval)
		apply("status_time_dcc_chat", v)
	}
	if v, ok := get(env, "old_statefile"); ok && v != "" {
		c.OldStateFile = strings.TrimSpace(v)
		apply("old_statefile", v)
	}
	if v, ok := get(env, "flood_protection_rate"); ok && v != "" {
		c.FloodProtectionRate = atoiOr(v, c.FloodProtectionRate)
		apply("flood_protection_rate", v)
	}
	if v, ok := get(env, "autoignore_time"); ok && v != "" {
		c.AutoIgnoreTime = atoiOr(v, c.AutoIgnoreTime)
		apply("autoignore_time", v)
	}

	return applied
}

func environMap(environ []string) map[string]string {
	env := make(map[string]string, len(environ))
	for _, kv := range environ {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			env[kv[:i]] = kv[i+1:]
		}
	}
	return env
}

// get looks up an IROFFER_<DIRECTIVE> environment variable.
func get(env map[string]string, directive string) (string, bool) {
	v, ok := env["IROFFER_"+strings.ToUpper(directive)]
	return v, ok
}

// splitMulti splits a ";;"-separated multi-value directive.
func splitMulti(s string) []string {
	parts := strings.Split(s, ";;")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// firstMulti returns the first ";;"-separated value, trimmed.
func firstMulti(s string) string {
	return splitMulti(s)[0]
}
