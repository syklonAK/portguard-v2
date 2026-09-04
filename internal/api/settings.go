package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"strings"
	"time"

	"portguard/internal/proxy"
	"portguard/internal/store"
)

func hashPassword(pw string) (string, error) {
	b, err := bcryptHash(pw)
	return string(b), err
}

func checkPassword(hash, pw string) bool {
	return bcryptCompare(hash, pw)
}

// ---- settings ----

func (a *App) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	paths := a.Svc.Paths
	interval := "30"
	scanInterval := "300"
	autoApply := "false"
	if v, err := a.St.GetSetting("check_interval"); err == nil && v != "" {
		interval = v
	}
	if v, err := a.St.GetSetting("scan_interval"); err == nil && v != "" {
		scanInterval = v
	}
	if v, err := a.St.GetSetting("auto_apply"); err == nil && v != "" {
		autoApply = v
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"paths":          paths,
		"check_interval": interval,
		"scan_interval":  scanInterval,
		"auto_apply":     autoApply,
		"panel_port":     a.PanelPort,
	})
}

func (a *App) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Paths         *proxy.Paths `json:"paths"`
		CheckInterval string       `json:"check_interval"`
		ScanInterval  string       `json:"scan_interval"`
		AutoApply     *string      `json:"auto_apply"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if body.Paths != nil {
		p := *body.Paths
		if p.NginxConf != "" {
			a.Svc.Paths.NginxConf = p.NginxConf
			_ = a.St.SetSetting("nginx_conf", p.NginxConf)
		}
		if p.HAProxyConf != "" {
			a.Svc.Paths.HAProxyConf = p.HAProxyConf
			_ = a.St.SetSetting("haproxy_conf", p.HAProxyConf)
		}
		if p.CertsDir != "" {
			a.Svc.Paths.CertsDir = p.CertsDir
			_ = a.St.SetSetting("certs_dir", p.CertsDir)
		}
		if p.BackupsDir != "" {
			a.Svc.Paths.BackupsDir = p.BackupsDir
			_ = a.St.SetSetting("backups_dir", p.BackupsDir)
		}
		if p.NginxBin != "" {
			a.Svc.Paths.NginxBin = p.NginxBin
			_ = a.St.SetSetting("nginx_bin", p.NginxBin)
		}
		if p.HAProxyBin != "" {
			a.Svc.Paths.HAProxyBin = p.HAProxyBin
			_ = a.St.SetSetting("haproxy_bin", p.HAProxyBin)
		}
		if p.HAProxySocket != "" {
			a.Svc.Paths.HAProxySocket = p.HAProxySocket
			_ = a.St.SetSetting("haproxy_socket", p.HAProxySocket)
		}
	}
	if body.CheckInterval != "" {
		if n, err := fmtAtoi(body.CheckInterval); err == nil && n >= 5 {
			_ = a.St.SetSetting("check_interval", body.CheckInterval)
		} else {
			errJSON(w, errors.New("check_interval must be >= 5 seconds"), http.StatusUnprocessableEntity)
			return
		}
	}
	if body.ScanInterval != "" {
		if n, err := fmtAtoi(body.ScanInterval); err == nil && n >= 30 {
			_ = a.St.SetSetting("scan_interval", body.ScanInterval)
		} else {
			errJSON(w, errors.New("scan_interval must be >= 30 seconds"), http.StatusUnprocessableEntity)
			return
		}
	}
	if body.AutoApply != nil {
		v := "false"
		if *body.AutoApply == "true" || *body.AutoApply == "1" {
			v = "true"
		}
		_ = a.St.SetSetting("auto_apply", v)
	}
	a.St.Audit(actorFrom(r.Context()), "settings.update", "settings changed", "ok")
	a.handleGetSettings(w, r)
}

func fmtAtoi(s string) (int, error) {
	n := 0
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty")
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errors.New("not a number")
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

// ---- cert helpers ----

func parseCertExpiry(certPEM string) (*time.Time, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("invalid certificate PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("cannot parse certificate: %w", err)
	}
	return &cert.NotAfter, nil
}

func certDomains(certPEM string) []string {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return nil
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil
	}
	set := map[string]bool{}
	for _, d := range cert.DNSNames {
		set[d] = true
	}
	for _, ip := range cert.IPAddresses {
		set[ip.String()] = true
	}
	if cert.Subject.CommonName != "" {
		set[cert.Subject.CommonName] = true
	}
	out := make([]string, 0, len(set))
	for d := range set {
		out = append(out, d)
	}
	return out
}

func generateSelfSigned(domains []string, days int) (string, string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", err
	}
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: domains[0], Organization: []string{"PortGuard"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(0, 0, days),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	for _, d := range domains {
		if ip := net.ParseIP(d); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, d)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return "", "", err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", "", err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return string(certPEM), string(keyPEM), nil
}

var _ = store.ErrNotFound
