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

	TelnetEnable bool
	TelnetPort   int
	AdminPass    string
	AdminHost    []string

	SXDCCEnable bool
	CertFile    string
	KeyFile     string

	Headline         string // announced to the channel on join
	PeriodicMsg      string // periodic channel message
	PeriodicInterval int    // seconds between periodic messages (0 = disabled)

	UploadDir string // directory where incoming DCC SEND uploads are written

	// UploadMaxSize caps incoming DCC SEND uploads (bytes; 0 = unlimited).
	UploadMaxSize int64

	// Anti-abuse / access control (hostmasks nick!user@host).
	XDCCAllow []string
	XDCCDeny  []string

	// Pack scan filters (applied to filedir scans).
	NoDuplicateFiles bool
	AddDirExclude    []string
	AddDirMatch      []string
	AddDirMinSize    int64 // bytes (0 = no minimum)
}

// base returns the built-in defaults (no file).
func base() Config {
	return Config{
		Server:         "irc.rizon.net",
		Port:           6667,
		Nick:           "dinoga",
		Channel:        "#frog",
		FileDir:        "/data",
		PortStart:      30000,
		PortLimit:      30001,
		ControlPort:    0,
		SlotsMax:       3,
		QueueSize:      100,
		IdleQueueSize:  50,
		ReconnectDelay: 30,
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

func splitChannelSpec(spec string) (string, string) {
	channel := strings.TrimSpace(spec)
	key := ""
	if i := strings.Index(spec, " -key "); i >= 0 {
		channel = strings.TrimSpace(spec[:i])
		key = strings.TrimSpace(spec[i+len(" -key "):])
	}
	return channel, key
}
