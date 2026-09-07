package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"portguard/internal/conntrack"
	"portguard/internal/nodeclient"
	"portguard/internal/nodehub"
	"portguard/internal/store"
	"portguard/internal/sysinfo"
	"portguard/internal/tools"
	"portguard/internal/tunnel"
)

// storeServerNode is the request shape for node CRUD: the API token is
// writable here (ServerNode itself tags it `json:"-"` so it never leaks out).
type storeServerNode struct {
	store.ServerNode
	APITokenWrite string `json:"api_token"`
}

// ---- node API (called by a master PortGuard panel) ----
//
// A panel accepts master requests when the setting "node_token" is non-empty.
// All /api/node/* endpoints authenticate with that token via the same
// Bearer scheme, but a separate middleware (no admin JWT required).

func (a *App) nodeAuthed(r *http.Request) bool {
	tok := a.St.GetSettingOr("node_token", "")
	if tok == "" {
		return false
	}
	auth := r.Header.Get("Authorization")
	return auth == "Bearer "+tok
}

func (a *App) handleNodePing(w http.ResponseWriter, r *http.Request) {
	if !a.nodeAuthed(r) {
		unauthorized(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": a.Version})
}

func (a *App) handleNodeSummary(w http.ResponseWriter, r *http.Request) {
	if !a.nodeAuthed(r) {
		unauthorized(w)
		return
	}
	sys, _ := sysinfoSnapshot(a)
	mappings, _ := a.St.ListMappings()
	enabled := 0
	for _, m := range mappings {
		if m.Enabled {
			enabled++
		}
	}
	ports, _ := a.St.ListPorts()
	unmanaged := 0
	for _, p := range ports {
		if !p.Managed && !p.Self {
			unmanaged++
		}
	}
	healthList, _ := a.St.ListHealth()
	up, down := 0, 0
	for _, h := range healthList {
		switch h.Status {
		case "up":
			up++
		case "down":
			down++
		}
	}
	role := a.St.GetSettingOr("node_role", "standalone")
	writeJSON(w, http.StatusOK, map[string]any{
		"version":  a.Version,
		"role":     role,
		"system":   sys["system"],
		"mappings": map[string]int{"total": len(mappings), "enabled": enabled},
		"ports":    map[string]int{"total": len(ports), "unmanaged": unmanaged},
		"health":   map[string]int{"up": up, "down": down},
		"tunnel":   tunnel.Detect(),
	})
}

func (a *App) handleNodeMappings(w http.ResponseWriter, r *http.Request) {
	if !a.nodeAuthed(r) {
		unauthorized(w)
		return
	}
	mappings, err := a.St.ListMappings()
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mappings": mappings})
}

func (a *App) handleNodeApply(w http.ResponseWriter, r *http.Request) {
	if !a.nodeAuthed(r) {
		unauthorized(w)
		return
	}
	if err := a.Svc.ApplyAll("node"); err != nil {
		a.St.Audit("node", "apply", err.Error(), "error")
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	a.St.Audit("node", "apply", "applied via master", "ok")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleNodeConnections(w http.ResponseWriter, r *http.Request) {
	if !a.nodeAuthed(r) {
		unauthorized(w)
		return
	}
	conns, err := a.St.ListConnections()
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	mappings, _ := a.St.ListMappings()
	conntrack.EnrichManaged(conns, mappings, a.PanelPort)
	talkers, _ := a.St.TopTalkers(10)
	writeJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"connections": conns,
			"top_talkers": talkers,
			"total":       len(conns),
		},
	})
}

// handleNodeBinary streams this panel's own linux-amd64 binary to a new
// server so `agent-install.sh` can install the node without any build step.
// The download link is protected by the node token (the master generates a
// per-server token exactly for this).
func (a *App) handleNodeBinary(w http.ResponseWriter, r *http.Request) {
	if !a.nodeAuthed(r) {
		unauthorized(w)
		return
	}
	exe, err := os.Executable()
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	f, err := os.Open(exe)
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="portguard"`)
	_, _ = io.Copy(w, f)
}

// handleAgentInstaller serves deploy/agent-install.sh so the one-liner can
// be copy-pasted from the master UI without a GitHub round-trip.
func (a *App) handleAgentInstaller(w http.ResponseWriter, r *http.Request) {
	// public: the script itself contains no secrets; the -token arg is added
	// by the user when running it
	w.Header().Set("Content-Type", "text/x-shellscript")
	_, _ = w.Write([]byte(agentInstallScript))
}

// agentInstallScript is the embedded node installer served by the master
// (kept in sync with deploy/agent-install.sh).
const agentInstallScript = `#!/usr/bin/env bash
set -euo pipefail
APP_DIR="/opt/portguard-agent"
UNIT="/etc/systemd/system/portguard-agent.service"
AGENT_PORT="${AGENT_PORT:-8081}"
MASTER=""
TOKEN=""
NAME=""
HOSTIP=""
ROLE="${ROLE:-generic}"
say() { echo -e "\033[1;35m[agent-install]\033[0m $*"; }
die() { echo -e "\033[1;31m[agent-install:ERROR]\033[0m $*" >&2; exit 1; }
[ "$(id -u)" = "0" ] || die "run as root (sudo)"
while [ $# -gt 0 ]; do
  case "$1" in
    -master) MASTER="$2"; shift 2 ;;
    -token)  TOKEN="$2"; shift 2 ;;
    -role)   ROLE="$2"; shift 2 ;;
    -name)   NAME="$2"; shift 2 ;;
    -host)   HOSTIP="$2"; shift 2 ;;
    -port)   AGENT_PORT="$2"; shift 2 ;;
    *) die "unknown option: $1" ;;
  esac
done
[ -n "$MASTER" ] || die "usage: bash portguard-agent-install.sh -master http://MASTER:8080 -token TOKEN [-role generic] [-port 8081]"
[ -n "$TOKEN" ]  || die "missing -token"
case "$ROLE" in generic|iran|foreign) ;; *) die "role must be generic, iran or foreign" ;; esac
command -v curl >/dev/null 2>&1 || {
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -y -qq >/dev/null 2>&1
  apt-get install -y -qq curl >/dev/null 2>&1
}
say "downloading portguard binary from the master (${MASTER})…"
mkdir -p "$APP_DIR"
curl -fsSL --connect-timeout 10 -H "Authorization: Bearer ${TOKEN}" "${MASTER}/api/node/binary" -o "${APP_DIR}/portguard.new" \
  || die "could not download the binary — check master reachability and the token"
chmod +x "${APP_DIR}/portguard.new"
# pipefail + SIGPIPE: head -1 closes the pipe and the binary exits 141,
# which would fail the pipeline even on a match - capture the line first
SMOKE="$("${APP_DIR}/portguard.new" agent 2>&1 | head -1 || true)"
echo "$SMOKE" | grep -qi portguard   || die "downloaded binary failed its smoke test — the master may serve a different architecture"
mv "${APP_DIR}/portguard.new" "${APP_DIR}/portguard"
mkdir -p /var/lib/portguard
if [ -n "$MASTER" ]; then
  # reverse mode: the agent dials the master over a persistent WS tunnel and
  # needs NO inbound port - -master keeps it behind firewalls/NAT
  AGENT_ARGS="agent -master ${MASTER} -token ${TOKEN} -role ${ROLE}"
else
  AGENT_ARGS="agent -port ${AGENT_PORT} -token ${TOKEN} -role ${ROLE}"
fi
cat > "$UNIT" <<EOF
[Unit]
Description=PortGuard managed node (agent)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=${APP_DIR}/portguard ${AGENT_ARGS}
Restart=always
RestartSec=3
LimitNOFILE=1000000

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now portguard-agent
if [ -z "$MASTER" ] && command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -qi "Status: active"; then
  ufw allow "${AGENT_PORT}/tcp" >/dev/null 2>&1 && say "ufw: allowed ${AGENT_PORT}/tcp"
fi
sleep 1
if systemctl is-active --quiet portguard-agent; then
  if [ -n "$MASTER" ]; then
    say "agent running in REVERSE mode -> ${MASTER} (role: ${ROLE})"
    say "   no inbound port open - the master manages it over the WS tunnel"
  else
    say "agent running on port ${AGENT_PORT} (role: ${ROLE})"
  fi
else
  journalctl -u portguard-agent -n 20 --no-pager; die "agent failed to start"
fi

# self-register: announce this server to the master so it appears in the
# Servers page immediately - no account, no manual "Add server" step
REG_NAME="${NAME:-$(hostname)}"
REG_HOST="${HOSTIP:-$(curl -fsSL -m 8 "${MASTER}/api/node/myip" 2>/dev/null || true)}"
REG_NAME="${REG_NAME//\"/}"
if [ -n "$REG_HOST" ]; then
  REG_CONN="direct"
  [ -n "$MASTER" ] && REG_CONN="reverse"
  if curl -fsSL -m 10 -X POST -H "Authorization: Bearer ${TOKEN}" -H 'Content-Type: application/json' \
      -d "{\"name\":\"${REG_NAME}\",\"host\":\"${REG_HOST}\",\"port\":${AGENT_PORT},\"role\":\"${ROLE}\",\"conn_mode\":\"${REG_CONN}\"}" \
      "${MASTER}/api/nodes/self-register" >/dev/null 2>&1; then
    say "registered with the master (${REG_HOST}:${AGENT_PORT}, ${REG_CONN}) — manage it from the Servers page"
  else
    say "WARNING: self-register failed — add the node manually in the master's Servers page"
  fi
else
  say "WARNING: could not determine this server's address — add the node manually in the master's Servers page"
fi
`

// handleNodeTools reports which managed tools are installed on this server.
func (a *App) handleNodeTools(w http.ResponseWriter, r *http.Request) {
	if !a.nodeAuthed(r) {
		unauthorized(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tools": tools.DetectAll()})
}

// handleNodeToolInstall installs a tool on this server (official installer).
func (a *App) handleNodeToolInstall(w http.ResponseWriter, r *http.Request) {
	if !a.nodeAuthed(r) {
		unauthorized(w)
		return
	}
	id := chi.URLParam(r, "tool")
	res, err := tools.Install(id)
	if err != nil {
		errJSON(w, err, http.StatusNotFound)
		return
	}
	status := "ok"
	if !res.OK {
		status = "error"
	}
	a.St.Audit("node", "tool.install "+id, "via master", status)
	writeJSON(w, http.StatusOK, res)
}

// handleTools is the local (admin JWT) view of the tool registry.
func (a *App) handleTools(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"tools": tools.DetectAll()})
}

// handleToolInstall is the local (admin JWT) install trigger.
// stream=1 runs the installer as a background job and returns a job id the
// UI polls/subscribes to for a live terminal view.
func (a *App) handleToolInstall(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "tool")
	if r.URL.Query().Get("stream") == "1" && a.Jobs != nil {
		jobName := "install " + id
		actor := actorFrom(r.Context())
		j := a.Jobs.Start(jobName, func(job *Job) error {
			res, err := tools.InstallStream(id, job.Write)
			if err == nil && !res.OK {
				// official installers often hit flaky CDN/network paths:
				// one automatic retry before giving up
				job.Write("[portguard] retrying install once after failure…")
				res, err = tools.InstallStream(id, job.Write)
			}
			status, detail := "ok", ""
			if err == nil {
				detail = res.Elapsed
			}
			if err != nil {
				status, detail = "error", err.Error()
			} else if !res.OK {
				status = "error"
				detail = res.Error
			}
			a.St.Audit(actor, "tool.install "+id, detail, status)
			if err != nil {
				return err
			}
			if !res.OK {
				job.Write("[portguard] install reported problems" + func() string {
					if res.Error != "" {
						return ": " + res.Error
					}
					return ""
				}())
				return fmt.Errorf("%s install failed", id)
			}
			job.Write("[portguard] done — " + id + " installed in " + res.Elapsed)
			return nil
		})
		writeJSON(w, http.StatusAccepted, map[string]any{"job_id": j.ID, "name": jobName})
		return
	}
	res, err := tools.Install(id)
	if err != nil {
		errJSON(w, err, http.StatusNotFound)
		return
	}
	status := "ok"
	if !res.OK {
		status = "error"
	}
	a.St.Audit(actorFrom(r.Context()), "tool.install "+id, res.Elapsed, status)
	writeJSON(w, http.StatusOK, res)
}

// ---- remote mapping management (master drives a node's mappings) ----

func (a *App) nodeClient(r *http.Request) (*nodeclient.Client, int64, error) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	n, err := a.St.GetServerNode(id)
	if err != nil {
		return nil, 0, err
	}
	if !n.Enabled {
		return nil, 0, errString("server is disabled")
	}
	return a.nodeClientFor(n), id, nil
}

// handleNodeMappingsProxy forwards the mapping list from a remote node.
func (a *App) handleNodeMappingsProxy(w http.ResponseWriter, r *http.Request) {
	cli, _, err := a.nodeClient(r)
	if err != nil {
		errJSON(w, err, http.StatusBadGateway)
		return
	}
	out, err := cli.Get("/mappings")
	if err != nil {
		errJSON(w, errString("node: "+err.Error()), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}

// handleNodeMappingCreate creates a mapping ON the remote node.
func (a *App) handleNodeMappingCreate(w http.ResponseWriter, r *http.Request) {
	cli, _, err := a.nodeClient(r)
	if err != nil {
		errJSON(w, err, http.StatusBadGateway)
		return
	}
	var body json.RawMessage
	if !readJSONRaw(w, r, &body) {
		return
	}
	out, err := cli.Post("/mappings", body)
	if err != nil {
		errJSON(w, errString("node: "+err.Error()), http.StatusBadGateway)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "node.mapping.create", "via "+chi.URLParam(r, "id"), "ok")
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}

// handleNodeMappingUpdate updates a mapping ON the remote node.
func (a *App) handleNodeMappingUpdate(w http.ResponseWriter, r *http.Request) {
	cli, _, err := a.nodeClient(r)
	if err != nil {
		errJSON(w, err, http.StatusBadGateway)
		return
	}
	var body json.RawMessage
	if !readJSONRaw(w, r, &body) {
		return
	}
	out, err := cli.Put("/mappings/"+chi.URLParam(r, "mid"), body)
	if err != nil {
		errJSON(w, errString("node: "+err.Error()), http.StatusBadGateway)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "node.mapping.update", "via "+chi.URLParam(r, "id"), "ok")
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}

// handleNodeMappingDelete deletes a mapping ON the remote node.
func (a *App) handleNodeMappingDelete(w http.ResponseWriter, r *http.Request) {
	cli, _, err := a.nodeClient(r)
	if err != nil {
		errJSON(w, err, http.StatusBadGateway)
		return
	}
	out, err := cli.Delete("/mappings/" + chi.URLParam(r, "mid"))
	if err != nil {
		errJSON(w, errString("node: "+err.Error()), http.StatusBadGateway)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "node.mapping.delete", "via "+chi.URLParam(r, "id"), "ok")
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}

// handleNodeCertsProxy forwards the (scrubbed) cert list from a node.
func (a *App) handleNodeCertsProxy(w http.ResponseWriter, r *http.Request) {
	cli, _, err := a.nodeClient(r)
	if err != nil {
		errJSON(w, err, http.StatusBadGateway)
		return
	}
	out, err := cli.Get("/certs")
	if err != nil {
		errJSON(w, errString("node: "+err.Error()), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}

// handleNodeCertCreate uploads a PEM cert pair to the remote node.
func (a *App) handleNodeCertCreate(w http.ResponseWriter, r *http.Request) {
	cli, _, err := a.nodeClient(r)
	if err != nil {
		errJSON(w, err, http.StatusBadGateway)
		return
	}
	var body json.RawMessage
	if !readJSONRaw(w, r, &body) {
		return
	}
	out, err := cli.Post("/certs", body)
	if err != nil {
		errJSON(w, errString("node: "+err.Error()), http.StatusBadGateway)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "node.cert.create", "via "+chi.URLParam(r, "id"), "ok")
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}

// readJSONRaw decodes the request body preserving the raw JSON.
func readJSONRaw(w http.ResponseWriter, r *http.Request, v *json.RawMessage) bool {
	defer r.Body.Close()
	b, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read failed"})
		return false
	}
	if !json.Valid(b) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return false
	}
	*v = json.RawMessage(b)
	return true
}

// handleNodeLogs tails one allowlisted log source on a node.
func (a *App) handleNodeLogs(w http.ResponseWriter, r *http.Request) {
	cli, _, err := a.nodeClient(r)
	if err != nil {
		errJSON(w, err, http.StatusBadGateway)
		return
	}
	source := chi.URLParam(r, "source")
	lines := 200
	if v := r.URL.Query().Get("lines"); v != "" {
		if n, err2 := strconv.Atoi(v); err2 == nil && n > 0 && n <= 500 {
			lines = n
		}
	}
	out, err := cli.Logs(source, lines)
	if err != nil {
		errJSON(w, errString("node: "+err.Error()), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"source": source, "lines": out})
}

// ---- master-side node CRUD ----

func (a *App) handleListNodes(w http.ResponseWriter, r *http.Request) {
	limit, offset := pagedParams(r)
	nodes, err := a.St.ListServerNodesPublicPaged(limit, offset)
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, nodes)
}

func (a *App) handleCreateNode(w http.ResponseWriter, r *http.Request) {
	var n storeServerNode
	if !readJSON(w, r, &n) {
		return
	}
	if n.Name == "" || n.Host == "" {
		errJSON(w, errString("name and host are required"), http.StatusUnprocessableEntity)
		return
	}
	if n.Port == 0 {
		n.Port = 8080
	}
	if n.Role == "" {
		n.Role = "generic"
	}
	if n.APITokenWrite != "" {
		n.APIToken = n.APITokenWrite
	}
	n.ID = 0
	n.Enabled = true
	id, err := a.St.CreateServerNode(&n.ServerNode)
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "node.create", "server "+n.Name, "ok")
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (a *App) handleUpdateNode(w http.ResponseWriter, r *http.Request) {
	id, _ := parseInt64(chi.URLParam(r, "id"))
	cur, err := a.St.GetServerNode(id)
	if err != nil {
		errJSON(w, err, http.StatusNotFound)
		return
	}
	var n storeServerNode
	if !readJSON(w, r, &n) {
		return
	}
	n.ID = id
	n.Name = defaultIfEmpty(n.Name, cur.Name)
	n.Host = defaultIfEmpty(n.Host, cur.Host)
	if n.Port == 0 {
		n.Port = cur.Port
	}
	if n.Role == "" {
		n.Role = cur.Role
	}
	if n.Notes == "" {
		n.Notes = cur.Notes
	}
	if n.ConnMode == "" {
		n.ConnMode = cur.ConnMode
	}
	if n.UID == "" {
		n.UID = cur.UID
	}
	if n.ConnMode != "direct" && n.ConnMode != "reverse" {
		n.ConnMode = "direct"
	}
	// enabled: zero-value false on a partial PUT would silently disable the
	// node — only apply the flag when the client explicitly sent it
	if r.ContentLength >= 0 {
		// the UI always sends the full object; treat missing "enabled" as keep
		n.Enabled = n.Enabled || cur.Enabled
	}
	// token replacement is explicit-only
	if n.APITokenWrite != "" {
		if err := a.St.UpdateServerNodeToken(id, n.APITokenWrite); err != nil {
			errJSON(w, err, http.StatusInternalServerError)
			return
		}
	}
	n.Status = cur.Status
	n.LastSeen = cur.LastSeen
	if err := a.St.UpdateServerNode(&n.ServerNode); err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "node.update", "server "+n.Name, "ok")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func defaultIfEmpty(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func (a *App) handleDeleteNode(w http.ResponseWriter, r *http.Request) {
	id, _ := parseInt64(chi.URLParam(r, "id"))
	if err := a.St.DeleteServerNode(id); err != nil {
		errJSON(w, err, http.StatusNotFound)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "node.delete", "server #"+chi.URLParam(r, "id"), "ok")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleNodeAction performs a remote operation on one node (probe/apply).
func (a *App) handleNodeAction(w http.ResponseWriter, r *http.Request) {
	id, _ := parseInt64(chi.URLParam(r, "id"))
	action := chi.URLParam(r, "action")
	n, err := a.St.GetServerNode(id)
	if err != nil {
		errJSON(w, err, http.StatusNotFound)
		return
	}
	if !n.Enabled {
		errJSON(w, errString("server is disabled"), http.StatusUnprocessableEntity)
		return
	}
	cli := a.nodeClientFor(n)
	switch action {
	case "probe":
		if err := cli.Ping(); err != nil {
			_ = a.St.TouchServerNode(id, "offline")
			errJSON(w, errString("unreachable: "+err.Error()), http.StatusBadGateway)
			return
		}
		if err := a.St.TouchServerNode(id, "online"); err != nil {
			errJSON(w, err, http.StatusInternalServerError)
			return
		}
		a.St.Audit(actorFrom(r.Context()), "node.probe", "server "+n.Name, "ok")
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "online"})
	case "summary":
		sum, err := cli.GetSummary()
		if err != nil {
			_ = a.St.TouchServerNode(id, "offline")
			errJSON(w, errString("unreachable: "+err.Error()), http.StatusBadGateway)
			return
		}
		_ = a.St.TouchServerNode(id, "online")
		writeJSON(w, http.StatusOK, sum)
	case "apply":
		if err := cli.Apply(); err != nil {
			errJSON(w, errString(err.Error()), http.StatusBadGateway)
			return
		}
		a.St.Audit(actorFrom(r.Context()), "node.apply", "server "+n.Name, "ok")
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case "tools":
		list, err := cli.Tools()
		if err != nil {
			_ = a.St.TouchServerNode(id, "offline")
			errJSON(w, errString("unreachable: "+err.Error()), http.StatusBadGateway)
			return
		}
		_ = a.St.TouchServerNode(id, "online")
		writeJSON(w, http.StatusOK, map[string]any{"tools": list})
	case "install":
		// tool id comes from the body: {"tool": "xray"}
		var body struct {
			Tool string `json:"tool"`
		}
		if !readJSON(w, r, &body) {
			return
		}
		if body.Tool == "" {
			errJSON(w, errString("tool is required"), http.StatusUnprocessableEntity)
			return
		}
		res, err := cli.InstallTool(body.Tool)
		if err != nil {
			errJSON(w, errString(err.Error()), http.StatusBadGateway)
			return
		}
		status := "ok"
		if !res.OK {
			status = "error"
		}
		a.St.Audit(actorFrom(r.Context()), "node.tool.install "+body.Tool, "server "+n.Name, status)
		writeJSON(w, http.StatusOK, res)
	default:
		errJSON(w, errString("unknown action"), http.StatusNotFound)
	}
}

// handleNodeTokenInfo reveals this panel's own node-token settings state.
func (a *App) handleNodeTokenInfo(w http.ResponseWriter, r *http.Request) {
	tok := a.St.GetSettingOr("node_token", "")
	role := a.St.GetSettingOr("node_role", "standalone")
	writeJSON(w, http.StatusOK, map[string]any{
		"configured": tok != "",
		"role":       role,
	})
}

// handlePutNodeToken sets/rotates this panel's node token + role (master<->node handshake).
func (a *App) handlePutNodeToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
		Role  string `json:"role"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if body.Token != "" {
		_ = a.St.SetSetting("node_token", body.Token)
	}
	if body.Role != "" {
		valid := map[string]bool{"standalone": true, "master": true, "iran": true, "foreign": true, "generic": true}
		if !valid[body.Role] {
			errJSON(w, errString("role must be standalone|master|iran|foreign|generic"), http.StatusUnprocessableEntity)
			return
		}
		_ = a.St.SetSetting("node_role", body.Role)
	}
	a.St.Audit(actorFrom(r.Context()), "node.self", "node token/role updated", "ok")
	a.handleNodeTokenInfo(w, r)
}

// helpers used by handlers in this file

func sysinfoSnapshot(a *App) (map[string]any, error) {
	snap := sysinfo.SnapshotNow()
	return map[string]any{
		"system": snap,
	}, nil
}

func parseInt64(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

// ---- reverse connections (PasarGuard-style node → panel tunnel) ----

// hubResolve maps an incoming agent WS bearer token to its node row.
// The token must match a known node's api_token (or the cluster token).
func (a *App) hubResolve(token string) (int64, string, bool) {
	if token == "" {
		return 0, "", false
	}
	nodes, err := a.St.ListServerNodes()
	if err != nil {
		return 0, "", false
	}
	for _, n := range nodes {
		if n.APIToken != "" && n.APIToken == token {
			return n.ID, n.UID, true
		}
	}
	return 0, "", false
}

// handleNodeWS upgrades a reverse-mode agent connection into the hub.
func (a *App) handleNodeWS(w http.ResponseWriter, r *http.Request) {
	if a.Hub == nil {
		http.Error(w, "hub disabled", http.StatusServiceUnavailable)
		return
	}
	a.Hub.ServeWS(w, r)
}

// NewHub builds the panel-side hub with store-backed callbacks.
func (a *App) NewHub() *nodehub.Hub {
	a.Hub = nodehub.NewHub(a.hubResolve, func(nodeID int64, online bool) {
		status := "offline"
		if online {
			status = "online"
		}
		_ = a.St.TouchServerNode(nodeID, status)
		if a.Broker != nil {
			a.Broker.Publish("nodes", map[string]any{"action": "state", "id": nodeID, "online": online})
		}
	})
	return a.Hub
}

// NodeClientFor is the exported hub-aware node client factory (wired into
// the alerter and other background loops by main).
func (a *App) NodeClientFor(n store.ServerNode) *nodeclient.Client {
	return a.nodeClientFor(n)
}

// nodeClientFor returns the right transport for a node: reverse-mode nodes
// that are connected through the hub get a hub-backed client (requests are
// pushed over the agent's persistent WS connection), everything else keeps
// the classic direct HTTP client.
func (a *App) nodeClientFor(n store.ServerNode) *nodeclient.Client {
	if a.Hub != nil && n.ConnMode == "reverse" && a.Hub.IsOnline(n.ID) {
		rt := nodeclient.RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
			var body []byte
			if req.Body != nil {
				b, err := io.ReadAll(req.Body)
				if err != nil {
					return nil, err
				}
				body = b
				req.Body.Close()
			}
			resp, err := a.Hub.Request(n.ID, req.Method, req.URL.Path, body, 15*time.Second)
			if err != nil {
				return nil, err
			}
			return &http.Response{
				StatusCode: resp.Status,
				Body:       io.NopCloser(bytes.NewReader(resp.Body)),
				Header:     make(http.Header),
			}, nil
		})
		return nodeclient.NewWithTransport(rt, n.APIToken)
	}
	return nodeclient.New(n.Host, n.Port, n.APIToken)
}
