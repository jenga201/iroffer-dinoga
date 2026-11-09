// Package config loads the bot's iroffer.config-style configuration file.
//
// The bot itself reads only the config file (see parse.go); environment
// handling is deferred to the Docker container, which may translate environment
// variables into the config file before starting the binary.
package config

import (
	"os"
	"strconv"
	"strings"
)

// Network is one IRC server connection (an iroffer "network block").
type Network struct {
	Name    string
	Server  string
	Port    int
	SSL     bool
	Nick    string
	Channel string
	Key     string

	// SASL (IRCv3) authentication. SASL is enabled when SASLPass is non-empty;
	// SASLUser defaults to Nick when empty (the common NickServ case).
	SASLUser string
	SASLPass string

	// NoAnnounce silences this channel's announcements (headline/periodic/
	// autoaddann). It is the `channel #chan -noannounce` flag.
	NoAnnounce bool
}

// Config is the runtime configuration for the bot. The Server/Port/SSL/Nick/
// Channel/ChannelKey fields always mirror Networks[0] for single-network
// accessors and remain for compatibility.
type Config struct {
	// Networks lists every IRC network to connect to.
	Networks []Network

	Server     string
	Port       int
	SSL        bool
	Nick       string
	Channel    string
	ChannelKey string

	DCCIP       string // advertised public IP for DCC/SXDCC (usenatip)
	FileDir     string // directory scanned into the packlist (filedir)
	PortStart   int    // first DCC/TLS port (tcprangestart)
	PortLimit   int    // last DCC/TLS port (tcprangelimit)
	ControlPort int    // single shared control port (control_port; 0 = use PortStart)
	AutoAdd     int    // seconds between FileDir rescans (autoadd_time; 0 = off)
	PassiveDCC  bool   // reverse/passive DCC (passive_dcc)

	ReconnectDelay         int
	StateFile              string
	LogFile                string
	SlotsMax               int
	QueueSize              int
	IdleQueueSize          int
	MaxTransfersPerPerson  int
	MaxQueuedPerPerson     int
	MaxIdleQueuedPerPerson int
	TransferMaxSpeed       int64 // bytes/sec (0 = unlimited)
	OverallMaxSpeed        int64 // bytes/sec (0 = unlimited)

	HTTPEnable bool
	HTTPPort   int
	HTTPAllow  []string
	HTTPDeny   []string

	AdminPass string
	OwnerNick string // owner_nick: answered by the SXDCC OWNER command

	SXDCCEnable bool
	CertFile    string
	KeyFile     string

	// XDCCEnable toggles classic cleartext XDCC/DCC (xdcc list/send/... plus the
	// DCC transfer engine). Disable it in single-port/VPN deployments so the bot
	// serves only SXDCC/SDCC/HTTP on the control port (xdcc_enable, default yes).
	XDCCEnable bool

	Headline         string // announced to the channel on join
	PeriodicMsg      string // periodic channel message
	PeriodicInterval int    // seconds between periodic messages (0 = disabled)

	UploadDir string // directory where incoming DCC SEND uploads are written

	// UploadMaxSize caps incoming DCC SEND uploads (bytes; 0 = unlimited).
	UploadMaxSize int64

	// Logging / stats (see docs/REWRITE-PLAN.md #15).
	LogRotate       string // logrotate: never/daily/weekly/monthly/<N> hours
	ExpireLogFiles  int    // expire_logfiles: days to retain rotated logs (0 = keep)
	LogMessages     bool   // log incoming PRIVMSG lines
	LogNotices      bool   // log incoming NOTICE lines
	LogFileMessages string // logfile_messages: separate transcript file
	LogFileNotices  string // logfile_notices: separate transcript file
	LogStats        bool   // periodic status line to the log

	// Anti-abuse / access control (hostmasks nick!user@host).
	XDCCAllow []string
	XDCCDeny  []string

	// Pack scan filters (applied to filedir scans).
	NoDuplicateFiles bool
	AddDirExclude    []string
	AddDirMatch      []string
	AddDirMinSize    int64 // bytes (0 = no minimum)

	// Announce newly-added packs after each scan (autoaddann).
	AutoAddAnn bool

	// NaturalSort orders the pack listing with strnatcmp-style ordering (nat_sort).
	NaturalSort bool
	// NoCRC32 suppresses CRC32 advertisement in "xdcc info" (nocrc32).
	NoCRC32 bool
	// NoMD5Sum suppresses MD5 advertisement in "xdcc info" (nomd5sum).
	NoMD5Sum bool
	// XDCCListFile is an optional plain-text pack list written after each scan
	// (xdcclistfile). Empty = disabled.
	XDCCListFile string
	// XDCCXMLFile is an optional XML pack list written after each scan
	// (xdccxmlfile). Empty = disabled.
	XDCCXMLFile string

	// TCP socket tuning.
	TCPNoDelay    bool  // tcp_nodelay: set TCP_NODELAY on transfer/chat sockets
	TCPBufferSize int64 // tcp_buffer_size: SO_SNDBUF/SO_RCVBUF in KB (0 = OS default)

	// Upload access control.
	UploadAllow    []string // uploadhost: hostmasks allowed to upload (empty = all)
	UploadMinSpace int64    // uploadminspace: min free MiB on uploaddir (bytes; 0 = none)

	// Channel-mode download gating (need_voice/need_level refine restrict*,
	// which must be set to take effect — matching upstream).
	RestrictList bool // restrictlist: only channel members may xdcc list/info
	RestrictSend bool // restrictsend: only channel members may xdcc send/batch
	NeedVoice    bool // need_voice: require +v or higher to download
	NeedLevel    int  // need_level: 0=none, 1=voice, 2=halfop, 3=op

	// Announcement formatting.
	AnnounceSize bool // announce_size: include size in autoaddann announce
	AnnounceDate bool // announce_date: include date in autoaddann announce

	// Status / persistence.
	StatusInterval int    // status_time_dcc_chat: status line interval in seconds
	OldStateFile   string // old_statefile: rolling backup of the previous statefile

	// Flood protection: drop a user for AutoIgnoreTime seconds once they send
	// more than FloodProtectionRate XDCC commands within a short window (0
	// disables flood protection).
	FloodProtectionRate int // 0 disables flood protection
	AutoIgnoreTime      int // seconds a flooding user is ignored
}

// base returns the built-in defaults (no file).
func base() Config {
	return Config{
		Server:              "irc.rizon.net",
		Port:                6667,
		Nick:                "dinoga",
		Channel:             "#frog",
		FileDir:             "/data",
		PortStart:           30000,
		PortLimit:           30001,
		ControlPort:         0,
		SlotsMax:            3,
		QueueSize:           100,
		IdleQueueSize:       50,
		ReconnectDelay:      30,
		FloodProtectionRate: 6,
		AutoIgnoreTime:      10,
		LogStats:            true,
		StatusInterval:      120,
		XDCCEnable:          true,
	}
}

// Load reads path (an iroffer.config-style file) onto built-in defaults. A
// missing file yields the defaults (no error); parse errors are returned.
func Load(path string) (Config, error) {
	c := base()
	if path == "" {
		return ensureNetworks(c), nil
	}
	if err := applyFile(&c, path); err != nil {
		if os.IsNotExist(err) {
			return ensureNetworks(c), nil
		}
		return c, err
	}
	return ensureNetworks(c), nil
}

// LoadWithEnv reads path (as Load), then overlays IROFFER_* environment
// variables, which win over any file-provided value. The returned slice
// describes each override applied, in stable order, for startup INFO logging.
func LoadWithEnv(path string, environ []string) (Config, []string, error) {
	c, err := Load(path)
	if err != nil {
		return c, nil, err
	}
	applied := ApplyEnvOverrides(&c, environ)
	return c, applied, nil
}

// ensureNetworks makes the single-network mirror fields and Networks consistent:
// with no networks it synthesizes one from the mirror fields; with networks it
// syncs the mirror fields from Networks[0].
func ensureNetworks(c Config) Config {
	if len(c.Networks) == 0 {
		c.Networks = []Network{{
			Server: c.Server, Port: c.Port, SSL: c.SSL,
			Nick: c.Nick, Channel: c.Channel, Key: c.ChannelKey,
		}}
	}
	n := c.Networks[0]
	c.Server, c.Port, c.SSL = n.Server, n.Port, n.SSL
	c.Nick, c.Channel, c.ChannelKey = n.Nick, n.Channel, n.Key
	return c
}

func splitServerSpec(spec, defHost string, defPort int) (string, int) {
	host := defHost
	port := defPort
	if i := strings.LastIndexByte(spec, ' '); i >= 0 {
		host = strings.TrimSpace(spec[:i])
		if p, err := strconv.Atoi(strings.TrimSpace(spec[i+1:])); err == nil {
			port = p
		}
	} else if strings.TrimSpace(spec) != "" {
		host = strings.TrimSpace(spec)
	}
	return host, port
}

func splitChannelSpec(spec string) (channel, key string, noannounce bool) {
	fields := strings.Fields(spec)
	if len(fields) == 0 {
		return "", "", false
	}
	channel = fields[0]
	for i := 1; i < len(fields); i++ {
		switch fields[i] {
		case "-noannounce":
			noannounce = true
		case "-key":
			if i+1 < len(fields) {
				key = fields[i+1]
				i++
			}
		}
	}
	return channel, key, noannounce
}
