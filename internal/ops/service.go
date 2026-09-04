package ops

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Runner executes a command with list args (no shell) and returns combined output.
type Runner func(timeout time.Duration, name string, args ...string) (string, error)

func SysctlRunner(timeout time.Duration, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// ServiceManager controls nginx/haproxy through systemd, mirroring
// haproxy-manager's ServiceManager (safe: argv list, no shell).
type ServiceManager struct {
	Run     Runner
	Service string // systemd unit name
}

func NewServiceManager(service string) *ServiceManager {
	if service == "" {
		service = "unknown"
	}
	return &ServiceManager{Run: SysctlRunner, Service: service}
}

func (m *ServiceManager) run(timeout time.Duration, args ...string) (string, error) {
	if m.Run == nil {
		m.Run = SysctlRunner
	}
	return m.Run(timeout, "systemctl", args...)
}

func validServiceName(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, c := range s {
		ok := c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == '@'
		if !ok {
			return false
		}
	}
	return true
}

// Status returns the systemd unit status: active state, sub state, unit enabled state and main PID.
func (m *ServiceManager) Status() (map[string]string, error) {
	if !validServiceName(m.Service) {
		return nil, fmt.Errorf("invalid service name %q", m.Service)
	}
	res := map[string]string{"unit": m.Service}
	props := map[string]string{
		"ActiveState":   "active",
		"SubState":      "sub",
		"UnitFileState": "enabled_state",
		"MainPID":       "pid",
		"Description":   "description",
	}
	for prop, key := range props {
		out, err := m.run(5*time.Second, "show", m.Service, "-p", prop, "--value")
		if err != nil {
			return nil, fmt.Errorf("systemctl show %s: %s", prop, strings.TrimSpace(out))
		}
		res[key] = strings.TrimSpace(strings.SplitN(out, "\n", 2)[0])
	}
	return res, nil
}

// Action performs a whitelisted systemd action on the unit.
// "reload" falls back to restart when the unit does not support reload.
func (m *ServiceManager) Action(action string) (string, error) {
	if !validServiceName(m.Service) {
		return "", fmt.Errorf("invalid service name %q", m.Service)
	}
	switch action {
	case "start", "stop", "restart", "enable", "disable":
	case "reload":
		if out, err := m.run(30*time.Second, "reload", m.Service); err == nil {
			return out, nil
		}
		out, err := m.run(30*time.Second, "reload-or-restart", m.Service)
		if err != nil {
			return out, fmt.Errorf("systemctl reload %s: %s", m.Service, strings.TrimSpace(out))
		}
		return out, nil
	default:
		return "", fmt.Errorf("unsupported action %q (use start/stop/restart/reload/enable/disable)", action)
	}
	out, err := m.run(30*time.Second, action, m.Service)
	if err != nil {
		return out, fmt.Errorf("systemctl %s %s: %s", action, m.Service, strings.TrimSpace(out))
	}
	return out, nil
}

// IsRunning reports the unit's active state.
func (m *ServiceManager) IsRunning() bool {
	st, err := m.Status()
	if err != nil {
		return false
	}
	return st["active"] == "active"
}
