package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"portguard/internal/conntrack"
	"portguard/internal/nodeclient"
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

// ---- node-side tools API ----

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
func (a *App) handleToolInstall(w http.ResponseWriter, r *http.Request) {
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
	a.St.Audit(actorFrom(r.Context()), "tool.install "+id, res.Elapsed, status)
	writeJSON(w, http.StatusOK, res)
}

// ---- master-side node CRUD ----

func (a *App) handleListNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := a.St.ListServerNodesPublic()
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
	cli := nodeClientNew(n.Host, n.Port, n.APIToken)
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

func nodeClientNew(host string, port int, token string) *nodeclient.Client {
	return nodeclient.New(host, port, token)
}
