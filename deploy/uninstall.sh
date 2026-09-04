#!/usr/bin/env bash
# =============================================================================
#  PortGuard uninstaller (self-contained — safe to run via curl)
#
#  Default (keeps data & packages):
#    sudo bash /opt/portguard/deploy/uninstall.sh
#    sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/syklonAK/portguard-v2/main/deploy/uninstall.sh)"
#
#  Flags:
#    --purge-data      also remove /var/lib/portguard  (admins, mappings, certs, backups)
#    --purge-packages  also remove nginx + haproxy + libnginx-mod-stream (purges their /etc configs)
#    --yes             no confirmation prompt
# =============================================================================
set -uo pipefail

APP_DIR="/opt/portguard"
DATA_DIR="/var/lib/portguard"
UNIT="/etc/systemd/system/portguard.service"

say() { echo -e "\033[1;35m[uninstall]\033[0m $*"; }
die() { echo -e "\033[1;31m[uninstall:ERROR]\033[0m $*" >&2; exit 1; }

PURGE_DATA=0
PURGE_PACKAGES=0
ASSUME_YES=0
for arg in "$@"; do
  case "$arg" in
    --purge-data)     PURGE_DATA=1 ;;
    --purge-packages) PURGE_PACKAGES=1 ;;
    --yes|-y)         ASSUME_YES=1 ;;
    *) die "unknown option: $arg" ;;
  esac
done

[ "$(id -u)" = "0" ] || die "run as root (sudo)"

# ---- confirm (only when interactive and not --yes) ----
if [ "$ASSUME_YES" = "0" ] && [ -t 0 ]; then
  echo "This will stop & remove the PortGuard service and delete ${APP_DIR}."
  [ "$PURGE_DATA" = "1" ] && echo "  --purge-data:     ${DATA_DIR} (admins, mappings, certs, backups) WILL BE DELETED"
  [ "$PURGE_PACKAGES" = "1" ] && echo "  --purge-packages: nginx/haproxy will be REMOVED (their /etc configs purged)"
  read -rp "Continue? [y/N] " ans
  case "${ans,,}" in y|yes) ;; *) die "aborted" ;; esac
fi

# ---- 1) service ----
say "stopping & removing the portguard service…"
systemctl stop portguard 2>/dev/null || true
systemctl disable portguard 2>/dev/null || true
rm -f "$UNIT"
systemctl daemon-reload
systemctl reset-failed 2>/dev/null || true

# ---- 2) source + binary ----
say "removing ${APP_DIR} (source + binary)…"
rm -rf "$APP_DIR"

# ---- 3) data ----
if [ "$PURGE_DATA" = "1" ]; then
  say "purging data at ${DATA_DIR}…"
  rm -rf "$DATA_DIR"
else
  say "keeping ${DATA_DIR} (pass --purge-data to delete admins, mappings, certs & backups)"
fi

# ---- 4) packages ----
if [ "$PURGE_PACKAGES" = "1" ]; then
  say "removing nginx, haproxy, libnginx-mod-stream…"
  DEBIAN_FRONTEND=noninteractive apt-get purge -y -qq nginx haproxy libnginx-mod-stream >/dev/null 2>&1 || true
  DEBIAN_FRONTEND=noninteractive apt-get autoremove -y -qq >/dev/null 2>&1 || true
  say "packages removed (their /etc configs were purged too)"
else
  say "keeping nginx & haproxy installed (pass --purge-packages to remove them)"
fi

say "done. PortGuard is fully removed from this server."
