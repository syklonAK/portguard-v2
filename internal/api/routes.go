package api

// routes.go is the single, grouped map of every HTTP route the panel and
// the node API expose. Handlers live in the domain files next to this one;
// this file only wires paths to them so the surface stays auditable.

import (
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"io/fs"
	"net/http"
	"portguard/web"
))

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
