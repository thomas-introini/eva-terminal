FROM golang:1.27.1-alpine3.24 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/woossh ./cmd/woossh
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/woossh ./cmd/woossh

FROM alpine:3.24.2
RUN apk add --no-cache ca-certificates netcat-openbsd \
    && addgroup -g 10001 eva-terminal \
    && adduser -D -H -u 10001 -G eva-terminal eva-terminal \
    && mkdir -p /data/state /etc/eva-terminal \
    && chown -R 10001:10001 /data \
    && chmod 0700 /data /data/state \
    && touch /etc/eva-terminal/allowlist_authorized_keys \
    && chmod 0644 /etc/eva-terminal/allowlist_authorized_keys
COPY --from=build /out/woossh /usr/local/bin/woossh
ENV SSH_ADDR=0.0.0.0:23234 \
    STATE_DIR=/data/state \
    SSH_HOSTKEY_PATH=/data/ssh_host_ed25519_key \
    SSH_ALLOWLIST_PATH=/etc/eva-terminal/allowlist_authorized_keys \
    SSH_AUTH_MODE=allowlist \
    CHECKOUT_ENABLED=false \
    UMAMI_ENABLED=false
WORKDIR /data
USER 10001:10001
EXPOSE 23234/tcp
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD nc -z -w 3 127.0.0.1 "${SSH_ADDR##*:}"
ENTRYPOINT ["/usr/local/bin/woossh"]
