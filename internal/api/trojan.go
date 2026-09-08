package api

// Trojan relay / ingress / hedioum-wizard / BBR / firewall handlers — the
// API surface of the hedioum-allinone port. Pure DB + render helpers live
// in internal/tunnel; this file wires them to HTTP and owns validation.

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"portguard/internal/store"
	"portguard/internal/tunnel"
)

// ---- trojan relays (iran side) ----

func (a *App) handleListTrojanRelays(w http.ResponseWriter, r *http.Request) {
	relays, err := a.St.ListTrojanRelays()
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	if relays == nil {
		relays = []store.TrojanRelay{}
	}
	writeJSON(w, http.StatusOK, relays)
}

func (a *App) validTrojanRelay(rel *store.TrojanRelay) error {
	if !tunnel.IsValidTrojanName(rel.Name) {
		return errString("name must be 1-64 chars of [A-Za-z0-9_-]")
	}
	if rel.Domain == "" {
		return errString("domain is required")
	}
	if strings.ContainsAny(rel.Domain, " \t;{}") || len(rel.Domain) > 255 {
		return errString("invalid domain")
	}
	if rel.HTTPSPort < 1 || rel.HTTPSPort > 65535 {
		return errString("https_port must be 1-65535")
	}
	if rel.ForeignIP == "" {
		return errString("foreign_ip is required")
	}
	if !netParseIPRelay(rel.ForeignIP) {
		return errString("foreign_ip must be an IP address")
	}
	if rel.ForeignPort < 1 || rel.ForeignPort > 65535 {
		return errString("foreign_port must be 1-65535")
	}
	if rel.BridgePort < 1 || rel.BridgePort > 65535 {
		return errString("bridge_port must be 1-65535")
	}
	switch rel.Route {
	case "", "tunnel", "direct":
	default:
		return errString("route must be empty (auto), tunnel or direct")
	}
	if rel.SocksPort < 0 || rel.SocksPort > 65535 {
		return errString("socks_port must be 0-65535 (0 = default)")
	}
	return nil
}

func (a *App) trojanBridgePortsUsed(excludeID int64) []int {
	relays, _ := a.St.ListTrojanRelays()
	var used []int
	for _, e := range relays {
		if e.ID != excludeID {
			used = append(used, e.BridgePort)
		}
	}
	mappings, _ := a.St.ListMappings()
	for _, m := range mappings {
		if m.Enabled {
			used = append(used, m.ListenPort)
		}
	}
	return used
}

func (a *App) handleCreateTrojanRelay(w http.ResponseWriter, r *http.Request) {
	var rel store.TrojanRelay
	if !readJSON(w, r, &rel) {
		return
	}
	// a plain create body omits "enabled" — default to enabled (the raw
	// zero-value false must not silently create a disabled relay)
	rel.Enabled = true
	if rel.BridgePort == 0 {
		rel.BridgePort = tunnel.NextTrojanBridgePort(a.trojanBridgePortsUsed(0), tunnel.TrojanBridgePortBase)
	}
	if rel.Route == "" {
		rel.Route = tunnel.ResolveTrojanRoute(rel.ForeignIP, "")
	}
	if rel.SocksPort == 0 {
		if _, p := a.tunnelSOCKS(); p > 0 {
			rel.SocksPort = p
		} else {
			rel.SocksPort = 40001
		}
	}
	if err := a.validTrojanRelay(&rel); err != nil {
		errJSON(w, err, http.StatusUnprocessableEntity)
		return
	}
	existing, _ := a.St.ListTrojanRelays()
	for _, e := range existing {
		if e.Name == rel.Name {
			errJSON(w, errString("a trojan relay with this name already exists"), http.StatusConflict)
			return
		}
		if e.HTTPSPort == rel.HTTPSPort {
			errJSON(w, errString(fmt.Sprintf("https_port %d already used by relay %s", rel.HTTPSPort, e.Name)), http.StatusConflict)
			return
		}
		if e.BridgePort == rel.BridgePort {
			errJSON(w, errString(fmt.Sprintf("bridge_port %d already used by relay %s", rel.BridgePort, e.Name)), http.StatusConflict)
			return
		}
	}
	id, err := a.St.CreateTrojanRelay(&rel)
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "trojan.relay.create", "relay "+rel.Name+" ("+rel.Domain+":"+itoa(int64(rel.HTTPSPort))+")", "ok")
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (a *App) handleUpdateTrojanRelay(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if _, err := a.St.GetTrojanRelay(id); err != nil {
		errJSON(w, err, http.StatusNotFound)
		return
	}
	var rel store.TrojanRelay
	if !readJSON(w, r, &rel) {
		return
	}
	rel.ID = id
	if err := a.validTrojanRelay(&rel); err != nil {
		errJSON(w, err, http.StatusUnprocessableEntity)
		return
	}
	existing, _ := a.St.ListTrojanRelays()
	for _, e := range existing {
		if e.ID == id {
			continue
		}
		if e.Name == rel.Name {
			errJSON(w, errString("a trojan relay with this name already exists"), http.StatusConflict)
			return
		}
		if e.HTTPSPort == rel.HTTPSPort {
			errJSON(w, errString(fmt.Sprintf("https_port %d already used by relay %s", rel.HTTPSPort, e.Name)), http.StatusConflict)
			return
		}
		if e.BridgePort == rel.BridgePort {
			errJSON(w, errString(fmt.Sprintf("bridge_port %d already used by relay %s", rel.BridgePort, e.Name)), http.StatusConflict)
			return
		}
	}
	if err := a.St.UpdateTrojanRelay(&rel); err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "trojan.relay.update", "relay "+rel.Name, "ok")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleDeleteTrojanRelay(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err := a.St.DeleteTrojanRelay(id); err != nil {
		errJSON(w, err, http.StatusNotFound)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "trojan.relay.delete", "relay #"+strconv.FormatInt(id, 10), "ok")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- trojan bridge apply (iran side) ----

// trojanRelaySpecs converts DB rows into render specs.
func (a *App) trojanRelaySpecs() ([]tunnel.TrojanRelaySpec, error) {
	relays, err := a.St.ListTrojanRelays()
	if err != nil {
		return nil, err
	}
	specs := make([]tunnel.TrojanRelaySpec, 0, len(relays))
	for _, rel := range relays {
		if !rel.Enabled {
			continue
		}
		sp := rel.SocksPort
		if sp == 0 {
			_, sp = a.tunnelSOCKS()
		}
		specs = append(specs, tunnel.TrojanRelaySpec{
			Name: rel.Name, Domain: rel.Domain, HTTPSPort: rel.HTTPSPort,
			ForeignIP: rel.ForeignIP, ForeignPort: rel.ForeignPort,
			BridgePort: rel.BridgePort, Route: rel.Route, SocksPort: sp,
		})
	}
	return specs, nil
}

func (a *App) handleTrojanBridgeValidate(w http.ResponseWriter, r *http.Request) {
	specs, err := a.trojanRelaySpecs()
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	if len(specs) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "note": "no trojan relays configured"})
		return
	}
	st := tunnel.Detect()
	if !st.XrayInstalled {
		errJSON(w, errString("xray is not installed on this server"), http.StatusUnprocessableEntity)
		return
	}
	_, socksPort := a.tunnelSOCKS()
	cfg, err := tunnel.BuildTrojanBridgeConfig(specs, socksPort)
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	if err := tunnel.Validate(cfg, st.XrayBinary); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "relays": len(specs)})
}

func (a *App) handleTrojanBridgeApply(w http.ResponseWriter, r *http.Request) {
	specs, err := a.trojanRelaySpecs()
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	st := tunnel.Detect()
	if !st.XrayInstalled {
		errJSON(w, errString("xray is not installed on this server — install it from Tools first"), http.StatusUnprocessableEntity)
		return
	}
	_, socksPort := a.tunnelSOCKS()
	cfg, err := tunnel.BuildTrojanBridgeConfig(specs, socksPort)
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	if len(specs) > 0 {
		if err := tunnel.Validate(cfg, st.XrayBinary); err != nil {
			a.St.Audit(actorFrom(r.Context()), "trojan.apply", err.Error(), "error")
			errJSON(w, err, http.StatusUnprocessableEntity)
			return
		}
	}
	confPath := "/etc/hedioum-suite/portguard-trojan-bridge.json"
	// install the unit on first use
	unitPath := "/etc/systemd/system/" + tunnel.TrojanBridgeUnit
	if _, err := os.Stat(unitPath); err != nil {
		if err := os.WriteFile(unitPath, []byte(tunnel.TrojanBridgeUnitBody(st.XrayBinary, confPath)), 0o644); err != nil {
			errJSON(w, errString("failed to install bridge service: "+err.Error()), http.StatusInternalServerError)
			return
		}
		_ = exec.Command("systemctl", "daemon-reload").Run()
	}
	// backup the previous config (same layout the Backups page expects)
	var prev []byte
	if data, err := os.ReadFile(confPath); err == nil {
		prev = data
		backupDir := filepath.Join(a.Svc.Paths.BackupsDir, "trojan-bridge")
		_ = os.MkdirAll(backupDir, 0o755)
		_ = os.WriteFile(filepath.Join(backupDir, "portguard-trojan-bridge.json"), data, 0o600)
	}
	// write atomically + restart with rollback (same contract as tunnel.Apply)
	if err := os.MkdirAll(filepath.Dir(confPath), 0o700); err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(confPath, cfg, 0o600); err != nil {
		errJSON(w, errString("write failed: "+err.Error()), http.StatusInternalServerError)
		return
	}
	if len(specs) == 0 {
		// nothing left: stop the unit instead of running an empty bridge
		_ = exec.Command("systemctl", "disable", "--now", tunnel.TrojanBridgeUnit).Run()
		a.St.Audit(actorFrom(r.Context()), "trojan.apply", "bridge stopped (no relays)", "ok")
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "relays": 0, "note": "no relays left, bridge stopped"})
		return
	}
	out, err := exec.Command("systemctl", "restart", tunnel.TrojanBridgeUnit).CombinedOutput()
	if err == nil {
		err = exec.Command("systemctl", "is-active", "--quiet", tunnel.TrojanBridgeUnit).Run()
	}
	if err != nil {
		// rollback
		if prev != nil {
			_ = os.WriteFile(confPath, prev, 0o600)
			_ = exec.Command("systemctl", "restart", tunnel.TrojanBridgeUnit).Run()
		}
		msg := fmt.Sprintf("bridge restart failed: %s", strings.TrimSpace(string(out)))
		a.St.Audit(actorFrom(r.Context()), "trojan.apply", msg+" (rolled back)", "error")
		errJSON(w, errString(msg+" (rolled back)"), http.StatusInternalServerError)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "trojan.apply", fmt.Sprintf("bridge applied (%d relays)", len(specs)), "ok")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "relays": len(specs)})
}

// ---- trojan ingresses (foreign side) ----

func (a *App) handleListTrojanIngresses(w http.ResponseWriter, r *http.Request) {
	list, err := a.St.ListTrojanIngresses()
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []store.TrojanIngress{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (a *App) validTrojanIngress(ing *store.TrojanIngress) error {
	if !tunnel.IsValidTrojanName(ing.Name) {
		return errString("name must be 1-64 chars of [A-Za-z0-9_-]")
	}
	if ing.ListenPort < 1 || ing.ListenPort > 65535 {
		return errString("listen_port must be 1-65535")
	}
	if ing.NodePort < 1 || ing.NodePort > 65535 {
		return errString("node_port must be 1-65535")
	}
	return nil
}

func (a *App) handleCreateTrojanIngress(w http.ResponseWriter, r *http.Request) {
	var ing store.TrojanIngress
	if !readJSON(w, r, &ing) {
		return
	}
	// default to enabled when the body omits the flag (same as relays)
	ing.Enabled = true
	if ing.NodePort == 0 {
		ing.NodePort = tunnel.NodeIngressPortDefault
	}
	if err := a.validTrojanIngress(&ing); err != nil {
		errJSON(w, err, http.StatusUnprocessableEntity)
		return
	}
	existing, _ := a.St.ListTrojanIngresses()
	for _, e := range existing {
		if e.Name == ing.Name {
			errJSON(w, errString("an ingress with this name already exists"), http.StatusConflict)
			return
		}
		if e.ListenPort == ing.ListenPort {
			errJSON(w, errString(fmt.Sprintf("listen_port %d already used by ingress %s", ing.ListenPort, e.Name)), http.StatusConflict)
			return
		}
	}
	id, err := a.St.CreateTrojanIngress(&ing)
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "trojan.ingress.create", "ingress "+ing.Name, "ok")
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (a *App) handleUpdateTrojanIngress(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if _, err := a.St.GetTrojanIngress(id); err != nil {
		errJSON(w, err, http.StatusNotFound)
		return
	}
	var ing store.TrojanIngress
	if !readJSON(w, r, &ing) {
		return
	}
	ing.ID = id
	if err := a.validTrojanIngress(&ing); err != nil {
		errJSON(w, err, http.StatusUnprocessableEntity)
		return
	}
	existing, _ := a.St.ListTrojanIngresses()
	for _, e := range existing {
		if e.ID == id {
			continue
		}
		if e.Name == ing.Name {
			errJSON(w, errString("an ingress with this name already exists"), http.StatusConflict)
			return
		}
		if e.ListenPort == ing.ListenPort {
			errJSON(w, errString(fmt.Sprintf("listen_port %d already used by ingress %s", ing.ListenPort, e.Name)), http.StatusConflict)
			return
		}
	}
	if err := a.St.UpdateTrojanIngress(&ing); err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "trojan.ingress.update", "ingress "+ing.Name, "ok")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleDeleteTrojanIngress(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err := a.St.DeleteTrojanIngress(id); err != nil {
		errJSON(w, err, http.StatusNotFound)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "trojan.ingress.delete", "ingress #"+strconv.FormatInt(id, 10), "ok")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleTrojanIngressApply(w http.ResponseWriter, r *http.Request) {
	st := tunnel.Detect()
	if !st.XrayInstalled {
		errJSON(w, errString("xray is not installed on this server — install it from Tools first"), http.StatusUnprocessableEntity)
		return
	}
	list, err := a.St.ListTrojanIngresses()
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	specs := make([]tunnel.TrojanIngressSpec, 0, len(list))
	for _, ing := range list {
		if !ing.Enabled {
			continue
		}
		specs = append(specs, tunnel.TrojanIngressSpec{Name: ing.Name, ListenPort: ing.ListenPort, NodePort: ing.NodePort})
	}
	cfg, err := tunnel.BuildTrojanIngressConfig(specs)
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	if err := tunnel.Validate(cfg, st.XrayBinary); err != nil && len(specs) > 0 {
		errJSON(w, err, http.StatusUnprocessableEntity)
		return
	}
	confPath := "/etc/hedioum-suite/portguard-trojan-ingress.json"
	unitPath := "/etc/systemd/system/" + tunnel.TrojanIngressUnit
	_ = os.MkdirAll(filepath.Dir(confPath), 0o700)
	if err := os.WriteFile(confPath, cfg, 0o600); err != nil {
		errJSON(w, errString("write failed: "+err.Error()), http.StatusInternalServerError)
		return
	}
	if _, err := os.Stat(unitPath); err != nil {
		if err := os.WriteFile(unitPath, []byte(tunnel.TrojanIngressUnitBody(st.XrayBinary, confPath)), 0o644); err != nil {
			errJSON(w, errString("failed to install ingress service: "+err.Error()), http.StatusInternalServerError)
			return
		}
		_ = exec.Command("systemctl", "daemon-reload").Run()
	}
	if len(specs) == 0 {
		_ = exec.Command("systemctl", "disable", "--now", tunnel.TrojanIngressUnit).Run()
		a.St.Audit(actorFrom(r.Context()), "trojan.ingress.apply", "ingress stopped (no forwarders)", "ok")
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "forwarders": 0, "note": "no forwarders left, ingress stopped"})
		return
	}
	out, err := exec.Command("systemctl", "restart", tunnel.TrojanIngressUnit).CombinedOutput()
	if err == nil {
		err = exec.Command("systemctl", "is-active", "--quiet", tunnel.TrojanIngressUnit).Run()
	}
	if err != nil {
		msg := fmt.Sprintf("ingress restart failed: %s", strings.TrimSpace(string(out)))
		a.St.Audit(actorFrom(r.Context()), "trojan.ingress.apply", msg, "error")
		errJSON(w, errString(msg), http.StatusInternalServerError)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "trojan.ingress.apply", fmt.Sprintf("ingress applied (%d forwarders)", len(specs)), "ok")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "forwarders": len(specs)})
}

// ---- hedioum wizards (egress/hub setup) ----
// Contract mirrors upstream hedioum-tunnel (see tunnel/hedioum.go): the
// foreign wizard prints a v2 pairing token; the iran wizard consumes it.
// Both refuse to overwrite a DIFFERENT existing role unless force=true.

func (a *App) handleHedioumSetupForeign(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Persona  string `json:"persona"`   // auto|cpanel|directadmin|devops
		Domain   string `json:"domain"`    // optional Let's Encrypt domain
		PublicIP string `json:"public_ip"` // optional pinned exit IP
		Token    string `json:"token"`     // optional explicit auth key
		MoveSSH  bool   `json:"move_ssh"`  // relocate OpenSSH to the decoy port
		Force    bool   `json:"force"`     // overwrite a different-role config
	}
	_ = readJSON(w, r, &body) // body is optional — an empty POST uses defaults
	cfg := tunnel.SetupForeignConfig{
		Persona:  body.Persona,
		Domain:   body.Domain,
		PublicIP: body.PublicIP,
		Token:    body.Token,
		MoveSSH:  body.MoveSSH,
		Force:    body.Force,
	}
	token, isV2, decoded, output, err := tunnel.SetupForeign(cfg)
	if err != nil {
		a.St.Audit(actorFrom(r.Context()), "hedioum.setup-foreign", trimAudit(err.Error()), "error")
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"ok": false, "error": err.Error(), "token": token, "output": output,
		})
		return
	}
	detail := "egress configured"
	if isV2 && decoded != nil {
		detail = fmt.Sprintf("egress configured: persona=%s exit_ip=%s endpoints=%d",
			decoded.Persona, decoded.ExitIP, len(decoded.Endpoints))
	} else if token != "" {
		detail = "egress configured: legacy hex token captured"
	}
	a.St.Audit(actorFrom(r.Context()), "hedioum.setup-foreign", detail, "ok")
	resp := map[string]any{"ok": true, "token": token, "token_is_v2": isV2, "output": output}
	if decoded != nil {
		resp["exit_ip"] = decoded.ExitIP
		resp["persona"] = decoded.Persona
		resp["sni"] = decoded.SNI
		resp["endpoints"] = decoded.Endpoints
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *App) handleHedioumSetupIran(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Alias     string `json:"alias"`
		Token     string `json:"token"`
		SocksPort int    `json:"socks_port"`
		Force     bool   `json:"force"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	host, port := a.tunnelSOCKS()
	if body.SocksPort > 0 {
		port = body.SocksPort
	}
	output, err := tunnel.SetupIran(tunnel.SetupIranConfig{Alias: body.Alias, Token: body.Token, SocksPort: port, Force: body.Force})
	if err != nil {
		a.St.Audit(actorFrom(r.Context()), "hedioum.setup-iran", trimAudit(err.Error()), "error")
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"ok": false, "error": err.Error(), "output": output})
		return
	}
	egress := tunnel.EgressIP(host, port)
	a.St.Audit(actorFrom(r.Context()), "hedioum.setup-iran", "hub configured, egress IP "+egress, "ok")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "output": output, "egress_ip": egress, "socks": fmt.Sprintf("%s:%d", host, port)})
}

func (a *App) handleHedioumEgressIP(w http.ResponseWriter, r *http.Request) {
	host, port := a.tunnelSOCKS()
	ip := tunnel.EgressIP(host, port)
	hub := tunnel.HubAlive(host, port)
	resp := map[string]any{
		"hub_alive":  hub,
		"egress_ip":  ip,
		"socks":      fmt.Sprintf("%s:%d", host, port),
		"hed_role":   tunnel.HedRole(),  // "" = unconfigured, iran, foreign
		"hed_version": tunnel.HedVersion(),
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---- network tuning (BBR) ----

func (a *App) handleBBRStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, tunnel.DetectBBR())
}

func (a *App) handleBBRApply(w http.ResponseWriter, r *http.Request) {
	st, err := tunnel.ApplyBBR()
	if err != nil {
		a.St.Audit(actorFrom(r.Context()), "bbr.apply", trimAudit(err.Error()), "error")
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "status": st})
		return
	}
	detail := "BBR active"
	if !st.Active {
		detail = "sysctl applied, BBR not active (kernel too old?)"
	}
	a.St.Audit(actorFrom(r.Context()), "bbr.apply", detail, "ok")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": st, "note": detail})
}

// ---- firewall ----

func (a *App) handleFirewallStatus(w http.ResponseWriter, r *http.Request) {
	kind := tunnel.DetectFirewall()
	writeJSON(w, http.StatusOK, map[string]any{"kind": string(kind)})
}

func (a *App) handleFirewallAllow(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Port   int    `json:"port"`
		Source string `json:"source"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	kind := tunnel.DetectFirewall()
	msg, err := tunnel.AllowPort(kind, body.Port, strings.TrimSpace(body.Source))
	if err != nil {
		a.St.Audit(actorFrom(r.Context()), "firewall.allow", trimAudit(err.Error()), "error")
		errJSON(w, errString(err.Error()), http.StatusUnprocessableEntity)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "firewall.allow", fmt.Sprintf("%s: port %d tcp", kind, body.Port), "ok")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "firewall": string(kind), "message": msg})
}

// ---- shared helpers ----

// netParseIPRelay is a tiny local wrapper so this file does not import net
// just for one call.
func netParseIPRelay(s string) bool {
	if s == "" {
		return false
	}
	for _, part := range strings.Split(s, ".") {
		if part == "" {
			return false
		}
	}
	return isIPv4Literal(s)
}

// isIPv4Literal validates a strict dotted-quad IPv4.
func isIPv4Literal(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		if len(p) == 0 || len(p) > 3 {
			return false
		}
		n := 0
		for _, c := range p {
			if c < '0' || c > '9' {
				return false
			}
			n = n*10 + int(c-'0')
		}
		if n > 255 {
			return false
		}
	}
	return true
}

func trimAudit(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
