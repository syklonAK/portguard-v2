package ops

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"time"
)

// Network diagnostics mirroring haproxy-manager's network/diagnostics module:
// TCP check, DNS resolve, TLS handshake, HTTP probe and a combined backend test.

const diagTimeout = 5 * time.Second

func msSince(start time.Time) float64 {
	return float64(time.Since(start).Microseconds()) / 1000.0
}

// TCPCheck dials host:port and reports latency.
func TCPCheck(host string, port int, timeout time.Duration) map[string]any {
	if timeout <= 0 {
		timeout = diagTimeout
	}
	start := time.Now()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), timeout)
	latency := msSince(start)
	if err != nil {
		return map[string]any{"success": false, "latency_ms": latency, "error": err.Error()}
	}
	conn.Close()
	return map[string]any{"success": true, "latency_ms": latency, "error": nil}
}

// DNSResolve resolves a hostname to its IP list.
func DNSResolve(host string) map[string]any {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), diagTimeout)
	defer cancel()
	res := &net.Resolver{}
	ips, err := res.LookupHost(ctx, host)
	latency := msSince(start)
	if err != nil {
		return map[string]any{"success": false, "ips": []string{}, "latency_ms": latency, "error": err.Error()}
	}
	return map[string]any{"success": true, "ips": ips, "latency_ms": latency, "error": nil}
}

// TLSCheck performs a TLS handshake and returns certificate + cipher info.
func TLSCheck(host string, port int, timeout time.Duration) map[string]any {
	if timeout <= 0 {
		timeout = diagTimeout
	}
	start := time.Now()
	dialer := &net.Dialer{Timeout: timeout}
	cfg := &tls.Config{
		ServerName:         host,
		InsecureSkipVerify: true, // diagnostics must report certs even when invalid
	}
	conn, err := tls.DialWithDialer(dialer, "tcp", net.JoinHostPort(host, strconv.Itoa(port)), cfg)
	latency := msSince(start)
	if err != nil {
		return map[string]any{"success": false, "latency_ms": latency, "error": err.Error()}
	}
	defer conn.Close()
	cs := conn.ConnectionState()
	certInfo := map[string]any{"subject": "", "issuer": "", "not_after": nil, "sans": []string{}}
	if len(cs.PeerCertificates) > 0 {
		c := cs.PeerCertificates[0]
		certInfo["subject"] = c.Subject.String()
		certInfo["issuer"] = c.Issuer.String()
		certInfo["not_after"] = c.NotAfter.Format(time.RFC3339)
		certInfo["sans"] = c.DNSNames
	}
	return map[string]any{
		"success":   true,
		"latency_ms": latency,
		"protocol":  tls.VersionName(cs.Version),
		"cipher":    tls.CipherSuiteName(cs.CipherSuite),
		"cert":      certInfo,
		"error":     nil,
	}
}

// HTTPCheck performs a GET and returns status + headers (no redirects followed).
func HTTPCheck(host string, port int, useTLS bool, path string, hostHeader string, timeout time.Duration) map[string]any {
	if timeout <= 0 {
		timeout = diagTimeout
	}
	if path == "" {
		path = "/"
	}
	scheme := "http"
	if useTLS {
		scheme = "https"
	}
	start := time.Now()
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true, ServerName: hostHeader},
			DisableKeepAlives: true,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	url := fmt.Sprintf("%s://%s:%d%s", scheme, host, port, path)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return map[string]any{"success": false, "latency_ms": msSince(start), "error": err.Error()}
	}
	req.Header.Set("User-Agent", "PortGuard-Diagnostics/2.0")
	if hostHeader != "" {
		req.Host = hostHeader
	}
	resp, err := client.Do(req)
	if err != nil {
		return map[string]any{"success": false, "latency_ms": msSince(start), "error": err.Error()}
	}
	defer resp.Body.Close()
	hdrs := map[string]string{}
	for k, v := range resp.Header {
		if len(v) > 0 {
			hdrs[k] = v[0]
		}
	}
	return map[string]any{
		"success":    true,
		"status":     resp.StatusCode,
		"reason":     resp.Status,
		"latency_ms": msSince(start),
		"headers":    hdrs,
		"error":      nil,
	}
}

// BackendTest runs the full chain: DNS → TCP → TLS → HTTP (like haproxy-manager's
// Backend Connection Test). Skipped steps carry success=null.
func BackendTest(host string, port int, useTLS bool, path string, hostHeader string) map[string]any {
	out := map[string]any{}
	dns := DNSResolve(host)
	out["dns"] = map[string]any{"success": dns["success"], "ips": dns["ips"], "latency_ms": dns["latency_ms"], "error": dns["error"]}

	tcp := TCPCheck(host, port, 0)
	out["tcp"] = tcp
	tcpOK, _ := tcp["success"].(bool)

	if useTLS || port == 443 {
		out["tls"] = TLSCheck(host, port, 0)
	} else {
		out["tls"] = map[string]any{"success": nil, "skipped": true}
	}

	if tcpOK {
		if useTLS || port == 443 || port == 80 || port == 8080 || port == 8443 {
			out["http"] = HTTPCheck(host, port, useTLS, path, hostHeader, 0)
		} else {
			out["http"] = map[string]any{"success": nil, "skipped": true}
		}
	} else {
		out["http"] = map[string]any{"success": false, "error": "TCP failed, skipping HTTP"}
	}
	return out
}

// ListeningPorts returns the raw `ss -tulpn` / netstat output.
func ListeningPorts() (string, error) {
	for _, cmd := range [][]string{
		{"ss", "-tulpn"},
		{"netstat", "-tulpn"},
	} {
		if _, err := exec.LookPath(cmd[0]); err == nil {
			out, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput()
			if err != nil && len(out) == 0 {
				continue
			}
			return string(out), nil
		}
	}
	return "", fmt.Errorf("neither ss nor netstat is available")
}

// MarshalDiag converts one of the above maps into stable JSON for the API.
func MarshalDiag(v map[string]any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
