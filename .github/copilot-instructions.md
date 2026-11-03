# Copilot Instructions for Dinoga

## One-line summary

Dinoga is a Go rewrite of iroffer-dinoex — an XDCC file server over IRC. It keeps
the DCC/XDCC wire protocol and the same config surface, with a clean concurrency
model replacing the single-threaded C event loop.

## Primary technologies

- Go (stdlib only so far)
- Docker (multi-stage, static binary)

## Critical files

- `cmd/dinoga/main.go`, `internal/{config,irc,xdcc,dcc,packs}`
- `compat/original/` — original C build kept for A/B comparison
- `docs/REWRITE-PLAN.md` — feature map and migration phases

## Rules

- Follow golang-standards/project-layout (`cmd/` + `internal/`).
- Keep the DCC/XDCC wire protocol **exactly** compatible with iroffer-dinoex.
- Never commit secrets; `.env.example` is safe to read; never read `.env`.