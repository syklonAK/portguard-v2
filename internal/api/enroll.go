// Agent self-registration: a freshly provisioned server runs
// agent-install.sh with the cluster node token; at the end of the install
// it calls back to the master and creates its own entry in server_nodes —
// no panel account, no manual "Add server" step.
package api

import (
	"net/http"
	"strings"

	"portguard/internal/store"
)

// handleNodeMyIP tells an installing agent which source address the master
// saw — the script feeds it back as the node's reachable address.
func (a *App) handleNodeMyIP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte(clientIP(r)))
}

// handleNodeSelfRegister creates/updates the server_nodes row for the
// calling agent. Authenticated with the cluster node token (the same one
// that protects the binary download).
func (a *App) handleNodeSelfRegister(w http.ResponseWriter, r *http.Request) {
	if !a.nodeAuthed(r) {
		unauthorized(w)
		return
	}
	var body struct {
		Name string `json:"name"`
		Host string `json:"host"`
		Port int    `json:"port"`
		Role string `json:"role"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	host := strings.TrimSpace(body.Host)
	if host == "" {
		host = clientIP(r)
	}
	if host == "" {
		errJSON(w, errString("cannot determine the node's address — pass -host"), http.StatusBadRequest)
		return
	}
	if body.Port <= 0 || body.Port > 65535 {
		body.Port = 8081
	}
	role := body.Role
	switch role {
	case "generic", "iran", "foreign":
	default:
		role = "generic"
	}
	name := strings.TrimSpace(body.Name)
	name = strings.ReplaceAll(name, "\"", "")
	if name == "" {
		name = host
	}
	clusterToken := a.St.GetSettingOr("node_token", "")

	n := store.ServerNode{
		Name: name, Host: host, Port: body.Port, APIToken: clusterToken,
		Role: role, Enabled: true,
	}
	id, created, err := a.St.UpsertServerNodeByHostPort(&n)
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	if created {
		a.St.Audit("agent", "node.self-register", name+" ("+host+")", "ok")
	} else {
		a.St.Audit("agent", "node.self-register", name+" ("+host+") re-registered", "ok")
	}
	if a.Broker != nil {
		a.Broker.Publish("nodes", map[string]any{"action": "registered", "id": id, "name": name})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "id": id, "created": created, "host": host, "port": body.Port,
		"note": "the node is now visible and manageable from the master's Servers page",
	})
}
