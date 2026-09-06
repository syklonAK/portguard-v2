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
	"github.com/go-chi/chi/v5/middleware"

	"portguard/internal/alerter"
	"portguard/internal/health"
	"portguard/internal/proxy"
	"portguard/internal/ratelimit"
	"portguard/internal/scanner"
	"portguard/internal/service"
	"portguard/internal/store"
	"portguard/internal/sysinfo"
	"portguard/web"
)

type App struct {
	St                 *store.Store
	Svc                *service.Service
	Auth               *Auth
	Broker             *Broker
	Scanner            *scanner.Scanner
	PanelPort          int
	Version            string
	RateApplierFactory func() *ratelimit.Applier // set by main; lazily builds the node applier
	Alerter            *alerter.Alerter          // optional (nil until main wires it)
	Jobs               *JobManager               // long shell ops with live terminal output
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

func (a *App) Router() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	// NOTE: middleware.RealIP is intentionally NOT used: trusting
	// X-Forwarded-For from arbitrary clients would let attackers bypass the
	// login rate limiter and poison audit entries with fake IPs. The panel
	// is meant to be exposed directly; put a trusted reverse proxy in front
	// only if you accept that trade-off.
	r.Use(middleware.Recoverer)
	// gzip responses (JSON APIs + embedded SPA) — large mapping lists and the
	// JS bundle compress 5-10x, noticeably faster over WAN links to nodes
	r.Use(middleware.Compress(5, "application/json", "text/html", "text/css",
		"application/javascript", "image/svg+xml"))
	r.Use(securityHeaders)
	r.Get("/api/healthz", a.handleHealthz)
	r.Get("/api/setup-status", a.handleSetupStatus)
	r.Post("/api/setup", a.handleSetup)
	r.Post("/api/login", a.handleLogin)
	// SSE accepts the JWT via query param because EventSource cannot set headers
	r.Get("/api/events", a.handleEvents)

	r.Group(func(pr chi.Router) {
		pr.Use(a.Auth.Middleware)

		// ---- read-only surface (viewer+) ----
		pr.Get("/api/system", a.handleSystem)
		pr.Get("/api/me", a.handleMe)
		pr.Get("/api/mappings", a.handleListMappings)
		pr.Get("/api/ports", a.handleListPorts)
		pr.Get("/api/connections", a.handleListConnections)
		pr.Get("/api/certs", a.handleListCerts)
		pr.Get("/api/certs/acme/providers", a.handleACMEProviders)
		pr.Get("/api/certs/{id}/validate", a.handleCertValidate)
		pr.Get("/api/health", a.handleHealthList)
		pr.Get("/api/audit", a.handleAudit)
		pr.Get("/api/backups", a.handleListBackups)
		pr.Get("/api/backups/{ts}/diff", a.handleBackupDiff)
		pr.Get("/api/templates", a.handleTemplates)
		pr.Get("/api/config/{engine}", a.handleConfigView)
		pr.Get("/api/runtime/haproxy", a.handleRuntimeHAProxy)
		pr.Get("/api/services", a.handleServiceStatus)
		pr.Get("/api/settings", a.handleGetSettings)
		pr.Get("/api/tunnels", a.handleTunnelStatus)
		pr.Get("/api/tunnels/relays", a.handleListRelays)
		pr.Get("/api/tools", a.handleTools)
		pr.Get("/api/jobs", a.handleListJobs)
		pr.Get("/api/jobs/{id}", a.handleGetJob)
		pr.Get("/api/nodes", a.handleListNodes)
		pr.Get("/api/node-self", a.handleNodeTokenInfo)
		// v2.6
		pr.Get("/api/rate-limits/status", a.handleRateLimitStatus)
		pr.Get("/api/rate-limits/profiles", a.handleListRateProfiles)
		pr.Get("/api/rate-limits/policies", a.handleListRatePolicies)
		pr.Get("/api/pasarguard/users", a.handleListPasarguardUsers)
		// v2.7
		pr.Get("/api/versions", a.handleListVersions)
		pr.Get("/api/versions/{v}", a.handleGetVersion)
		pr.Get("/api/versions/{v}/download", a.handleDownloadVersion)
		pr.Get("/api/versions/{from}/diff/{to}", a.handleDiffVersions)
		pr.Get("/api/alerts", a.handleListAlerts)
		pr.Get("/api/nodes/{id}/mappings", a.handleNodeMappingsProxy)
		pr.Get("/api/nodes/{id}/certs", a.handleNodeCertsProxy)
		pr.Get("/api/nodes/{id}/logs/{source}", a.handleNodeLogs)

		// v2.8: services + metrics
		pr.Get("/api/app-services", a.handleListServices)
		pr.Get("/api/metrics/{id}", a.handleMetrics)
		// (writes under admin)

		// ---- operator+ (deploy & operate) ----
		pr.Post("/api/apply", a.Auth.RequireRole("operator", a.handleApply))
		pr.Post("/api/validate", a.Auth.RequireRole("operator", a.handleValidate))
		pr.Post("/api/services/{engine}/{action}", a.Auth.RequireRole("operator", a.handleServiceAction))
		pr.Post("/api/runtime/haproxy/servers/{backend}/{server}/state", a.Auth.RequireRole("operator", a.handleRuntimeServerState))
		pr.Post("/api/ports/scan", a.Auth.RequireRole("operator", a.handleScan))
		pr.Post("/api/diagnostics", a.Auth.RequireRole("operator", a.handleDiagnostics))
		pr.Post("/api/tunnels/validate", a.Auth.RequireRole("operator", a.handleTunnelValidate))
		pr.Post("/api/tunnels/apply", a.Auth.RequireRole("operator", a.handleTunnelApply))
		pr.Post("/api/rate-limits/sync", a.Auth.RequireRole("operator", a.handlePasarGuardSync))
		pr.Post("/api/rate-limits/push", a.Auth.RequireRole("operator", a.handleRateLimitPush))
		pr.Post("/api/alerts/test", a.Auth.RequireRole("operator", a.handleTestAlert))
		pr.Post("/api/backups/{ts}/restore", a.Auth.RequireRole("operator", a.handleBackupRestore))
		pr.Post("/api/nodes/{id}/{action}", a.Auth.RequireRole("operator", a.handleNodeAction))
		pr.Post("/api/tools/{tool}/install", a.Auth.RequireRole("operator", a.handleToolInstall))

		// ---- admin+ (change configuration) ----
		pr.Post("/api/mappings", a.Auth.RequireRole("admin", a.handleCreateMapping))
		pr.Put("/api/mappings/{id}", a.Auth.RequireRole("admin", a.handleUpdateMapping))
		pr.Delete("/api/mappings/{id}", a.Auth.RequireRole("admin", a.handleDeleteMapping))
		pr.Post("/api/certs", a.Auth.RequireRole("admin", a.handleCreateCert))
		pr.Post("/api/certs/selfsigned", a.Auth.RequireRole("admin", a.handleSelfSignedCert))
		pr.Get("/api/discover", a.Auth.RequireRole("admin", a.handleDiscover))
		pr.Post("/api/discover/apply", a.Auth.RequireRole("admin", a.handleDiscoverApply))
		pr.Post("/api/certs/issue", a.Auth.RequireRole("admin", a.handleIssueCert))
		pr.Post("/api/certs/{id}/renew", a.Auth.RequireRole("admin", a.handleRenewCert))
		pr.Delete("/api/certs/{id}", a.Auth.RequireRole("admin", a.handleDeleteCert))
		pr.Put("/api/settings", a.Auth.RequireRole("admin", a.handlePutSettings))
		pr.Delete("/api/backups/{ts}", a.Auth.RequireRole("admin", a.handleBackupDelete))
		pr.Get("/api/export", a.Auth.RequireRole("admin", a.handleExport))
		pr.Post("/api/import", a.Auth.RequireRole("admin", a.handleImport))
		pr.Get("/api/import/scan", a.Auth.RequireRole("admin", a.handleImportScan))
		pr.Post("/api/import/confirm", a.Auth.RequireRole("admin", a.handleImportConfirm))
		pr.Post("/api/tunnels/relays", a.Auth.RequireRole("admin", a.handleCreateRelay))
		pr.Put("/api/tunnels/relays/{id}", a.Auth.RequireRole("admin", a.handleUpdateRelay))
		pr.Delete("/api/tunnels/relays/{id}", a.Auth.RequireRole("admin", a.handleDeleteRelay))
		pr.Put("/api/node-self", a.Auth.RequireRole("admin", a.handlePutNodeToken))
		pr.Post("/api/rate-limits/profiles", a.Auth.RequireRole("admin", a.handleCreateRateProfile))
		pr.Put("/api/rate-limits/profiles/{id}", a.Auth.RequireRole("admin", a.handleUpdateRateProfile))
		pr.Delete("/api/rate-limits/profiles/{id}", a.Auth.RequireRole("admin", a.handleDeleteRateProfile))
		pr.Post("/api/rate-limits/policies", a.Auth.RequireRole("admin", a.handleUpsertRatePolicy))
		pr.Delete("/api/rate-limits/policies/{uuid}", a.Auth.RequireRole("admin", a.handleDeleteRatePolicy))
		pr.Post("/api/versions/{v}/restore", a.Auth.RequireRole("admin", a.handleRestoreVersion))
		pr.Post("/api/alerts/{id}/ack", a.Auth.RequireRole("admin", a.handleAckAlert))
		pr.Post("/api/nodes", a.Auth.RequireRole("admin", a.handleCreateNode))
		pr.Put("/api/nodes/{id}", a.Auth.RequireRole("admin", a.handleUpdateNode))
		pr.Delete("/api/nodes/{id}", a.Auth.RequireRole("admin", a.handleDeleteNode))
		pr.Post("/api/nodes/{id}/mappings", a.Auth.RequireRole("admin", a.handleNodeMappingCreate))
		pr.Put("/api/nodes/{id}/mappings/{mid}", a.Auth.RequireRole("admin", a.handleNodeMappingUpdate))
		pr.Delete("/api/nodes/{id}/mappings/{mid}", a.Auth.RequireRole("admin", a.handleNodeMappingDelete))
		pr.Post("/api/nodes/{id}/certs", a.Auth.RequireRole("admin", a.handleNodeCertCreate))
		pr.Post("/api/update", a.Auth.RequireRole("admin", a.handleUpdate))
		pr.Post("/api/account/password", a.handleChangePassword)
		// v2.8: services (admin writes)
		pr.Post("/api/app-services", a.Auth.RequireRole("admin", a.handleCreateService))
		pr.Get("/api/app-services/{id}", a.Auth.RequireRole("admin", a.handleServiceDetail))
		pr.Put("/api/app-services/{id}", a.Auth.RequireRole("admin", a.handleUpdateService))
		pr.Delete("/api/app-services/{id}", a.Auth.RequireRole("admin", a.handleDeleteService))

		// ---- owner-only (user management) ----
		pr.Get("/api/users", a.Auth.RequireRole("owner", a.handleListUsers))
		pr.Post("/api/users", a.Auth.RequireRole("owner", a.handleCreateUser))
		pr.Put("/api/users/{id}", a.Auth.RequireRole("owner", a.handleUpdateUser))
		pr.Delete("/api/users/{id}", a.Auth.RequireRole("owner", a.handleDeleteUser))
	})

	// node API (master→node, token-authenticated, no admin JWT). Panels and
	// headless agents expose the same surface; the Agent router carries the
	// full contract (mappings/certs CRUD, apply, tools, tunnel, ...).
	r.Mount("/api/node/", (&Agent{App: a}).Router())

	// node bootstrap: the master serves its own binary + the installer
	// script so a new server needs nothing but curl.
	r.Get("/api/node/binary", a.handleNodeBinary)
	r.Get("/api/agent-install.sh", a.handleAgentInstaller)

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
	token, err := a.Auth.IssueRole(admin.Username, admin.ID, admin.Role)
	if err != nil {
		errJSON(w, err, 500)
		return
	}
	a.St.TouchAdminLogin(admin.ID)
	a.St.Audit(admin.Username, "login", "logged in from "+ip, "ok")
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "username": admin.Username, "role": admin.Role})
}

func (a *App) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"username": actorFrom(r.Context()), "role": roleFrom(r.Context())})
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
