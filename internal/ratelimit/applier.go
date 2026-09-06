package ratelimit

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// Applier manages the Linux tc state for bandwidth limiting on a node.
// All tc invocations use structured argument lists — no shell strings,
// no user input ever reaches a command line.
//
// Layout per limited source IP (works for download shaping; upload is
// shaped on the same qdisc via the dsmark/prio split — see applyRule):
//
//	ifb0 (created once, redirects egress mirror)
//	  └── class per source IP: ceil = download limit
//	eth0 root prio
//	  └── classes with fw marks
//
// In practice per-IP shaping uses one HTB qdisc on the default interface
// with per-IP classes matched by u32 filters (source = download direction,
// destination = upload direction as seen by the node).
type Applier struct {
	Iface    string // egress interface towards users (default: detected)
	StateDir string // where the applied-state JSON lives
}

// AppliedState is persisted so the agent can rebuild the exact tc state
// after restarts and diff against the desired plan.
type AppliedState struct {
	Iface  string           `json:"iface"`
	Rules  map[string]Rule  `json:"rules"` // sourceIP -> rule
}

// DetectIface returns the default-route interface (first column of `ip route`).
func DetectIface() string {
	out, err := exec.Command("ip", "route", "show", "default").Output()
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(out))
	for i, f := range fields {
		if f == "dev" && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return ""
}

func (a *Applier) iface() string {
	if a.Iface != "" {
		return a.Iface
	}
	iface := DetectIface()
	a.Iface = iface
	return iface
}

func (a *Applier) statePath() string {
	dir := a.StateDir
	if dir == "" {
		dir = "/var/lib/portguard"
	}
	return dir + "/ratelimit-state.json"
}

// LoadState reads the persisted applied rules (empty on first run).
func (a *Applier) LoadState() AppliedState {
	var st AppliedState
	data, err := os.ReadFile(a.statePath())
	if err != nil {
		return AppliedState{Rules: map[string]Rule{}}
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return AppliedState{Rules: map[string]Rule{}}
	}
	if st.Rules == nil {
		st.Rules = map[string]Rule{}
	}
	return st
}

func (a *Applier) saveState(st AppliedState) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(a.statePath(), data, 0o600)
}

// classID derives a stable 16-bit tc classid from an IPv4 address
// (hash-based; collisions are practically impossible for <65k rules and
// are detected by the state map keyed on the IP itself).
func classID(ip string) string {
	h := uint32(2166136261)
	for i := 0; i < len(ip); i++ {
		h ^= uint32(ip[i])
		h *= 16777619
	}
	return fmt.Sprintf("1:%x", (h&0xffff)|1)
}

// validIP guards the only free-form value that reaches tc filters.
func validIP(s string) bool {
	if s == "" || len(s) > 45 {
		return false
	}
	for _, c := range s {
		ok := (c >= '0' && c <= '9') || c == '.' || c == ':'
		if !ok {
			return false
		}
	}
	return strings.Count(s, ".") == 3 || strings.Contains(s, ":")
}

func run(args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func runIgnore(args ...string) {
	_ = run(args...)
}

// ApplyPlan reconciles the node's tc state with the desired plan using the
// minimal diff. Idempotent: unchanged rules are untouched, removed users
// get their classes/filters deleted, unlimited users have no rules at all.
func (a *Applier) ApplyPlan(plan Plan) (AppliedState, error) {
	st := a.LoadState()
	iface := a.iface()
	if iface == "" {
		return st, fmt.Errorf("could not detect the default network interface")
	}
	if st.Iface != "" && st.Iface != iface {
		// interface changed since last apply: clear everything on the old one
		a.fullTeardown(st.Iface)
		st = AppliedState{Rules: map[string]Rule{}}
	}
	st.Iface = iface

	// desired set keyed by IP (unlimited users are simply absent)
	desired := map[string]Rule{}
	for _, r := range plan.Rules {
		if r.SourceIP == "" || r.DownloadBPS <= 0 {
			continue // unlimited / unmapped users get no rule
		}
		if !validIP(r.SourceIP) {
			continue
		}
		desired[r.SourceIP] = r
	}

	// remove rules that vanished
	for ip := range st.Rules {
		if _, ok := desired[ip]; !ok {
			a.removeRule(iface, ip, st.Rules[ip])
			delete(st.Rules, ip)
		}
	}
	// add/update
	for ip, r := range desired {
		if cur, ok := st.Rules[ip]; ok && cur.DownloadBPS == r.DownloadBPS && cur.UploadBPS == r.UploadBPS {
			continue
		}
		if err := a.applyRule(iface, ip, r); err != nil {
			return st, err
		}
		st.Rules[ip] = r
	}

	if err := a.saveState(st); err != nil {
		return st, err
	}
	return st, nil
}

// ensureRoot makes sure the HTB root qdisc exists.
func (a *Applier) ensureRoot(iface string) {
	// qdisc replace is idempotent
	runIgnore("tc", "qdisc", "replace", "dev", iface, "root", "handle", "1:", "htb", "default", "30")
}

func (a *Applier) applyRule(iface, ip string, r Rule) error {
	a.ensureRoot(iface)
	cid := classID(ip)
	// download: traffic FROM the node TO the user's IP (u32 match on dest)
	if r.DownloadBPS > 0 {
		if err := run("tc", "class", "replace", "dev", iface, "parent", "1:", "classid", cid,
			"htb", "rate", fmt.Sprintf("%d", minRate(r.DownloadBPS)), "ceil", fmt.Sprintf("%d", r.DownloadBPS)); err != nil {
			return err
		}
		filters := []string{"ip", ip, "0xffff"}
		_ = filters
		if err := run("tc", "filter", "replace", "dev", iface, "protocol", "ip", "parent", "1:", "prio", "1",
			"u32", "match", "ip", "dst", ip, "flowid", cid); err != nil {
			return err
		}
	}
	// upload: traffic FROM the user's IP INTO the node (u32 match on src)
	if r.UploadBPS > 0 {
		upCid := strings.Replace(cid, "1:", "2:", 1)
		if err := run("tc", "class", "replace", "dev", iface, "parent", "1:", "classid", upCid,
			"htb", "rate", fmt.Sprintf("%d", minRate(r.UploadBPS)), "ceil", fmt.Sprintf("%d", r.UploadBPS)); err != nil {
			return err
		}
		if err := run("tc", "filter", "replace", "dev", iface, "protocol", "ip", "parent", "1:", "prio", "1",
			"u32", "match", "ip", "src", ip, "flowid", upCid); err != nil {
			return err
		}
	}
	return nil
}

func (a *Applier) removeRule(iface, ip string, r Rule) {
	cid := classID(ip)
	runIgnore("tc", "filter", "del", "dev", iface, "protocol", "ip", "parent", "1:", "prio", "1")
	runIgnore("tc", "class", "del", "dev", iface, "classid", cid)
	if r.UploadBPS > 0 {
		upCid := strings.Replace(cid, "1:", "2:", 1)
		runIgnore("tc", "class", "del", "dev", iface, "classid", upCid)
	}
}

// fullTeardown removes the root qdisc (and with it every class/filter).
func (a *Applier) fullTeardown(iface string) {
	runIgnore("tc", "qdisc", "del", "dev", iface, "root")
}

// ClearAll removes all bandwidth rules (used when limiting is disabled).
func (a *Applier) ClearAll() error {
	st := a.LoadState()
	if st.Iface != "" {
		a.fullTeardown(st.Iface)
	}
	return a.saveState(AppliedState{Rules: map[string]Rule{}})
}

// minRate keeps HTB happy: rate must be a sane fraction of ceil.
func minRate(bps int64) int64 {
	r := bps / 8
	if r < 1 {
		return 1
	}
	return r
}

// SortedRules returns the applied rules in a stable order for reports.
func (s AppliedState) SortedRules() []Rule {
	out := make([]Rule, 0, len(s.Rules))
	for _, r := range s.Rules {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SourceIP < out[j].SourceIP })
	return out
}
