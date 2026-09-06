package proxy

import (
	"strconv"
	"strings"

	"portguard/internal/store"
)

// NginxSites is the structured result of parsing live nginx http/stream
// config: each server{} or stream server{} becomes a mapping candidate.
type NginxSites struct {
	HTTPServers     []NginxServer       `json:"http_servers"`
	StreamServers   []NginxServer       `json:"stream_servers"`
	Skipped         []string            `json:"skipped"`   // unparseable/foreign blocks
	UpstreamsByName map[string][]string `json:"upstreams"` // upstream name -> host:port list
}

// NginxServer is one server block (http) or server{} in stream{}.
type NginxServer struct {
	ListenPort  int             `json:"listen_port"`
	ListenSSL   bool            `json:"listen_ssl"`
	ServerNames []string        `json:"server_names"`
	Locations   []NginxLocation `json:"locations"`
	Return      string          `json:"return,omitempty"` // return 301 <url>
	IsDefault   bool            `json:"is_default"`
}

// NginxLocation is one location block.
type NginxLocation struct {
	Path      string `json:"path"`
	ProxyPass string `json:"proxy_pass,omitempty"`
	Return    string `json:"return,omitempty"`
	IsRegex   bool   `json:"is_regex"`
}

// ParseNginxConfig parses server blocks, locations and upstreams from a
// live nginx.conf (http and stream sections). Line-oriented with a small
// state machine: upstream{} / server{} / location{} nesting.
func ParseNginxConfig(text string) NginxSites {
	out := NginxSites{UpstreamsByName: map[string][]string{}}
	var (
		inUpstream string // "" = not in upstream
		inStream   bool   // inside stream{}
		curServer  *NginxServer
		curLoc     *NginxLocation
	)
	// track what stream{} and upstream{} we opened so plain `}` closes them
	upstreamDepth := 0
	streamDepth := 0

	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		directive := strings.TrimSpace(strings.TrimSuffix(line, ";"))
		fields := strings.Fields(directive)
		if len(fields) == 0 {
			continue
		}
		opensBlock := strings.HasSuffix(line, "{")

		// ---- upstream NAME { ----
		if inUpstream == "" && len(fields) >= 2 && fields[0] == "upstream" && opensBlock {
			inUpstream = fields[1]
			upstreamDepth = 1
			continue
		}
		if inUpstream != "" {
			if line == "}" {
				upstreamDepth--
				if upstreamDepth == 0 {
					inUpstream = ""
				}
				continue
			}
			if len(fields) >= 2 && fields[0] == "server" {
				out.UpstreamsByName[inUpstream] = append(out.UpstreamsByName[inUpstream], fields[1])
			}
			continue
		}

		// ---- stream { ----
		if directive == "stream {" {
			inStream = true
			streamDepth = 0
			continue
		}
		if inStream && directive == "}" && streamDepth == 0 {
			inStream = false
			continue
		}

		// ---- server { ----
		if curServer == nil && fields[0] == "server" && opensBlock {
			s := NginxServer{}
			curServer = &s
			if inStream {
				streamDepth++
			}
			continue
		}

		// ---- location X { ----
		if curServer != nil && curLoc == nil && fields[0] == "location" && opensBlock {
			path := strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(directive, "{"), ""))
			path = strings.TrimSpace(strings.Join(fields[1:], " "))
			path = strings.TrimSuffix(path, "{")
			path = strings.TrimSpace(path)
			isRegex := strings.HasPrefix(path, "~")
			path = strings.TrimPrefix(strings.TrimPrefix(path, "~*"), "~")
			l := NginxLocation{Path: path, IsRegex: isRegex}
			curServer.Locations = append(curServer.Locations, l)
			curLoc = &curServer.Locations[len(curServer.Locations)-1]
			continue
		}

		// ---- closing braces ----
		if directive == "}" {
			if curLoc != nil {
				curLoc = nil
			} else if curServer != nil {
				if curServer.ListenPort > 0 && !isPortGuardManaged(*curServer) {
					if inStream {
						out.StreamServers = append(out.StreamServers, *curServer)
					} else {
						out.HTTPServers = append(out.HTTPServers, *curServer)
					}
				}
				curServer = nil
				if inStream && streamDepth > 0 {
					streamDepth--
				}
			}
			continue
		}

		// ---- directives ----
		if curLoc != nil {
			switch fields[0] {
			case "proxy_pass":
				if len(fields) >= 2 {
					curLoc.ProxyPass = fields[1]
				}
			case "return":
				if len(fields) >= 2 && fields[1] == "404" {
					curLoc.Return = "404"
				}
			}
			continue
		}
		if curServer != nil {
			switch fields[0] {
			case "listen":
				if len(fields) >= 2 {
					addr := fields[1]
					port := 80
					if i := strings.LastIndex(addr, ":"); i >= 0 {
						if p, err := strconv.Atoi(addr[i+1:]); err == nil {
							port = p
						}
					} else if p, err := strconv.Atoi(addr); err == nil {
						// bare port: "listen 443 ssl;"
						port = p
					} else if addr == "*" || addr == "0.0.0.0" {
						// wildcard without port keeps the default
					}
					curServer.ListenPort = port
					if containsTok(fields, "ssl") {
						curServer.ListenSSL = true
					}
					if containsTok(fields, "default_server") {
						curServer.IsDefault = true
					}
				}
			case "server_name":
				for _, n := range fields[1:] {
					if n == "_" {
						continue
					}
					curServer.ServerNames = append(curServer.ServerNames, n)
				}
			case "return":
				if len(fields) >= 3 && fields[1] == "301" {
					curServer.Return = strings.Join(fields[2:], " ")
				}
			case "proxy_pass": // stream{} servers use proxy_pass directly
				if len(fields) >= 2 && inStream {
					curServer.Return = "" // stream target handled in conversion
					// stash the stream target in the first location
					l := NginxLocation{Path: "/", ProxyPass: fields[1]}
					curServer.Locations = append(curServer.Locations, l)
				}
			}
			continue
		}
	}
	return out
}

func containsTok(fields []string, s string) bool {
	for _, f := range fields {
		if f == s {
			return true
		}
	}
	return false
}

// isPortGuardManaged filters out blocks the panel itself generated.
func isPortGuardManaged(s NginxServer) bool {
	for _, n := range s.ServerNames {
		if strings.Contains(n, "portguard") {
			return true
		}
	}
	return false
}

// NginxSitesToMappings converts parsed server blocks into PortGuard mapping
// candidates. Simple reverse proxies and redirects convert cleanly; complex
// blocks become partially-filled mappings and are reported in issues — they
// are created DISABLED so nothing goes live unsupervised.
func NginxSitesToMappings(sites NginxSites, unknown map[string]bool) (out []store.Mapping, issues []ConversionIssue) {
	if unknown == nil {
		unknown = map[string]bool{}
	}
	for _, s := range sites.HTTPServers {
		if s.IsDefault && len(s.ServerNames) == 0 && len(s.Locations) == 0 {
			issues = append(issues, ConversionIssue{Section: "http default server", Reason: "empty catch-all — skipped"})
			continue
		}
		m := store.Mapping{
			Enabled:     false, // imports start disabled until reviewed
			Engine:      "nginx",
			ListenIP:    "0.0.0.0",
			ListenPort:  s.ListenPort,
			ServerNames: s.ServerNames,
			Notes:       "imported from the existing nginx config",
		}
		if s.ListenSSL {
			m.Protocol = "https"
		} else {
			m.Protocol = "http"
		}
		if s.Return != "" {
			m.RedirectTo = s.Return
		}
		found := false
		for _, loc := range s.Locations {
			if loc.ProxyPass == "" {
				continue
			}
			targets := resolveProxyPass(loc.ProxyPass, sites.UpstreamsByName)
			if len(targets) == 0 {
				continue
			}
			if !found {
				m.Targets = targets
				if loc.Path != "/" && !loc.IsRegex {
					m.PathPrefix = loc.Path
				}
				found = true
			}
		}
		if s.Return == "" && !found {
			issues = append(issues, ConversionIssue{
				Section: "http :" + strconv.Itoa(s.ListenPort) + " (" + strings.Join(s.ServerNames, ",") + ")",
				Reason:  "no simple proxy_pass/redirect found — review manually",
			})
		}
		m.Name = uniqueMappingName("imported-nginx-"+strconv.Itoa(s.ListenPort), unknown)
		unknown[m.Name] = true
		out = append(out, m)
	}
	for _, s := range sites.StreamServers {
		m := store.Mapping{
			Enabled: false, Engine: "nginx", Protocol: "tcp",
			ListenIP: "0.0.0.0", ListenPort: s.ListenPort,
			Notes: "imported from the existing nginx stream config",
		}
		if len(s.Locations) > 0 {
			targets := resolveProxyPass(s.Locations[0].ProxyPass, sites.UpstreamsByName)
			if len(targets) > 0 {
				m.Targets = targets
			}
		}
		if len(m.Targets) == 0 {
			issues = append(issues, ConversionIssue{Section: "stream :" + strconv.Itoa(s.ListenPort), Reason: "stream target not parsed — fill it in"})
		}
		m.Name = uniqueMappingName("imported-stream-"+strconv.Itoa(s.ListenPort), unknown)
		unknown[m.Name] = true
		out = append(out, m)
	}
	return out, issues
}

// resolveProxyPass turns "http://upstream_name", "http://host:port" and
// "http://127.0.0.1:1234/path" into mapping targets.
func resolveProxyPass(pass string, upstreams map[string][]string) []store.Target {
	pass = strings.TrimPrefix(pass, "http://")
	pass = strings.TrimPrefix(pass, "https://")
	if i := strings.Index(pass, "/"); i >= 0 {
		pass = pass[:i]
	}
	if hosts, ok := upstreams[pass]; ok {
		var targets []store.Target
		for _, h := range hosts {
			if t, ok := parseHostPort(h); ok {
				targets = append(targets, t)
			}
		}
		return targets
	}
	if t, ok := parseHostPort(pass); ok {
		return []store.Target{t}
	}
	return nil
}

func parseHostPort(s string) (store.Target, bool) {
	s = strings.TrimSuffix(s, ";")
	i := strings.LastIndex(s, ":")
	if i <= 0 || i == len(s)-1 {
		return store.Target{}, false
	}
	host := strings.Trim(s[:i], "[]")
	port, err := strconv.Atoi(s[i+1:])
	if err != nil {
		return store.Target{}, false
	}
	return store.Target{Host: host, Port: port}, true
}
