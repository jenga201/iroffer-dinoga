# Dinoga

A Go rewrite of [iroffer-dinoex](https://iroffer.net/) — an XDCC file server /
IRC bot. It keeps the DCC/XDCC wire protocol and the same config surface, but is
a clean Go codebase without the single-threaded stalls of the original.

**Status:** early milestone. Core XDCC `LIST`/`SEND` + active DCC transfer is
implemented and has passed an A/B test against the original C bot (same file,
same md5). See `docs/REWRITE-PLAN.md` for the full feature map and phases.

## Layout

    cmd/dinoga        entrypoint
    internal/         config, irc, xdcc, dcc, packs (implemented) + stubs for the rest
    compat/original/  the original C build, for A/B comparison
    original-dinoex/  upstream C source (git submodule)
    web/              Go templates for the HTTP interface (future)

## Build & run

    docker build -t dinoga .
    docker run --rm --env-file .env \
      -v "$PWD/files:/data" \
      -p 30000-30001:30000-30001 \
      dinoga

Configuration is read from the same `IROFFER_*` environment variables as the
original container (see `.env.example`). `IROFFER_USENATIP` sets the public IP
advertised for DCC; the published port range must match `PORT_RANGE`.