#!/usr/bin/env bash
# =============================================================================
#  PortGuard — one-line remote installer
#
#  Install:
#    sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/syklonAK/portguard-v2/main/deploy/remote-install.sh)"
#
#  Custom panel port:
#    sudo PORTGUARD_PORT=9000 bash -c "$(curl -fsSL ...)"
#
#  This script clones the repo into /opt/portguard (or refreshes an existing
#  checkout) and runs deploy/install.sh. Everything else happens there.
# =============================================================================
set -euo pipefail

REPO="https://github.com/syklonAK/portguard-v2"
APP_DIR="/opt/portguard"

say() { echo -e "\033[1;35m[install]\033[0m $*"; }
die() { echo -e "\033[1;31m[install:ERROR]\033[0m $*" >&2; exit 1; }

[ "$(id -u)" = "0" ] || die "run as root:  sudo bash -c \"\$(curl -fsSL ${REPO}/main/deploy/remote-install.sh)\""
command -v git >/dev/null 2>&1 || {
  say "installing git…"
  apt-get update -y -qq >/dev/null 2>&1 || true
  DEBIAN_FRONTEND=noninteractive apt-get install -y -qq git >/dev/null
}

if [ -d "$APP_DIR/.git" ]; then
  say "updating existing checkout at $APP_DIR…"
  git -C "$APP_DIR" fetch --all --force >/dev/null 2>&1 || die "git fetch failed in $APP_DIR"
  git -C "$APP_DIR" reset --hard origin/main >/dev/null 2>&1 || die "git reset failed"
  git -C "$APP_DIR" clean -fdx >/dev/null 2>&1 || true
else
  say "cloning $REPO → $APP_DIR…"
  rm -rf "$APP_DIR"
  git clone --depth 1 "$REPO" "$APP_DIR" >/dev/null 2>&1 || die "git clone failed"
fi

say "running the installer…"
exec bash "$APP_DIR/deploy/install.sh"
