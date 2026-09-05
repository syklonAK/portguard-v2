// Package conntrack samples live TCP connections on the host (via `ss`) and
// turns them into a security-focused live log: who is connected, to which
// port, through which process — with managed/unmanaged classification.
package conntrack

import (
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"portguard/internal/store"
)

// Sampler periodically runs Collect and persists the snapshot.
type Sampler struct {
	st        *store.Store
	panelPort int
	mu        sync.Mutex
	lastCount int
	onChange  func(count int)
}

func NewSampler(st *store.Store, panelPort int) *Sampler {
	return &Sampler{st: st, panelPort: panelPort}
}

func (s *Sampler) OnChange(fn func(count int)) { s.onChange = fn }

func (s *Sampler) Run(interval time.Duration, stop <-chan struct{}) {
	t := time.NewTicker(interval)
	defer t.Stop()
	s.sample() // initial
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			s.sample()
		}
	}
}

func (s *Sampler) sample() {
	conns, err := Collect()
	if err != nil {
		return
	}
	// enrich before persisting so the DB flags (managed/inner/self) and the
	// top-talker SQL filters are meaningful
	mappings, _ := s.st.ListMappings()
	EnrichManaged(conns, mappings, s.panelPort)
	now := time.Now().Unix()
	if err := s.st.ReplaceConnections(conns, now); err != nil {
		return
	}
	s.mu.Lock()
	changed := len(conns) != s.lastCount
	s.lastCount = len(conns)
	s.mu.Unlock()
	if changed && s.onChange != nil {
		s.onChange(len(conns))
	}
}

// addr splits "1.2.3.4:5678" / "[::1]:22" into ip and port.
func addr(s string) (string, int) {
	if strings.HasPrefix(s, "[") {
		end := strings.Index(s, "]")
		if end < 0 {
			return s, 0
		}
		ip := s[1:end]
		rest := s[end+1:]
		port, _ := strconv.Atoi(strings.TrimPrefix(rest, ":"))
		return ip, port
	}
	if i := strings.LastIndex(s, ":"); i >= 0 {
		ip := s[:i]
		port, _ := strconv.Atoi(s[i+1:])
		return ip, port
	}
	return s, 0
}

// Collect parses `ss -tnp` output into live connection entries. Only
// established connections from remote (non-loopback) sources are kept —
// this is the inbound-traffic security view.
func Collect() ([]store.ConnEntry, error) {
	out, err := exec.Command("ss", "-tnp").CombinedOutput()
	if err != nil {
		// ss missing (non-Linux dev box) — empty snapshot, not an error
		if strings.Contains(string(out), "not found") || len(out) == 0 {
			return nil, nil
		}
		return nil, err
	}
	var entries []store.ConnEntry
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "State") || strings.HasPrefix(line, "Recv-Q") {
			continue
		}
		// fields: State Recv-Q Send-Q LocalAddr:Port PeerAddr:Port [process...]
		f := strings.Fields(line)
		if len(f) < 5 {
			continue
		}
		state := f[0]
		if state != "ESTAB" {
			continue
		}
		localIP, localPort := addr(f[3])
		peerIP, peerPort := addr(f[4])

		// inbound view: the peer must be remote and connect to a local listening port
		if isLocal(peerIP) {
			continue
		}
		_ = localPort
		entry := store.ConnEntry{
			SrcIP:   peerIP,
			SrcPort: peerPort,
			DstIP:   localIP,
			DstPort: localPort,
			State:   "ESTAB",
		}
		// process info: users:(("name",pid=123,fd=4))
		if idx := strings.Index(line, "users:"); idx >= 0 {
			entry.Process, entry.PID = parseProc(line[idx:])
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// parseProc extracts the process name and pid from `users:(("nginx",pid=1234,fd=9))`.
func parseProc(s string) (string, int) {
	open := strings.Index(s, "((")
	close_ := strings.Index(s, "))")
	if open < 0 || close_ < open {
		return "", 0
	}
	inner := s[open+2 : close_]
	parts := strings.Split(inner, ",")
	if len(parts) == 0 {
		return "", 0
	}
	name := strings.Trim(parts[0], "\"")
	pid := 0
	for _, p := range parts[1:] {
		if strings.HasPrefix(p, "pid=") {
			pid, _ = strconv.Atoi(p[4:])
			break
		}
	}
	return name, pid
}

func isLocal(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return true
	}
	return parsed.IsLoopback() || parsed.IsUnspecified() || ip == ""
}

// EnrichManaged fills the Managed flag: a connection is "managed" when its
// local port belongs to an enabled mapping (nginx/haproxy front), or when it
// is served by nginx/haproxy/xray itself. Self marks connections to the panel.
func EnrichManaged(entries []store.ConnEntry, mappings []store.Mapping, panelPort int) {
	managedPorts := map[int]bool{}
	for _, m := range mappings {
		if m.Enabled {
			managedPorts[m.ListenPort] = true
		}
	}
	for i := range entries {
		e := &entries[i]
		e.Managed = managedPorts[e.DstPort] ||
			e.Process == "nginx" || e.Process == "haproxy" || e.Process == "xray"
		e.Inner = isLocal(e.DstIP)
		e.Self = e.DstPort == panelPort
	}
}
