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
