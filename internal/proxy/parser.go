package proxy

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"portguard/internal/store"
)

// HAProxyCfg is the structured result of parsing a live haproxy.cfg
// (ported from haproxy-manager's config/parser.py). It preserves unknown
// directives so the config view can show them.
type HAProxyCfg struct {
	Global    GlobalCfg    `json:"global"`
	Defaults  DefaultsCfg  `json:"defaults"`
	Frontends []FrontendCfg `json:"frontends"`
	Backends  []BackendCfg  `json:"backends"`
	Listens   []ListenCfg   `json:"listens"`
	Raw       []string     `json:"raw"` // header comments / unparsed lines
}

type GlobalCfg struct {
	Directives   map[string]string `json:"directives"`
	RawLines     []string         `json:"raw_lines"`
	Maxconn      string           `json:"maxconn,omitempty"`
	StatsSocket  string           `json:"stats_socket,omitempty"`
	User         string           `json:"user,omitempty"`
	Group        string           `json:"group,omitempty"`
	Nbthread     string           `json:"nbthread,omitempty"`
}

type DefaultsCfg struct {
	Directives map[string]string `json:"directives"`
	RawLines   []string          `json:"raw_lines"`
	Mode       string            `json:"mode,omitempty"`
	Timeouts   map[string]string `json:"timeouts,omitempty"`
}

type FrontendCfg struct {
	Name          string    `json:"name"`
	Binds         []BindCfg `json:"binds"`
	Mode          string    `json:"mode,omitempty"`
	DefaultBackend string   `json:"default_backend,omitempty"`
	ACLs          []ACLLine `json:"acls"`
	Rules         []RuleLine `json:"rules"`
	Other         []string  `json:"other"` // any directive we don't model
}

type BackendCfg struct {
	Name    string       `json:"name"`
	Mode    string       `json:"mode,omitempty"`
	Balance string       `json:"balance,omitempty"`
	Servers []ServerLine `json:"servers"`
	Other   []string     `json:"other"`
}

type ListenCfg struct {
	Name    string       `json:"name"`
	Mode    string       `json:"mode,omitempty"`
	Binds   []BindCfg    `json:"binds"`
	Servers []ServerLine `json:"servers"`
	Other   []string     `json:"other"`
}

type BindCfg struct {
	IP    string `json:"ip"`
	Port  int    `json:"port"`
	SSL   bool   `json:"ssl,omitempty"`
	Crt   string `json:"crt,omitempty"`
	Extra string `json:"extra,omitempty"`
}

type ACLLine struct {
	Name      string `json:"name"`
	Criterion string `json:"criterion"`
	Value     string `json:"value"`
}

type RuleLine struct {
	Backend   string `json:"backend"`
	Condition string `json:"condition"`
}

type ServerLine struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Port    int    `json:"port"`
	Weight  int    `json:"weight,omitempty"`
	Backup  bool   `json:"backup,omitempty"`
	Check   bool   `json:"check,omitempty"`
}

var haSectionRe = regexp.MustCompile(`^(global|defaults|frontend|backend|listen|userlist|peers|mailers|cache)\s*(.*)$`)

// ParseHAProxyConfig parses a haproxy.cfg text into a structured config,
// preserving unknown sections/directives as raw lines (parser.py port).
func ParseHAProxyConfig(text string) HAProxyCfg {
	cfg := HAProxyCfg{
		Global:    GlobalCfg{Directives: map[string]string{}},
		Defaults:  DefaultsCfg{Directives: map[string]string{}, Timeouts: map[string]string{}},
		Frontends: nil,
		Backends:  nil,
		Listens:   nil,
	}
	type section int
	const (
		secNone section = iota
		secGlobal
		secDefaults
		secFrontend
		secBackend
		secListen
		secUnknown
	)
	cur := secNone
	var fe *FrontendCfg
	var be *BackendCfg
	var li *ListenCfg
	var unknownKey string
	unknownLines := map[string][]string{}

	finish := func() {
		fe, be, li = nil, nil, nil
	}

	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		if m := haSectionRe.FindStringSubmatch(line); m != nil {
			finish()
			sec := strings.ToLower(m[1])
			name := strings.TrimSpace(m[2])
			switch sec {
			case "global":
				cur = secGlobal
			case "defaults":
				cur = secDefaults
			case "frontend":
				cur = secFrontend
				nm := firstToken(name)
				fe = &FrontendCfg{Name: nm, Mode: "http"}
				cfg.Frontends = append(cfg.Frontends, *fe)
				fe = &cfg.Frontends[len(cfg.Frontends)-1]
			case "backend":
				cur = secBackend
				nm := firstToken(name)
				be = &BackendCfg{Name: nm, Mode: "http", Balance: "roundrobin"}
				cfg.Backends = append(cfg.Backends, *be)
				be = &cfg.Backends[len(cfg.Backends)-1]
			case "listen":
				cur = secListen
				nm := firstToken(name)
				li = &ListenCfg{Name: nm, Mode: "tcp"}
				cfg.Listens = append(cfg.Listens, *li)
				li = &cfg.Listens[len(cfg.Listens)-1]
			default: // userlist, peers, mailers, cache
				cur = secUnknown
				unknownKey = sec + " " + name
			}
			continue
		}

		switch cur {
		case secNone:
			cfg.Raw = append(cfg.Raw, raw)
		case secGlobal:
			cfg.Global.RawLines = append(cfg.Global.RawLines, raw)
			key, val := splitDirective(line)
			cfg.Global.Directives[key] = val
			switch key {
			case "maxconn", "nbthread", "nbproc", "user", "group":
				// typed fields
				if key == "user" {
					cfg.Global.User = val
				} else if key == "group" {
					cfg.Global.Group = val
				} else if key == "maxconn" {
					cfg.Global.Maxconn = val
				} else if key == "nbthread" {
					cfg.Global.Nbthread = val
				}
			case "stats":
				if strings.HasPrefix(val, "socket") {
					if tok := firstToken(strings.TrimPrefix(val, "socket")); tok != "" {
						cfg.Global.StatsSocket = tok
					}
				}
			}
		case secDefaults:
			cfg.Defaults.RawLines = append(cfg.Defaults.RawLines, raw)
			key, val := splitDirective(line)
			cfg.Defaults.Directives[key] = val
			if key == "mode" {
				cfg.Defaults.Mode = firstToken(val)
			}
			if key == "timeout" {
				if toks := strings.Fields(val); len(toks) >= 2 {
					cfg.Defaults.Timeouts[toks[0]] = toks[1]
				}
			}
		case secFrontend:
			if fe == nil {
				continue
			}
			low := strings.ToLower(line)
			switch {
			case strings.HasPrefix(low, "bind"):
				fe.Binds = append(fe.Binds, ParseBindLine(line))
			case strings.HasPrefix(low, "mode"):
				fe.Mode = firstToken(strings.TrimPrefix(low, "mode"))
			case strings.HasPrefix(low, "default_backend"):
				fe.DefaultBackend = firstToken(strings.TrimPrefix(low, "default_backend"))
			case strings.HasPrefix(low, "acl"):
				if a := ParseACLLine(line); a != nil {
					fe.ACLs = append(fe.ACLs, *a)
				}
			case strings.HasPrefix(low, "use_backend"):
				if r := ParseUseBackendLine(line); r != nil {
					fe.Rules = append(fe.Rules, *r)
				}
			default:
				fe.Other = append(fe.Other, raw)
			}
		case secBackend:
			if be == nil {
				continue
			}
			low := strings.ToLower(line)
			switch {
			case strings.HasPrefix(low, "mode"):
				be.Mode = firstToken(strings.TrimPrefix(low, "mode"))
			case strings.HasPrefix(low, "balance"):
				be.Balance = firstToken(strings.TrimPrefix(low, "balance"))
			case strings.HasPrefix(low, "server"):
				if s, err := ParseServerLine(line); err == nil {
					be.Servers = append(be.Servers, s)
				} else {
					be.Other = append(be.Other, raw)
				}
			default:
				be.Other = append(be.Other, raw)
			}
		case secListen:
			if li == nil {
				continue
			}
			low := strings.ToLower(line)
			switch {
			case strings.HasPrefix(low, "bind"):
				li.Binds = append(li.Binds, ParseBindLine(line))
			case strings.HasPrefix(low, "mode"):
				li.Mode = firstToken(strings.TrimPrefix(low, "mode"))
			case strings.HasPrefix(low, "server"):
				if s, err := ParseServerLine(line); err == nil {
					li.Servers = append(li.Servers, s)
				} else {
					li.Other = append(li.Other, raw)
				}
			default:
				li.Other = append(li.Other, raw)
			}
		case secUnknown:
			unknownLines[unknownKey] = append(unknownLines[unknownKey], raw)
		}
	}
	finish()
	// re-attach unknown sections as raw lines at the end for the viewer
	for k, lines := range unknownLines {
		cfg.Raw = append(cfg.Raw, append([]string{k}, lines...)...)
	}
	return cfg
}

// ParseBindLine parses "bind *:80 ssl crt /path/x.pem [extra]" (parser.py port).
func ParseBindLine(line string) BindCfg {
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "bind"))
	b := BindCfg{IP: "*", Port: 80}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return b
	}
	addr := fields[0]
	switch {
	case strings.HasPrefix(addr, "["):
		if m := regexp.MustCompile(`^\[([^\]]+)\]:(\d+)$`).FindStringSubmatch(addr); m != nil {
			b.IP = m[1]
			b.Port, _ = strconv.Atoi(m[2])
		} else {
			b.IP = strings.Trim(addr, "[]")
		}
	case strings.Contains(addr, ":"):
		if i := strings.LastIndex(addr, ":"); i >= 0 {
			ipPart := addr[:i]
			if ipPart != "" {
				b.IP = ipPart
			}
			b.Port, _ = strconv.Atoi(addr[i+1:])
		}
	default:
		if p, err := strconv.Atoi(addr); err == nil {
			b.Port = p
		} else {
			b.IP = addr
		}
	}
	extra := fields[1:]
	for i, tok := range extra {
		switch {
		case tok == "ssl":
			b.SSL = true
		case tok == "crt" && i+1 < len(extra):
			b.Crt = extra[i+1]
		}
	}
	return b
}

// ParseACLLine parses "acl name criterion value".
func ParseACLLine(line string) *ACLLine {
	fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "acl")))
	if len(fields) < 2 {
		return nil
	}
	a := &ACLLine{Name: fields[0], Criterion: fields[1]}
	if len(fields) > 2 {
		a.Value = strings.Join(fields[2:], " ")
	}
	return a
}

// ParseUseBackendLine parses "use_backend <be> [if <cond>]".
func ParseUseBackendLine(line string) *RuleLine {
	fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "use_backend")))
	if len(fields) == 0 {
		return nil
	}
	r := &RuleLine{Backend: fields[0]}
	if len(fields) >= 3 && strings.EqualFold(fields[1], "if") {
		r.Condition = strings.Join(fields[2:], " ")
	}
	return r
}

// ParseServerLine parses "server <name> <addr>:<port> [check|backup|weight N|...]".
func ParseServerLine(line string) (ServerLine, error) {
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "server"))
	fields := strings.Fields(rest)
	if len(fields) < 2 {
		return ServerLine{}, fmt.Errorf("invalid server line: %s", line)
	}
	s := ServerLine{Name: fields[0], Port: 80}
	addr := fields[1]
	if strings.Count(addr, ":") == 1 {
		if i := strings.Index(addr, ":"); i >= 0 {
			s.Address = strings.Trim(addr[:i], "[]")
			if p, err := strconv.Atoi(addr[i+1:]); err == nil {
				s.Port = p
			} else {
				s.Address = addr
			}
		}
	} else {
		s.Address = strings.Trim(addr, "[]")
	}
	for i := 2; i < len(fields); i++ {
		tok := fields[i]
		switch {
		case tok == "check":
			s.Check = true
		case tok == "backup":
			s.Backup = true
		case tok == "weight" && i+1 < len(fields):
			s.Weight, _ = strconv.Atoi(fields[i+1])
			i++
		}
	}
	return s, nil
}

func splitDirective(line string) (key, val string) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", ""
	}
	key = strings.ToLower(fields[0])
	if len(fields) > 1 {
		val = strings.Join(fields[1:], " ")
	}
	return key, val
}

func firstToken(s string) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// HAProxyCfgToMappings converts a parsed haproxy.cfg into PortGuard mappings,
// so users can adopt an existing config through the panel (haproxy-manager
// import flow). Only frontends with a single bind + default_backend/listen can
// be represented; complex ones are skipped and reported.
type ConversionIssue struct {
	Section string `json:"section"`
	Reason  string `json:"reason"`
}

func HAProxyCfgToMappings(cfg HAProxyCfg, unknown map[string]bool) (out []store.Mapping, issues []ConversionIssue) {
	if unknown == nil {
		unknown = map[string]bool{}
	}
	usedBE := map[string]bool{}

	// frontends
	for _, fe := range cfg.Frontends {
		if len(fe.Binds) != 1 {
			issues = append(issues, ConversionIssue{Section: "frontend " + fe.Name, Reason: "needs exactly one bind to map to a PortGuard listener"})
			continue
		}
		b := fe.Binds[0]
		if fe.DefaultBackend == "" && len(fe.Rules) == 0 {
			issues = append(issues, ConversionIssue{Section: "frontend " + fe.Name, Reason: "no default_backend/use_backend — cannot determine target"})
			continue
		}
		beName := fe.DefaultBackend
		if beName == "" && len(fe.Rules) > 0 {
			beName = fe.Rules[0].Backend
		}
		be := findBackend(cfg, beName)
		if be == nil || len(be.Servers) == 0 {
			issues = append(issues, ConversionIssue{Section: "frontend " + fe.Name, Reason: "backend " + beName + " not found or has no servers"})
			continue
		}
		m := store.Mapping{
			Enabled:      true,
			Engine:       "haproxy",
			ListenIP:     normalizeBindIP(b.IP),
			ListenPort:   b.Port,
			Targets:      serverLinesToTargets(be.Servers),
			Balance:      be.Balance,
			Notes:        "imported from haproxy.cfg: frontend " + fe.Name + " → backend " + beName,
		}
		switch {
		case b.SSL:
			m.Protocol = "https"
		case fe.Mode == "tcp":
			m.Protocol = "tcp"
		default:
			m.Protocol = "http"
		}
		m.Name = uniqueMappingName("imported-"+fe.Name, unknown)
		unknown[m.Name] = true
		usedBE[beName] = true
		out = append(out, m)
	}

	// listen sections (bind + servers in one block)
	for _, li := range cfg.Listens {
		if len(li.Binds) != 1 {
			issues = append(issues, ConversionIssue{Section: "listen " + li.Name, Reason: "needs exactly one bind"})
			continue
		}
		if len(li.Servers) == 0 {
			issues = append(issues, ConversionIssue{Section: "listen " + li.Name, Reason: "no servers"})
			continue
		}
		b := li.Binds[0]
		m := store.Mapping{
			Enabled:    true,
			Engine:     "haproxy",
			Protocol:   "tcp",
			ListenIP:   normalizeBindIP(b.IP),
			ListenPort: b.Port,
			Targets:    serverLinesToTargets(li.Servers),
			Notes:      "imported from haproxy.cfg: listen " + li.Name,
		}
		m.Name = uniqueMappingName("imported-"+li.Name, unknown)
		unknown[m.Name] = true
		out = append(out, m)
	}
	return out, issues
}

func findBackend(cfg HAProxyCfg, name string) *BackendCfg {
	for i := range cfg.Backends {
		if cfg.Backends[i].Name == name {
			return &cfg.Backends[i]
		}
	}
	return nil
}

func serverLinesToTargets(servers []ServerLine) []store.Target {
	targets := make([]store.Target, 0, len(servers))
	for _, s := range servers {
		t := store.Target{Host: s.Address, Port: s.Port}
		if s.Weight > 0 && s.Weight != 1 {
			t.Weight = s.Weight
		}
		t.Backup = s.Backup
		targets = append(targets, t)
	}
	return targets
}

func normalizeBindIP(ip string) string {
	if ip == "*" || ip == "" {
		return "0.0.0.0"
	}
	return ip
}

func uniqueMappingName(base string, taken map[string]bool) string {
	if !taken[base] {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !taken[candidate] {
			return candidate
		}
	}
}
