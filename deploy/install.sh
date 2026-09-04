#!/usr/bin/env bash
# PortGuard installer for Ubuntu 20.04/22.04/24.04 (Debian should work too)
#
# One-line install (from a GitHub checkout / tarball):
#   sudo bash deploy/install.sh
#
# What it does:
#   1. installs nginx + haproxy + libnginx-mod-stream
#   2. installs Go only if a build is needed and no toolchain exists
#   3. builds the panel binary (frontend is embedded — no Node needed)
#   4. installs + starts the systemd service
set -euo pipefail

APP_DIR="/opt/portguard"
DATA_DIR="/var/lib/portguard"
PANEL_PORT="${PORTGUARD_PORT:-8080}"
GO_VERSION="${GO_VERSION:-1.24.5}"

log()  { echo -e "\033[1;34m[PortGuard]\033[0m $*"; }
fail() { echo -e "\033[1;31m[ERROR]\033[0m $*" >&2; exit 1; }

[ "$(id -u)" = "0" ] || fail "run as root (sudo bash install.sh)"

log "updating apt…"
apt-get update -y -qq

log "installing nginx, haproxy and helpers…"
# libnginx-mod-stream lets nginx forward tcp/udp; nginx/haproxy are stopped and
# disabled here — PortGuard owns their configs from the panel afterwards
# (their packaged defaults would collide with services on this server).
DEBIAN_FRONTEND=noninteractive apt-get install -y -qq nginx haproxy libnginx-mod-stream curl >/dev/null
systemctl stop nginx haproxy 2>/dev/null || true
systemctl disable nginx haproxy 2>/dev/null || true
rm -f /etc/nginx/sites-enabled/default 2>/dev/null || true

cd "$APP_DIR"

# ---- build (Go is installed only when missing) ----
if ! command -v go >/dev/null 2>&1; then
  log "installing Go ${GO_VERSION}…"
  ARCH="$(dpkg --print-architecture)"
  case "$ARCH" in amd64) GOARCH=amd64 ;; arm64) GOARCH=arm64 ;; *) fail "unsupported arch $ARCH" ;; esac
  curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${GOARCH}.tar.gz" -o /tmp/go.tgz
  rm -rf /usr/local/go
  tar -C /usr/local -xzf /tmp/go.tgz
  ln -sf /usr/local/go/bin/go /usr/local/bin/go
  ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt
  rm -f /tmp/go.tgz
else
  log "Go already installed: $(go version)"
fi

log "building PortGuard…"
export CGO_ENABLED=0
go mod tidy
go build -trimpath -ldflags "-s -w" -o bin/portguard ./cmd/server
log "binary ready: $APP_DIR/bin/portguard"

# ---- data dirs ----
mkdir -p "$DATA_DIR" "$DATA_DIR/certs" "$DATA_DIR/backups"
chmod 750 "$DATA_DIR/certs"

# ---- firewall: allow the panel port when ufw is active ----
if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -qi "Status: active"; then
  ufw allow "${PANEL_PORT}/tcp" >/dev/null 2>&1 && log "ufw: allowed port ${PANEL_PORT}/tcp"
fi

# ---- systemd ----
sed "s|-port 8080|-port ${PANEL_PORT}|; s|/var/lib/portguard/portguard.db|${DATA_DIR}/portguard.db|" \
  "$APP_DIR/deploy/portguard.service" > /etc/systemd/system/portguard.service
systemctl daemon-reload
systemctl enable --now portguard

sleep 1
if systemctl is-active --quiet portguard; then
  log "PortGuard is running on port ${PANEL_PORT}"
  log "open: http://<server-ip>:${PANEL_PORT}  (first visit creates the admin account)"
else
  journalctl -u portguard --no-pager -n 20
  fail "service failed to start"
fi

log "done. nginx & haproxy are installed; PortGuard manages their configs from the panel."
