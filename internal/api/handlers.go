package api

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"portguard/internal/alerter"
	"portguard/internal/health"
	"portguard/internal/nodehub"
	"portguard/internal/proxy"
	"portguard/internal/ratelimit"
	"portguard/internal/scanner"
	"portguard/internal/service"
	"portguard/internal/store"
	"portguard/internal/sysinfo"
)

type App struct {
	St                 *store.Store
	Svc                *service.Service
	Auth               *Auth
	Broker             *Broker
	Scanner            *scanner.Scanner
	PanelPort          int
	Version            string
	RateApplierFactory func() *ratelimit.Applier
	Alerter            *alerter.Alerter
	Jobs               *JobManager
	Hub                *nodehub.Hub
}

// RateApplier returns the bandwidth applier for this host (agent or panel).
func (a *App) RateApplier() *ratelimit.Applier {
	if a.RateApplierFactory != nil {
		return a.RateApplierFactory()
	}
	return &ratelimit.Applier{}
}

type ctxKey int

const actorKey ctxKey = 1

func withActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, actorKey, actor)
}

func actorFrom(ctx context.Context) string {
	if v, ok := ctx.Value(actorKey).(string); ok {
		return v
	}
	return "system"
}

func spaHandler(dist fs.FS, fileServer http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(filepath.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(dist, p); err != nil {
			// SPA fallback
			r2 := new(http.Request)
			*r2 = *r
			r2.URL = &(*r.URL)
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		fileServer.ServeHTTP(w, r)
	}
}

// ---------- helpers ----------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return false
	}
	return true
}

func unauthorized(w http.ResponseWriter) {
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
}

func errJSON(w http.ResponseWriter, err error, status int) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ---------- public endpoints ----------

func (a *App) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": a.Version})
}

// ---------- system ----------

func (a *App) handleSystem(w http.ResponseWriter, r *http.Request) {
	s := sysinfo.SnapshotNow()

	mappings, _ := a.St.ListMappings()
	ports, _ := a.St.ListPorts()
	healths, _ := a.St.ListHealth()

	managedPorts := 0
	unmanaged := 0
	for _, p := range ports {
		if p.Self {
			continue
		}
		if p.Managed {
			managedPorts++
		} else {
			unmanaged++
		}
	}
	up, down, unknown := 0, 0, 0
	for _, h := range healths {
		switch h.Status {
		case "up":
			up++
		case "down":
			down++
		default:
			unknown++
		}
	}
	summary, _ := health.Summary(mappings, healths)
	writeJSON(w, http.StatusOK, map[string]any{
		"system":   s,
		"mappings": map[string]any{"total": len(mappings), "enabled": countEnabled(mappings)},
		"ports":    map[string]any{"managed": managedPorts, "unmanaged": unmanaged, "total": len(ports)},
		"health":   map[string]any{"up": up, "down": down, "unknown": unknown, "summary": summary},
		"version":  a.Version,
	})
}

func countEnabled(ms []store.Mapping) int {
	n := 0
	for _, m := range ms {
		if m.Enabled {
			n++
		}
	}
	return n
}

// ---------- mappings ----------

// pagedParams reads ?limit=&offset= (both optional, clamped to sane
// ranges). limit < 0 means "no limit" — the default, which keeps existing
// clients that never send the parameters working unchanged.
func pagedParams(r *http.Request) (limit, offset int) {
	limit, offset = -1, 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			offset = n
		}
	}
	if limit > 500 {
		limit = 500 // sensible maximum for a dashboard
	}
	return limit, offset
}

func (a *App) handleListMappings(w http.ResponseWriter, r *http.Request) {
	limit, offset := pagedParams(r)
	ms, err := a.St.ListMappingsPaged(limit, offset)
	if err != nil {
		errJSON(w, err, 500)
		return
	}
	if ms == nil {
		ms = []store.Mapping{}
	}
	writeJSON(w, http.StatusOK, ms)
}

func (a *App) mappingFromBody(w http.ResponseWriter, r *http.Request) (store.Mapping, bool) {
	var m store.Mapping
	if !readJSON(w, r, &m) {
		return m, false
	}
	if m.ListenIP == "" {
		m.ListenIP = "0.0.0.0"
	}
	if m.Engine == "" {
		m.Engine = "nginx"
	}
	if m.Protocol == "" {
		m.Protocol = "http"
	}
	if m.ExtraHeaders == nil {
		m.ExtraHeaders = map[string]string{}
	}
	if m.Targets == nil {
		m.Targets = []store.Target{}
	}
	return m, true
}

func (a *App) handleCreateMapping(w http.ResponseWriter, r *http.Request) {
	m, ok := a.mappingFromBody(w, r)
	if !ok {
		return
	}
	if !a.certExists(w, m) {
		return
	}
	all, err := a.St.ListMappings()
	if err != nil {
		errJSON(w, err, 500)
		return
	}
	if err := proxy.ValidateMapping(m, all, a.PanelPort); err != nil {
		errJSON(w, err, http.StatusUnprocessableEntity)
		return
	}
	id, err := a.St.CreateMapping(&m)
	if err != nil {
		errJSON(w, err, 500)
		return
	}
	m.ID = id
	a.St.Audit(actorFrom(r.Context()), "mapping.create", "mapping #"+itoa(id)+" "+m.Name+" ("+m.Engine+" "+m.Protocol+" :"+itoa(int64(m.ListenPort))+")", "ok")
	a.maybeAutoApply(actorFrom(r.Context()))
	writeJSON(w, http.StatusCreated, m)
}

func (a *App) handleUpdateMapping(w http.ResponseWriter, r *http.Request) {
	id := idParam(r)
	if id == 0 {
		errJSON(w, errors.New("bad id"), http.StatusBadRequest)
		return
	}
	m, ok2 := a.mappingFromBody(w, r)
	if !ok2 {
		return
	}
	m.ID = id
	if !a.certExists(w, m) {
		return
	}
	all, err := a.St.ListMappings()
	if err != nil {
		errJSON(w, err, 500)
		return
	}
	if err := proxy.ValidateMapping(m, all, a.PanelPort); err != nil {
		errJSON(w, err, http.StatusUnprocessableEntity)
		return
	}
	if err := a.St.UpdateMapping(&m); err != nil {
		errJSON(w, err, 500)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "mapping.update", "mapping #"+itoa(id)+" "+m.Name, "ok")
	a.maybeAutoApply(actorFrom(r.Context()))
	writeJSON(w, http.StatusOK, m)
}

// certExists verifies that an https mapping's certificate actually exists,
// so a deleted/stale cert id is rejected at save time (422) instead of
// exploding at nginx -t / haproxy -c during apply.
func (a *App) certExists(w http.ResponseWriter, m store.Mapping) bool {
	if m.Protocol != "https" || m.SSLCertID == nil {
		return true
	}
	if _, err := a.St.GetCert(*m.SSLCertID); err != nil {
		errJSON(w, errors.New("ssl_cert_id points to a certificate that no longer exists"), http.StatusUnprocessableEntity)
		return false
	}
	return true
}

func (a *App) handleDeleteMapping(w http.ResponseWriter, r *http.Request) {
	id := idParam(r)
	if id == 0 {
		errJSON(w, errors.New("bad id"), http.StatusBadRequest)
		return
	}
	if err := a.St.DeleteMapping(id); err != nil {
		errJSON(w, err, 500)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "mapping.delete", "mapping #"+itoa(id), "ok")
	a.maybeAutoApply(actorFrom(r.Context()))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func idParam(r *http.Request) int64 {
	v := chi.URLParam(r, "id")
	var id int64
	for _, c := range v {
		if c < '0' || c > '9' {
			return 0
		}
		id = id*10 + int64(c-'0')
	}
	return id
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// ---------- apply ----------

func (a *App) handleApply(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	if err := a.Svc.ApplyAll(actor); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "applied"})
}

func (a *App) handleValidate(w http.ResponseWriter, r *http.Request) {
	results := a.Svc.ValidateOnly(actorFrom(r.Context()))
	writeJSON(w, http.StatusOK, results)
}

func (a *App) maybeAutoApply(actor string) {
	if v, err := a.St.GetSetting("auto_apply"); err == nil && v == "true" {
		go func() {
			_ = a.Svc.ApplyAll(actor)
		}()
	}
}

// ---------- ports ----------

func (a *App) handleListPorts(w http.ResponseWriter, r *http.Request) {
	ports, err := a.St.ListPorts()
	if err != nil {
		errJSON(w, err, 500)
		return
	}
	if ports == nil {
		ports = []store.PortEntry{}
	}
	last, _ := a.St.GetSetting("last_scan_at")
	writeJSON(w, http.StatusOK, map[string]any{"ports": ports, "last_scan_at": last})
}

func (a *App) handleScan(w http.ResponseWriter, r *http.Request) {
	entries, err := a.Scanner.Scan(a.PanelPort)
	if err != nil {
		errJSON(w, err, 500)
		return
	}
	mappings, _ := a.St.ListMappings()
	managedPorts := map[int]bool{}
	for _, m := range mappings {
		if m.Enabled {
			managedPorts[m.ListenPort] = true
		}
	}
	for i := range entries {
		entries[i].Managed = managedPorts[entries[i].Port] ||
			(entries[i].Classification == "web-server" || entries[i].Classification == "load-balancer")
	}
	if err := a.St.ReplacePorts(entries); err != nil {
		errJSON(w, err, 500)
		return
	}
	_ = a.St.SetSetting("last_scan_at", time.Now().Format(time.RFC3339))
	a.Broker.Publish("scan", map[string]int{"count": len(entries)})
	writeJSON(w, http.StatusOK, map[string]any{"ports": entries, "count": len(entries)})
}

// ---------- certs ----------

func (a *App) handleListCerts(w http.ResponseWriter, r *http.Request) {
	limit, offset := pagedParams(r)
	certs, err := a.St.ListCertsPaged(limit, offset)
	if err != nil {
		errJSON(w, err, 500)
		return
	}
	if certs == nil {
		certs = []store.Cert{}
	}
	writeJSON(w, http.StatusOK, certs)
}

func (a *App) handleCreateCert(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name    string   `json:"name"`
		CertPEM string   `json:"cert_pem"`
		KeyPEM  string   `json:"key_pem"`
		Domains []string `json:"domains"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if body.CertPEM == "" || body.KeyPEM == "" {
		errJSON(w, errors.New("cert_pem and key_pem are required"), http.StatusBadRequest)
		return
	}
	exp, err := parseCertExpiry(body.CertPEM)
	if err != nil {
		errJSON(w, err, http.StatusUnprocessableEntity)
		return
	}
	if len(body.Domains) == 0 {
		body.Domains = certDomains(body.CertPEM)
	}
	c := store.Cert{Name: body.Name, Type: "manual", CertPEM: body.CertPEM, KeyPEM: body.KeyPEM, Domains: body.Domains, ExpiresAt: exp}
	if c.Name == "" && len(c.Domains) > 0 {
		c.Name = c.Domains[0]
	}
	id, err := a.St.CreateCert(&c)
	if err != nil {
		errJSON(w, err, 500)
		return
	}
	c.ID = id
	a.St.Audit(actorFrom(r.Context()), "cert.create", "certificate "+c.Name, "ok")
	writeJSON(w, http.StatusCreated, c)
}

func (a *App) handleSelfSignedCert(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name    string   `json:"name"`
		Domains []string `json:"domains"`
		Days    int      `json:"days"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if len(body.Domains) == 0 {
		errJSON(w, errors.New("at least one domain is required"), http.StatusBadRequest)
		return
	}
	if body.Days < 1 || body.Days > 3650 {
		body.Days = 825
	}
	certPEM, keyPEM, err := generateSelfSigned(body.Domains, body.Days)
	if err != nil {
		errJSON(w, err, 500)
		return
	}
	name := body.Name
	if name == "" {
		name = "self-signed " + body.Domains[0]
	}
	exp := time.Now().AddDate(0, 0, body.Days)
	c := store.Cert{Name: name, Type: "selfsigned", CertPEM: certPEM, KeyPEM: keyPEM, Domains: body.Domains, ExpiresAt: &exp}
	id, err := a.St.CreateCert(&c)
	if err != nil {
		errJSON(w, err, 500)
		return
	}
	c.ID = id
	a.St.Audit(actorFrom(r.Context()), "cert.selfsigned", "self-signed certificate for "+strings.Join(body.Domains, ","), "ok")
	writeJSON(w, http.StatusCreated, c)
}

func (a *App) handleDeleteCert(w http.ResponseWriter, r *http.Request) {
	id := idParam(r)
	if id == 0 {
		errJSON(w, errors.New("bad id"), http.StatusBadRequest)
		return
	}
	if err := a.St.DeleteCert(id); err != nil {
		errJSON(w, err, http.StatusUnprocessableEntity)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "cert.delete", "certificate #"+itoa(id), "ok")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---------- health ----------

func (a *App) handleHealthList(w http.ResponseWriter, r *http.Request) {
	hs, err := a.St.ListHealth()
	if err != nil {
		errJSON(w, err, 500)
		return
	}
	if hs == nil {
		hs = []store.TargetHealth{}
	}
	writeJSON(w, http.StatusOK, hs)
}

// ---------- audit ----------

func (a *App) handleAudit(w http.ResponseWriter, r *http.Request) {
	logs, err := a.St.ListAudit(300)
	if err != nil {
		errJSON(w, err, 500)
		return
	}
	if logs == nil {
		logs = []store.AuditLog{}
	}
	writeJSON(w, http.StatusOK, logs)
}

// securityHeaders sets conservative baseline headers on every response.
// The panel is admin-facing; framing, sniffing and referrer leaks are cheap
// wins. HSTS is only sent over TLS so plain-HTTP LAN installs stay clean.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if r.TLS != nil {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}
