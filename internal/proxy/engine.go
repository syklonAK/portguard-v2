package proxy

import (
	"fmt"
	"net"
	"strings"

	"portguard/internal/store"
)

// Engine renders, validates and applies configuration files for one proxy engine.
// Implementations must never touch live config unless Apply succeeds after Validate.
type Engine interface {
	Name() string
	// Render produces a set of relative-path -> file-content files for the given mappings.
	Render(mappings []store.Mapping, certs map[int64]store.Cert, paths Paths) (map[string]string, error)
	// Validate runs the engine's config test against staged files (files written to a temp dir).
	Validate(staged map[string]string, paths Paths) error
	// ActiveFiles returns the absolute paths this engine owns on the live system.
	ActiveFiles(paths Paths) []string
	// LivePath maps a staged relative path to its absolute live location.
	LivePath(paths Paths, rel string) string
	// StagedRel maps an absolute live path back to the staged relative path (ok=false if unknown).
	StagedRel(paths Paths, live string) (string, bool)
	// Reload reloads the running service.
	Reload(paths Paths) error
}

// Paths holds configurable locations (persisted in settings).
type Paths struct {
	NginxConf     string `json:"nginx_conf"`     // /etc/nginx/nginx.conf
	HAProxyConf   string `json:"haproxy_conf"`   // /etc/haproxy/haproxy.cfg
	CertsDir      string `json:"certs_dir"`      // /var/lib/portguard/certs
	BackupsDir    string `json:"backups_dir"`    // /var/lib/portguard/backups
	NginxBin      string `json:"nginx_bin"`      // /usr/sbin/nginx
	HAProxyBin    string `json:"haproxy_bin"`    // /usr/sbin/haproxy
	HAProxySocket string `json:"haproxy_socket"` // /run/haproxy/admin.sock (runtime API)
}

func DefaultPaths() Paths {
	return Paths{
		NginxConf:     "/etc/nginx/nginx.conf",
		HAProxyConf:   "/etc/haproxy/haproxy.cfg",
		CertsDir:      "/var/lib/portguard/certs",
		BackupsDir:    "/var/lib/portguard/backups",
		NginxBin:      "/usr/sbin/nginx",
		HAProxyBin:    "/usr/sbin/haproxy",
		HAProxySocket: "/run/haproxy/admin.sock",
	}
}

var _ Engine = NginxEngine{}

func ValidEngine(e string) bool { return e == "nginx" || e == "haproxy" }
func ValidProtocol(p string) bool {
	switch p {
	case "http", "https", "tcp", "udp":
		return true
	}
	return false
}

// ValidBalance reports whether algo is a valid LB algorithm for the engine/protocol,
// mirroring haproxy-manager's balance options. "" means engine default.
func ValidBalance(engine, protocol, algo string) bool {
	if algo == "" {
		return true
	}
	if engine == "haproxy" {
		switch algo {
		case "roundrobin", "leastconn", "source", "uri", "random", "first", "static-rr":
			return true
		}
		return false
	}
	// nginx
	if protocol == "tcp" || protocol == "udp" {
		switch algo {
		case "round_robin", "least_conn", "random":
			return true
		}
		return false
	}
	switch algo {
	case "round_robin", "least_conn", "ip_hash", "random":
		return true
	}
	return false
}

// posixDir returns the directory part of a POSIX-style path (the live paths are
// Linux paths regardless of the OS the panel binary runs on).
func posixDir(p string) string {
	if i := strings.LastIndexByte(p, '/'); i > 0 {
		return p[:i]
	}
	return "/"
}

// posixJoin joins path elements with "/" without cleaning to the host OS separator.
func posixJoin(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p == "" {
			continue
		}
		out = append(out, strings.Trim(p, "/"))
	}
	return "/" + strings.Join(out, "/")
}

// ValidateMapping performs structural validation shared by API and apply.
func ValidateMapping(m store.Mapping, all []store.Mapping, panelPort int) error {
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if hasControlChars(m.Name) {
		return fmt.Errorf("name must not contain control characters or newlines")
	}
	for _, h := range m.ServerNames {
		if hasControlChars(h) {
			return fmt.Errorf("server_names must not contain control characters or newlines")
		}
	}
	if hasControlChars(m.HostHeader) {
		return fmt.Errorf("host_header must not contain control characters or newlines")
	}
	if hasControlChars(m.RedirectTo) {
		return fmt.Errorf("redirect_to must not contain control characters or newlines")
	}
	for k, v := range m.ExtraHeaders {
		if hasControlChars(k) || hasControlChars(v) {
			return fmt.Errorf("extra_headers must not contain control characters or newlines")
		}
	}
	for _, pr := range m.PathRoutes {
		if hasControlChars(pr.Prefix) {
			return fmt.Errorf("path route prefix must not contain control characters")
		}
	}
	if !ValidEngine(m.Engine) {
		return fmt.Errorf("engine must be nginx or haproxy")
	}
	if !ValidProtocol(m.Protocol) {
		return fmt.Errorf("protocol must be http, https, tcp or udp")
	}
	if m.ListenPort < 1 || m.ListenPort > 65535 {
		return fmt.Errorf("listen_port must be 1-65535")
	}
	if net.ParseIP(m.ListenIP) == nil {
		return fmt.Errorf("listen_ip %q is not a valid IP address", m.ListenIP)
	}
	if m.Protocol == "udp" && m.Engine == "haproxy" {
		return fmt.Errorf("HAProxy does not support UDP forwarding; use nginx")
	}
	if m.Protocol == "https" && m.SSLCertID == nil {
		return fmt.Errorf("https mappings require an SSL certificate")
	}
	if m.RedirectTo == "" && len(m.Targets) == 0 && len(m.PathRoutes) == 0 {
		return fmt.Errorf("at least one target, a path route, or a redirect_to is required")
	}
	if m.RedirectTo != "" && !strings.Contains(m.RedirectTo, "://") {
		return fmt.Errorf("redirect_to must be a full URL, e.g. https://example.com")
	}
	for i, t := range m.Targets {
		if t.Host == "" {
			return fmt.Errorf("target #%d: host is required", i+1)
		}
		if t.Port < 1 || t.Port > 65535 {
			return fmt.Errorf("target #%d: port must be 1-65535", i+1)
		}
		if net.ParseIP(t.Host) == nil && !validHostname(t.Host) {
			return fmt.Errorf("target #%d: invalid host %q", i+1, t.Host)
		}
	}
	if m.Protocol == "http" || m.Protocol == "https" {
		if len(m.ServerNames) == 0 && m.RedirectTo == "" {
			return fmt.Errorf("http/https mappings require at least one server_name (domain)")
		}
		for _, sn := range m.ServerNames {
			if strings.TrimSpace(sn) == "" {
				return fmt.Errorf("server_name cannot be empty")
			}
		}
	}
	if m.PathPrefix != "" {
		if m.Protocol != "http" && m.Protocol != "https" {
			return fmt.Errorf("path_prefix is only supported for http/https mappings")
		}
		if !strings.HasPrefix(m.PathPrefix, "/") || strings.ContainsAny(m.PathPrefix, " \t{};") {
			return fmt.Errorf("path_prefix must start with / and contain no spaces or special characters")
		}
	}
	if !ValidBalance(m.Engine, m.Protocol, m.Balance) {
		return fmt.Errorf("balance %q is not valid for %s/%s", m.Balance, m.Engine, m.Protocol)
	}
	for i, r := range m.AccessRules {
		if r.Action != "allow" && r.Action != "deny" {
			return fmt.Errorf("access rule #%d: action must be allow or deny", i+1)
		}
		if _, _, err := net.ParseCIDR(r.Value); err != nil {
			if net.ParseIP(r.Value) == nil {
				return fmt.Errorf("access rule #%d: value %q is not a valid IP or CIDR", i+1, r.Value)
			}
		}
	}
	if err := validatePathRoutes(m); err != nil {
		return err
	}
	if err := validateRouteRules(m); err != nil {
		return err
	}
	if m.Decoy != "" && m.Decoy != "builtin" && m.Decoy != "custom" {
		return fmt.Errorf("decoy must be empty, \"builtin\" or \"custom\"")
	}
	if m.Decoy == "custom" && strings.TrimSpace(m.DecoyHTML) == "" {
		return fmt.Errorf("custom decoy requires decoy_html")
	}
	if m.Decoy != "" {
		if m.Protocol != "http" && m.Protocol != "https" {
			return fmt.Errorf("decoy site is only supported for http/https mappings")
		}
		if m.Engine != "nginx" {
			return fmt.Errorf("decoy site is only supported by the nginx engine")
		}
		if m.RedirectTo != "" {
			return fmt.Errorf("decoy site cannot be combined with redirect_to")
		}
	}
	if m.ListenPort == panelPort {
		return fmt.Errorf("listen_port %d is the PortGuard panel port", panelPort)
	}
	// conflict check against all other enabled mappings. A wildcard bind
	// (0.0.0.0 / :: / empty) conflicts with ANY ip on the same port, since
	// only one socket can own it.
	for _, other := range all {
		if other.ID == m.ID || !other.Enabled || !m.Enabled {
			continue
		}
		if other.ListenPort == m.ListenPort && listenIPsOverlap(other.ListenIP, m.ListenIP) {
			return fmt.Errorf("listen conflict: mapping #%d (%s) already uses %s:%d",
				other.ID, other.Name, other.ListenIP, other.ListenPort)
		}
	}
	return nil
}

// listenIPsOverlap reports whether two listen addresses would fight for the
// same socket: equal addresses, or either side being a wildcard.
func listenIPsOverlap(a, b string) bool {
	norm := func(ip string) string {
		if ip == "" || ip == "*" {
			return "0.0.0.0"
		}
		return ip
	}
	a, b = norm(a), norm(b)
	if a == b {
		return true
	}
	wildA := a == "0.0.0.0" || a == "::"
	wildB := b == "0.0.0.0" || b == "::"
	// v4 wildcard vs v6 wildcard both try to own every port on all
	// interfaces — treat any wildcard pair as overlapping for the same port
	return wildA || wildB
}

// validateRouteRules checks the ordered path-routing rules: prefixes must
// start with "/", be unique, and each rule needs targets or a redirect.
func validateRouteRules(m store.Mapping) error {
	if len(m.Routes) == 0 {
		return nil
	}
	if m.Protocol != "http" && m.Protocol != "https" {
		return fmt.Errorf("route rules are only supported for http/https mappings")
	}
	if m.RedirectTo != "" {
		return fmt.Errorf("route rules cannot be combined with redirect_to")
	}
	if m.Engine != "nginx" {
		// HAProxy path routing needs ACLs per rule — supported but with a
		// stricter shape: no wildcards, plain prefixes only
		for i, r := range m.Routes {
			if strings.Contains(r.Path, "*") {
				return fmt.Errorf("route rule #%d: HAProxy routing uses plain prefixes (no * wildcards)", i+1)
			}
		}
	}
	seen := map[string]bool{}
	for i, r := range m.Routes {
		if !strings.HasPrefix(r.Path, "/") {
			return fmt.Errorf("route rule #%d: path must start with /", i+1)
		}
		if hasControlChars(r.Path) {
			return fmt.Errorf("route rule #%d: path contains control characters", i+1)
		}
		if seen[r.Path] {
			return fmt.Errorf("route rule #%d: duplicate path %q", i+1, r.Path)
		}
		seen[r.Path] = true
		if r.Redirect == "" && len(r.Targets) == 0 {
			return fmt.Errorf("route rule #%d: needs at least one target or a redirect", i+1)
		}
		for _, t := range r.Targets {
			if t.Port < 1 || t.Port > 65535 || t.Host == "" {
				return fmt.Errorf("route rule #%d: invalid target %s:%d", i+1, t.Host, t.Port)
			}
			if hasControlChars(t.Host) {
				return fmt.Errorf("route rule #%d: target host contains control characters", i+1)
			}
		}
	}
	return nil
}

// RouteRulePathForNginx converts a rule path to an nginx location prefix:
// "/ws/*" becomes the prefix "/ws/" (the wildcard is implied by prefix
// matching), anything else is used verbatim.
func RouteRulePathForNginx(p string) string {
	if strings.HasSuffix(p, "/*") {
		return strings.TrimSuffix(p, "*")
	}
	return p
}

// hasControlChars reports whether s contains characters that could break out
// of a config directive line (newline, CR, NUL or other control bytes).
func hasControlChars(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func validHostname(h string) bool {
	h = strings.TrimSuffix(strings.ToLower(h), ".")
	if h == "" || len(h) > 253 {
		return false
	}
	for _, part := range strings.Split(h, ".") {
		if part == "" || len(part) > 63 {
			return false
		}
		if strings.HasPrefix(part, "-") || strings.HasSuffix(part, "-") {
			return false
		}
		for _, r := range part {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
				return false
			}
		}
	}
	return true
}

// validPathPrefixToken reports whether s is a safe URL path segment (the
// path-routing prefix, e.g. "ws" in /ws/10000).
func validPathPrefixToken(s string) bool {
	if s == "" || len(s) > 32 || s != pathClean(s) {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func pathClean(s string) string {
	return strings.Trim(strings.ReplaceAll(s, "/", ""), " ")
}

// validatePathRoutes checks the dynamic port-in-path transport routes
// (ws / httpupgrade / xhttp). They are L7-only and need the transport
// prefixes to be unique within the mapping.
func validatePathRoutes(m store.Mapping) error {
	if len(m.PathRoutes) == 0 {
		return nil
	}
	if m.Protocol != "http" && m.Protocol != "https" {
		return fmt.Errorf("path routes are only supported for http/https mappings")
	}
	if m.RedirectTo != "" {
		return fmt.Errorf("path routes cannot be combined with redirect_to")
	}
	seenPrefix := map[string]bool{}
	seenTransport := map[string]bool{}
	for i, pr := range m.PathRoutes {
		if !store.ValidPathTransport(string(pr.Transport)) {
			return fmt.Errorf("path route #%d: transport must be ws, httpupgrade or xhttp", i+1)
		}
		if seenTransport[string(pr.Transport)] {
			return fmt.Errorf("path route #%d: duplicate transport %q (one route per transport)", i+1, pr.Transport)
		}
		seenTransport[string(pr.Transport)] = true
		if !validPathPrefixToken(pr.Prefix) {
			return fmt.Errorf("path route #%d: prefix %q must be 1-32 chars of [a-zA-Z0-9_-], no slashes", i+1, pr.Prefix)
		}
		if seenPrefix[pr.Prefix] {
			return fmt.Errorf("path route #%d: duplicate prefix %q — prefixes must be unique", i+1, pr.Prefix)
		}
		seenPrefix[pr.Prefix] = true
		def := pr.MinPort == 0 && pr.MaxPort == 0
		if !def {
			if pr.MinPort < 1 || pr.MaxPort > 65535 || pr.MinPort > pr.MaxPort {
				return fmt.Errorf("path route #%d: port range must satisfy 1 <= min_port <= max_port <= 65535", i+1)
			}
		}
		if pr.Prefix == strings.TrimPrefix(m.PathPrefix, "/") {
			return fmt.Errorf("path route #%d: prefix %q collides with the mapping's path_prefix", i+1, pr.Prefix)
		}
	}
	return nil
}

// backendLine renders one HAProxy server line; prefix is the unique server name (e.g. "s0").
func backendLine(prefix string, t store.Target, check bool) string {
	w := ""
	if t.Weight > 0 && t.Weight != 1 {
		w = fmt.Sprintf(" weight %d", t.Weight)
	}
	bk := ""
	if t.Backup {
		bk = " backup"
	}
	chk := ""
	if check {
		chk = " check"
	}
	return fmt.Sprintf("    server %s %s:%d%s%s%s", prefix, t.Host, t.Port, w, bk, chk)
}
