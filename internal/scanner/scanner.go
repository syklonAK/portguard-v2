package scanner

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"portguard/internal/store"
)

var knownServices = map[string]string{
	"nginx": "web-server", "haproxy": "load-balancer", "caddy": "web-server", "traefik": "proxy",
	"apache2": "web-server", "httpd": "web-server", "lighttpd": "web-server",
	"sshd": "ssh", "dropbear": "ssh",
	"xray": "xray-proxy", "v2ray": "v2ray-proxy", "sing-box": "proxy", "hysteria": "proxy",
	"trojan-go": "proxy", "gost": "proxy", "3x-ui": "panel", "x-ui": "panel", "marzban": "panel",
	"pg-node-service": "pasarguard-node", "main": "pasarguard-node",
	"docker-proxy": "docker", "containerd": "docker", "dockerd": "docker",
	"node": "nodejs", "bun": "nodejs", "deno": "nodejs",
	"python3": "python", "python": "python", "gunicorn": "python", "uvicorn": "python", "python3.14": "python",
	"postgres": "database", "postgresql": "database", "mysqld": "database", "mariadbd": "database",
	"mongod": "database", "redis-server": "database", "valkey": "database",
	"systemd-resolved": "dns", "dnsmasq": "dns", "named": "dns", "unbound": "dns", "coredns": "dns",
	"wg-quick": "wireguard", "wireguard": "wireguard",
	"portguard": "portguard-panel", "./portguard": "portguard-panel",
	"wg": "wireguard", "socat": "relay", "frps": "tunnel", "frpc": "tunnel",
	"code-server": "ide", "grafana": "monitoring", "prometheus": "monitoring",
}

type Scanner struct{}

func New() *Scanner { return &Scanner{} }

// Scan returns listening TCP/UDP sockets with owning process info.
// Primary source is `ss -H -tulnp`; falls back to /proc parsing.
func (s *Scanner) Scan(selfPort int) ([]store.PortEntry, error) {
	ResetPidUserCache()
	entries, err := scanSS()
	if err != nil {
		entries, err = scanProc()
		if err != nil {
			return nil, fmt.Errorf("scan failed (ss and /proc): %w", err)
		}
	}

	// dedupe by (proto, port, listenIP) merging process names
	merged := map[string]*store.PortEntry{}
	for _, e := range entries {
		key := e.Proto + "|" + e.ListenIP + "|" + strconv.Itoa(e.Port)
		if exist, ok := merged[key]; ok {
			if e.Process != "" && !strings.Contains(exist.Process, e.Process) {
				exist.Process = strings.TrimSpace(exist.Process + ", " + e.Process)
			}
			if e.PID != 0 && exist.PID == 0 {
				exist.PID = e.PID
			}
			if e.User != "" && exist.User == "" {
				exist.User = e.User
			}
			continue
		}
		ec := e
		merged[key] = &ec
	}

	out := make([]store.PortEntry, 0, len(merged))
	users := map[int]string{} // pid -> owner name cache
	for _, e := range merged {
		e.Classification = classify(e.Process)
		e.Self = selfPort > 0 && e.Port == selfPort && e.Classification == "portguard-panel"
		if e.User == "" && e.PID != 0 {
			if u, ok := users[e.PID]; ok {
				e.User = u
			} else {
				u := psUser(e.PID)
				users[e.PID] = u
				e.User = u
			}
		}
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Port != out[j].Port {
			return out[i].Port < out[j].Port
		}
		return out[i].Proto < out[j].Proto
	})
	return out, nil
}

func classify(process string) string {
	name := strings.ToLower(strings.TrimSpace(process))
	if name == "" {
		return "unknown"
	}
	// first token of the process name, stripped of paths
	first := name
	if i := strings.IndexByte(first, ','); i >= 0 {
		first = strings.TrimSpace(first[:i])
	}
	first = strings.TrimPrefix(strings.TrimPrefix(first, "./"), "/usr/bin/")
	first = strings.TrimPrefix(first, "/usr/sbin/")
	first = strings.TrimPrefix(first, "/usr/local/bin/")
	first = strings.TrimPrefix(first, "/opt/")
	if cls, ok := knownServices[first]; ok {
		return cls
	}
	// substring match for versioned binaries like xray-1.8, python3.12
	for svc, cls := range knownServices {
		if strings.HasPrefix(first, svc) {
			return cls
		}
	}
	return "other"
}

var (
	// matches ("name",pid=123) for the first and every additional process in users:(("a",pid=1,fd=2),("b",pid=3,fd=4))
	ssProcRe = regexp.MustCompile(`\("([^"]+)",pid=(\d+)`)
)

// scanSS parses `ss -H -tulnp` output.
func scanSS() ([]store.PortEntry, error) {
	out, err := exec.Command("ss", "-H", "-tulnp").CombinedOutput()
	if err != nil {
		// exit code 1 with no listening sockets is still valid output on some systems
		if len(out) == 0 {
			return nil, err
		}
	}
	var entries []store.PortEntry
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		e, ok := parseSSLine(line)
		if ok {
			entries = append(entries, e)
		}
	}
	return entries, nil
}

func parseSSLine(line string) (store.PortEntry, bool) {
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return store.PortEntry{}, false
	}
	proto := strings.TrimSuffix(fields[0], "6") // tcp/tcp6/udp/udp6 -> tcp/udp
	if proto != "tcp" && proto != "udp" {
		return store.PortEntry{}, false
	}
	// tcp lines: Netid State Recv-Q Send-Q LocalAddr:Port PeerAddr:Port Process
	// udp lines: Netid State Recv-Q Send-Q LocalAddr:Port PeerAddr:Port Process (state=UNCONN)
	state := ""
	local := ""
	if proto == "tcp" {
		if len(fields) < 6 {
			return store.PortEntry{}, false
		}
		state = fields[1]
		local = fields[4]
		if state != "LISTEN" {
			return store.PortEntry{}, false
		}
	} else {
		if len(fields) < 5 {
			return store.PortEntry{}, false
		}
		if fields[1] != "UNCONN" {
			// some ss versions put empty state for udp; accept fields where addr is fields[4] anyway
		}
		local = fields[4]
	}
	// find LocalAddr:Port — the field containing a colon, after Recv-Q/Send-Q
	host, portStr, err := splitSSAddr(local)
	if err != nil {
		// fallback: scan fields for first colon-containing field after index 3
		for i := 3; i < len(fields); i++ {
			if strings.Contains(fields[i], ":") && !strings.Contains(fields[i], "/") {
				host, portStr, err = splitSSAddr(fields[i])
				if err == nil {
					local = fields[i]
					break
				}
			}
		}
		if err != nil {
			return store.PortEntry{}, false
		}
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 0 || port > 65535 {
		return store.PortEntry{}, false
	}

	e := store.PortEntry{Port: port, Proto: proto, ListenIP: normalizeIP(host)}

	// process info: users:(("name",pid=...)) possibly multiple, comma-separated
	rest := strings.Join(fields[5:], " ")
	for _, m := range ssProcRe.FindAllStringSubmatch(rest, -1) {
		pid, _ := strconv.Atoi(m[2])
		e.PID = pid
		e.Process = strings.TrimSpace(e.Process + ", " + m[1])
	}
	e.Process = strings.TrimPrefix(e.Process, ", ")
	return e, true
}

func splitSSAddr(a string) (string, string, error) {
	if strings.HasPrefix(a, "[") { // ipv6 [::]:22
		i := strings.LastIndexByte(a, ']')
		if i < 0 || i+1 >= len(a) || a[i+1] != ':' {
			return "", "", fmt.Errorf("bad addr %q", a)
		}
		return a[1:i], a[i+2:], nil
	}
	i := strings.LastIndexByte(a, ':')
	if i < 0 {
		return "", "", fmt.Errorf("bad addr %q", a)
	}
	return a[:i], a[i+1:], nil
}

func normalizeIP(h string) string {
	switch h {
	case "*", "0.0.0.0":
		return "0.0.0.0"
	case "[::]", "::":
		return "::"
	}
	return h
}

var (
	pidUserCache = map[int]string{}
	pidUserMu    sync.Mutex
)

// psUser resolves the owner of a PID via ps (cached; PIDs are re-resolved
// when their cache entry is older than the cache generation, which resets
// each scan so recycled PIDs don't keep stale owners).
var psUserCacheGen uint64

func psUser(pid int) string {
	pidUserMu.Lock()
	if n, ok := pidUserCache[pid]; ok {
		pidUserMu.Unlock()
		return n
	}
	pidUserMu.Unlock()
	name := ""
	if out, err := exec.Command("ps", "-o", "user=", "-p", strconv.Itoa(pid)).Output(); err == nil {
		name = strings.TrimSpace(string(out))
	}
	pidUserMu.Lock()
	pidUserCache[pid] = name
	pidUserMu.Unlock()
	return name
}

// ResetPidUserCache clears the owner cache; called at the start of every
// scan so recycled PIDs resolve to their current owners.
func ResetPidUserCache() {
	pidUserMu.Lock()
	pidUserCache = map[int]string{}
	psUserCacheGen++
	pidUserMu.Unlock()
}

// scanProc is the no-ss fallback: /proc/net/{tcp,tcp6,udp,udp6} + inode->pid map.
func scanProc() ([]store.PortEntry, error) {
	if _, err := os.Stat("/proc/net/tcp"); err != nil {
		return nil, fmt.Errorf("/proc unavailable")
	}
	inodes := mapInodeOwners()
	var entries []store.PortEntry
	add := func(path, proto string, listening func(st string) bool) {
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		for _, line := range strings.Split(string(data), "\n")[1:] {
			f := strings.Fields(strings.TrimSpace(line))
			if len(f) < 10 {
				continue
			}
			if !listening(f[3]) {
				continue
			}
			host, port, err := parseHexAddr(f[1])
			if err != nil {
				continue
			}
			e := store.PortEntry{Port: port, Proto: proto, ListenIP: host}
			if inode, ok := inodes[f[9]]; ok {
				e.PID = inode.pid
				e.Process = inode.name
				e.User = inode.user
			}
			entries = append(entries, e)
		}
	}
	add("/proc/net/tcp", "tcp", func(st string) bool { return st == "0A" })
	add("/proc/net/tcp6", "tcp", func(st string) bool { return st == "0A" })
	add("/proc/net/udp", "udp", func(st string) bool { return st != "FF" && st != "" && st == "07" })
	add("/proc/net/udp6", "udp", func(st string) bool { return st == "07" })
	return entries, nil
}

type procOwner struct{ pid int; name, user string }

func mapInodeOwners() map[string]procOwner {
	out := map[string]procOwner{}
	links, err := os.ReadDir("/proc")
	if err != nil {
		return out
	}
	for _, l := range links {
		pid, err := strconv.Atoi(l.Name())
		if err != nil {
			continue
		}
		comm, _ := os.ReadFile("/proc/" + l.Name() + "/comm")
		name := strings.TrimSpace(string(comm))
		fdDir := "/proc/" + l.Name() + "/fd"
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			link, err := os.Readlink(filepath2(fdDir, fd.Name()))
			if err != nil || !strings.HasPrefix(link, "socket:[") {
				continue
			}
			inode := strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")
			if _, exists := out[inode]; !exists {
				uid := fileOwnerUID("/proc/" + l.Name())
				out[inode] = procOwner{pid: pid, name: name, user: uid}
			}
		}
	}
	return out
}

func filepath2(a, b string) string { return a + "/" + b }

func fileOwnerUID(path string) string {
	out, err := exec.Command("stat", "-c", "%U", path).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// parseHexAddr parses /proc/net hex "00000000:0019".
func parseHexAddr(s string) (string, int, error) {
	i := strings.IndexByte(s, ':')
	if i < 0 {
		return "", 0, fmt.Errorf("bad hex addr")
	}
	ipHex, portHex := s[:i], s[i+1:]
	port, err := strconv.ParseUint(portHex, 16, 16)
	if err != nil {
		return "", 0, err
	}
	return hexIP(ipHex), int(port), nil
}

func hexIP(h string) string {
	if len(h) == 8 { // ipv4 little-endian words
		b := make([]byte, 4)
		for i := 0; i < 4; i++ {
			v, err := strconv.ParseUint(h[i*2:i*2+2], 16, 8)
			if err != nil {
				return h
			}
			b[i] = byte(v)
		}
		return strconv.Itoa(int(b[3])) + "." + strconv.Itoa(int(b[2])) + "." + strconv.Itoa(int(b[1])) + "." + strconv.Itoa(int(b[0]))
	}
	return "::" // ipv6 compressed display omitted in fallback
}
