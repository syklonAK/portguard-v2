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
- **Anti-DPI decoy site**: serve a realistic built-in (or your own custom) website on unmatched paths instead of a suspicious 404 — proxies hide under a normal-looking site.
- **Hedioum Pool Tunnel management**: the Iran side of the tunnel topology (user → Iran → xray dokodemo bridge → SOCKS5 hub → foreign egress → node) managed from the panel — relays (raw passthrough or local TLS termination), bridge config generation with `xray run -test` validation, backups and automatic rollback.

### Multi-server management (master ↔ nodes)

- **One-liner node deploy**: the master serves its own binary plus the installer script, so a fresh Ubuntu box becomes a fully managed node in seconds — no panel, no Go, no GitHub on the node.
- **Two connection modes (v2.12)**:
  - **Direct** — the master dials the node at `host:port` (the node must be reachable from the master).
  - **Reverse** — the node dials the master itself and keeps a **persistent WebSocket tunnel** open; every management call travels through it. The node needs **no inbound port at all** and works behind NAT/firewalls. Tunnel up/down state shows live on the Servers page.
- **Tunnel roles**: generic / Iran hub / foreign egress — the tunnel topology stays obvious from the server list.
- **Full remote management**: mappings (Manage), certificates, tools (xray, hedioum, certbot…), the safe apply pipeline and the live overview — all driven from the master.
- **Version match by construction**: nodes download the master's own binary.
- **Live connection log**: every established inbound connection (source IP, target port, serving process, duration) in real time, classified as managed / unmanaged, plus top-talker aggregation.

### Operations infrastructure

- **Config versioning (v2.7)**: every apply records an immutable snapshot (author, time, deploy result). View, diff, download and one-click restore any version — restores are themselves snapshotted, so they can be undone.
- **Alerting engine (v2.7)**: node offline/online, backend down (consecutive-failure threshold), certificate expiry, CPU/RAM/disk thresholds — deduplicated with cooldowns, delivered to Telegram and generic webhooks (Discord-compatible), with history and acknowledge in the UI.
- **RBAC (v2.7)**: owner / admin / operator / viewer roles enforced on every endpoint.
- **Log management (v2.7)**: bounded tails of allowlisted sources only (agent, panel, nginx, haproxy, xray, hedioum, bridge, syslog) from any node, with live refresh and severity coloring.
- **Existing-config import (v2.7)**: on first run, scan the server's live nginx/HAProxy configs and import existing server blocks as (disabled) mappings for review.
- **Per-UUID bandwidth limiting (v2.6)**: sync PasarGuard users by Xray UUID, assign bandwidth profiles per user, enforce download/upload ceilings on nodes with Linux tc — incremental applies, full audit trail. [docs/BANDWIDTH.md](docs/BANDWIDTH.md).
- **Built-in self-updater**: one click in Settings pulls the latest release, rebuilds the binary and restarts the service with rollback on failure.
- **Backups & restore**: timestamped config backups with diff and one-click validated restore.
- **Diagnostics**: TCP / DNS / TLS / HTTP / backend connectivity tests from the panel.
- **Free-form config authoring**: every mapping can be built with the visual editor **or** a raw JSON editor.
- **HAProxy runtime API**: live `show info`/`show stat`, ready/drain/maint server states without a reload.
- **Safe apply pipeline**: render → validate (`nginx -t` / `haproxy -c`) → timestamped backup → atomic write → zero-downtime reload → **automatic rollback** on failure. Invalid configs never reach the live services.
- **SSL certificates**: upload PEM pairs or generate self-signed (SAN-aware); `.crt/.key` for nginx and a combined `.pem` for HAProxy are produced automatically.
- **Monitoring**: periodic TCP+HTTP checks with latency and consecutive-fail counters, live updates over SSE.
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

### Fleet install with Ansible

```bash
cd portguard/ansible
# edit inventory/hosts.ini: one [portguard_master] + your [portguard_nodes]
ansible-playbook site.yml --limit portguard_master            # install the panel
ansible-playbook site.yml --limit portguard_nodes \
  -e portguard_agent_token=...                                # onboard every agent
ansible-playbook service.yml -e portguard_service_target=apply \
  -e portguard_api_user=admin -e portguard_api_pass=...       # deploy configs via the API
```

See [ansible/README.md](ansible/README.md) for the full guide.

### Add a managed node (no panel install on the node!)

1. In the master panel: **Servers → Deploy new node** — a token and a one-liner are generated for you.
2. On the fresh Ubuntu server, paste the one-liner:

```bash
sudo bash -c "$(curl -fsSL http://MASTER:8080/api/agent-install.sh)" -- -master http://MASTER:8080 -token TOKEN -role generic
```

3. Done — the node self-registers and appears in the Servers list.

**Connection modes:**

| Mode | Behavior | When to use |
|---|---|---|
| **Reverse** (default with `-master`) | The node dials the master and keeps a WS tunnel open; all management calls ride the tunnel | The node is behind NAT/firewall, or you don't want any inbound port open |
| **Direct** | The node listens on `port` (default 8081); the master dials it directly | Internal networks / VPN where the master can reach the node |

Reverse mode needs **zero inbound ports** on the node; direct mode requires port 8081 open to the master's IP only.

The node runs `portguard agent` — a headless service exposing only the token-authenticated node API (through the tunnel or directly). Versions of master and nodes match by construction (nodes download the master's own binary). Rotate the node token from Servers → the node's card → Edit. Remove a node with `systemctl disable --now portguard-agent && rm -rf /opt/portguard-agent /etc/systemd/system/portguard-agent.service`.

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
cmd/server/          entry point (panel + `agent` subcommand for headless nodes: direct or reverse)
internal/api/        chi router, JWT auth, SSE broker, handlers, Agent router (node API)
internal/nodehub/    master-side hub: upgrades reverse-agent WS tunnels, connection registry, req/reply
internal/nodeagent/  agent-side dialer: dials the master, serves the node API over the tunnel
internal/nodeclient/ master→node client (direct HTTP or hub-backed transport, chosen per node)
internal/service/    apply orchestration: render → validate → backup → atomic → reload → rollback
internal/proxy/      Engine interface, nginx/haproxy renderers, live-config parser, HAProxy runtime client
internal/tunnel/     Hedioum bridge builder + xray validation + systemd unit + rollback
internal/conntrack/  live inbound-connection sampler (ss) for the security log
internal/scanner/    listening-port discovery & service classification
internal/health/     periodic TCP/HTTP health checker
internal/tools/      managed tool registry: detect + official installers (xray, hedioum, certbot…)
internal/store/      SQLite (modernc, CGO-free) schema + queries
internal/sysinfo/    gopsutil system stats
web/                 React 19 + Vite + Tailwind dashboard (embedded)
deploy/              install.sh + remote-install.sh + agent-install.sh + update.sh + uninstall.sh + systemd units
docs/                full usage guide (USAGE.md)
```

**Data flow (direct):** browser ← REST/JWT + SSE ← master panel ← token-authenticated HTTP ← node API

**Data flow (reverse):** the node dials `/api/node/ws` on the master and holds it open ← the master pushes each node-API call as a `req` message over the WS ← the agent runs it against its local router and returns a `resp`

Full usage guide: [docs/USAGE.md](docs/USAGE.md)

## Troubleshooting

| Problem | Fix |
|---|---|
| Panel not reachable | `systemctl status portguard`, `journalctl -u portguard -n 50` |
| Reverse node shows offline | On the node: `journalctl -u portguard-agent -n 50` — dial errors mean the master is unreachable or the token is wrong |
| Direct node shows offline | `systemctl status portguard-agent` on the node; check the token and that port 8081 is open to the master |
| Apply validation error | The message is the real `nginx -t`/`haproxy -c` output; fix the offending mapping |
| Apply bind error | The listen port is owned by another process (see the Ports page); the apply was rolled back automatically |
| Forgot admin password | `systemctl stop portguard && rm /var/lib/portguard/portguard.db && systemctl start portguard` (full reset — mappings are wiped too) |

## Security notes

- The panel runs as root (it writes `/etc/nginx`, `/etc/haproxy` and reloads services). Restrict port 8080 with a firewall to trusted IPs when possible.
- Agents trust exactly one master token; rotate it from Servers → the node's card if a server is redeployed.
- In reverse mode the node has **no inbound ports open at all** — its network attack surface is effectively zero.
- Certificate private keys are stored `0600` under `/var/lib/portguard/certs`; the node API never returns PEM bodies.
- Every apply leaves a timestamped backup in `/var/lib/portguard/backups/` (20 most recent kept).

## License

MIT
