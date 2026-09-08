#!/usr/bin/env bash
# =============================================================================
#  PortGuard self-updater — run from the panel or manually:
#    sudo bash /opt/portguard/deploy/update.sh
#
#  Flow: git fetch → fast-forward → rebuild binary (frontend embedded, no
#  Node needed) → swap binary → restart the systemd service.
#  On any failure the old binary is restored and the service restarted.
# =============================================================================
set -euo pipefail

APP_DIR="/opt/portguard"
BIN="${APP_DIR}/bin/portguard"
BACKUP="${BIN}.old.$$"
UNIT="portguard"

say() { echo -e "\033[1;35m[update]\033[0m $*"; }
die() { echo -e "\033[1;31m[update:ERROR]\033[0m $*" >&2; exit 1; }

[ "$(id -u)" = "0" ] || die "run as root (sudo)"
[ -d "${APP_DIR}/.git" ] || die "${APP_DIR} is not a git checkout — reinstall with remote-install.sh"
command -v go >/dev/null 2>&1 || {
  say "go toolchain not found — installing via multi-mirror installer…"
  GO_VERSION="${GO_VERSION:-1.24.5}"
  # shellcheck source=ensure_go.sh
  source "${APP_DIR}/deploy/ensure_go.sh"
  GOARCH="$(dpkg --print-architecture)"
  ensure_go || die "go toolchain could not be installed — see guidance above"
}

cd "${APP_DIR}"

say "fetching latest release…"
OLD="$(git rev-parse HEAD)"
git fetch --all --force >/dev/null 2>&1 || die "git fetch failed (network?)"
git reset --hard origin/main >/dev/null 2>&1 || die "git reset failed"
NEW="$(git rev-parse HEAD)"
if [ "${OLD}" = "${NEW}" ]; then
  say "already up to date (${NEW:0:8}) — rebuilding anyway (binary may be stale)"
else
  say "updated: ${OLD:0:8} -> ${NEW:0:8}"
fi

say "building (CGO_ENABLED=0, frontend embedded)…"
export CGO_ENABLED=0
# module proxy: probe candidates and pin the first that answers, because
# go does NOT fall back on HTTP errors (a 403 from proxy.golang.org aborts
# the whole build). Override freely with GOPROXY=... .
pick_goproxy() {
  for p in https://proxy.golang.org https://goproxy.cn https://goproxy.io; do
    code="$(curl -s -o /dev/null --connect-timeout 5 --max-time 10 \
      -w '%{http_code}' "$p/modernc.org/sqlite/@v/list" 2>/dev/null)"
    case "$code" in 2*) echo "$p,direct"; return ;; esac
  done
  echo "direct"
}
export GOPROXY="${GOPROXY:-$(pick_goproxy)}"
log "GOPROXY=${GOPROXY}"
go mod tidy >/dev/null 2>&1 || true
go build -trimpath -ldflags "-s -w" -o "${BIN}.new" ./cmd/server || die "build failed — keeping the running binary"

say "swapping binary…"
if [ -f "${BIN}" ]; then
  cp "${BIN}" "${BACKUP}"
fi
mv "${BIN}.new" "${BIN}"

if ! systemctl restart "${UNIT}"; then
  say "service failed to start — rolling back"
  [ -f "${BACKUP}" ] && mv "${BACKUP}" "${BIN}"
  systemctl restart "${UNIT}" || true
  die "rollback performed; check journalctl -u ${UNIT}"
fi
rm -f "${BACKUP}"
sleep 1
systemctl is-active --quiet "${UNIT}" && say "done — PortGuard is running the new build." || die "service not active after update"
