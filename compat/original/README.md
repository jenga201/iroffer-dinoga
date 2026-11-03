# Original C bot (A/B reference)

The upstream iroffer-dinoex C source lives in the `original-dinoex/` git
submodule. This directory holds the container harness that builds and runs that
original bot, kept so it can be compared against the Go rewrite at the repo root.

## Build (from the repo root)

    docker build -f compat/original/Dockerfile -t iroffer-dinoga:latest .

The build context is the repo root, so the Dockerfile copies `original-dinoex/`
and `compat/original/entrypoint.sh`.

## Run

    docker run -d -t --name dinoga --restart unless-stopped \
      -v ~/dinoga/config:/home/iroffer \
      -v ~/dinoga/data:/data \
      -p 30000-30001:30000-30001 \
      --env-file .env \
      iroffer-dinoga:latest

Notes:

- The image needs a pseudo-TTY (`-t`); iroffer runs in foreground mode.
- Keep the published port range tiny (1-2 ports): each published port spawns a
  docker-proxy process, and a wide range OOMs small hosts.
- See `../../.env.example` for the supported variables and
  `../../docs/REWRITE-PLAN.md` for the overall plan.