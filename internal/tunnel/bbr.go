// Package tunnel: host network tuning (BBR + fq + buffers) — the PortGuard
// port of hedioum-allinone's enable_bbr. Writes one sysctl drop-in,
// applies it and reports whether BBR is actually active (very old kernels
// may lack tcp_bbr; the apply still succeeds for the buffer tunables).
package tunnel

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// SysctlTuningFile is the drop-in written by ApplyBBR.
const SysctlTuningFile = "/etc/sysctl.d/99-portguard-tuning.conf"

// bbrSysctl is the exact tuning set the bash script shipped (same values).
const bbrSysctl = `# managed by PortGuard — network tuning (BBR + fq)
net.core.default_qdisc = fq
net.ipv4.tcp_congestion_control = bbr
net.core.rmem_max = 16777216
net.core.wmem_max = 16777216
net.core.somaxconn = 65535
net.ipv4.tcp_rmem = 4096 87380 16777216
net.ipv4.tcp_wmem = 4096 65536 16777216
net.ipv4.tcp_fastopen = 3
net.ipv4.tcp_max_syn_backlog = 65535
net.ipv4.ip_local_port_range = 10240 65000
net.ipv4.tcp_fin_timeout = 15
net.ipv4.tcp_slow_start_after_idle = 0
fs.file-max = 1000000
`

// BBRStatus reports the current host state before/after tuning.
type BBRStatus struct {
	SysctlFile   bool   `json:"sysctl_file"`
	Available    bool   `json:"available"`      // tcp_bbr listed in /proc/sys/net/ipv4/tcp_available_congestion_control
	Active       bool   `json:"active"`         // current cc == bbr
	CurrentCC    string `json:"current_cc"`     // e.g. cubic
	Qdisc        string `json:"qdisc,omitempty"` // default qdisc (fq after tuning)
	SysctlOutput string `json:"sysctl_output,omitempty"`
}

// DetectBBR reads the live congestion-control state.
func DetectBBR() BBRStatus {
	st := BBRStatus{}
	st.SysctlFile = fileExists(SysctlTuningFile)
	if data, err := os.ReadFile("/proc/sys/net/ipv4/tcp_available_congestion_control"); err == nil {
		st.Available = strings.Contains(string(data), "bbr")
	}
	if data, err := os.ReadFile("/proc/sys/net/ipv4/tcp_congestion_control"); err == nil {
		st.CurrentCC = strings.TrimSpace(string(data))
		st.Active = st.CurrentCC == "bbr"
	}
	if data, err := os.ReadFile("/proc/sys/net/core/default_qdisc"); err == nil {
		st.Qdisc = strings.TrimSpace(string(data))
	}
	return st
}

// ApplyBBR writes the sysctl drop-in, loads tcp_bbr and applies it. It does
// not fail when the kernel cannot enable BBR itself (buffers/qdisc still
// help); the returned status tells the caller what actually took effect.
func ApplyBBR() (BBRStatus, error) {
	if err := os.MkdirAll("/etc/sysctl.d", 0o755); err != nil {
		return DetectBBR(), err
	}
	if err := os.WriteFile(SysctlTuningFile, []byte(bbrSysctl), 0o644); err != nil {
		return DetectBBR(), err
	}
	_ = exec.Command("modprobe", "tcp_bbr").Run()
	out, sysErr := exec.Command("sysctl", "--system").CombinedOutput()
	st := DetectBBR()
	st.SysctlOutput = strings.TrimSpace(string(out))
	if sysErr != nil && !st.Active {
		return st, fmt.Errorf("sysctl --system failed: %s", st.SysctlOutput)
	}
	return st, nil
}
