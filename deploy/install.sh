#!/usr/bin/env bash
# =============================================================================
#  PortGuard installer — Ubuntu 20.04/22.04/24.04 (Debian should work too)
#
#  One-line install (from a GitHub checkout / tarball):
#    sudo bash deploy/install.sh
#
#  Flow:
#    0. preflight: root check, arch detection, network sanity
#    1. deps: nginx + haproxy + libnginx-mod-stream (panel owns their configs)
#    2. Go toolchain via deploy/ensure_go.sh (multi-mirror, Iran/CN friendly)
#    3. build: per-proxy retry (go aborts on HTTP 403, never advances the
#       GOPROXY list itself — so we drive the retries)
#    4. systemd unit + start
#
#  Env overrides: PORTGUARD_PORT, GO_VERSION, GOPROXY, SKIP_APT=1
# =============================================================================
set -uo pipefail

APP_DIR="/opt/portguard"
DATA_DIR="/var/lib/portguard"
PANEL_PORT="${PORTGUARD_PORT:-8080}"
GO_VERSION="${GO_VERSION:-1.24.5}"

log()  { echo -e "\033[1;34m[PortGuard]\033[0m $*"; }
warn() { echo -e "\033[1;33m[PortGuard:warn]\033[0m $*"; }
fail() { echo -e "\033[1;31m[ERROR]\033[0m $*" >&2; exit 1; }

trap 'fail "installer aborted at line $LINENO"' ERR

# =============================================================================
# 0. preflight
# =============================================================================
[ "$(id -u)" = "0" ] || fail "run as root (sudo bash install.sh)"

ARCH="$(dpkg --print-architecture 2>/dev/null || uname -m)"
case "$ARCH" in
  amd64|x86_64)   GOARCH="amd64" ;;
  arm64|aarch64)  GOARCH="arm64" ;;
  *) fail "unsupported architecture: $ARCH" ;;
esac
log "preflight: arch=${ARCH} port=${PANEL_PORT} go=${GO_VERSION}"

cd "$APP_DIR" || fail "$APP_DIR not found — run deploy/remote-install.sh first"

# =============================================================================
# 1. system deps
# =============================================================================
if [ "${SKIP_APT:-0}" = "1" ]; then
  warn "SKIP_APT=1 — skipping package installation"
else
  log "updating apt…"
  apt-get update -y -qq

  log "installing nginx, haproxy and helpers…"
  # libnginx-mod-stream lets nginx forward tcp/udp; nginx/haproxy are stopped
  # and disabled here — PortGuard owns their configs from the panel afterwards
  # (their packaged defaults would collide with services on this server).
  DEBIAN_FRONTEND=noninteractive apt-get install -y -qq \
    nginx haproxy libnginx-mod-stream curl >/dev/null
  systemctl stop nginx haproxy 2>/dev/null || true
  systemctl disable nginx haproxy 2>/dev/null || true
  rm -f /etc/nginx/sites-enabled/default 2>/dev/null || true
fi

# =============================================================================
# 2. Go toolchain (multi-mirror: dl.google → go.dev → GitHub → aliyun →
#    golang.google.cn → TUNA → USTC → apt → snap)
# =============================================================================
# shellcheck source=ensure_go.sh
source "$(cd "$(dirname "$0")" && pwd)/ensure_go.sh"
ensure_go || fail "Go toolchain could not be installed — see guidance above"

# =============================================================================
# 3. build — per-proxy retry
#
# go only advances the GOPROXY list on 404/410; any other HTTP error (403 —
# how google CDNs treat some Iranian datacenter IPs at the .zip fetch while
# metadata endpoints still answer 200) aborts the whole build. So we do NOT
# trust probes or static chains: run the real build per candidate and keep
# the first that succeeds. A user-provided GOPROXY always wins as candidate 1.
# =============================================================================
export CGO_ENABLED=0

build_once() {  # $1 = GOPROXY value
  GOPROXY="$1" go mod tidy >/dev/null 2>&1 || true
  GOPROXY="$1" go build -trimpath -ldflags "-s -w" -o bin/portguard ./cmd/server
}

build_with_fallback() {
  local gp seen="" tried=0
  local candidates=(
    "${GOPROXY:-}"
    "https://proxy.golang.org,direct"
    "https://goproxy.cn,direct"
    "https://goproxy.io,direct"
    "https://goproxy.cn"
    "direct"
  )
  for gp in "${candidates[@]}"; do
    [ -n "$gp" ] || continue
    case ",$seen," in *",$gp,"*) continue ;; esac
    seen="$seen,$gp"; tried=$((tried+1))
    log "build attempt ${tried}: GOPROXY=${gp}"
    if build_once "$gp"; then
      export GOPROXY="$gp"
      log "build succeeded with GOPROXY=${gp}"
      return 0
    fi
    warn "build failed with GOPROXY=${gp} — trying next"
  done
  return 1
}

rm -f bin/portguard
log "building PortGuard…"
if ! build_with_fallback; then
  echo "---- last build errors (direct mode) ----" >&2
  GOPROXY=direct go build -trimpath -ldflags "-s -w" -o bin/portguard ./cmd/server 2>&1 | tail -30 >&2 || true
  fail "build failed with every module proxy — check this server's network, or set GOPROXY manually and rerun"
fi
log "binary ready: $APP_DIR/bin/portguard"

# =============================================================================
# 4. data dirs + firewall + systemd
# =============================================================================
mkdir -p "$DATA_DIR" "$DATA_DIR/certs" "$DATA_DIR/backups"
chmod 750 "$DATA_DIR/certs"

if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -qi "Status: active"; then
  ufw allow "${PANEL_PORT}/tcp" >/dev/null 2>&1 && log "ufw: allowed port ${PANEL_PORT}/tcp"
fi

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
