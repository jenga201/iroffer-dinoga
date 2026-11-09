# Dinoga Go rewrite — Implementation summary

Status as of 2026-11-09. Companion to `REWRITE-PLAN.md`; the wire contracts are
`IRC/dcc-protocol/{DCC,XDCC,SXDCC,SDCC}.md`.

## What's implemented

The module (`module github.com/jenga201/dinoga`, `go 1.25`) is **stdlib-only**:
`go.mod` has no external `require` lines, so `go test` / `go build` run offline.

| Package | What it provides | Status |
|---|---|---|
| `internal/config` | `iroffer.config` parser (tolerant, multiple `network {…}` blocks, full-line `#` comments), `IROFFER_*` env overlay (env wins), legacy→`dinoga.config` migration, template writer | done + tests |
| `internal/packs` | O(number-of-files) `Scan(dir, opts…)`, no eager hashing, lazy memoized `MD5()`/`CRC32()`, `WithRecurse`/`WithMatch`/`WithExclude`/`WithMinSize`/`WithNaturalSort`/`WithDedup`/`WithHashes`, natural-sort `Less` | done + tests + benchmark |
| `internal/queue` | Slot/queue manager: FIFO main + idle queues, per-user concurrent/queued caps, `Submit`/`Wait`/`TryWait`/`Cancel` | done + tests |
| `internal/transfer` | Token-bucket bandwidth throttle (`ThrottledReader`/`Writer`) | done + tests |
| `internal/xdcc` | `LIST`/`SEND`/`INFO`/`BATCH`/`REMOVE` routing; descriptions in `LIST`/`INFO`; MD5/CRC32 with `nomd5sum`/`nocrc32` toggles | done + tests |
| `internal/dcc` | DCC wire helpers: `IP↔int`, `Listen`, `SEND`/`ACCEPT`/passive builders, file streamer (active/passive/resume), TCP socket tuning (`ConfigureTCP`) | done + tests |
| `internal/state` | JSON `statefile`: pack listing, runtime extras, descriptions, exclusions, resume offsets (atomic save/load + `old_statefile` backup) | done + tests |
| `internal/sxdcc` | SXDCC TLS 1.3 server: Ed25519 self-signed IP-SAN identity, SPKI pin, single-use tokens, control + data channels, active + passive data, key regeneration | done + tests |
| `internal/sdcc` | SDCC admin chat over the shared TLS listener (`ADMIN <token>` → `PASS`) | done + tests |
| `internal/admin` | admin command registry (`status`/`list`/`help`/`quit` + `addpack`/`removepack`/`chdesc`/`regenkey`) | done + tests |
| `internal/web` | HTTP packlist (list/search/JSON/MD5/SFV, `/dl/{name}`) + CIDR access control | done + tests |
| `internal/acl` | hostmask allow/deny (glob `*`/`?`) | done + tests |
| `internal/ratelimit` | flood protection (per-user XDCC command limiter + auto-ignore) | done + tests |
| `internal/token` | single-use tokens | done + tests |
| `internal/logging` | file logging, rotation, message/notice transcripts, `logstats` | done + tests |
| `internal/vpn` | hotio forwarded-port + public-IP discovery; live re-advertise + identity rotate | done + tests |
| `internal/irc` | multi-network client (one `netConn` per `network{}`), auto-reconnect, SASL, XDCC/SXDCC/SDCC routing, DCC active/passive/resume, uploads (ACL + min-space), announce (`announce_size`/`date`, `-noannounce`), channel-mode gating (`restrict*`/`need_*`), pack management, ACL/flood | done + tests |
| `internal/geoip`, `internal/fish` | placeholders (MaxMind mmdb / Blowfish) — DEFERred (external deps) | stub |

Commands: `cmd/dinoga` (entrypoint; `--print-config`, `--hash-password`, `--tui`),
`cmd/sxdccclient` (minimal SXDCC client for E2E), `cmd/scanbench` (scan benchmark).

## Transport model

SXDCC/SDCC ride a **single shared control port**. The listener demultiplexes each
connection by first byte (HTTP vs TLS) and, for TLS, by first line (`AUTH` control
/ `DATA` data / `ADMIN` admin) per `SXDCC.md` §13.3 and `SDCC.md`. The earlier
QUIC/ICE/TURN/STUN/bounce NAT-traversal transport was **removed**; NAT traversal
is handled at the deployment layer by the hotio/base WireGuard VPN (one forwarded
port), and `internal/vpn` re-advertises the discovered address (rotating the SXDCC
identity when the public IP changes).

## Build / deploy

- **Two-stage `Dockerfile`:** `golang:1.25-alpine` builds a static `CGO_ENABLED=0`
  `dinoga`; the runtime extends `ghcr.io/hotio/base:noblevpn` and copies in `root/`
  s6-overlay services (init-wireguard, PIA region picker, dinoga).
- **`docker-compose.yml`:** `cap_add NET_ADMIN`, `/dev/net/tun`, mounts
  `./config:/config` and `./files:/data`. No published ports in VPN mode — the
  provider's forwarded port is the single inbound path (hotio `VPN_PORT_REDIRECTS`).
- **Config:** config-file-first (`/config/dinoga.config`, `--config`) with
  `IROFFER_*` env overlay (`.env.example` documents both the bot directives and the
  hotio/VPN variables).

## Key results

- **1M-file scan** (stress): `packs.Scan` is linear in file count, not bytes —
  ~4 s on desktop (Ryzen 9 9950X), ~12 s on the remote `dinoga` (2 vCPU / 3.7 GB).
- **A/B vs the C bot** (remote `dinoga`, `#frog`): identical `DCC SEND` line and
  identical md5 — the Go bot interoperates with the standard PircBotX client
  exactly as the original does.
- **SXDCC:** active + passive TLS round-trips (bot always the TLS server, client
  verifies the SPKI pin); `SEND` gated on a transfer slot via the `queue.Manager`.

## Verification

    docker run --rm -v "$PWD":/src -w /src -e GOFLAGS=-buildvcs=false golang:1.25 \
      sh -c "go vet ./... && go build ./... && go test ./..."
    docker build -t dinoga:test .

## Out of scope / next steps

- GeoIP (MaxMind mmdb) and FISH (Blowfish) — DEFERred (external deps).
- Remaining parity: `auto_crc_check`/sidecar (`*.md5`/`*.crc32`/SFV) reading,
  `announce_separator`, `restrictpriv*`, `lowbdwth`/`punishslowusers`/`requeue_sends`,
  queue re-offer across restart, IPv6/`local_vhost`/raw commands, CI `go test` job.