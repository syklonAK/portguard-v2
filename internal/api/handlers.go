package api

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"portguard/internal/health"
	"portguard/internal/proxy"
	"portguard/internal/scanner"
	"portguard/internal/service"
	"portguard/internal/store"
	"portguard/internal/sysinfo"
	"portguard/web"
)

type App struct {
	St        *store.Store
	Svc       *service.Service
	Auth      *Auth
	Broker    *Broker
	Scanner   *scanner.Scanner
	PanelPort int
	Version   string
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

func (a *App) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	r.Get("/api/healthz", a.handleHealthz)
	r.Get("/api/setup-status", a.handleSetupStatus)
	r.Post("/api/setup", a.handleSetup)
	r.Post("/api/login", a.handleLogin)
	// SSE accepts the JWT via query param because EventSource cannot set headers
	r.Get("/api/events", a.handleEvents)

	r.Group(func(pr chi.Router) {
		pr.Use(a.Auth.Middleware)
		pr.Get("/api/system", a.handleSystem)
		pr.Get("/api/me", a.handleMe)
		pr.Post("/api/account/password", a.handleChangePassword)

		pr.Get("/api/mappings", a.handleListMappings)
		pr.Post("/api/mappings", a.handleCreateMapping)
		pr.Put("/api/mappings/{id}", a.handleUpdateMapping)
		pr.Delete("/api/mappings/{id}", a.handleDeleteMapping)

		pr.Post("/api/apply", a.handleApply)
		pr.Post("/api/validate", a.handleValidate)

		// v2: service control (nginx + haproxy)
		pr.Get("/api/services", a.handleServiceStatus)
		pr.Post("/api/services/{engine}/{action}", a.handleServiceAction)

		// v2: HAProxy runtime API (stats socket)
		pr.Get("/api/runtime/haproxy", a.handleRuntimeHAProxy)
		pr.Post("/api/runtime/haproxy/servers/{backend}/{server}/state", a.handleRuntimeServerState)

		// v2: backups
		pr.Get("/api/backups", a.handleListBackups)
		pr.Get("/api/backups/{ts}/diff", a.handleBackupDiff)
		pr.Post("/api/backups/{ts}/restore", a.handleBackupRestore)
		pr.Delete("/api/backups/{ts}", a.handleBackupDelete)

		// v2: diagnostics + templates + import/export
		pr.Post("/api/diagnostics", a.handleDiagnostics)
		pr.Get("/api/templates", a.handleTemplates)
		pr.Get("/api/export", a.handleExport)
		pr.Post("/api/import", a.handleImport)

		// v2: live config viewer
		pr.Get("/api/config/{engine}", a.handleConfigView)

		pr.Get("/api/ports", a.handleListPorts)
		pr.Post("/api/ports/scan", a.handleScan)
		pr.Get("/api/connections", a.handleListConnections)

		pr.Get("/api/certs", a.handleListCerts)
		pr.Post("/api/certs", a.handleCreateCert)
		pr.Post("/api/certs/selfsigned", a.handleSelfSignedCert)
		pr.Get("/api/certs/{id}/validate", a.handleCertValidate)
		pr.Delete("/api/certs/{id}", a.handleDeleteCert)

		pr.Get("/api/health", a.handleHealthList)

		pr.Get("/api/audit", a.handleAudit)

		pr.Get("/api/settings", a.handleGetSettings)
		pr.Put("/api/settings", a.handlePutSettings)

		// v2.2: Hedioum tunnel management
		pr.Get("/api/tunnels", a.handleTunnelStatus)
		pr.Get("/api/tunnels/relays", a.handleListRelays)
		pr.Post("/api/tunnels/relays", a.handleCreateRelay)
		pr.Put("/api/tunnels/relays/{id}", a.handleUpdateRelay)
		pr.Delete("/api/tunnels/relays/{id}", a.handleDeleteRelay)
		pr.Post("/api/tunnels/apply", a.handleTunnelApply)
		pr.Post("/api/tunnels/validate", a.handleTunnelValidate)

		// v2.2: self-updater
		pr.Post("/api/update", a.handleUpdate)

		// v2.3: multi-server management (master side)
		pr.Get("/api/nodes", a.handleListNodes)
		pr.Post("/api/nodes", a.handleCreateNode)
		pr.Put("/api/nodes/{id}", a.handleUpdateNode)
		pr.Delete("/api/nodes/{id}", a.handleDeleteNode)
		pr.Post("/api/nodes/{id}/{action}", a.handleNodeAction)
		pr.Get("/api/node-self", a.handleNodeTokenInfo)
		pr.Put("/api/node-self", a.handlePutNodeToken)
	})

	// node API (master→node, token-authenticated, no admin JWT)
	r.Get("/api/node/ping", a.handleNodePing)
	r.Get("/api/node/summary", a.handleNodeSummary)
	r.Get("/api/node/mappings", a.handleNodeMappings)
	r.Post("/api/node/apply", a.handleNodeApply)
	r.Get("/api/node/connections", a.handleNodeConnections)

	// SPA (embedded)
	dist, err := fs.Sub(web.Dist, "dist")
	if err == nil {
		fileServer := http.FileServer(http.FS(dist))
		r.Handle("/*", spaHandler(dist, fileServer))
	}
	return r
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

func (a *App) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	n, err := a.St.AdminCount()
	if err != nil {
		errJSON(w, err, 500)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"setup_required": n == 0})
}

func (a *App) handleSetup(w http.ResponseWriter, r *http.Request) {
	n, err := a.St.AdminCount()
	if err != nil {
		errJSON(w, err, 500)
		return
	}
	if n > 0 {
		errJSON(w, errors.New("setup already completed"), http.StatusForbidden)
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if len(body.Username) < 3 || len(body.Password) < 8 {
		errJSON(w, errors.New("username must be 3+ chars and password 8+ chars"), http.StatusBadRequest)
		return
	}
	hash, err := hashPassword(body.Password)
	if err != nil {
		errJSON(w, err, 500)
		return
	}
	if _, err := a.St.CreateAdmin(strings.ToLower(strings.TrimSpace(body.Username)), hash); err != nil {
		errJSON(w, err, 500)
		return
	}
	a.St.Audit(body.Username, "setup", "initial admin created", "ok")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !a.Auth.Allow(ip) {
		errJSON(w, errors.New("too many failed attempts, try again in a minute"), http.StatusTooManyRequests)
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	admin, err := a.St.GetAdminByUsername(strings.ToLower(strings.TrimSpace(body.Username)))
	if err != nil {
		a.Auth.Fail(ip)
		errJSON(w, errors.New("invalid credentials"), http.StatusUnauthorized)
		return
	}
	if !checkPassword(admin.PasswordHash, body.Password) {
		a.Auth.Fail(ip)
		errJSON(w, errors.New("invalid credentials"), http.StatusUnauthorized)
		return
	}
	token, err := a.Auth.Issue(admin.Username, admin.ID)
	if err != nil {
		errJSON(w, err, 500)
		return
	}
	a.St.TouchAdminLogin(admin.ID)
	a.St.Audit(admin.Username, "login", "logged in from "+ip, "ok")
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "username": admin.Username})
}

func (a *App) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"username": actorFrom(r.Context())})
}

// handleEvents authenticates via ?token= (EventSource) or the Authorization header.
func (a *App) handleEvents(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			token = strings.TrimPrefix(h, "Bearer ")
		}
	}
	if _, err := a.Auth.Parse(token); err != nil {
		unauthorized(w)
		return
	}
	a.Broker.Handler(w, r)
}

func (a *App) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	var body struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	admin, err := a.St.GetAdminByUsername(actor)
	if err != nil {
		errJSON(w, err, 500)
		return
	}
	if !checkPassword(admin.PasswordHash, body.Old) {
		errJSON(w, errors.New("current password is wrong"), http.StatusBadRequest)
		return
	}
	if len(body.New) < 8 {
		errJSON(w, errors.New("new password must be 8+ chars"), http.StatusBadRequest)
		return
	}
	hash, err := hashPassword(body.New)
	if err != nil {
		errJSON(w, err, 500)
		return
	}
	if err := a.St.SetAdminPassword(admin.ID, hash); err != nil {
		errJSON(w, err, 500)
		return
	}
	a.St.Audit(actor, "password", "password changed", "ok")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
		"system":    s,
		"mappings":  map[string]any{"total": len(mappings), "enabled": countEnabled(mappings)},
		"ports":     map[string]any{"managed": managedPorts, "unmanaged": unmanaged, "total": len(ports)},
		"health":    map[string]any{"up": up, "down": down, "unknown": unknown, "summary": summary},
		"version":   a.Version,
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

func (a *App) handleListMappings(w http.ResponseWriter, r *http.Request) {
	ms, err := a.St.ListMappings()
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
	certs, err := a.St.ListCerts()
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
