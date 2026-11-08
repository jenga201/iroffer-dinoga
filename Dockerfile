# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/dinoga ./cmd/dinoga

# ---- Stage 2: extend hotio/base (noblevpn amd64) ----
FROM ghcr.io/hotio/base:noblevpn
ARG IMAGE_STATS
ENV IMAGE_STATS=${IMAGE_STATS}

# Install the compiled bot into hotio's app directory.
COPY --from=build /out/dinoga "${APP_DIR}/dinoga"

# Fetch the PIA server list once at build time (best-effort, non-fatal). The
# region picker falls back to hotio's /config/wireguard/pia-regions.json if
# this file is absent, so the URL is not hammered at runtime.
RUN curl -fsSL --retry 3 --retry-delay 5 --max-time 30 "https://serverlist.piaservers.net/vpninfo/servers/v6" -o /tmp/pia-servers.json && head -1 /tmp/pia-servers.json > "${APP_DIR}/pia-servers.json" || true

# dinoga's s6 service + bundle membership.
COPY root/ /
RUN find /etc/s6-overlay/s6-rc.d -name "run*" -execdir chmod +x {} + && \
    chmod 755 "${APP_DIR}/dinoga"