# Dinoga

A **Go rewrite** of [iroffer-dinoex](https://iroffer.net/) — an XDCC file server /
IRC bot. It keeps the DCC/XDCC **wire protocol** and the `iroffer.config` directive
surface, but is a clean, **stdlib-only** Go module (no external dependencies).

## Status

Core XDCC (`LIST`/`SEND`/`INFO`/`BATCH`/`REMOVE`) plus active/passive DCC transfer
is implemented and A/B-tested against the original C bot (same `DCC SEND` line,
same md5). Secure **SXDCC** (XDCC over TLS 1.3) and **SDCC** (secure admin chat)
are implemented against the specs in `../dcc-protocol/`. A small set of parity
directives remains — see `docs/REWRITE-PLAN.md`.

## Protocols

The wire formats are spec'd in the sibling repo `/opt/code/IRC/dcc-protocol/`:

- `DCC.md` / `XDCC.md` — the classic cleartext protocol the rewrite preserves.
- `SXDCC.md` — XDCC over TLS 1.3 (SPKI pin, IP SAN, single shared control port).
- `SDCC.md` — secure admin chat over the same control port.

Classic `xdcc` and the TLS `sxdcc`/`sdcc` variants coexist; routing is by the
message prefix.

## Layout

```
cmd/dinoga          entrypoint (config, logging, TUI, flags)
cmd/sxdccclient     minimal SXDCC client for E2E testing
cmd/scanbench       packlist-index stress benchmark
internal/config     iroffer.config parser + env overlay + legacy migration
internal/irc        multi-network IRC client + bot wiring + DCC
internal/packs      packlist scan (lazy hash, natural sort, filters, dedup)
internal/xdcc       XDCC command vocabulary
internal/dcc        DCC wire helpers (SEND/ACCEPT, active/passive/resume)
internal/sxdcc      SXDCC TLS 1.3 server (control + data)
internal/sdcc       SDCC admin chat server
internal/queue      slots/queues/idle queue + per-user caps
internal/transfer   per-transfer / global bandwidth throttle
internal/state      JSON statefile (packs, descs, extras, resume)
internal/admin      admin command registry
internal/web        HTTP packlist + CIDR access control
internal/acl        hostmask allow/deny
internal/ratelimit  flood protection
internal/token      single-use tokens (SXDCC/SDCC)
internal/logging    file logging / rotation / stats
internal/vpn        hotio forwarded-port + public-IP discovery
internal/geoip      placeholder (MaxMind mmdb) — deferred
internal/fish       placeholder (FISH/Blowfish) — deferred
compat/original/    original C build, kept for A/B comparison
original-dinoex/    upstream C source (git submodule)
config/             runtime volume (dinoga.config, certs, wireguard state)
root/               hotio/base s6-overlay services + PIA region picker
docs/               REWRITE-PLAN.md, IMPLEMENTATION-SUMMARY.md
sample.config       drop-in config example (for running without Docker)
Dockerfile / docker-compose.yml / .env.example
```

## Configuration

The binary reads **one config file** — `/config/dinoga.config` by default
(`--config`). A dropped-in `iroffer.config` / `sample.config` is migrated
automatically on start (the old file is renamed `.bak`). `IROFFER_*` environment
variables then overlay the file (env wins) and are INFO-logged.

Flags:

- `--print-config` — print a template `dinoga.config`.
- `--hash-password <pass>` — print the SHA-256 admin-password hash.
- `--tui` — interactive admin console on stdin (no password).

The directive surface mirrors the C bot's `iroffer.config` (pruned to what's
implemented). See `--print-config`, `.env.example`, and `sample.config` (a
concise non-Docker example).

### Serve modes

- **SXDCC + XDCC**: `sxdcc_enable yes` + `xdcc_enable yes`, with `control_port`
  (SXDCC/SDCC/HTTP) and a disjoint `tcprangestart–tcprangelimit` (classic DCC),
  all ports reachable.
- **SXDCC only** (single forwarded port / VPN): `sxdcc_enable yes` +
  `xdcc_enable no`; classic `xdcc` is rejected and only the shared `control_port`
  needs to be reachable.

## Build & run

```bash
docker compose build     # two-stage: golang:1.25-alpine -> ghcr.io/hotio/base:noblevpn
docker compose up -d     # reads .env; mounts ./config:/config and ./files:/data
```

The image extends `ghcr.io/hotio/base:noblevpn`. In VPN mode there are **no
published ports**: hotio's `VPN_PORT_REDIRECTS` forwards the provider's forwarded
port to the control port (the single inbound path for HTTP packlist + SXDCC/SDCC
TLS). Without the VPN, publish the control port directly (see the commented block
in `docker-compose.yml`). `PUID`/`PGID` default to `1000`.

Native build alternative: `./build.sh` (uses the host `go` toolchain if present,
otherwise Docker) → `bin/dinoga` (static `linux/amd64`).

## Test

```bash
docker run --rm -v "$PWD":/src -w /src -e GOFLAGS=-buildvcs=false golang:1.25 \
  sh -c "go vet ./... && go build ./... && go test ./..."
```

The module is stdlib-only, so the test/build run is offline.

## Docs

- `docs/REWRITE-PLAN.md` — feature map, keep/drop decisions, implementation checklist.
- `docs/IMPLEMENTATION-SUMMARY.md` — status summary and key results.
- `/opt/code/IRC/dcc-protocol/` — the wire specifications.