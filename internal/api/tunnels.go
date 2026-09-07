package api

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"portguard/internal/store"
	"portguard/internal/tunnel"
)

// ---- tunnel environment & relays ----

func (a *App) handleTunnelStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, tunnel.Detect())
}

func (a *App) handleListRelays(w http.ResponseWriter, r *http.Request) {
	relays, err := a.St.ListTunnelRelays()
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, relays)
}

func validRelay(r store.TunnelRelay, certs map[int64]bool) error {
	if strings.TrimSpace(r.Name) == "" {
		return errString("name is required")
	}
	if r.Mode != "raw" && r.Mode != "tls" {
		return errString("mode must be raw or tls")
	}
	if r.TargetHost == "" {
		return errString("target_host is required")
	}
	if r.TargetPort < 1 || r.TargetPort > 65535 {
		return errString("target_port must be 1-65535")
	}
	if r.Mode == "raw" {
		if r.ListenPort < 1 || r.ListenPort > 65535 {
			return errString("raw mode requires listen_port 1-65535")
		}
		if r.ListenIP == "" {
			r.ListenIP = "0.0.0.0"
		}
	} else {
		if r.BridgePort < 1 || r.BridgePort > 65535 {
			return errString("tls mode requires bridge_port 1-65535")
		}
		if r.Domain == "" {
			return errString("tls mode requires domain")
		}
		if r.SSLCertID == nil || !certs[*r.SSLCertID] {
			return errString("tls mode requires a valid ssl_cert_id")
		}
		if r.HostHeader == "" {
			return errString("tls mode requires host_header (the foreign node's Host)")
		}
	}
	return nil
}

func errString(s string) error { return &validationError{s} }

type validationError struct{ msg string }

func (e *validationError) Error() string { return e.msg }

func (a *App) certsByID() map[int64]bool {
	certs, _ := a.St.ListCerts()
	m := map[int64]bool{}
	for _, c := range certs {
		m[c.ID] = true
	}
	return m
}

func (a *App) handleCreateRelay(w http.ResponseWriter, r *http.Request) {
	var rel store.TunnelRelay
	if !readJSON(w, r, &rel) {
		return
	}
	if rel.ListenIP == "" {
		rel.ListenIP = "0.0.0.0"
	}
	if err := validRelay(rel, a.certsByID()); err != nil {
		errJSON(w, err, http.StatusUnprocessableEntity)
		return
	}
	// unique name + unique listen/bridge ports
	existing, _ := a.St.ListTunnelRelays()
	for _, e := range existing {
		if e.Name == rel.Name {
			errJSON(w, errString("a relay with this name already exists"), http.StatusConflict)
			return
		}
		if e.Mode == rel.Mode && e.Mode == "raw" && e.ListenPort == rel.ListenPort {
			errJSON(w, errString("listen_port already used by relay "+e.Name), http.StatusConflict)
			return
		}
		if e.Mode == rel.Mode && e.Mode == "tls" && e.BridgePort == rel.BridgePort {
			errJSON(w, errString("bridge_port already used by relay "+e.Name), http.StatusConflict)
			return
		}
	}
	id, err := a.St.CreateTunnelRelay(&rel)
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "tunnel.relay.create", "relay "+rel.Name, "ok")
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (a *App) handleUpdateRelay(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	cur, err := a.St.GetTunnelRelay(id)
	if err != nil {
		errJSON(w, err, http.StatusNotFound)
		return
	}
	var rel store.TunnelRelay
	if !readJSON(w, r, &rel) {
		return
	}
	rel.ID = id
	if rel.ListenIP == "" {
		rel.ListenIP = "0.0.0.0"
	}
	if err := validRelay(rel, a.certsByID()); err != nil {
		errJSON(w, err, http.StatusUnprocessableEntity)
		return
	}
	existing, _ := a.St.ListTunnelRelays()
	for _, e := range existing {
		if e.ID == id {
			continue
		}
		if e.Name == rel.Name {
			errJSON(w, errString("a relay with this name already exists"), http.StatusConflict)
			return
		}
		if e.Mode == rel.Mode && e.Mode == "raw" && e.ListenPort == rel.ListenPort {
			errJSON(w, errString("listen_port already used by relay "+e.Name), http.StatusConflict)
			return
		}
		if e.Mode == rel.Mode && e.Mode == "tls" && e.BridgePort == rel.BridgePort {
			errJSON(w, errString("bridge_port already used by relay "+e.Name), http.StatusConflict)
			return
		}
	}
	if err := a.St.UpdateTunnelRelay(&rel); err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	_ = cur
	a.St.Audit(actorFrom(r.Context()), "tunnel.relay.update", "relay "+rel.Name, "ok")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleDeleteRelay(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err := a.St.DeleteTunnelRelay(id); err != nil {
		errJSON(w, err, http.StatusNotFound)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "tunnel.relay.delete", "relay #"+strconv.FormatInt(id, 10), "ok")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) tunnelSOCKS() (string, int) {
	host := a.St.GetSettingOr("tunnel_socks_host", "127.0.0.1")
	port := 40001
	if v := a.St.GetSettingOr("tunnel_socks_port", ""); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 65535 {
			port = n
		}
	}
	return host, port
}

func (a *App) handleTunnelValidate(w http.ResponseWriter, r *http.Request) {
	relays, err := a.St.ListTunnelRelays()
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	host, port := a.tunnelSOCKS()
	cfg, err := tunnel.BuildBridgeConfig(relays, host, port)
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	st := tunnel.Detect()
	if !st.XrayInstalled {
		errJSON(w, errString("xray is not installed on this server"), http.StatusUnprocessableEntity)
		return
	}
	if len(relays) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "note": "no relays configured"})
		return
	}
	if err := tunnel.Validate(cfg, st.XrayBinary); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleTunnelApply(w http.ResponseWriter, r *http.Request) {
	relays, err := a.St.ListTunnelRelays()
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	st := tunnel.Detect()
	if !st.XrayInstalled {
		errJSON(w, errString("xray is not installed on this server"), http.StatusUnprocessableEntity)
		return
	}
	host, port := a.tunnelSOCKS()
	cfg, err := tunnel.BuildBridgeConfig(relays, host, port)
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	if len(relays) > 0 {
		if err := tunnel.Validate(cfg, st.XrayBinary); err != nil {
			a.St.Audit(actorFrom(r.Context()), "tunnel.apply", err.Error(), "error")
			errJSON(w, err, http.StatusUnprocessableEntity)
			return
		}
	}
	// install the unit on first use
	unitPath := "/etc/systemd/system/" + tunnel.BridgeUnit
	if _, err := os.Stat(unitPath); err != nil {
		if err := tunnel.InstallUnit(st.XrayBinary, tunnel.BridgeConfPath); err != nil {
			errJSON(w, errString("failed to install bridge service: "+err.Error()), http.StatusInternalServerError)
			return
		}
	}
	var prev []byte
	if data, err := os.ReadFile(tunnel.BridgeConfPath); err == nil {
		prev = data
	}
	if _, err := tunnel.BackupBridge(a.Svc.Paths.BackupsDir); err != nil {
		// a failed pre-apply backup removes the rollback safety net,
		// so refuse to touch the live config
		a.St.Audit(actorFrom(r.Context()), "tunnel.apply", "backup failed: "+err.Error(), "error")
		errJSON(w, errString("backup failed, refusing to apply: "+err.Error()), http.StatusInternalServerError)
		return
	}
	if err := tunnel.Apply(cfg, prev, st.XrayBinary); err != nil {
		a.St.Audit(actorFrom(r.Context()), "tunnel.apply", err.Error(), "error")
		errJSON(w, errString(err.Error()+" (rolled back)"), http.StatusInternalServerError)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "tunnel.apply", "bridge applied", "ok")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "relays": len(relays)})
}

// ---- self-updater ----

func (a *App) handleUpdate(w http.ResponseWriter, r *http.Request) {
	// triggered from the panel; runs deploy/update.sh in the background
	script := "/opt/portguard/deploy/update.sh"
	if _, err := os.Stat(script); err != nil {
		errJSON(w, errString("updater script not found at "+script), http.StatusNotFound)
		return
	}
	cmd := exec.Command("bash", script)
	if err := cmd.Start(); err != nil {
		errJSON(w, errString("failed to start updater: "+err.Error()), http.StatusInternalServerError)
		return
	}
	// detach: the panel service will be restarted by the script itself
	go func() { _ = cmd.Wait() }()
	a.St.Audit(actorFrom(r.Context()), "update.start", "self-update started", "ok")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "note": "update started; the panel will restart shortly"})
}

// ---- ICMP tunnel (PingTunnel) — Iran ↔ foreign over ICMP echo ----

// handlePingTunnelStatus reports the ICMP tunnel environment.
func (a *App) handlePingTunnelStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, tunnel.DetectPingTunnel())
}

// handlePingTunnelInstall installs the pingtunnel core (pinned upstream release).
func (a *App) handlePingTunnelInstall(w http.ResponseWriter, r *http.Request) {
	if err := tunnel.InstallPingTunnelCore(""); err != nil {
		a.St.Audit(actorFrom(r.Context()), "pingtunnel.install", err.Error(), "error")
		errJSON(w, errString(err.Error()), http.StatusInternalServerError)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "pingtunnel.install", "core installed", "ok")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handlePingTunnelCreate provisions one tunnel side as a systemd unit.
// Body: {"side":"iran"|"foreign", "port":8443, "foreign_ip":"1.2.3.4", "target_port":8443}
func (a *App) handlePingTunnelCreate(w http.ResponseWriter, r *http.Request) {
	if !tunnel.DetectPingTunnel().Installed {
		errJSON(w, errString("pingtunnel core is not installed — install it first"), http.StatusUnprocessableEntity)
		return
	}
	var body struct {
		Side       string `json:"side"`
		Port       int    `json:"port"`
		ForeignIP  string `json:"foreign_ip"`
		TargetPort int    `json:"target_port"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	var side int
	switch body.Side {
	case "iran":
		side = 0
	case "foreign":
		side = 1
	default:
		errJSON(w, errString("side must be iran or foreign"), http.StatusUnprocessableEntity)
		return
	}
	// refuse duplicate ports
	for _, u := range tunnel.DetectPingTunnel().Services {
		if u.Role == "iran" && u.Port == body.Port {
			errJSON(w, errString(fmt.Sprintf("an ICMP tunnel on port %d already exists", body.Port)), http.StatusConflict)
			return
		}
	}
	name, unit, err := tunnel.BuildPingTunnelUnit(side, body.Port, body.ForeignIP, body.TargetPort)
	if err != nil {
		errJSON(w, errString(err.Error()), http.StatusUnprocessableEntity)
		return
	}
	if err := tunnel.InstallPingTunnelUnit(name, unit); err != nil {
		a.St.Audit(actorFrom(r.Context()), "pingtunnel.create", name+": "+err.Error(), "error")
		errJSON(w, errString("failed to install service: "+err.Error()), http.StatusInternalServerError)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "pingtunnel.create", name+" ("+body.Side+")", "ok")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "unit": name})
}

// handlePingTunnelDelete removes one pingtunnel unit.
func (a *App) handlePingTunnelDelete(w http.ResponseWriter, r *http.Request) {
	unit := chi.URLParam(r, "unit")
	if err := tunnel.DeletePingTunnelUnit(unit); err != nil {
		errJSON(w, errString(err.Error()), http.StatusBadRequest)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "pingtunnel.delete", unit, "ok")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handlePingTunnelCoreRemove deletes the core when no services remain.
func (a *App) handlePingTunnelCoreRemove(w http.ResponseWriter, r *http.Request) {
	if err := tunnel.RemovePingTunnelCore(); err != nil {
		errJSON(w, errString(err.Error()), http.StatusConflict)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "pingtunnel.core-remove", "core removed", "ok")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
