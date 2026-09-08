#!/usr/bin/env bash
# =============================================================================
#  ensure_go.sh — resilient Go toolchain installer (sourced by install.sh)
#
#  Tries several mirrors/methods in order until one works:
#    1. existing toolchain on PATH or in /usr/local/go
#    2. dl.google.com            (official)
#    3. go.dev/dl                (official redirect)
#    4. mirrors.aliyun.com       (CN/IR friendly)
#    5. golang.google.cn         (CN mirror)
#    6. apnic/hi.tsinghua mirror (TUNA)
#    7. apt golang-go / golang   (distro package — older but buildable)
#    8. snap classic go
#
#  Exported by caller: GO_VERSION (e.g. 1.24.5), GOARCH (amd64|arm64)
#  Provides: ensure_go  → exits 0 with `go` on PATH, or fails with guidance.
# =============================================================================

# self-contained logging (also sourced standalone by update.sh)
log()  { echo -e "\033[1;34m[PortGuard]\033[0m $*"; }
fail() { echo -e "\033[1;31m[ERROR]\033[0m $*" >&2; }

GO_TARBALL_PREFIX=""   # built lazily inside ensure_go once GOARCH is known
GO_DL_LOG="/tmp/pg-go-dl.log"

# candidate URLs for the official tarball; first success wins
go_mirror_urls() {
  cat <<EOF
https://dl.google.com/go/${GO_TARBALL_PREFIX}.tar.gz
https://go.dev/dl/${GO_TARBALL_PREFIX}.tar.gz
EOF
  github_go_urls
  cat <<EOF
https://mirrors.aliyun.com/golang/${GO_TARBALL_PREFIX}.tar.gz
https://golang.google.cn/dl/${GO_TARBALL_PREFIX}.tar.gz
https://mirrors.tuna.tsinghua.edu.cn/golang/${GO_TARBALL_PREFIX}.tar.gz
https://mirrors.ustc.edu.cn/golang/${GO_TARBALL_PREFIX}.tar.gz
EOF
}

# resolve GitHub-hosted Go tarballs for this version. actions/go-versions
# pins the full run tag (e.g. 1.24.5-16210585985) and names its asset
# go-<ver>-linux-x64; golang/go release assets use the canonical name.
github_go_urls() {
  local ver  # GO_TARBALL_PREFIX = go1.24.5.linux-amd64
  ver="$(echo "${GO_TARBALL_PREFIX}" | sed -E 's/^go([0-9]+\.[0-9]+\.[0-9]+)\..*/\1/')"
  local gharch="${GOARCH}"; [ "$gharch" = "amd64" ] && gharch="x64"
  local tag
  tag="$(curl -fsSL --connect-timeout 8 --max-time 20 \
      "https://api.github.com/repos/actions/go-versions/releases?per_page=100" 2>/dev/null \
      | grep -o "\"tag_name\": \"${ver}[^\"]*\"" 2>/dev/null | head -1 \
      | sed -E 's/.*"(1\.[0-9]+\.[0-9]+[^"]*)"/\1/')" 
  if [ -n "$tag" ]; then
    echo "https://github.com/actions/go-versions/releases/download/${tag}/go-${ver}-linux-${gharch}.tar.gz"
  fi
  echo "https://github.com/golang/go/releases/download/go${ver}/go${ver}.linux-${GOARCH}.tar.gz"
}

# a working go binary we can exec?
go_works() {
  local g="$1"
  command -v "$g" >/dev/null 2>&1 && "$g" version >/dev/null 2>&1
}

# verify the downloaded tarball is really a gzip archive (error pages lie)
tarball_ok() {
  [ -s "$1" ] && { gzip -t "$1" 2>/dev/null; }
}

# probe a mirror's reachability cheaply before the big download (4s timeout)
mirror_reachable() {
  curl -sI --connect-timeout 4 --max-time 8 -o /dev/null "$1"
}

install_go_tarball() {
  local url="$1" tries=0
  log "trying Go download: $url"
  rm -f /tmp/go.tgz
  while [ $tries -lt 2 ]; do
    if curl -fSL --connect-timeout 12 --max-time 420 --retry 2 --retry-delay 2 \
         -o /tmp/go.tgz "$url" 2>>"$GO_DL_LOG" && tarball_ok /tmp/go.tgz; then
      rm -rf /usr/local/go
      if tar -C /usr/local -xzf /tmp/go.tgz 2>>"$GO_DL_LOG"; then
        ln -sf /usr/local/go/bin/go /usr/local/bin/go
        ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt
        rm -f /tmp/go.tgz
        go_works /usr/local/bin/go && return 0
      fi
    fi
    tries=$((tries+1))
    sleep 2
  done
  return 1
}

install_go_apt() {
  log "falling back to apt golang package…"
  apt-get update -y -qq || return 1
  DEBIAN_FRONTEND=noninteractive apt-get install -y -qq golang-go >/dev/null 2>&1 \
    || DEBIAN_FRONTEND=noninteractive apt-get install -y -qq golang >/dev/null 2>&1 \
    || return 1
  go_works go
}

install_go_snap() {
  command -v snap >/dev/null 2>&1 || return 1
  log "falling back to snap go (classic)…"
  snap install go --classic >/dev/null 2>&1 || return 1
  # snap puts go in /snap/bin; make sure it is on PATH for systemd shells too
  ln -sf "$(command -v go || echo /snap/bin/go)" /usr/local/bin/go 2>/dev/null
  go_works go
}

# ---- main entry ------------------------------------------------------------
ensure_go() {
  # 0) already usable?
  if go_works go; then
    log "Go already installed: $(go version)"
    return 0
  fi
  if go_works /usr/local/bin/go; then
    log "found Go at /usr/local/bin/go: $(/usr/local/bin/go version)"
    return 0
  fi

  local arch
  arch="$(dpkg --print-architecture 2>/dev/null || uname -m)"
  case "$arch" in
    amd64|x86_64) GOARCH=amd64 ;;
    arm64|aarch64) GOARCH=arm64 ;;
    *) fail "unsupported architecture: $arch" ;;
  esac

  log "installing Go ${GO_VERSION} (${GOARCH})…"
  GO_TARBALL_PREFIX="go${GO_VERSION}.linux-${GOARCH}"
  : > "$GO_DL_LOG"

  # 1..6) tarball mirrors — probe reachability first so dead mirrors cost
  # seconds, not minutes of a hanging download
  while read -r url; do
    [ -z "$url" ] && continue
    if mirror_reachable "$url" && install_go_tarball "$url"; then
      log "Go installed from $url: $(go version)"
      return 0
    fi
  done < <(go_mirror_urls)

  # 7) apt
  if install_go_apt; then
    log "Go installed from apt: $(go version)"
    log "note: distro Go may be older than ${GO_VERSION}; if the build fails on toolchain version, rerun this installer with GO_VERSION=$(go version | grep -o '[0-9]*\.[0-9]*' | head -1)"
    return 0
  fi

  # 8) snap
  if install_go_snap; then
    log "Go installed from snap: $(go version)"
    return 0
  fi

  cat >&2 <<'EOF'
[ERROR] could not install the Go toolchain by any method.
Tried: dl.google.com, go.dev/dl, mirrors.aliyun.com, golang.google.cn,
       TUNA, USTC mirrors, apt golang-go, snap.

Manual fixes:
  - If github raw works but google is blocked, download the tarball
    elsewhere and:  tar -C /usr/local -xzf go<ver>.linux-<arch>.tar.gz
    ln -sf /usr/local/go/bin/go /usr/local/bin/go
  - Or set a reachable mirror:  GO_MIRROR=https://<mirror>/golang bash install.sh
EOF
  return 1
}
