// Dynamic integration discovery: instead of asking the operator to type
// panel URLs and tokens by hand, PortGuard scans the host it runs on —
// docker containers and known install paths — reads the panels' env files,
// probes their API ports and returns ready-to-apply integration profiles.
// Values that are secrets are never returned, only their presence.
package api

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// discoveredPanel is one detected integration candidate.
type discoveredPanel struct {
	Kind           string `json:"kind"`            // pasarguard | marzban | xui | unknown
	Source         string `json:"source"`          // docker | env
	Name           string `json:"name"`            // container name or directory
	URL            string `json:"url"`             // probed base URL (scheme+127.0.0.1+port)
	Port           int    `json:"port"`
	TLS            bool   `json:"tls"`
	Alive          bool   `json:"alive"`           // API answered (any status < 500)
	EnvPath        string `json:"env_path,omitempty"`
	EnvUsername    string `json:"env_username,omitempty"` // SUDO_USERNAME if present
	EnvHasPassword bool   `json:"env_has_password,omitempty"`
	Note           string `json:"note,omitempty"`
}

// knownPanelKind maps container/dir name fragments to an integration kind.
func knownPanelKind(name string) string {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "pasarguard"):
		return "pasarguard"
	case strings.Contains(n, "marzban"):
		return "marzban"
	case strings.Contains(n, "x-ui"), strings.Contains(n, "3x-ui"), strings.Contains(n, "s-ui"):
		return "xui"
	}
	return ""
}

// handleDiscover scans the host for supported panels and returns profiles.
func (a *App) handleDiscover(w http.ResponseWriter, r *http.Request) {
	seen := map[string]*discoveredPanel{}
	add := func(p discoveredPanel) {
		key := p.Kind + "|" + p.Source + "|" + p.Name
		if _, ok := seen[key]; !ok {
			seen[key] = &p
		}
	}

	// 1) docker containers (host-network panels bind ports directly; mapped
	// ports appear in the Ports column either way)
	if out, err := exec.Command("docker", "ps", "--format", "{{.Names}}\t{{.Image}}\t{{.Ports}}").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			parts := strings.Split(strings.TrimSpace(line), "\t")
			if len(parts) < 2 {
				continue
			}
			name, image := parts[0], parts[1]
			kind := knownPanelKind(name)
			if kind == "" {
				kind = knownPanelKind(image)
			}
			if kind == "" {
				continue
			}
			p := discoveredPanel{Kind: kind, Source: "docker", Name: name, Alive: false}
			if port, tls, ok := probePanelPort(kind, dockerPortHint(parts)); ok {
				p.Port, p.TLS, p.Alive = port, tls, true
				p.URL = fmt.Sprintf("%s://127.0.0.1:%d", scheme(tls), port)
			}
			add(p)
		}
	}

	// 2) .env files under common install roots — catches host installs and
	// stopped containers, and gives us the env port/SSL even when probing
	// needs credentials
	for _, root := range []string{"/opt", "/root", "/usr/local"} {
		dirs, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, d := range dirs {
			if !d.IsDir() {
				continue
			}
			kind := knownPanelKind(d.Name())
			envPath := filepath.Join(root, d.Name(), ".env")
			raw, err := os.ReadFile(envPath)
			if err != nil {
				continue
			}
			if kind == "" {
				kind = kindFromEnv(string(raw))
			}
			if kind == "" {
				continue
			}
			env := parseDotEnv(string(raw))
			p := discoveredPanel{
				Kind: kind, Source: "env", Name: d.Name(), EnvPath: envPath,
				EnvUsername:    env["SUDO_USERNAME"],
				EnvHasPassword: envValue(env, "SUDO_PASSWORD") != "",
			}
			if port := envPort(env); port > 0 {
				p.Port = port
				p.TLS = env["UVICORN_SSL_CERTFILE"] != "" || env["UVICORN_SSL_KEYFILE"] != ""
				_, alive := probeURL(fmt.Sprintf("%s://127.0.0.1:%d", scheme(p.TLS), port), p.TLS)
				p.Alive = alive
				p.URL = fmt.Sprintf("%s://127.0.0.1:%d", scheme(p.TLS), port)
				if p.EnvHasPassword {
					p.Note = "credentials found in .env — can be applied automatically"
				}
			}
			add(p)
		}
	}

	out := make([]discoveredPanel, 0, len(seen))
	for _, p := range seen {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, k int) bool {
		if out[i].Alive != out[k].Alive {
			return out[i].Alive
		}
		return out[i].Name < out[k].Name
	})
	writeJSON(w, http.StatusOK, out)
}

// applyDiscoverRequest is the POST /api/discover/apply body.
type applyDiscoverRequest struct {
	Kind     string `json:"kind"` // pasarguard | marzban (same admin API family)
	URL      string `json:"url"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Token    string `json:"token,omitempty"`
	EnvPath  string `json:"env_path,omitempty"` // read credentials from this .env server-side
}

// handleDiscoverApply writes the discovered profile into the integration
// settings (pasarguard_* keys serve the whole marzban-family admin API).
func (a *App) handleDiscoverApply(w http.ResponseWriter, r *http.Request) {
	var req applyDiscoverRequest
	if !readJSON(w, r, &req) {
		return
	}
	if req.Kind != "pasarguard" && req.Kind != "marzban" {
		errJSON(w, fmt.Errorf("unsupported kind %q (only pasarguard/marzban integrations can be auto-applied)", req.Kind), http.StatusBadRequest)
		return
	}
	req.URL = strings.TrimRight(strings.TrimSpace(req.URL), "/")
	if req.URL == "" {
		errJSON(w, fmt.Errorf("url is required"), http.StatusBadRequest)
		return
	}
	// credentials found in the panel's own .env are applied server-side —
	// secrets never travel to the browser
	if req.EnvPath != "" {
		if !safeEnvPath(req.EnvPath) {
			errJSON(w, fmt.Errorf("env_path not allowed"), http.StatusBadRequest)
			return
		}
		if raw, err := os.ReadFile(req.EnvPath); err == nil {
			env := parseDotEnv(string(raw))
			if req.Username == "" {
				req.Username = envValue(env, "SUDO_USERNAME")
			}
			if req.Password == "" {
				req.Password = envValue(env, "SUDO_PASSWORD")
			}
		}
	}
	_ = a.St.SetSetting("pasarguard_url", req.URL)
	if req.Username != "" {
		_ = a.St.SetSetting("pasarguard_username", strings.TrimSpace(req.Username))
	}
	if req.Password != "" {
		_ = a.St.SetSetting("pasarguard_password", strings.TrimSpace(req.Password))
	}
	if req.Token != "" {
		_ = a.St.SetSetting("pasarguard_token", strings.TrimSpace(req.Token))
	} else {
		// credentials changed → drop the cached token so the next sync
		// logs in fresh
		_ = a.St.SetSetting("pasarguard_token", "")
	}
	a.St.Audit(actorFrom(r.Context()), "discover.apply", req.Kind+" → "+req.URL, "ok")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- helpers ----

// safeEnvPath restricts server-side .env reads to known install roots.
func safeEnvPath(p string) bool {
	if filepath.Base(p) != ".env" {
		return false
	}
	for _, root := range []string{"/opt/", "/root/", "/usr/local/"} {
		if strings.HasPrefix(p, root) {
			return true
		}
	}
	return false
}

func scheme(tls bool) string {
	if tls {
		return "https"
	}
	return "http"
}

func kindFromEnv(env string) string {
	switch {
	case strings.Contains(env, "PASARGUARD"):
		return "pasarguard"
	case strings.Contains(env, "MARZBAN"), strings.Contains(env, "UVICORN_PORT"):
		return "marzban"
	}
	return ""
}

func parseDotEnv(raw string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"'`)
		out[k] = v
	}
	return out
}

// envValue returns the value if set and not a commented-out placeholder.
func envValue(env map[string]string, key string) string {
	v := strings.TrimSpace(env[key])
	if v == "" || strings.HasPrefix(v, "\" #") {
		return ""
	}
	return v
}

func envPort(env map[string]string) int {
	if p, err := strconv.Atoi(envValue(env, "UVICORN_PORT")); err == nil && p > 0 && p < 65536 {
		return p
	}
	return 0
}

// dockerPortHint extracts the first host-side port from a `docker ps` Ports
// column (e.g. "0.0.0.0:2096->8000/tcp"); host-network panels yield "".
func dockerPortHint(parts []string) int {
	if len(parts) < 3 {
		return 0
	}
	for _, seg := range strings.Split(parts[2], ",") {
		seg = strings.TrimSpace(seg)
		if i := strings.Index(seg, ":["); i >= 0 { // IPv6 entry — skip
			continue
		}
		if i := strings.Index(seg, ":"); i >= 0 {
			if p, err := strconv.Atoi(strings.Split(seg[i+1:], "->")[0]); err == nil && p > 0 {
				return p
			}
		}
	}
	return 0
}

// probePanelPort finds a working scheme/port for a panel of the given kind.
func probePanelPort(kind string, port int) (int, bool, bool) {
	paths := map[string]string{
		"pasarguard": "/api/system",
		"marzban":    "/api/system",
		"xui":        "/login",
	}
	path, ok := paths[kind]
	if !ok {
		path = "/"
	}
	ports := []int{}
	if port > 0 {
		ports = append(ports, port)
	}
	// common defaults per family, probed as fallback
	ports = append(ports, 2096, 8000, 8081, 54321, 443)
	tried := map[int]bool{}
	for _, p := range ports {
		if tried[p] {
			continue
		}
		tried[p] = true
		for _, tlsOn := range []bool{true, false} {
			if _, alive := probeURL(fmt.Sprintf("%s://127.0.0.1:%d%s", scheme(tlsOn), p, path), tlsOn); alive {
				return p, tlsOn, true
			}
		}
	}
	return 0, false, false
}

// probeURL returns the HTTP status (0 = unreachable) and whether the API
// answered at all. 401/403/422 still mean "alive" — the endpoint exists and
// only auth is missing. Self-signed certs are accepted during discovery.
func probeURL(url string, skipTLS bool) (int, bool) {
	client := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: skipTLS},
		},
	}
	resp, err := client.Get(url)
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()
	return resp.StatusCode, resp.StatusCode < 500
}
