package api

import (
	
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"

	"portguard/internal/ops"
	"portguard/internal/proxy"
	"portguard/internal/service"
	"portguard/internal/store"
)

// Handlers for the v2 feature set ported from haproxy-manager:
// service control, HAProxy runtime API, backups, diagnostics,
// config view/diff, templates, import/export and certificate validation.

// ---------- service management ----------

func (a *App) handleServiceStatus(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{}
	for _, engine := range []string{"nginx", "haproxy"} {
		mgr, err := a.Svc.ManagerFor(engine)
		if err != nil {
			continue
		}
		status, serr := mgr.Status()
		binInstalled := !service.BinaryMissing(engine, a.Svc.Paths)
		entry := map[string]any{"unit": engine, "binary_installed": binInstalled}
		if serr != nil {
			// systemctl "show" fails for missing units — report as inactive
			entry["active"] = "unknown"
			entry["error"] = serr.Error()
		} else {
			for k, v := range status {
				entry[k] = v
			}
		}
		out[engine] = entry
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) handleServiceAction(w http.ResponseWriter, r *http.Request) {
	engine := chi.URLParam(r, "engine")
	action := chi.URLParam(r, "action")
	if engine != "nginx" && engine != "haproxy" {
		errJSON(w, errors.New("unknown engine"), http.StatusNotFound)
		return
	}
	if action == "reload" {
		// safe reload: validate the generated config first (haproxy-manager behaviour)
		if err := a.Svc.ValidateOne(engine); err != nil {
			errJSON(w, fmt.Errorf("reload rejected — validation failed: %w", err), http.StatusUnprocessableEntity)
			return
		}
	}
	mgr, err := a.Svc.ManagerFor(engine)
	if err != nil {
		errJSON(w, err, http.StatusNotFound)
		return
	}
	out, err := mgr.Action(action)
	detail := fmt.Sprintf("%s %s", action, engine)
	if err != nil {
		a.St.Audit(actorFrom(r.Context()), "service.action", detail+" failed: "+err.Error(), "error")
		errJSON(w, err, http.StatusUnprocessableEntity)
		return
	}
	if strings.TrimSpace(out) != "" {
		detail += ": " + strings.TrimSpace(out)
	}
	a.St.Audit(actorFrom(r.Context()), "service.action", detail, "ok")
	a.Broker.Publish("service", map[string]string{"engine": engine, "action": action})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "output": strings.TrimSpace(out)})
}

// ---------- HAProxy runtime API ----------

func (a *App) handleRuntimeHAProxy(w http.ResponseWriter, r *http.Request) {
	client := &proxy.RuntimeClient{SocketPath: a.Svc.Paths.HAProxySocket}
	info, err := client.ShowInfo()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"available": false, "socket": a.Svc.Paths.HAProxySocket, "error": err.Error(),
		})
		return
	}
	rows, serr := client.ShowStat()
	resp := map[string]any{
		"available": true,
		"socket":    a.Svc.Paths.HAProxySocket,
		"info":      info,
	}
	if serr == nil {
		resp["summary"] = proxy.SummaryStats(rows)
		resp["servers"] = serverRows(rows)
	} else {
		resp["stats_error"] = serr.Error()
	}
	writeJSON(w, http.StatusOK, resp)
}

// serverRows keeps only real server rows (skips FRONTEND/BACKEND aggregates).
func serverRows(rows []proxy.StatRow) []proxy.StatRow {
	out := []proxy.StatRow{}
	for _, r := range rows {
		if r["svname"] == "FRONTEND" || r["svname"] == "BACKEND" {
			continue
		}
		out = append(out, r)
	}
	return out
}

func (a *App) handleRuntimeServerState(w http.ResponseWriter, r *http.Request) {
	var body struct {
		State string `json:"state"` // ready | drain | maint
	}
	if !readJSON(w, r, &body) {
		return
	}
	client := &proxy.RuntimeClient{SocketPath: a.Svc.Paths.HAProxySocket}
	backend, server := chi.URLParam(r, "backend"), chi.URLParam(r, "server")
	if err := client.SetServerState(backend, server, body.State); err != nil {
		a.St.Audit(actorFrom(r.Context()), "runtime.server_state",
			fmt.Sprintf("set server %s/%s state %s failed: %v", backend, server, body.State, err), "error")
		errJSON(w, err, http.StatusUnprocessableEntity)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "runtime.server_state",
		fmt.Sprintf("set server %s/%s state %s", backend, server, body.State), "ok")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---------- backups ----------

func (a *App) handleListBackups(w http.ResponseWriter, r *http.Request) {
	bk := serviceBackups(a)
	list, err := bk.List()
	if err != nil {
		errJSON(w, err, 500)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (a *App) handleBackupDiff(w http.ResponseWriter, r *http.Request) {
	bk := serviceBackups(a)
	diffs, err := bk.Diff(chi.URLParam(r, "ts"))
	if err != nil {
		errJSON(w, err, http.StatusUnprocessableEntity)
		return
	}
	writeJSON(w, http.StatusOK, diffs)
}

func (a *App) handleBackupRestore(w http.ResponseWriter, r *http.Request) {
	bk := serviceBackups(a)
	ts := chi.URLParam(r, "ts")
	if err := bk.Restore(actorFrom(r.Context()), ts); err != nil {
		errJSON(w, err, http.StatusUnprocessableEntity)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "restored"})
}

func (a *App) handleBackupDelete(w http.ResponseWriter, r *http.Request) {
	bk := serviceBackups(a)
	ts := chi.URLParam(r, "ts")
	if err := bk.Delete(ts); err != nil {
		errJSON(w, err, http.StatusUnprocessableEntity)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "backup.delete", "backup "+ts+" deleted", "ok")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func serviceBackups(a *App) *service.Backups {
	return &service.Backups{Svc: a.Svc, St: a.St}
}

// ---------- diagnostics ----------

func (a *App) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Check      string `json:"check"` // tcp | dns | tls | http | backend
		Host       string `json:"host"`
		Port       int    `json:"port"`
		UseTLS     bool   `json:"use_tls"`
		Path       string `json:"path"`
		HostHeader string `json:"host_header"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if body.Host == "" {
		errJSON(w, errors.New("host is required"), http.StatusBadRequest)
		return
	}
	if err := validateDiagHost(body.Host); err != nil {
		errJSON(w, err, http.StatusUnprocessableEntity)
		return
	}
	actor := actorFrom(r.Context())
	var result any
	switch body.Check {
	case "tcp":
		if body.Port < 1 || body.Port > 65535 {
			errJSON(w, errors.New("port must be 1-65535"), http.StatusUnprocessableEntity)
			return
		}
		result = ops.TCPCheck(body.Host, body.Port, 0)
	case "dns":
		result = ops.DNSResolve(body.Host)
	case "tls":
		result = ops.TLSCheck(body.Host, orDefault(body.Port, 443), 0)
	case "http":
		result = ops.HTTPCheck(body.Host, orDefault(body.Port, 80), body.UseTLS, body.Path, body.HostHeader, 0)
	case "backend":
		if body.Port < 1 || body.Port > 65535 {
			errJSON(w, errors.New("port must be 1-65535"), http.StatusUnprocessableEntity)
			return
		}
		result = ops.BackendTest(body.Host, body.Port, body.UseTLS, body.Path, body.HostHeader)
	default:
		errJSON(w, errors.New("check must be one of tcp, dns, tls, http, backend"), http.StatusBadRequest)
		return
	}
	a.St.Audit(actor, "diagnostics", body.Check+" "+body.Host, "ok")
	writeJSON(w, http.StatusOK, result)
}

func validateDiagHost(host string) error {
	if len(host) > 253 || strings.ContainsAny(host, " \t/\\@") {
		return fmt.Errorf("invalid host %q", host)
	}
	return nil
}

func orDefault(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

// ---------- live config view ----------

func (a *App) handleConfigView(w http.ResponseWriter, r *http.Request) {
	engine := chi.URLParam(r, "engine")
	eng, ok := a.Svc.Engines[engine]
	if !ok {
		errJSON(w, errors.New("unknown engine"), http.StatusNotFound)
		return
	}
	type configFile struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Exists  bool   `json:"exists"`
	}
	files := []configFile{}
	for _, rel := range stagedOrder(eng) {
		live := eng.LivePath(a.Svc.Paths, rel)
		data, err := os.ReadFile(live)
		files = append(files, configFile{Path: live, Content: string(data), Exists: err == nil})
	}
	writeJSON(w, http.StatusOK, map[string]any{"engine": engine, "files": files})
}

// stagedOrder returns the engine's staged rel paths in stable order.
func stagedOrder(eng proxy.Engine) []string {
	for _, e := range []struct {
		name  string
		paths []string
	}{
		{"nginx", []string{"nginx.conf", "portguard/http.conf", "portguard/stream.conf"}},
		{"haproxy", []string{"haproxy.cfg"}},
	} {
		if e.name == eng.Name() {
			return e.paths
		}
	}
	return nil
}

// ---------- templates ----------

func (a *App) handleTemplates(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, ops.Templates())
}

// ---------- import / export ----------

func (a *App) handleExport(w http.ResponseWriter, r *http.Request) {
	ms, err := a.St.ListMappings()
	if err != nil {
		errJSON(w, err, 500)
		return
	}
	certs, _ := a.St.ListCerts()
	certMeta := []map[string]any{}
	for _, c := range certs {
		certMeta = append(certMeta, map[string]any{
			"id": c.ID, "name": c.Name, "type": c.Type, "domains": c.Domains,
			"expires_at": c.ExpiresAt,
			// private keys and PEM bodies are intentionally not exported
		})
	}
	snapshot := map[string]any{
		"version":   2,
		"tool":      "portguard",
		"mappings":  ms,
		"cert_meta": certMeta,
	}
	w.Header().Set("Content-Disposition", "attachment; filename=portguard-snapshot.json")
	writeJSON(w, http.StatusOK, snapshot)
}

func (a *App) handleImport(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mappings []store.Mapping `json:"mappings"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	all, err := a.St.ListMappings()
	if err != nil {
		errJSON(w, err, 500)
		return
	}
	existing := map[string]bool{}
	for _, m := range all {
		existing[m.Name] = true
	}
	created, skipped := 0, 0
	var errorsList []string
	for i, m := range body.Mappings {
		m.ID = 0
		m.CreatedAt, m.UpdatedAt = timeNow(), timeNow()
		if existing[m.Name] {
			skipped++
			continue
		}
		if err := proxy.ValidateMapping(m, all, a.PanelPort); err != nil {
			errorsList = append(errorsList, fmt.Sprintf("#%d %s: %v", i+1, m.Name, err))
			continue
		}
		id, err := a.St.CreateMapping(&m)
		if err != nil {
			errorsList = append(errorsList, fmt.Sprintf("%s: %v", m.Name, err))
			continue
		}
		m.ID = id // keep the conflict check aware of imported mappings
		all = append(all, m)
		existing[m.Name] = true
		created++
		_ = id
	}
	a.St.Audit(actorFrom(r.Context()), "import",
		fmt.Sprintf("imported %d mapping(s), skipped %d, errors %d", created, skipped, len(errorsList)), "ok")
	writeJSON(w, http.StatusOK, map[string]any{
		"created": created, "skipped": skipped, "errors": errorsList,
	})
}

// ---------- certificate validation ----------

func (a *App) handleCertValidate(w http.ResponseWriter, r *http.Request) {
	id := idParam(r)
	c, err := a.St.GetCert(id)
	if err != nil {
		errJSON(w, err, http.StatusNotFound)
		return
	}
	res := validateCertPEMAndKey(c.CertPEM, c.KeyPEM)
	a.St.Audit(actorFrom(r.Context()), "cert.validate",
		fmt.Sprintf("certificate #%d %s: %v", id, c.Name, res["overall"]), "ok")
	writeJSON(w, http.StatusOK, res)
}

// validateCertPEMAndKey parses the certificate and verifies the key matches
// (like haproxy-manager tls/certificate.check_key_matches_cert).
func validateCertPEMAndKey(certPEM, keyPEM string) map[string]any {
	res := map[string]any{"overall": false}
	cert, err := parseCertBlock(certPEM)
	if err != nil {
		res["error"] = err.Error()
		return res
	}
	now := timeNow()
	daysLeft := int(cert.NotAfter.Sub(now).Hours() / 24)
	certInfo := map[string]any{
		"subject":        cert.Subject.String(),
		"issuer":         cert.Issuer.String(),
		"not_before":     cert.NotBefore.Format("2006-01-02"),
		"not_after":      cert.NotAfter.Format("2006-01-02"),
		"days_left":      daysLeft,
		"expired":        now.After(cert.NotAfter),
		"near_expiry":    daysLeft >= 0 && daysLeft < 30,
		"dns_names":      cert.DNSNames,
		"key_algorithm":  cert.PublicKeyAlgorithm.String(),
		"serial_number":  cert.SerialNumber.String(),
	}
	res["cert"] = certInfo

	if keyPEM == "" {
		res["key_match"] = map[string]any{"ok": false, "msg": "no private key stored"}
		res["overall"] = !now.After(cert.NotAfter)
		return res
	}
	keyPub, err := publicKeyOfKeyPEM(keyPEM)
	if err != nil {
		res["key_match"] = map[string]any{"ok": false, "msg": err.Error()}
		return res
	}
	certPub, okPub := publicKeyOfCert(cert)
	if !okPub {
		res["key_match"] = map[string]any{"ok": false, "msg": "unsupported certificate public key type"}
		return res
	}
	if keysEqual(certPub, keyPub) {
		res["key_match"] = map[string]any{"ok": true, "msg": "certificate and key match"}
	} else {
		res["key_match"] = map[string]any{"ok": false, "msg": "certificate and key DO NOT match"}
		return res
	}
	res["overall"] = !now.After(cert.NotAfter)
	return res
}

var _ = store.ErrNotFound
