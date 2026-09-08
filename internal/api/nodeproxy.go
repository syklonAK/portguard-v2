package api

// Remote node management proxies: the master drives a node's mappings,
// certificates, tools and logs through its node API — over the hub tunnel
// for reverse nodes or direct HTTP otherwise. Split out of nodes.go (v2.12
// refactor): nodes.go keeps CRUD/hub/deploy, this file keeps master→node
// pass-through handlers.

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"portguard/internal/nodeclient"
)

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

// handleNodeCertDelete deletes a certificate ON the remote node.
func (a *App) handleNodeCertDelete(w http.ResponseWriter, r *http.Request) {
	cli, _, err := a.nodeClient(r)
	if err != nil {
		errJSON(w, err, http.StatusBadGateway)
		return
	}
	out, err := cli.Delete("/certs/" + chi.URLParam(r, "cid"))
	if err != nil {
		errJSON(w, errString("node: "+err.Error()), http.StatusBadGateway)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "node.cert.delete", "via "+chi.URLParam(r, "id"), "ok")
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}

// ---- v2.13: trojan suite / tuning / firewall pass-through ----
// Raw JSON bodies flow through untouched; the node validates exactly like
// the local endpoints (the same App handlers run on the agent side).

// handleNodeTrojanRelays lists trojan relays ON a node.
func (a *App) handleNodeTrojanRelays(w http.ResponseWriter, r *http.Request) {
	cli, _, err := a.nodeClient(r)
	if err != nil {
		errJSON(w, err, http.StatusBadGateway)
		return
	}
	out, err := cli.Get("/trojan/relays")
	if err != nil {
		errJSON(w, errString("node: "+err.Error()), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}

// handleNodeTrojanIngresses lists trojan ingresses ON a node.
func (a *App) handleNodeTrojanIngresses(w http.ResponseWriter, r *http.Request) {
	cli, _, err := a.nodeClient(r)
	if err != nil {
		errJSON(w, err, http.StatusBadGateway)
		return
	}
	out, err := cli.Get("/trojan/ingresses")
	if err != nil {
		errJSON(w, errString("node: "+err.Error()), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}

// handleNodeTrojanBridgeApply applies the trojan bridge ON a node.
func (a *App) handleNodeTrojanBridgeApply(w http.ResponseWriter, r *http.Request) {
	cli, _, err := a.nodeClient(r)
	if err != nil {
		errJSON(w, err, http.StatusBadGateway)
		return
	}
	var body json.RawMessage
	if !readJSONRaw(w, r, &body) {
		return
	}
	out, err := cli.Post("/trojan/bridge/apply", body)
	if err != nil {
		errJSON(w, errString("node: "+err.Error()), http.StatusBadGateway)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "node.trojan.bridge", "applied via "+chi.URLParam(r, "id"), "ok")
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}

// handleNodeTrojanIngressApply applies the trojan ingress ON a node.
func (a *App) handleNodeTrojanIngressApply(w http.ResponseWriter, r *http.Request) {
	cli, _, err := a.nodeClient(r)
	if err != nil {
		errJSON(w, err, http.StatusBadGateway)
		return
	}
	var body json.RawMessage
	if !readJSONRaw(w, r, &body) {
		return
	}
	out, err := cli.Post("/trojan/ingress/apply", body)
	if err != nil {
		errJSON(w, errString("node: "+err.Error()), http.StatusBadGateway)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "node.trojan.ingress", "applied via "+chi.URLParam(r, "id"), "ok")
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}

// handleNodeBBRStatus reports the tuning state ON a node.
func (a *App) handleNodeBBRStatus(w http.ResponseWriter, r *http.Request) {
	cli, _, err := a.nodeClient(r)
	if err != nil {
		errJSON(w, err, http.StatusBadGateway)
		return
	}
	out, err := cli.Get("/tuning/bbr")
	if err != nil {
		errJSON(w, errString("node: "+err.Error()), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}

// handleNodeBBRApply applies network tuning ON a node.
func (a *App) handleNodeBBRApply(w http.ResponseWriter, r *http.Request) {
	cli, _, err := a.nodeClient(r)
	if err != nil {
		errJSON(w, err, http.StatusBadGateway)
		return
	}
	out, err := cli.Post("/tuning/bbr/apply", nil)
	if err != nil {
		errJSON(w, errString("node: "+err.Error()), http.StatusBadGateway)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "node.bbr.apply", "via "+chi.URLParam(r, "id"), "ok")
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}

// handleNodeFirewallStatus reports the firewall kind ON a node.
func (a *App) handleNodeFirewallStatus(w http.ResponseWriter, r *http.Request) {
	cli, _, err := a.nodeClient(r)
	if err != nil {
		errJSON(w, err, http.StatusBadGateway)
		return
	}
	out, err := cli.Get("/firewall")
	if err != nil {
		errJSON(w, errString("node: "+err.Error()), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}

// handleNodeFirewallAllow opens a port ON a node.
func (a *App) handleNodeFirewallAllow(w http.ResponseWriter, r *http.Request) {
	cli, _, err := a.nodeClient(r)
	if err != nil {
		errJSON(w, err, http.StatusBadGateway)
		return
	}
	var body json.RawMessage
	if !readJSONRaw(w, r, &body) {
		return
	}
	out, err := cli.Post("/firewall/allow", body)
	if err != nil {
		errJSON(w, errString("node: "+err.Error()), http.StatusBadGateway)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "node.firewall.allow", "via "+chi.URLParam(r, "id"), "ok")
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

