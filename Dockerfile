# syntax=docker/dockerfile:1

FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/dinoga ./cmd/dinoga

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S dinoga \
    && adduser -S -G dinoga dinoga
WORKDIR /home/dinoga
COPY --from=build /out/dinoga /usr/local/bin/dinoga
USER dinoga
ENTRYPOINT ["dinoga"]