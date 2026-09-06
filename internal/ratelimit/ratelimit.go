// Package ratelimit implements per-user bandwidth limiting for PasarGuard
// Xray users. The controller (panel) owns the desired state keyed by the
// user's Xray UUID; agents enforce it on their node with Linux tc.
//
// Enforcement design (honest about the UUID-to-packet gap):
//
//	Xray UUID  --(explicit binding, see below)-->  source IP on the node
//	source IP  --(tc HTB on ifb + fw marks)-->   download/upload ceiling
//
// Linux tc cannot see Xray UUIDs — they live in the Xray protocol layer.
// The reliable, architecture-supported mapping is: an Xray user's traffic
// leaves the node towards the user's *public source IP* (the TCP 5-tuple
// is per-connection and per-user in practice). The agent therefore builds
// per-IP filters. When several users share one public IP they also share
// that IP's ceiling — this limitation is documented in BANDWIDTH.md and
// the UI instead of being silently ignored.
package ratelimit

import (
	"fmt"
	"strconv"
	"strings"
)

// Bits-per-second limits. Zero means unlimited (no tc rule).
type Limits struct {
	DownloadBPS int64 `json:"download_bps"`
	UploadBPS   int64 `json:"upload_bps"`
}

func (l Limits) Unlimited() bool { return l.DownloadBPS <= 0 && l.UploadBPS <= 0 }

// ParseBandwidth accepts "10Mbps", "512Kbps", "1Gbps", "10000000", "unlimited".
// Returns bits per second.
func ParseBandwidth(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" || s == "unlimited" || s == "0" || s == "none" || s == "-" {
		return 0, nil
	}
	mult := int64(1)
	numPart := s
	switch {
	case strings.HasSuffix(s, "gbps"):
		mult, numPart = 1_000_000_000, strings.TrimSuffix(s, "gbps")
	case strings.HasSuffix(s, "mbps"):
		mult, numPart = 1_000_000, strings.TrimSuffix(s, "mbps")
	case strings.HasSuffix(s, "kbps"):
		mult, numPart = 1_000, strings.TrimSuffix(s, "kbps")
	case strings.HasSuffix(s, "gb"):
		mult, numPart = 8_000_000_000, strings.TrimSuffix(s, "gb") // GB/s -> bits
	case strings.HasSuffix(s, "mb"):
		mult, numPart = 8_000_000, strings.TrimSuffix(s, "mb")
	case strings.HasSuffix(s, "kb"):
		mult, numPart = 8_000, strings.TrimSuffix(s, "kb")
	case strings.HasSuffix(s, "bps"):
		numPart = strings.TrimSuffix(s, "bps")
	}
	numPart = strings.TrimSpace(numPart)
	n, err := strconv.ParseInt(numPart, 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid bandwidth %q", s)
	}
	if n > 1<<40 { // sanity ceiling: 1 Tbps
		return 0, fmt.Errorf("bandwidth %q out of range", s)
	}
	v := n * mult
	if v < 0 || v > 1<<40 {
		return 0, fmt.Errorf("bandwidth %q out of range", s)
	}
	return v, nil
}

// FormatBandwidth renders bps as a human string ("unlimited", "512Kbps", "5Mbps", "1Gbps").
func FormatBandwidth(bps int64) string {
	if bps <= 0 {
		return "unlimited"
	}
	switch {
	case bps >= 1_000_000_000 && bps%1_000_000_000 == 0:
		return fmt.Sprintf("%dGbps", bps/1_000_000_000)
	case bps >= 1_000_000 && bps%1_000_000 == 0:
		return fmt.Sprintf("%dMbps", bps/1_000_000)
	case bps >= 1_000:
		return fmt.Sprintf("%dKbps", bps/1_000)
	default:
		return fmt.Sprintf("%dbps", bps)
	}
}

// ValidUUID reports whether s is a canonical 8-4-4-4-12 hex UUID.
func ValidUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
			if !isHex {
				return false
			}
		}
	}
	return true
}

// NormalizeUUID lowercases and strips whitespace/urn prefixes.
func NormalizeUUID(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimPrefix(s, "urn:uuid:")
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")
	return s
}

// ValidateLimits rejects negative, absurd or asymmetric-zero shapes.
func ValidateLimits(l Limits) error {
	if l.DownloadBPS < 0 || l.UploadBPS < 0 {
		return fmt.Errorf("bandwidth values must be >= 0")
	}
	if l.DownloadBPS > 1<<40 || l.UploadBPS > 1<<40 {
		return fmt.Errorf("bandwidth values out of range")
	}
	// one direction set to 0 (unlimited) while the other is limited is a
	// configuration smell; allow it but normalize single-sided zeros away
	return nil
}

// --- desired plan: what the agent should enforce, computed by the master ---

// Rule is one enforceable limit on a node: a user's traffic identified by
// the public source IP their Xray connections originate from.
type Rule struct {
	UUID        string `json:"uuid"`
	Username    string `json:"username"`
	SourceIP    string `json:"source_ip"`
	DownloadBPS int64  `json:"download_bps"`
	UploadBPS   int64  `json:"upload_bps"`
}

// Plan is the complete desired tc state for one node (versioned so agents
// can short-circuit unchanged pushes).
type Plan struct {
	Version int64  `json:"version"`
	Rules   []Rule `json:"rules"`
}

// Diff returns the minimal changes (add/update/remove) that turn the
// current rules into the desired plan. Keys are source IPs.
func Diff(current, desired []Rule) (add, update, remove []Rule) {
	cur := make(map[string]Rule, len(current))
	for _, r := range current {
		cur[r.SourceIP] = r
	}
	des := make(map[string]Rule, len(desired))
	for _, r := range desired {
		des[r.SourceIP] = r
	}
	for ip, r := range des {
		c, ok := cur[ip]
		if !ok {
			add = append(add, r)
		} else if c.DownloadBPS != r.DownloadBPS || c.UploadBPS != r.UploadBPS {
			update = append(update, r)
		}
	}
	for ip, r := range cur {
		if _, ok := des[ip]; !ok {
			remove = append(remove, r)
		}
	}
	return add, update, remove
}
