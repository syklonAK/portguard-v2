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
# module proxy probe: go only advances the GOPROXY list on 404/410 — a 403
# (export-blocked or rate-limited proxy, e.g. google CDNs vs Iranian IPs)
# aborts the whole build. The probe pre-selects a candidate by fetching the
# exact resource type a build needs (a module .zip, not just metadata); the
# build itself then retries every candidate via build_with_fallback so a
# lying probe can never kill the install. Override freely with GOPROXY=...
pick_goproxy() {
  for p in https://proxy.golang.org https://goproxy.cn https://goproxy.io; do
    code="$(curl -s -o /dev/null -r 0-1023 --connect-timeout 5 --max-time 15 \
      -w '%{http_code}' "$p/modernc.org/sqlite/@v/v1.38.0.zip" 2>/dev/null)"
    case "$code" in 200|206) echo "$p,direct"; return ;; esac
  done
  echo "direct"
}

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
# source the resilient multi-mirror installer (dl.google → go.dev → aliyun →
# golang.google.cn → TUNA → USTC → apt → snap) so hosts behind filtering
# (Iran/CN) still get a toolchain
source "$(dirname "$0")/ensure_go.sh"
GOARCH="$(dpkg --print-architecture)"
ensure_go || fail "Go toolchain could not be installed — see guidance above"

log "building PortGuard…"
export CGO_ENABLED=0

# try a REAL build per candidate proxy — first success wins and stays
# exported for future go commands on this machine
build_with_fallback() {
  local gp seen=""
  local candidates=("${GOPROXY:-$(pick_goproxy)}" "https://goproxy.cn,direct" "https://proxy.golang.org,direct" "https://goproxy.io,direct" "direct")
  for gp in "${candidates[@]}"; do
    [ -n "$gp" ] || continue
    case ",$seen," in *",$gp,"*) continue ;; esac
    seen="$seen,$gp"
    log "building with GOPROXY=${gp}…"
    GOPROXY="$gp" go mod tidy >/dev/null 2>&1 || true
    if GOPROXY="$gp" go build -trimpath -ldflags "-s -w" -o bin/portguard ./cmd/server; then
      export GOPROXY="$gp"
      log "module proxy OK: ${gp}"
      return 0
    fi
  done
  return 1
}
build_with_fallback || fail "build failed with every module proxy — check this server's network (or set GOPROXY manually)"
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
