# iroffer-dinoex (Docker)

Run [iroffer-dinoex](https://iroffer.net/) (XDCC bot) with Docker.

**Every iroffer config directive is supported as an environment variable** —
`IROFFER_<DIRECTIVE_UPPERCASE>` (e.g. `slotsmax` → `IROFFER_SLOTSMAX`,
`adminpass` → `IROFFER_ADMINPASS`). Values are applied to `iroffer.config`
on **every container start**; an empty/unset variable keeps the file's value.

## Quick start (copy/paste)

From the project root:

```bash
# 1. Generate the encrypted admin password required by the config file.
#    `iroffer -c` prompts for your password twice and prints the ready-to-use
#    line (e.g. "adminpass $1$XY$...."). Enter it interactively:
docker run -it --rm ghcr.io/jenga201/iroffer-dinoex -c /tmp/config
#    Copy the adminpass hash into .env as IROFFER_ADMINPASS, or into
#    ${CONFIG_DIR}/config/iroffer.config as `adminpass <hash>`.

# 2. Set up and start
cp .env.example .env      # then set IROFFER_ADMINPASS (and IROFFER_BOT_NAME)
mkdir -p ./config ./files
docker compose pull       # CI-built image from ghcr.io (default)
docker compose up -d
docker compose logs -f --tail=100
```

That is enough to stand up an instance.

Prefer to build locally instead of pulling? Run `docker compose up -d --build`.

## CI/CD

GitHub Actions (`.github/workflows/docker-publish.yml`) builds the image and
publishes it to GitHub Container Registry:

- on push to `main` → `ghcr.io/jenga201/iroffer-dinoex:latest` (+ commit sha)
- on `v*` tags → semver tags (`:1.2.3`, `:1.2`)
- on pull requests → build only (no push)

If the package is private, log in once before pulling:
`echo $GHCR_PAT | docker login ghcr.io -u <github-user> --password-stdin`.

## Where files end up on your host

- Bot config: `${CONFIG_DIR}/config/iroffer.config`
- Logs: `${CONFIG_DIR}/logs/`
- Pack list file: `${CONFIG_DIR}/data/packlist.txt`
- Shared XDCC files: `${DATA_DIR}/`

## First things to edit

1. Open `.env` and set `IROFFER_BOT_NAME` (optional, but recommended).
2. Configure the IRC network either
   - fully in `.env` with `IROFFER_NETWORK_*` variables (see below), or
   - by editing `${CONFIG_DIR}/config/iroffer.config` directly.
3. Put files you want to share into `${DATA_DIR}/`.

## Environment variables

### Deployment variables

| Variable | Default | Purpose |
|---|---|---|
| `IMAGE_NAME` / `IMAGE_TAG` | `ghcr.io/jenga201/iroffer-dinoex` / `latest` | image to pull/build |
| `CONTAINER_NAME` | `iroffer` | container name |
| `IROFFER_USER_ID` / `IROFFER_GROUP_ID` | `1000` / `1000` | build args for the in-image user |
| `CONFIG_DIR` | `./config` | mounted to `/home/iroffer` |
| `DATA_DIR` | `./files` | mounted to `/data` (shared files) |
| `PORT_RANGE` | `30000-31000` | DCC passive port range; published by Docker and applied as `tcprangestart`/`tcprangelimit` |
| `HTTP_PORT` | (unset) | built-in webserver port; applied as `http_port` |

### Config-directive variables

All 220+ iroffer config directives are available as
`IROFFER_<DIRECTIVE_UPPERCASE>`. The complete documented list (grouped as in
upstream `sample.config`) is in [`.env.example`](.env.example).

Rules:

- **Empty/unset = keep** the value in `iroffer.config`.
- Applied on **every start** into a managed region at the top of the config:
  `# BEGIN IROFFER ENV OVERRIDES` … `# END IROFFER ENV OVERRIDES`.
  For an overridden directive the env value wins (the old active line outside
  the region is removed). Don't edit inside the managed markers — changes
  there are discarded on the next start.
- **Multi-value directives** (lists such as `filedir`, `http_vhost`,
  `autoadd_dir`) accept `;;`-separated values, one config line per value:
  `IROFFER_FILEDIR="/data;;/data2"`.
- **Bool directives** accept only `yes` or `no`; anything else aborts startup
  with an error.
- Superseded lines are commented out as `#removed-by-env:` (not deleted);
  removing the env var restores them on the next start.
- Global `IROFFER_*` variables apply **before** the first `network` block,
  i.e. to the first network (same as writing the directive at the top of
  the config file). Values set **inside** an existing `network { … }` block
  still win for that network — use `IROFFER_NETWORK_<n>_*` variables
  (below) to override per-network values.
- Legacy aliases keep working: `IROFFER_BOT_NAME` → `user_nick`,
  `PORT_RANGE` → `tcprangestart`/`tcprangelimit`, `HTTP_PORT` → `http_port`
  (the specific `IROFFER_*` variable wins if both are set).

Example `.env` snippet:

```bash
IROFFER_BOT_NAME=MyXDCCBot
IROFFER_SLOTSMAX=10
IROFFER_QUEUESIZE=20
IROFFER_OVERALLMAXSPEED=5000
IROFFER_FILEDIR=/data
IROFFER_ADMINPASS=...
```

### Network blocks via environment

Set `IROFFER_NETWORK_COUNT=N` and per-network variables to generate the
`network { … }` blocks on startup (replaces any existing blocks, which are
commented out as `#removed-by-env:` and can be restored by hand):

```bash
IROFFER_NETWORK_COUNT=1
IROFFER_NETWORK_1_NAME=rizon
IROFFER_NETWORK_1_SERVER=irc.rizon.net 6667        # ';;' for multiple servers
IROFFER_NETWORK_1_CHANNEL=#mychannel -noannounce    # ';;' for multiple channels
IROFFER_NETWORK_1_CONNECTIONMETHOD=ssl
IROFFER_NETWORK_1_USER_NICK=MyBot
IROFFER_NETWORK_1_NICKSERV_PASS=secret
```

Any network-scoped directive works (`user_nick`, `server`, `channel`,
`connectionmethod`, `nickserv_pass`, `restrictsend`, `need_level`,
`slow_privmsg`, …); `server`, `channel`, `connectionmethod`, `proxyinfo`,
`server_join_raw`, `server_connected_raw` and `channel_join_raw` accept
`;;`-separated multiples. Unknown `IROFFER_NETWORK_<n>_*` names produce a
warning. Replaced original blocks are kept as `#removed-by-env-net:`
comments and are restored automatically when `IROFFER_NETWORK_COUNT` is
unset again.

Known limitation: directives in a **braceless** `network foo` section (no
`{ … }`) are not protected from global overrides — use braces (the upstream
sample config style).

### Webserver port

If you set `HTTP_PORT`, also publish it in `docker-compose.yml` (see the
commented `ports:` entry there).

### Published ports must match the config

Docker publishes `PORT_RANGE` (and optionally `HTTP_PORT`) from `.env`. If
you instead set `IROFFER_TCPRANGESTART`/`IROFFER_TCPRANGELIMIT` or
`IROFFER_HTTP_PORT` directly, keep them consistent with the published port
mappings — the container's bindings come from compose, not from the bot.

### Security note on secrets

`adminpass`, `hadminpass`, `nickserv_pass` and similar secrets passed as
environment variables are visible via `docker inspect` and
`/proc/<pid>/environ` to anyone with access to the Docker daemon/host. On a
shared host, prefer setting them directly in `iroffer.config` (which stays
in `${CONFIG_DIR}` with the permissions you choose) instead of `.env`.

## Common commands

```bash
docker compose ps
docker compose logs -f
docker compose restart
docker compose down
```

## Notes

- On first start, the container creates `${CONFIG_DIR}/config` and
  `${CONFIG_DIR}/logs` if missing, and seeds `iroffer.config` from the
  image's customized sample.
- The default image config serves files from `/data`, which maps to
  `${DATA_DIR}`.
- `PORT_RANGE` must be `START-END` within 1-65535; `HTTP_PORT` within
  1-65535. Invalid values abort startup with an error.
- The directive tables in `entrypoint.sh` were generated from the config
  parser of the iroffer-dinoex snapshot pinned by `IROFFER_SHA256` in the
  Dockerfile. After bumping the snapshot, regenerate the tables (see the
  comment block in `entrypoint.sh`).
