package api

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"portguard/internal/conntrack"
	"portguard/internal/proxy"
	"portguard/internal/ratelimit"
	"portguard/internal/store"
	"portguard/internal/sysinfo"
	"portguard/internal/tools"
	"portguard/internal/tunnel"
)

// AgentRouter is the HTTP surface a managed PortGuard node exposes to its
// master. It is the same /api/node/* contract as before, extended with the
// operations the master UI proxies: mapping CRUD, certificate upload and
// remote apply — so a new server needs only the agent binary + token, never
// a full panel install.
//
// Auth: every route requires the master token (setting "node_token") via
// the Authorization: Bearer header. No admin account, no JWT, no web UI.
type Agent struct {
	App *App
}

func (ag *Agent) authed(r *http.Request) bool {
	tok := ag.App.St.GetSettingOr("node_token", "")
	if tok == "" {
		return false
	}
	return r.Header.Get("Authorization") == "Bearer "+tok
}

func (ag *Agent) guard(w http.ResponseWriter, r *http.Request) bool {
	if !ag.authed(r) {
		unauthorized(w)
		return false
	}
	return true
}

func (ag *Agent) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if !ag.guard(w, req) {
				return
			}
			next.ServeHTTP(w, req)
		})
	})

	r.Get("/ping", ag.ping)
	r.Get("/summary", ag.summary)
	r.Get("/connections", ag.connections)
	r.Post("/apply", ag.apply)
	r.Post("/validate", ag.validate)

	// mapping CRUD (mirror of the panel endpoints, master-driven)
	r.Get("/mappings", ag.listMappings)
	r.Post("/mappings", ag.createMapping)
	r.Put("/mappings/{id}", ag.updateMapping)
	r.Delete("/mappings/{id}", ag.deleteMapping)

	// certificates: the master uploads PEM pairs for https mappings
	r.Get("/certs", ag.listCerts)
	r.Post("/certs", ag.createCert)
	r.Delete("/certs/{id}", ag.deleteCert)

	// health + ports snapshots for the master overview
	r.Get("/health", ag.healthList)
	r.Get("/ports", ag.ports)

	// tools
	r.Get("/tools", ag.listTools)
	r.Post("/tools/{tool}/install", ag.installTool)

	// tunnel state
	r.Get("/tunnel", ag.tunnelState)
	// v2.13: the master can also drive the trojan suite, tuning and firewall
	// on managed nodes — same contract as the local endpoints.
	r.Get("/trojan/relays", ag.trojanRelays)
	r.Get("/trojan/ingresses", ag.trojanIngresses)
	r.Post("/trojan/bridge/apply", ag.trojanBridgeApply)
	r.Post("/trojan/ingress/apply", ag.trojanIngressApply)
	r.Get("/tuning/bbr", ag.bbrStatus)
	r.Post("/tuning/bbr/apply", ag.bbrApply)
	r.Get("/firewall", ag.firewallStatus)
	r.Post("/firewall/allow", ag.firewallAllow)

	// rate limiting (bandwidth plans pushed by the master)
	r.Post("/ratelimit/apply", ag.ratelimitApply)
	r.Get("/ratelimit/state", ag.ratelimitState)
	r.Post("/ratelimit/clear", ag.ratelimitClear)

	// logs: bounded tails of allowlisted sources only — no free-form paths
	r.Get("/logs/{source}", ag.logs)

	return r
}

// ---- basics ----

func (ag *Agent) ping(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": ag.App.Version, "role": ag.role()})
}

func (ag *Agent) role() string {
	return ag.App.St.GetSettingOr("node_role", "generic")
}

func (ag *Agent) summary(w http.ResponseWriter, r *http.Request) {
	a := ag.App
	s := sysinfo.SnapshotNow()
	mappings, _ := a.St.ListMappings()
	ports, _ := a.St.ListPorts()
	healths, _ := a.St.ListHealth()

	unmanaged := 0
	for _, p := range ports {
		if !p.Self && !p.Managed {
			unmanaged++
		}
	}
	up, down := 0, 0
	for _, h := range healths {
		if h.Status == "up" {
			up++
		} else if h.Status == "down" {
			down++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version": a.Version,
		"role":    ag.role(),
		"system":  s,
		"mappings": map[string]int{
			"total": len(mappings), "enabled": countEnabled(mappings),
		},
		"ports":  map[string]int{"total": len(ports), "unmanaged": unmanaged},
		"health": map[string]int{"up": up, "down": down},
		"tunnel": tunnel.Detect(),
	})
}

func (ag *Agent) connections(w http.ResponseWriter, r *http.Request) {
	a := ag.App
	conns, err := a.St.ListConnections()
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	mappings, _ := a.St.ListMappings()
	conntrack.EnrichManaged(conns, mappings, a.PanelPort)
	talkers, _ := a.St.TopTalkers(10)
	writeJSON(w, http.StatusOK, map[string]any{
		"connections": conns,
		"top_talkers": talkers,
		"total":       len(conns),
	})
}

func (ag *Agent) apply(w http.ResponseWriter, r *http.Request) {
	if err := ag.App.Svc.ApplyAll("master"); err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (ag *Agent) validate(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, ag.App.Svc.ValidateOnly("master"))
}

// ---- mappings ----

func (ag *Agent) listMappings(w http.ResponseWriter, r *http.Request) {
	ms, err := ag.App.St.ListMappings()
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	if ms == nil {
		ms = []store.Mapping{}
	}
	writeJSON(w, http.StatusOK, ms)
}

func (ag *Agent) createMapping(w http.ResponseWriter, r *http.Request) {
	a := ag.App
	m, ok := a.mappingFromBody(w, r)
	if !ok {
		return
	}
	if !a.certExists(w, m) {
		return
	}
	all, err := a.St.ListMappings()
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	if err := proxy.ValidateMapping(m, all, a.PanelPort); err != nil {
		errJSON(w, err, http.StatusUnprocessableEntity)
		return
	}
	id, err := a.St.CreateMapping(&m)
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	m.ID = id
	a.St.Audit("master", "mapping.create", "mapping #"+strconv.FormatInt(id, 10)+" "+m.Name, "ok")
	writeJSON(w, http.StatusCreated, m)
}

func (ag *Agent) updateMapping(w http.ResponseWriter, r *http.Request) {
	a := ag.App
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	m, ok := a.mappingFromBody(w, r)
	if !ok {
		return
	}
	m.ID = id
	if !a.certExists(w, m) {
		return
	}
	all, err := a.St.ListMappings()
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	if err := proxy.ValidateMapping(m, all, a.PanelPort); err != nil {
		errJSON(w, err, http.StatusUnprocessableEntity)
		return
	}
	if err := a.St.UpdateMapping(&m); err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	a.St.Audit("master", "mapping.update", "mapping #"+strconv.FormatInt(id, 10)+" "+m.Name, "ok")
	writeJSON(w, http.StatusOK, m)
}

func (ag *Agent) deleteMapping(w http.ResponseWriter, r *http.Request) {
	a := ag.App
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err := a.St.DeleteMapping(id); err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	a.St.Audit("master", "mapping.delete", "mapping #"+strconv.FormatInt(id, 10), "ok")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- certs ----

func (ag *Agent) listCerts(w http.ResponseWriter, r *http.Request) {
	certs, err := ag.App.St.ListCerts()
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	// scrub PEM bodies: the master only needs id/name/type/expiry
	type certMeta struct {
		ID        int64      `json:"id"`
		Name      string     `json:"name"`
		Type      string     `json:"type"`
		Domains   []string   `json:"domains"`
		ExpiresAt *time.Time `json:"expires_at"`
	}
	out := make([]certMeta, 0, len(certs))
	for _, c := range certs {
		out = append(out, certMeta{ID: c.ID, Name: c.Name, Type: c.Type, Domains: c.Domains, ExpiresAt: c.ExpiresAt})
	}
	writeJSON(w, http.StatusOK, out)
}

func (ag *Agent) createCert(w http.ResponseWriter, r *http.Request) {
	// request shape: PEM bodies are writable (the store model tags them "-"
	// so they never leak back out)
	var body struct {
		Name    string   `json:"name"`
		Type    string   `json:"type"`
		CertPEM string   `json:"cert_pem"`
		KeyPEM  string   `json:"key_pem"`
		Domains []string `json:"domains"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if body.CertPEM == "" || body.KeyPEM == "" {
		errJSON(w, errString("cert_pem and key_pem are required"), http.StatusUnprocessableEntity)
		return
	}
	if body.Type == "" {
		body.Type = "manual"
	}
	c := store.Cert{
		Name: body.Name, Type: body.Type, CertPEM: body.CertPEM, KeyPEM: body.KeyPEM, Domains: body.Domains,
	}
	id, err := ag.App.St.CreateCert(&c)
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	ag.App.St.Audit("master", "cert.create", "cert #"+strconv.FormatInt(id, 10)+" "+c.Name, "ok")
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (ag *Agent) deleteCert(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err := ag.App.St.DeleteCert(id); err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	ag.App.St.Audit("master", "cert.delete", "cert #"+strconv.FormatInt(id, 10), "ok")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- health / ports / tools / tunnel ----

func (ag *Agent) healthList(w http.ResponseWriter, r *http.Request) {
	hs, err := ag.App.St.ListHealth()
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, hs)
}

func (ag *Agent) ports(w http.ResponseWriter, r *http.Request) {
	ports, err := ag.App.St.ListPorts()
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, ports)
}

func (ag *Agent) listTools(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"tools": tools.DetectAll()})
}

func (ag *Agent) installTool(w http.ResponseWriter, r *http.Request) {
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
	ag.App.St.Audit("master", "tool.install "+id, "remote install", status)
	writeJSON(w, http.StatusOK, res)
}

func (ag *Agent) tunnelState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, tunnel.Detect())
}

// ---- v2.13: trojan suite / tuning / firewall on this node ----
// The remote handlers reuse the local App methods: the payload shapes and
// validation are identical, so the master gets exactly what a local admin
// gets. (The functions live in trojan.go.)

func (ag *Agent) trojanRelays(w http.ResponseWriter, r *http.Request)  { ag.App.handleListTrojanRelays(w, r) }
func (ag *Agent) trojanIngresses(w http.ResponseWriter, r *http.Request) { ag.App.handleListTrojanIngresses(w, r) }
func (ag *Agent) trojanBridgeApply(w http.ResponseWriter, r *http.Request) {
	ag.App.handleTrojanBridgeApply(w, r)
}
func (ag *Agent) trojanIngressApply(w http.ResponseWriter, r *http.Request) {
	ag.App.handleTrojanIngressApply(w, r)
}
func (ag *Agent) bbrStatus(w http.ResponseWriter, r *http.Request)  { ag.App.handleBBRStatus(w, r) }
func (ag *Agent) bbrApply(w http.ResponseWriter, r *http.Request)   { ag.App.handleBBRApply(w, r) }
func (ag *Agent) firewallStatus(w http.ResponseWriter, r *http.Request) { ag.App.handleFirewallStatus(w, r) }
func (ag *Agent) firewallAllow(w http.ResponseWriter, r *http.Request)  { ag.App.handleFirewallAllow(w, r) }

// ---- rate limiting (bandwidth enforcement on this node) ----

// ratelimitApply reconciles the node's Linux tc state with the pushed plan.
// Idempotent and incremental: only changed/removed rules are touched, and
// a plan identical to the applied state is a no-op.
func (ag *Agent) ratelimitApply(w http.ResponseWriter, r *http.Request) {
	var plan ratelimit.Plan
	if !readJSON(w, r, &plan) {
		return
	}
	ap := ag.App.RateApplier()
	st, err := ap.ApplyPlan(plan)
	if err != nil {
		ag.App.St.Audit("master", "ratelimit.apply", err.Error(), "error")
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	ag.App.St.Audit("master", "ratelimit.apply", fmt.Sprintf("%d rules active", len(st.Rules)), "ok")
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"iface":   st.Iface,
		"rules":   len(st.Rules),
		"version": plan.Version,
	})
}

func (ag *Agent) ratelimitState(w http.ResponseWriter, r *http.Request) {
	st := ag.App.RateApplier().LoadState()
	writeJSON(w, http.StatusOK, map[string]any{
		"iface": st.Iface,
		"rules": st.SortedRules(),
	})
}

func (ag *Agent) ratelimitClear(w http.ResponseWriter, r *http.Request) {
	if err := ag.App.RateApplier().ClearAll(); err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	ag.App.St.Audit("master", "ratelimit.clear", "all bandwidth rules removed", "ok")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- logs: bounded tails of allowlisted sources only ----

// logSources is the fixed allowlist the master can tail. No free-form
// filesystem access: paths never come from the request.
var logSources = map[string]struct {
	args   []string // journalctl args or a static file path
	isFile bool
}{
	"nginx":   {isFile: true},
	"haproxy": {isFile: true},
	"agent":   {args: []string{"journalctl", "-u", "portguard-agent", "-n", "200", "--no-pager"}},
	"panel":   {args: []string{"journalctl", "-u", "portguard", "-n", "200", "--no-pager"}},
	"syslog":  {args: []string{"journalctl", "-n", "200", "--no-pager"}},
	"xray":    {args: []string{"journalctl", "-u", "xray", "-n", "200", "--no-pager"}},
	"hedioum": {args: []string{"journalctl", "-u", "hedioum", "-n", "200", "--no-pager"}},
	"bridge":  {args: []string{"journalctl", "-u", "portguard-tunnel-bridge", "-n", "200", "--no-pager"}},
}

var logFilePaths = map[string]string{
	"nginx":   "/var/log/nginx/error.log",
	"haproxy": "/var/log/haproxy/haproxy.log",
}

// logs returns the tail of one allowlisted source. maxLines is clamped so a
// master can never demand unbounded output.
func (ag *Agent) logs(w http.ResponseWriter, r *http.Request) {
	source := chi.URLParam(r, "source")
	src, ok := logSources[source]
	if !ok {
		errJSON(w, errString("unknown log source"), http.StatusNotFound)
		return
	}
	maxLines := 200
	if v := r.URL.Query().Get("lines"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			maxLines = n
		}
	}

	var out string
	if src.isFile {
		path := logFilePaths[source]
		data, err := os.ReadFile(path)
		if err != nil {
			out = "(log file not readable: " + path + ")"
		} else {
			out = tailLines(string(data), maxLines)
		}
	} else {
		args := append([]string{}, src.args...)
		// patch the -n value for the requested size
		for i, a := range args {
			if a == "200" {
				args[i] = strconv.Itoa(maxLines)
			}
		}
		cmd := exec.Command(args[0], args[1:]...)
		data, err := cmd.Output()
		if err != nil {
			out = "(source unavailable: " + err.Error() + ")"
		} else {
			out = tailLines(string(data), maxLines)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"source": source, "lines": out})
}

// tailLines keeps the last n lines of s.
func tailLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
