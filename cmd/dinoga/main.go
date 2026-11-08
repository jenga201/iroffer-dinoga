// Command dinoga is the Go rewrite of the iroffer-dinoex XDCC bot. It reads an
// optional iroffer.config (or IROFFER_* environment variables), serves packs over
// XDCC/DCC and SXDCC, and exposes HTTP plus an interactive (TUI) admin console
// frontends.
package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/jenga201/dinoga/internal/admin"
	"github.com/jenga201/dinoga/internal/config"
	"github.com/jenga201/dinoga/internal/irc"
	"github.com/jenga201/dinoga/internal/logging"
)

func main() {
	printConfig := flag.Bool("print-config", false, "print a template iroffer.config + .env and exit")
	hashPass := flag.String("hash-password", "", "print the SHA-256 hash of the given admin password and exit")
	tui := flag.Bool("tui", false, "run an interactive terminal admin loop (no password)")
	configFile := flag.String("config", "/config/dinoga.config", "path to the dinoga.config file")
	flag.Parse()

	if *printConfig {
		c, _ := config.Load("")
		_ = config.Write(os.Stdout, c)
		return
	}
	if *hashPass != "" {
		sum := sha256.Sum256([]byte(*hashPass))
		fmt.Printf("%x\n", sum)
		return
	}

	cfgPath := *configFile
	resolved, migrated, err := config.Resolve(filepath.Dir(cfgPath), filepath.Base(cfgPath))
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if migrated {
		log.Printf("configure: migrated legacy iroffer config to %s", resolved)
	}
	cfg, overrides, err := config.LoadWithEnv(resolved, os.Environ())
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	for _, o := range overrides {
		log.Printf("configure: env override %s", o)
	}

	logger, err := logging.New(cfg)
	if err != nil {
		log.Fatalf("logging: %v", err)
	}
	defer logger.Close()
	log.SetFlags(0)
	log.SetOutput(logger)

	client := irc.New(cfg, logger)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *tui {
		go func() {
			if err := client.Run(ctx); err != nil {
				log.Printf("dinoga: %v", err)
			}
		}()
		runTUI(client.AdminRegistry())
		stop()
		return
	}

	if err := client.Run(ctx); err != nil {
		log.Printf("dinoga: %v", err)
		os.Exit(1)
	}
}

// runTUI drives the bot's admin command registry from stdin, without a password.
// It mirrors the chat admin command experience (status/list/quit/help).
func runTUI(reg *admin.Registry) {
	sc := bufio.NewScanner(os.Stdin)
	prompt := func() { fmt.Fprint(os.Stdout, "> ") }
	prompt()
	for sc.Scan() {
		fields := strings.Fields(strings.TrimSpace(sc.Text()))
		if len(fields) == 0 {
			prompt()
			continue
		}
		cmd, ok := reg.Lookup(fields[0])
		if !ok {
			fmt.Fprintf(os.Stdout, "unknown command: %s\n", fields[0])
			prompt()
			continue
		}
		if err := cmd.Run(os.Stdout, fields[1:]); err != nil {
			if err == admin.ErrQuit {
				return
			}
			fmt.Fprintf(os.Stdout, "error: %v\n", err)
		}
		prompt()
	}
}

// templateConfig is printed by --print-config. It documents the directives the
// Go bot understands; the bot reads only this file (env is handled by the
// container, not the binary).
const templateConfig = `# Dinoga (iroffer-dinoex Go rewrite) template config.
# Start the bot with:  dinoga --config /path/to/iroffer.config
# Comment lines start with '#'; each 'network { ... }' block adds an IRC network.

# --- IRC networks (one block per server) ---
# Optional SASL (IRCv3) login to a pre-registered account:
#   sasl_username <account>  (defaults to user_nick)
#   sasl_password <secret>
network {
  server irc.rizon.net 9999
  user_nick dinoga
  channel #frog
  connectionmethod ssl
}

# --- files served (XDCC/DCC) ---
filedir /data
autoadd_time 60            # rescan filedir every N seconds (0 = off)
noduplicatefiles yes       # drop files with duplicate name+size
adddir_exclude *.tmp       # skip files matching these globs (repeatable)
# adddir_match *.mkv        # only include files matching these globs
# adddir_min_size 4096      # skip files smaller than N bytes
slotsmax 3
queuesize 100
idlequeuesize 25

# --- access control (hostmasks nick!user@host, repeatable) ---
# xdcc_allow *!*@*.good.com   # allow-list (empty = allow all except deny)
# downloadhost *!*@friend.net # alias for xdcc_allow
# xdcc_deny *!*@banned.net    # deny-list (wins over allow)

# --- DCC ---
usenatip 2.31.76.192       # public IP advertised for DCC/SXDCC
tcprangestart 30000        # first DCC/TLS port
tcprangelimit 30001        # last DCC/TLS port
passive_dcc yes            # reverse DCC (bot connects out to the downloader)
# overallmaxspeed 1000      # global bandwidth cap (KB/s; 0 = unlimited)
# transfermaxspeed 500      # per-transfer cap (KB/s; 0 = unlimited)

# --- SXDCC (secure XDCC) ---
# sxdcc_enable yes         # serve XDCC over TLS 1.3 (needs usenatip; auto-generates
#                          # an Ed25519 self-signed cert for the IP)

# --- SXDCC data channel ---
# Data rides the shared control port: the bot advertises its control address in
# SEND and the client opens a second TLS connection (DATA <datatoken>) on that
# port. No separate data-port range or NAT traversal is required.

# --- admin / web ---
# adminpass <secret>       plaintext admin password
# http_port 8080           enable the HTTP pack interface
# http_allow 127.0.0.1     optional CIDR allow-list

# --- misc ---
# headline <msg>           announced on channel join
# periodicmsg <msg>        periodic channel message
# periodicmsg_time 3600    seconds between periodic messages
# uploaddir /uploads       receive incoming DCC SEND uploads here
# uploadmaxsize 104857600  # cap incoming uploads (bytes; 0 = unlimited)
# statefile /home/dinoga/state.json
# logfile /home/dinoga.log
`
