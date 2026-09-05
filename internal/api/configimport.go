package api

import (
	"net/http"
	"os"
	"strings"

	"portguard/internal/proxy"
	"portguard/internal/store"
)

// ---- existing-config import (nginx / haproxy) ----

// ImportScanResult reports what was found in the live config files and what
// would be imported. Nothing is created until the user confirms.
func (a *App) handleImportScan(w http.ResponseWriter, r *http.Request) {
	mappings, issues, found := a.scanExistingConfigs()
	writeJSON(w, http.StatusOK, map[string]any{
		"mappings": mappings,
		"issues":   issues,
		"found":    found,
	})
}

// handleImportConfirm creates the scanned mappings (all start disabled).
func (a *App) handleImportConfirm(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Indices []int `json:"indices"` // which scan results to create
	}
	if !readJSON(w, r, &body) {
		return
	}
	mappings, issues, _ := a.scanExistingConfigs()
	created, skipped := 0, 0
	names := map[string]bool{}
	existing, _ := a.St.ListMappings()
	for _, m := range existing {
		names[m.Name] = true
	}
	all := existing
	for _, idx := range body.Indices {
		if idx < 0 || idx >= len(mappings) {
			continue
		}
		m := mappings[idx]
		// conflict check against current + already-imported
		if err := proxy.ValidateMapping(m, all, a.PanelPort); err != nil {
			issues = append(issues, proxy.ConversionIssue{Section: "import " + m.Name, Reason: err.Error()})
			skipped++
			continue
		}
		id, err := a.St.CreateMapping(&m)
		if err != nil {
			skipped++
			continue
		}
		m.ID = id
		all = append(all, m)
		created++
	}
	a.St.Audit(actorFrom(r.Context()), "config.import", "imported existing configs", "ok")
	writeJSON(w, http.StatusOK, map[string]any{
		"created": created,
		"skipped": skipped,
		"issues":  issues,
	})
}

// scanExistingConfigs reads the live nginx/haproxy files and parses them into
// importable mapping candidates (disabled). Runs on every call so the preview
// always reflects the current files.
func (a *App) scanExistingConfigs() ([]store.Mapping, []proxy.ConversionIssue, bool) {
	var mappings []store.Mapping
	var issues []proxy.ConversionIssue
	found := false
	taken := map[string]bool{}
	existing, _ := a.St.ListMappings()
	for _, m := range existing {
		taken[m.Name] = true
	}

	// ---- nginx ----
	ngText := readLiveFile(a.Svc.Paths.NginxConf)
	if ngText != "" {
		found = true
		sites := proxy.ParseNginxConfig(ngText)
		ngMappings, ngIssues := proxy.NginxSitesToMappings(sites, taken)
		mappings = append(mappings, ngMappings...)
		issues = append(issues, ngIssues...)
	}

	// ---- haproxy ----
	haText := readLiveFile(a.Svc.Paths.HAProxyConf)
	if haText != "" {
		found = true
		cfg := proxy.ParseHAProxyConfig(haText)
		haMappings, haIssues := proxy.HAProxyCfgToMappings(cfg, taken)
		mappings = append(mappings, haMappings...)
		issues = append(issues, haIssues...)
	}

	// filter out candidates that collide with existing enabled mappings
	var clean []store.Mapping
	for _, m := range mappings {
		dup := false
		for _, e := range existing {
			if e.ListenPort == m.ListenPort && e.Enabled {
				dup = true
				break
			}
		}
		if !dup {
			clean = append(clean, m)
		}
	}
	return clean, issues, found
}

// readLiveFile reads a config file, tolerating absence (fresh installs).
func readLiveFile(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	s := string(data)
	// an untouched packaged default is not worth importing
	if strings.Contains(s, "Welcome to nginx") && len(s) < 1200 {
		return ""
	}
	return s
}
