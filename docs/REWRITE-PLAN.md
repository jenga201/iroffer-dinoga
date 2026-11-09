# Dinoga — Go rewrite plan

## Decisions (agreed)

- **Language:** Go.
- **License:** GPL-2.0 (this is a derivative of iroffer-dinoex, not clean-room).
  Keep upstream copyright notices; distribute source.
- **Compatibility target:**
  - Config: mostly drop-in — keep the `iroffer.config` file format and the commonly
    used directives; prune the dead ones.
  - Protocol: DCC/XDCC wire protocol must interoperate with all IRC clients and
    other XDCC bots (including dinoex). This is the one part that must be exact.
  - Admin: freely redesigned.
- **Ruby:** dropped (only used by the HTTP `htdocs/*.rb` templates and an
  off-by-default `ruby_script` hook — confirmed in source).
- **Original C** kept in `original-dinoex/` (git submodule) for A/B testing.
- **SXDCC (new):** an additive, secure TLS 1.3 wrapper for XDCC/DCC, specified in
  `IRC/dcc-protocol/SXDCC.md`. Upstream C has no equivalent, so it is designed from
  the spec rather than from parity, and does not change the existing DCC/XDCC wire
  behavior.
- **SXDCC transport (REVISED):** the earlier NAT-traversal transport layer
  (pluggable `tcp`/`quic`, ICE, TURN, STUN, and the reverse-tunnel "bounce"
  baseline, formerly `IRC/dcc-protocol/SXDCC-TRANSPORT.md`) was **removed**. The
  SXDCC data channel now multiplexes onto the bot's **single shared control port**
  (`SXDCC.md` §13.3), demultiplexed on the connection's first line (`AUTH` / `DATA`
  / `ADMIN`). NAT traversal moved to the deployment layer: the hotio/base image's
  WireGuard VPN forwards one port; the bot re-advertises it at runtime
  (`internal/vpn`). The module is stdlib-only — no `quic-go`/`pion`/`go.uber.org`.

## Feature inventory (keep / drop)

Legend: KEEP = implement, KEEP* = implement but redesign, PRUNE = drop some
directives, DROP = remove, DEFER = decide later.

| # | Feature | What it does | Modules / directives | Decision |
|---|---|---|---|---|
| 1 | Multi-network IRC client | connect to many networks, SSL, IPv4/IPv6, reconnect, raw commands, bnc/proxy | `dinoex_irc.c`; `network`, `server`, `channel`, `connectionmethod`, `server_*_raw`, `local_vhost`, `reconnect_delay` | KEEP |
| 2 | XDCC pack serving | XDCC LIST/SEND/INFO/BATCH/REMOVE, pack list + descriptions | `iroffer_*`; `xdcclistfile`, `xdccxmlfile`, `xdcclist_by_privmsg` | KEEP (DROP `xdcclist_grouponly`) |
| 3 | DCC transfer engine | DCC SEND + RESUME, active + passive/reverse DCC, TCP tuning | `dinoex_transfer.c`, `iroffer_transfer.c`; `passive_dcc`, `tcprangestart/limit`, `tcp_buffer_size` | KEEP |
| 4 | DCC chat | in-client admin/status over DCC CHAT | `iroffer_dccchat.c`; `status_time_dcc_chat` | KEEP* |
| 5 | File hashing | CRC32 + MD5 for resume/verification | `crc32.c`, `plumb_md5.c`; `auto_crc_check`, `nocrc32`, `nomd5sum`, `verbose_crc32` | KEEP |
| 6 | Queue / slot / bandwidth | slots, queues, idle queue, per-user + overall speed limits | `dinoex_queue.c`; `slotsmax`, `queuesize`, `idlequeuesize`, `max_uploads`, `overallmaxspeed`, `transfermaxspeed`, `punishslowusers`, `requeue_sends` | KEEP |
| 7 | Pack management | add/remove/describe packs, MD5/SFV sidecars, watch dir for new files | `autoadd_dir`, `autoadd_time`, `adddir_*`, `filedir`, `noduplicatefiles` | KEEP (scan off the main loop) |
| 8 | Uploads | accept incoming DCC SEND to the bot | `dinoex_upload.c`, `iroffer_upload.c`; `uploaddir`, `uploadhost`, `uploadmaxsize`, `uploadminspace` | KEEP |
| 9 | Config system | parse `iroffer.config`, env override | `dinoex_config.c`; ~220 directives | KEEP*, PRUNE unneeded directives |
| 10 | State persistence | resume points, packlist, queues across restarts | `dinoex_statefile.c`, `iroffer_statefile.c`; `statefile`, `old_statefile` | KEEP |
| 11 | HTTP web interface | list/search/JSON/MD5/SFV/info pages, access control | `dinoex_http.c`; `http_*`, `tcp params` | KEEP* (native Go templates) |
| 12 | Admin console | remote administration over chat/TUI | `iroffer_admin.c`; `adminpass` | KEEP* (redesign commands; **telnet DROPped**) |
| 13 | User / anti-abuse | hostmask allow/deny, per-user limits, flood protection, ignores | `dinoex_user.c`, `dinoex_badip.c`; `downloadhost`, `xdcc_allow/deny`, `need_level/voice`, `restrict*`, `flood_protection_rate`, `autoignore_*` | KEEP |
| 14 | Announcements | announce packs/headlines/periodic messages | `announce_*`, `headline`, `periodicmsg`, `autoaddann` | KEEP |
| 15 | Logging / stats | log files, rotation, statistics | `logfile*`, `logrotate`, `logstats`, `logmessages/notices` | KEEP* (native) |
| 16 | GeoIP / MaxMind | country lookups (+ HTTP geoip check) | `dinoex_geoip.c`, `dinoex_maxminddb.c`; `http_geoip` | DEFER (needs mmdb dep) |
| 17 | Ruby scripting | `ruby_script` hook + `.rb` web templates | `dinoex_ruby.c`, `htdocs/*.rb`, `ruby-sample.rb` | DROP |
| 18 | FISH / Blowfish | encrypted transfers (`fish`/`blowfish`) | `blowfish.c`; `fish_only`, `privmsg_fish` | DEFER (needs Blowfish dep) |
| 19 | UPnP | let router open the DCC ports | `upnp.c`; `getipfromupnp`, `upnp_router` | DROP |
| 20 | libcurl | URL fetching | `dinoex_curl.c` | DROP (Go stdlib HTTP) |
| 21 | Admin job files | read a file of commands, run on schedule | `dinoex_jobs.c`; `admin_job_file`, `atfind` | PRUNE / DROP |
| 22 | Misc utils | natural sort, kqueue/select loop, status display | `strnatcmp.c`, `dinoex_kqueue.c`, `iroffer_display.c` | KEEP (natural sort) / N/A (event loop → Go netpoller) |
| 23 | **SXDCC (secure XDCC)** | XDCC control + DCC data over TLS 1.3; self-signed leaf + SPKI pin; IRC rendezvous only; no client-auth gate | new: `internal/sxdcc/`; `sxdcc_*` directives; cert/key | KEEP (new — not in upstream C; spec `IRC/dcc-protocol/SXDCC.md`) |
| 24 | Config template flag | print a template `iroffer.config` / `.env` to stdout | `cmd/dinoga` `--print-config` | ADD |
| 25 | Password hash flag | generate an admin-password SHA-256 hash | `cmd/dinoga` `--hash-password` | ADD |
| 26 | TUI mode | interactive terminal admin (no password, DCC-chat-equivalent commands) | `cmd/dinoga` `--tui` | ADD |
| 27 | ~~SXDCC transport (NAT traversal)~~ | ~~pluggable `tcp`/`quic` transport, ICE, TURN, STUN, reverse-tunnel bounce~~ | ~~`internal/bounce`, `cmd/bounce`, `sxdcc_bounce`, `sxdcc_tunnel*`, `sxdcc_stun*`, `sxdcc_turn*`~~ | REMOVED — data channel multiplexes on the shared control port (`SXDCC.md` §13.3); NAT traversal via hotio/base WireGuard VPN (`internal/vpn`) |

## Repo structure

### Current (repo root = Go module)

The vendored C at `src/` is **removed**; the upstream C lives only in the
`original-dinoex/` submodule (reference/A-B) with a runnable harness in
`compat/original/`.

```
iroffer-dinoga/
├── cmd/
│   ├── dinoga/main.go          # entrypoint (config, logging, TUI, flags)
│   ├── sxdccclient/main.go     # minimal SXDCC client (E2E testing)
│   └── scanbench/main.go       # packlist-index stress benchmark
├── internal/
│   ├── config/                 # iroffer.config parser + env overlay + migration
│   ├── irc/                    # multi-network IRC client + bot wiring + DCC
│   ├── packs/                  # scan, lazy hash, natural sort, filters, dedup
│   ├── xdcc/                   # XDCC command vocabulary
│   ├── dcc/                    # DCC wire helpers (SEND/ACCEPT, passive/resume)
│   ├── sxdcc/                  # SXDCC TLS 1.3 server (control + data)
│   ├── sdcc/                   # SDCC admin chat server
│   ├── queue/                  # slots/queues/idle queue
│   ├── transfer/               # bandwidth throttle
│   ├── state/                  # JSON statefile (packs, descs, extras, resume)
│   ├── admin/                  # admin command registry
│   ├── web/                    # HTTP packlist interface
│   ├── acl/                    # hostmask allow/deny
│   ├── ratelimit/              # flood protection
│   ├── token/                  # single-use tokens (SXDCC/SDCC)
│   ├── logging/                # file logging / rotation / stats
│   ├── vpn/                    # hotio forwarded-port + public-IP discovery
│   ├── geoip/                  # placeholder (MaxMind mmdb) — deferred
│   └── fish/                   # placeholder (FISH/Blowfish) — deferred
├── compat/original/            # original C build (Dockerfile/entrypoint) for A/B
├── original-dinoex/            # upstream C source (git submodule)
├── config/                     # runtime volume: dinoga.config, certs, wireguard
├── root/                       # hotio/base s6-overlay services + PIA region picker
├── docs/                       # REWRITE-PLAN.md, IMPLEMENTATION-SUMMARY.md
├── go.mod / go.sum             # stdlib-only (no external requires)
├── Dockerfile / docker-compose.yml / .env.example
├── build.sh                    # static linux/amd64 build (host go or Docker)
└── README.md
```

## A/B testing

- Original: build the C bot from `original-dinoex/` (via `compat/original/`).
- New: build the Go bot from the root.
- Compare behavior with `IRC/irc-test-client` (XDCC LIST/SEND + DCC receive)
  against both bots, same `iroffer.config`.
- SXDCC: validate the TLS control/data path against the PircBotX fork client
  (`IRC/irc-test-client/pircbotx`) once it implements the SXDCC client role.

## Migration phases (suggested)

1. Scaffold the Go module (empty `cmd/dinoga`, package layout) + CI/Docker.
2. Config parser that reads the same `iroffer.config` (validated directive subset).
3. IRC connection + channel join (parity with multi-network core).
4. Packlist + hashing + XDCC LIST/SEND (core server path).
5. DCC transfer (active, then passive), resume, queues/slots.
6. Admin (DCC chat + SDCC chat), web interface, logging.
7. Remaining parity (FISH, GeoIP, uploads, upload/download limits, announce).
8. SXDCC (secure XDCC/DCC over TLS 1.3): server cert + SPKI pin, IRC rendezvous,
   TLS control channel, per-pack TLS data channel — per `IRC/dcc-protocol/SXDCC.md`.

## Implementation checklist

`[x]` done · `[ ]` not done. Each feature is keyed to the inventory table above.

### 0. Infrastructure / scaffolding

- [x] Go module scaffold (`go.mod`, `cmd/dinoga/main.go`, `internal/` package layout)
- [x] Multi-stage Dockerfile (static binary) + `docker-compose.yml`
- [x] `.env.example` (same `IROFFER_*` surface), `.dockerignore`, `.gitignore`
- [x] `compat/original/` C harness + `original-dinoex/` submodule for A/B
- [x] `IRC/irc-test-client` additions: `DCC_PUBLIC_IP`, `DCC_PORTS`, `RESUME_POSITION`
- [ ] CI (`.github/workflows/docker-publish.yml`) updated to build/test the Go module

### 1. Multi-network IRC client — KEEP (partial)

- [x] TLS connect to a single server (`crypto/tls`)
- [x] NICK/USER registration, PING/PONG, JOIN (with key)
- [x] PRIVMSG / CTCP parsing
- [x] Auto-reconnect + `reconnect_delay`
- [x] Multiple `network { … }` blocks (one connection goroutine per network)
- [x] SASL (IRCv3 PLAIN) authentication per network (`sasl_username` / `sasl_password`)
- [ ] IPv6, `local_vhost`, raw commands (`server_*_raw`, `channel_join_raw`)
- [ ] bnc/proxy/custom `connectionmethod` beyond direct/ssl

### 2. XDCC pack serving — KEEP (partial)

- [x] `XDCC LIST`
- [x] `XDCC SEND #N` (active / passive / resume)
- [x] `XDCC INFO #N`
- [x] `XDCC BATCH`
- [x] `XDCC REMOVE` / `CANCEL`
- [x] pack descriptions (no release-group grouping; `xdcclist_grouponly` is DROP) — shown inline in `LIST` and `INFO`
- [x] `xdcclistfile` / `xdccxmlfile` export (plain-text + XML, written after each scan/persist)
- [x] `xdcc_enable` toggle — `no` disables classic XDCC/DCC for single-port/VPN (SXDCC-only) deployments

### 3. DCC transfer engine — KEEP ✅

- [x] Active DCC SEND (bot listens, streams file)
- [x] Passive/reverse DCC SEND (bot connects out)
- [x] DCC RESUME / DCC ACCEPT + seek
- [x] IP `<->` 32-bit-int conversion (`35605696 <-> 2.31.76.192`)
- [x] TCP tuning (`tcp_buffer_size`, `tcp_nodelay`) — applied to DCC send/chat/upload sockets

### 4. DCC chat — KEEP* (redesign)

- [x] DCC CHAT connect + send/receive (basic `status`/`list`/`quit`)
- [x] status screen (`status_time_dcc_chat`) — multi-line slots/queue/packs status + periodic refresh

### 5. File hashing — KEEP ✅

- [x] CRC32 (IEEE)
- [x] MD5
- [x] `nomd5sum` / `nocrc32` toggles (suppress MD5/CRC32 in `xdcc info`)
- [ ] Wire `auto_crc_check` / `verbose_crc32` toggles
- [ ] `.crc32` / `.md5` sidecar reading

### 6. Queue / slot / bandwidth — KEEP

- [x] `slotsmax`, `queuesize`, `idlequeuesize`
- [x] idle queue + promote to main queue
- [x] per-user transfer/queued/idle-queued caps
- [x] `transfermaxspeed` (per-transfer token-bucket throttle in `internal/transfer`)
- [x] `overallmaxspeed` (shared global throttle)
- [ ] `lowbdwth`, `punishslowusers`, `requeue_sends`

### 7. Pack management — KEEP (partial)

- [x] Scan `filedir` / `autoadd_dir`
- [x] `autoadd_time` periodic rescan (off the IRC loop)
- [x] Natural sort (`strnatcmp` style) — `packs.WithNaturalSort()` option, wired to the `nat_sort` config directive
- [x] `addpack` / `removepack` / `chdesc` (with #12; descriptions persisted and shown in `xdcc info`)
- [x] `noduplicatefiles` / `no_duplicate_filenames`
- [x] `adddir_exclude` / `adddir_match` / `adddir_min_size`
- [ ] MD5/SFV sidecars + groups

### 8. Uploads — KEEP

- [x] Accept incoming `DCC SEND` to the bot (ACK receive → `uploaddir`)
- [x] `uploadmaxsize`
- [x] `uploadhost` allow-list + `uploadminspace` (free-disk check)

### 9. Config system — KEEP* / PRUNE (partial)

- [x] Real `iroffer.config` file parser (config-file-first), plus in-binary `IROFFER_*` env overlay (env wins; no container-side translation needed)
- [x] Multi-`network { … }` blocks + formerly env-only directives (`usenatip`, `tcprangestart/limit`, `passive_dcc`, `autoadd_*`, `sxdcc_*`, `headline`, `periodicmsg*`, `uploaddir`, http, `tcp_*`, `restrict*`, `need_*`, `announce_*`, `status_time_dcc_chat`, `old_statefile` …)
- [ ] Prune unneeded directives

### 10. State persistence — KEEP

- [x] Save/load packlist (JSON `statefile` via `internal/state`)
- [x] resume-point persistence (per-pack offsets) + `old_statefile` rolling backup
- [ ] queue re-offer across restart (pending queued transfers are not re-sent)

### 11. HTTP web interface — KEEP* (native templates)

- [x] Go `net/http` server
- [x] list / search / JSON / MD5 / SFV endpoints (Go templates; `/dl/{name}` added)
- [x] access control (`http_allow` / `http_deny`)
- [x] wired into the bot (`http_port`)

### 12. Admin console — KEEP* (redesign; telnet DROPped)

- [x] Admin command set (redesigned: `status`/`list`/`quit`/`help` + registry, plus `addpack`/`removepack`/`chdesc`)
- [x] Frontends: SDCC chat, classic DCC chat (basic), and `--tui` (no password)
- [x] Telnet listener removed; `telnet_port`/`adminhost` DROPped; `adminpass` remains for SDCC chat auth

### 13. User / anti-abuse — KEEP

- [x] hostmask allow/deny (`downloadhost`, `xdcc_allow`/`xdcc_deny`) via `internal/acl`
- [x] per-user limits (`maxtransfersperperson`, `maxqueueitemsperperson`, `maxidlequeuedperperson`) via `internal/queue`
- [x] flood protection + auto-ignore (`flood_protection_rate` / `autoignore_time`) via `internal/ratelimit`
- [x] `need_level` / `need_voice` (channel-mode gating via `internal/irc` NAMES/MODE tracking)
- [x] `restrictlist` / `restrictsend` (channel-membership gating)
- [ ] `restrictpriv*` (restrictprivlist/restrictprivlistmsg/restrictprivlistmain/full)

### 14. Announcements — KEEP

- [x] `headline` (announce on join) + `periodicmsg`
- [x] `autoaddann` (announce newly-scanned packs to every network channel)
- [x] `announce_size` / `announce_date` formatting, per-channel `-noannounce`
- [ ] `announce_separator`

### 15. Logging / stats — KEEP*

- [x] Structured stdout logging
- [x] log files (`logfile`) via `cmd/dinoga/main.go`
- [x] rotate + levels + expiry (`logrotate`, `logmessages`/`lognotices`, `expire_logfiles`)
- [x] `logstats` (periodic "Stat:" line every `status_time_dcc_chat` default of 120s)

### 16. GeoIP / MaxMind — DEFER

Deferred: requires an external mmdb dependency. Revisit only if requested.

- [ ] MaxMind mmdb lookup
- [ ] `http_geoip` integration

### 17–21. Dropped / no-op

- [x] 17 Ruby scripting — DROP
- [x] 19 UPnP — DROP
- [x] 20 libcurl — DROP
- [x] 21 Admin job files — PRUNE/DROP

### 18. FISH / Blowfish — DEFER

Deferred: Blowfish is not in the Go stdlib. Revisit only if requested.

- [ ] Blowfish/FISH encryption + `fish_only`, `fish_exclude_nick`, `privmsg_fish`

### 22. Misc utils

- [x] Event loop → Go netpoller (replaces kqueue/select)
- [x] Natural sort (`strnatcmp` semantics) shared with #7 (`packs.Less`, `nat_sort`)

### 23. SXDCC — KEEP (new feature)

Spec: `IRC/dcc-protocol/SXDCC.md`.

- [x] Read the spec (§6 crypto, §7 trust/TOFU, §8 rendezvous, §9 control, §10 data)
- [x] `sxdcc_enable` flag; when set, auto-generate an Ed25519 self-signed leaf (IP SAN) on start
  and persist the key (survives restart)
- [x] Admin key-regen (`regenkey` admin command → `sxdcc.Server.RegenerateKey`)
- [x] SPKI pin: compute `sha256:<base64url>`; constant-time compare + publish over IRC
- [x] IRC trigger `/msg <bot> sxdcc <verb> [args]` → bot replies `SXDCC CONNECT <ip> <port> <token> <pin>`
  (single-use short-TTL token)
- [x] Reuse DCC `tcprange` + `usenatip` for SXDCC listeners/IP (drop-in config)
- [x] TLS 1.3 listeners (control + data): MinVersion 1.3, Ed25519, AES-256-GCM / ChaCha20-Poly1305
- [x] Control protocol: `AUTH <token>`; LIST/SEND/BATCH/INFO/OWNER/CANCEL/QUIT; status lines
  (`OK`/`ERR`/`SEND`/`DONE`/`BYE`)
- [x] Keep filenames off IRC: pack list + filename returned over TLS only; `SEND` line carries filename + size
- [x] Data channel: `DATA <datatoken>`; raw stream over TLS, no ACK; byte-count **and `sha256:<digest>`** in `DONE`
- [x] Passive/reverse data channel (`port 0` + `ACCEPT`; bot stays TLS server, client is TLS client)
- [x] `CANCEL [<pack>]` — cancels queued/in-flight transfers (non-blocking SEND so a queued session stays responsive)
- [x] `OWNER` — answered from the `owner_nick` directive
- [x] `datatoken` single-use enforced in both active and passive paths
- [x] Reuse `packs` for LIST/SEND via the `sxdcc.Host` interface; queue/slot gating via `SlotGate`
- [x] Config + env: `sxdcc_enable`, cert/key location, pin publish (`IROFFER_SXDCC_*` in `config.go`)
- [x] SXDCC client via the PircBotX fork (`org.pircbotx.sxdcc`): pin/trust/messages/TOFU/client + tests (Gradle green)

### 24. Config template flag — ADD

- [x] `--print-config` prints a template `iroffer.config` to stdout
- [x] `sample.config` — concise drop-in config for non-Docker users (both serve modes)

### 25. Password hash flag — ADD

- [x] `--hash-password <pass>` prints the SHA-256 hash (matches admin password hashing)

### 26. TUI mode — ADD

- [x] `--tui` runs an interactive admin command loop on stdin (no password)

### 27. SXDCC transport (NAT traversal) — REMOVED

This feature was **removed**. The pluggable `tcp`/`quic` transports, ICE/STUN/TURN
candidate exchange, and the reverse-tunnel "bounce" baseline (formerly spec'd in
`IRC/dcc-protocol/SXDCC-TRANSPORT.md`, since deleted) are no longer in the codebase,
and the external deps they required (`quic-go`, `pion/turn`) were dropped — the
module is stdlib-only.

The replacement model is:

- **Data channel:** multiplexed onto the single shared control port per
  `SXDCC.md` §13.3, demultiplexed by first line (`AUTH` control / `DATA` data /
  `ADMIN` admin). The control listener already implements this (`serveSecureDCC`).
- **NAT traversal:** handled at the deployment layer by the hotio/base image's
  WireGuard VPN (one forwarded port), with the bot re-advertising the discovered
  address at runtime via `internal/vpn` (and rotating the SXDCC identity when the
  public IP changes).