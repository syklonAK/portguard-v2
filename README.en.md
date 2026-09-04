# 🛡️ PortGuard

**Port & Reverse-Proxy Manager — everything in a single binary, made for Ubuntu servers.**

> 🇮🇷 نسخه‌ی فارسی: [README.md](README.md)

PortGuard is a lightweight, single-binary web tool that runs on your Ubuntu server and does three things:

1. **Detect** — scans every listening port, tells you which process owns it, and flags ports that aren't managed yet.
2. **Manage** — routes domains/ports to backends through **Nginx** or **HAProxy** from a polished web UI.
3. **Monitor** — live health checks per backend plus CPU/RAM/disk dashboards.

## Features

- **Port discovery**: scans TCP/UDP listening sockets via `ss` (with a `/proc` fallback), showing process, PID and owner; auto-classifies services (web server, load balancer, ssh, xray, database, docker…) and marks **unmanaged** ports with a one-click "Map" action.
- **Mappings**: HTTP/HTTPS/TCP/UDP listeners with multiple weighted backends, backup targets, WebSocket support, HTTP/2, redirects, custom headers, and port-conflict detection before saving.
- **Dynamic path routing** for the **ws / httpupgrade / xhttp** transports: the `/<prefix>/<port>` pattern extracts the destination port from the request path itself (Xray-style) — one domain + one TLS port serves all of your inbounds, on both nginx and HAProxy.
- **Free-form config authoring**: every mapping can be built with the visual editor **or** a raw JSON editor; a live preview shows the final URLs as you type.
- **HAProxy runtime API**: live `show info`/`show stat`, ready/drain/maint server states without a reload; service start/stop/restart for nginx & haproxy.
- **Backups & restore**: timestamped config backups with diff and one-click validated restore.
- **Diagnostics**: TCP / DNS / TLS / HTTP / backend connectivity tests from the panel.
- **Templates**: ready-made presets (TCP LB, HTTP LB, HTTPS proxy, TLS passthrough, Xray path routing, UDP forward…).
- **Import/export**: JSON snapshots of all mappings.
- **Safe apply pipeline**: render → validate (`nginx -t` / `haproxy -c`) → timestamped backup → atomic write → zero-downtime reload → **automatic rollback** on failure. Invalid configs never reach the live services.
- **SSL certificates**: upload PEM pairs or generate self-signed (SAN-aware); `.crt/.key` for nginx and a combined `.pem` for HAProxy are produced automatically.
- **Monitoring**: periodic TCP+HTTP checks with latency and consecutive-fail counters, live updates over SSE, resource dashboard.
- **Security**: first-run setup wizard, JWT sessions (HS256), bcrypt password hashing, per-IP login rate limiting, and a full audit log.

## Install (Ubuntu 20.04/22.04/24.04)

One line:

```bash
sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/syklonAK/portguard-v2/main/deploy/remote-install.sh)"
# open http://<server-ip>:8080 — the first visit creates the admin account
```

Custom panel port:

```bash
sudo PORTGUARD_PORT=9000 bash -c "$(curl -fsSL https://raw.githubusercontent.com/syklonAK/portguard-v2/main/deploy/remote-install.sh)"
```

The installer clones the repo to `/opt/portguard`, sets up **nginx + haproxy + libnginx-mod-stream**, installs Go if missing, builds the binary (the dashboard is prebuilt and embedded — no Node needed) and registers the `portguard` systemd service.

From an existing checkout:

```bash
sudo bash /opt/portguard/deploy/install.sh
```

### Uninstall

```bash
# remove the panel (data and nginx/haproxy kept)
sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/syklonAK/portguard-v2/main/deploy/uninstall.sh)"

# full purge including data (admins, mappings, certs, backups)
sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/syklonAK/portguard-v2/main/deploy/uninstall.sh)" -- --purge-data --yes

# everything, including nginx & haproxy
sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/syklonAK/portguard-v2/main/deploy/uninstall.sh)" -- --purge-data --purge-packages --yes
```

From an existing checkout: `sudo bash /opt/portguard/deploy/uninstall.sh [--purge-data] [--purge-packages] [--yes]`

### Build from source

```bash
make web      # rebuild the React dashboard (Node 20+)
make linux    # build bin/portguard-linux-amd64 (dashboard embedded via go:embed)
make test     # Go unit tests
```

## Architecture

```
cmd/server/          entry point
internal/api/        chi router, JWT auth, SSE broker, handlers
internal/service/    apply orchestration: render → validate → backup → atomic → reload → rollback
internal/proxy/      Engine interface, nginx/haproxy renderers, live-config parser, HAProxy runtime client
internal/scanner/    listening-port discovery & service classification
internal/health/     periodic TCP/HTTP health checker
internal/store/      SQLite (modernc, CGO-free) schema + queries
internal/sysinfo/    gopsutil system stats
web/                 React 19 + Vite + Tailwind dashboard (embedded)
deploy/              install.sh + remote-install.sh + systemd unit
```

## Troubleshooting

| Problem | Fix |
|---|---|
| Panel not reachable | `systemctl status portguard`, `journalctl -u portguard -n 50` |
| Apply validation error | The message is the real `nginx -t`/`haproxy -c` output; fix the offending mapping |
| Apply bind error | The listen port is owned by another process (see the Ports page); the apply was rolled back automatically |
| Forgot admin password | `systemctl stop portguard && rm /var/lib/portguard/portguard.db && systemctl start portguard` (full reset — mappings are wiped too) |

## Security notes

- The panel runs as root (it writes `/etc/nginx`, `/etc/haproxy` and reloads services). Restrict port 8080 with a firewall to trusted IPs when possible.
- Certificate private keys are stored `0600` under `/var/lib/portguard/certs`.
- Every apply leaves a timestamped backup in `/var/lib/portguard/backups/`.

## License

MIT
