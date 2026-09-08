#!/usr/bin/env bash
# =============================================================================
#  PortGuard self-updater — run from the panel or manually:
#    sudo bash /opt/portguard/deploy/update.sh
#
#  Flow: git fetch → fast-forward → build (per-proxy retry, see install.sh)
#  → swap binary → restart the systemd service.
#  On any failure the old binary is restored and the service restarted.
# =============================================================================
set -uo pipefail

APP_DIR="/opt/portguard"
BIN="${APP_DIR}/bin/portguard"
BACKUP="${BIN}.old.$$"
UNIT="portguard"
GO_VERSION="${GO_VERSION:-1.24.5}"

say() { echo -e "\033[1;35m[update]\033[0m $*"; }
warn() { echo -e "\033[1;33m[update:warn]\033[0m $*"; }
die() { echo -e "\033[1;31m[update:ERROR]\033[0m $*" >&2; exit 1; }

[ "$(id -u)" = "0" ] || die "run as root (sudo)"
[ -d "${APP_DIR}/.git" ] || die "${APP_DIR} is not a git checkout — reinstall with remote-install.sh"

command -v go >/dev/null 2>&1 || {
  say "go toolchain not found — installing via multi-mirror installer…"
  # shellcheck source=ensure_go.sh
  source "${APP_DIR}/deploy/ensure_go.sh"
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

# =============================================================================
# build — per-proxy retry (go aborts on HTTP 403 and never advances the
# GOPROXY list itself, so the updater drives the retries; first success wins
# and is exported for future go commands on this machine)
# =============================================================================
export CGO_ENABLED=0

build_once() {  # $1 = GOPROXY value
  GOPROXY="$1" go mod tidy >/dev/null 2>&1 || true
  GOPROXY="$1" go build -trimpath -ldflags "-s -w" -o "${BIN}.new" ./cmd/server
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
    say "build attempt ${tried}: GOPROXY=${gp}"
    if build_once "$gp"; then
      export GOPROXY="$gp"
      say "build succeeded with GOPROXY=${gp}"
      return 0
    fi
    warn "build failed with GOPROXY=${gp} — trying next"
  done
  return 1
}

say "building (CGO_ENABLED=0, frontend embedded)…"
if ! build_with_fallback; then
  echo "---- last build errors (direct mode) ----" >&2
  GOPROXY=direct go build -trimpath -ldflags "-s -w" -o "${BIN}.new" ./cmd/server 2>&1 | tail -30 >&2 || true
  die "build failed with every module proxy — keeping the running binary"
fi

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
