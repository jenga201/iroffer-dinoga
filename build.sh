#!/bin/sh
# Builds a static native Linux binary of dinoga.
#
# Uses the host "go" toolchain when available, otherwise falls back to Docker
# (golang:1.23-alpine). Output goes to bin/ by default; override with OUT, and
# cross-compile with GOOS/GOARCH (defaults: linux/amd64).
set -eu
cd "$(dirname "$0")"

OUT="${OUT:-bin/dinoga}"
GOOS="${GOOS:-linux}"
GOARCH="${GOARCH:-amd64}"
mkdir -p "$(dirname "$OUT")"

if command -v go >/dev/null 2>&1; then
	CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
		go build -trimpath -ldflags="-s -w" -o "$OUT" ./cmd/dinoga
else
	docker run --rm -v "$PWD":/src -w /src golang:1.23-alpine \
		sh -c "CGO_ENABLED=0 GOOS=$GOOS GOARCH=$GOARCH go build -trimpath -ldflags='-s -w' -o /src/$OUT ./cmd/dinoga"
fi

echo "built $OUT ($GOOS/$GOARCH)"