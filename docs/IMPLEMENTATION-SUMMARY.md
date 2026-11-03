# Dinoga Go rewrite — Implementation summary

Status as of 2026-11-02. Companion to `REWRITE-PLAN.md`; the SXDCC wire contract
is `IRC/dcc-protocol/SXDCC.md`.

## What was implemented

New/rewritten packages and wiring in the Go rewrite (stdlib-only, `go 1.23`):

| Package | What it provides | Status |
|---|---|---|
| `internal/config` | `Load(path)` reads an `iroffer.config`-style file (tolerant parser, multiple `network {…}` blocks, full-line `#` comments, formerly env-only directives). The binary is **file-only** (no env); env→config translation is deferred to the container | done + tests |
| `internal/packs` | O(number-of-files) `Scan(dir, opts…)`; **no eager hashing**; lazy memoized `MD5()`/`CRC32()`; options `WithRecurse`, `WithMatch`, `WithExclude`, `WithMinSize`, `WithNaturalSort`, `WithDedup`, `WithHashes` | done + tests + benchmark |
| `internal/queue` | Slot/queue manager: FIFO main + idle queues, per-user concurrent/queued caps, `Submit`/`Wait`/`TryWait`/`Cancel`, idempotent release | done + tests |
| `internal/transfer` | Token-bucket bandwidth throttle (`ThrottledReader`/`Writer`) | done + tests |
| `internal/xdcc` | `LIST`/`SEND`/`INFO`/`BATCH`/`REMOVE` command routing | done + tests |
| `internal/state` | JSON `statefile` persistence: pack listing + resume offsets (atomic save/load) | done + tests |
| `internal/sxdcc` | SXDCC bot server: Ed25519 self-signed IP-SAN identity (persist + load), SPKI pin, single-use tokens, TLS 1.3 control + data channels, **active and passive** data (`ACCEPT`), optional slot gating | done + tests (active + passive TLS roundtrips) |
| `internal/irc` | **Multi-network** client (one `netConn` goroutine per `network {}` block), auto-reconnect, queue workers, SXDCC trigger, state save, announce, DCC chat, uploads (ACK receive), DCC active/passive/resume handlers | done |
| `cmd/dinoga` | `Load(--config)`, optional `logfile`, and flags `--print-config` (template output), `--hash-password <pass>` (admin hash), `--tui` (interactive admin, no password) | done |
| `internal/web` / `internal/admin` | HTTP pack listing/JSON/MD5/SFV + telnet admin — **wired into the bot** (live-verified) | done + tests |

## Key result — 1M-file scan (stress)

`packs.Scan` was refactored from "MD5+CRC32 every file on every scan" (O(total
bytes)) to "stat + list only" (O(number of files)). Benchmark (docker,
`golang:1.23-alpine`):

| Files | Machine | Scan time | Rate |
|---|---|---|---|
| 200,000 | desktop Ryzen 9 9950X | 0.749 s | ~267k files/s |
| **1,000,000** | desktop Ryzen 9 9950X | **4.23 s** | ~236k files/s |
| **1,000,000** | **remote `dinoga` (2 vCPU / 3.7 GB)** | **12.07 s** | ~83k files/s |

File creation (45 s on the remote) is not part of the timed scan. The scan is
linear in file count, not total bytes, so a 1M-pack listing is ~4 s to index on
desktop and ~12 s on the constrained remote — within the "1M files without much
indexing overhead" target.

## A/B test (remote `dinoga`, channel `#frog`)

Deployed the new Go bot (`dinoga-go`, active DCC, ports 30000-30001) and the
original C fork (`iroffer-dinoga:latest`, `dinoga-c`, ports 30010-30011) side by
side and drove both with `irc-test-client`:

| Step | Go bot | C bot |
|---|---|---|
| `xdcc list` | `#1  28 [testpack.bin]` | (C bot via SEND) |
| `xdcc send #1` | `DCC SEND testpack.bin 35605696 30000 28` | `DCC SEND testpack.bin 35605696 30010 28` |
| DCC receive | 28 bytes, `DONE` | 28 bytes, `Completed … md5sum` |
| md5 | `dfeecf181d0e3844a64f8dd6708e7733` | `dfeecf181d0e3844a64f8dd6708e7733` |

Both bots produced identical wire behaviour and identical file content (same
MD5), i.e. the Go rewrite interoperates with the standard PircBotX client
exactly as the original bot does.

## Web + telnet (live-verified on remote)

- `GET /list.json` → `[{"number":1,"name":"testpack.bin","size":28}]`.
- telnet (port 2323): `Password:` → `Welcome` → `status` → `packs: 1` → `quit` → `bye`.

## SXDCC status

The bot (server) side is implemented and unit-tested end-to-end over real TLS 1.3
(identity generation, IP SAN, SPKI pin, `AUTH` token single-use, `LIST`, active
**and passive** `SEND` + `DATA` streaming, `DONE`). In both modes the **bot remains
the TLS server** and the **client the TLS client** (verifies the pin); only the TCP
direction flips for passive, exactly as classic reverse DCC. SEND is gated on a
transfer slot through an optional `SlotGate` (the `queue.Manager`). Deferred only:

- Admin command to regenerate the bot key.

The **PircBotX client library** now lives at `IRC/irc-test-client/pircbotx`
(`org.pircbotx.sxdcc`): `SxdccPin` (SPKI pin + constant-time compare),
`SxdccTrustManager` (pin + optional IP-SAN trust), `SxdccMessages`
(invite/trigger/SEND parsing), `SxdccTrustStore` (IRC TOFU), and `SxdccClient`
(TLS connect + AUTH + line protocol). stdlib-only, Java-8-compatible source
(TLS 1.3 requires a Java 11+ runtime); a dependency-free `SxdccSelfTest` verifies
pin/parsing/trust-store/fail-closed trust-manager behavior.

Classic cleartext XDCC/DCC remains fully intact and dual-mode: `xdcc <verb>` is
cleartext, `sxdcc <verb>` is the TLS rendezvous.

## Verification

- `go test ./...` — all packages pass (`config`, `dcc`, `packs`, `queue`,
  `transfer`, `sxdcc`, `xdcc`, `state`; plus `web`/`admin` from a sub-agent).
- `docker build -t dinoga:test .` — image builds cleanly (multi-stage,
  CGO_ENABLED=0, alpine runtime).

## How to build / run / test

Build and unit-test inside Docker (no host Go):

    wsl bash -c 'cd /opt/code/IRC/iroffer-dinoga && docker build -t dinoga:test .'
    wsl bash -c 'cd /opt/code/IRC/iroffer-dinoga && docker run --rm -v /opt/code/IRC/iroffer-dinoga:/src -w /src golang:1.23-alpine go test ./...'

Run (same `IROFFER_*` env as the original container):

    Run (config file only — mount it at the default `--config` path):

        docker run --rm -v "$PWD/iroffer.config:/home/dinoga/iroffer.config" -v "$PWD/files:/data" -p 30000-30001:30000-30001 dinoga:test

    The binary reads only `iroffer.config` — `usenatip`, `tcprangestart/limit`,
    `passive_dcc`, `sxdcc_*`, `headline`, `periodicmsg*`, `uploaddir`, `statefile`,
    `logfile`, `http_port`, `telnet_port` are all file directives (see
    `--print-config`). Env→config translation is deferred to the container, not the
    binary.

    ## Out of scope / next steps

    - SXDCC admin key-regen command.
    - Env→config translation in the Docker container entrypoint.
    - GeoIP (MaxMind mmdb) and FISH (Blowfish) — DEFERred (external deps).
    - Remaining parity: DCC chat status screen, `uploadhost` limits, `announce_*`,
      pack descriptions.