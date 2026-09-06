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
	"strconv"
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
	socksHost := a.St.GetSettingOr("tunnel_socks_host", "127.0.0.1")
	socksPort := a.St.GetSettingOr("tunnel_socks_port", "40001")
	pgURL := a.St.GetSettingOr("pasarguard_url", "")
	pgToken := a.St.GetSettingOr("pasarguard_token", "")
	pgTokenSet := pgToken != ""
	pgUsername := a.St.GetSettingOr("pasarguard_username", "")
	pgPasswordSet := a.St.GetSettingOr("pasarguard_password", "") != ""
	rlEnabled := a.St.GetSettingOr("rate_limiting_enabled", "false")
	rlSync := a.St.GetSettingOr("rate_limiting_sync_interval", "60")
	// alerting
	alertsEnabled := a.St.GetSettingOr("alerts_enabled", "true")
	alertCooldown := a.St.GetSettingOr("alert_cooldown_min", "10")
	alertTGTokenSet := a.St.GetSettingOr("alert_telegram_token", "") != ""
	alertTGChat := a.St.GetSettingOr("alert_telegram_chat", "")
	alertHookSet := a.St.GetSettingOr("alert_webhook_url", "") != ""
	alertCPU := a.St.GetSettingOr("alert_cpu_min", "0")
	alertRAM := a.St.GetSettingOr("alert_ram_min", "0")
	alertDisk := a.St.GetSettingOr("alert_disk_min", "0")
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
		"paths":                     paths,
		"check_interval":            interval,
		"scan_interval":             scanInterval,
		"auto_apply":                autoApply,
		"panel_port":                a.PanelPort,
		"tunnel_socks_host":          socksHost,
		"tunnel_socks_port":          socksPort,
		"pasarguard_url":             pgURL,
		"pasarguard_username":         pgUsername,
		"pasarguard_password_set":     pgPasswordSet,
		"pasarguard_token_set":       pgTokenSet,
		"rate_limiting_enabled":      rlEnabled,
		"rate_limiting_sync_interval": rlSync,
		"alerts_enabled":              alertsEnabled,
		"alert_cooldown_min":          alertCooldown,
		"alert_telegram_token_set":    alertTGTokenSet,
		"alert_telegram_chat":          alertTGChat,
		"alert_webhook_set":           alertHookSet,
		"alert_cpu_min":               alertCPU,
		"alert_ram_min":               alertRAM,
		"alert_disk_min":              alertDisk,
	})
}

func (a *App) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Paths          *proxy.Paths `json:"paths"`
		CheckInterval  string       `json:"check_interval"`
		ScanInterval   string       `json:"scan_interval"`
		AutoApply      *string      `json:"auto_apply"`
		TunnelSocksHost *string     `json:"tunnel_socks_host"`
		TunnelSocksPort *string     `json:"tunnel_socks_port"`
		PasarGuardURL  *string      `json:"pasarguard_url"`
		PasarGuardToken *string     `json:"pasarguard_token"`
		PasarGuardUsername *string  `json:"pasarguard_username"`
		PasarGuardPassword *string  `json:"pasarguard_password"`
		RateLimitingEnabled *string `json:"rate_limiting_enabled"`
		RateLimitingSyncInterval *string `json:"rate_limiting_sync_interval"`
		AlertsEnabled  *string       `json:"alerts_enabled"`
		AlertCooldownMin *string     `json:"alert_cooldown_min"`
		AlertTelegramToken *string  `json:"alert_telegram_token"`
		AlertTelegramChat *string    `json:"alert_telegram_chat"`
		AlertWebhookURL *string      `json:"alert_webhook_url"`
		AlertCPUMin    *int           `json:"alert_cpu_min"`
		AlertRAMMin    *int           `json:"alert_ram_min"`
		AlertDiskMin   *int           `json:"alert_disk_min"`
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
	if body.TunnelSocksHost != nil && *body.TunnelSocksHost != "" {
		_ = a.St.SetSetting("tunnel_socks_host", *body.TunnelSocksHost)
	}
	if body.TunnelSocksPort != nil {
		if n, err := fmtAtoi(*body.TunnelSocksPort); err == nil && n >= 1 && n <= 65535 {
			_ = a.St.SetSetting("tunnel_socks_port", *body.TunnelSocksPort)
		} else {
			errJSON(w, errors.New("tunnel_socks_port must be 1-65535"), http.StatusUnprocessableEntity)
			return
		}
	}
	if body.PasarGuardURL != nil {
		_ = a.St.SetSetting("pasarguard_url", strings.TrimSpace(*body.PasarGuardURL))
	}
	if body.PasarGuardToken != nil && *body.PasarGuardToken != "" {
		// empty string keeps the existing token (never clears by accident)
		_ = a.St.SetSetting("pasarguard_token", strings.TrimSpace(*body.PasarGuardToken))
	}
	if body.PasarGuardUsername != nil {
		_ = a.St.SetSetting("pasarguard_username", strings.TrimSpace(*body.PasarGuardUsername))
	}
	if body.PasarGuardPassword != nil && *body.PasarGuardPassword != "" {
		// empty keeps the stored password; the sync client uses it to
		// mint/refresh admin tokens automatically when they expire
		_ = a.St.SetSetting("pasarguard_password", strings.TrimSpace(*body.PasarGuardPassword))
	}
	// rotating credentials invalidates the cached token; drop it so the
	// next sync logs in fresh
	if (body.PasarGuardUsername != nil || (body.PasarGuardPassword != nil && *body.PasarGuardPassword != "")) {
		_ = a.St.SetSetting("pasarguard_token", "")
	}
	if body.RateLimitingEnabled != nil {
		v := "false"
		if *body.RateLimitingEnabled == "true" || *body.RateLimitingEnabled == "1" {
			v = "true"
		}
		_ = a.St.SetSetting("rate_limiting_enabled", v)
	}
	if body.RateLimitingSyncInterval != nil {
		if n, err := fmtAtoi(*body.RateLimitingSyncInterval); err == nil && n >= 10 && n <= 3600 {
			_ = a.St.SetSetting("rate_limiting_sync_interval", *body.RateLimitingSyncInterval)
		} else {
			errJSON(w, errors.New("rate_limiting_sync_interval must be 10-3600 seconds"), http.StatusUnprocessableEntity)
			return
		}
	}
	// alerting
	if body.AlertsEnabled != nil {
		v := "false"
		if *body.AlertsEnabled == "true" || *body.AlertsEnabled == "1" {
			v = "true"
		}
		_ = a.St.SetSetting("alerts_enabled", v)
	}
	if body.AlertCooldownMin != nil {
		if n, err := fmtAtoi(*body.AlertCooldownMin); err == nil && n >= 1 && n <= 1440 {
			_ = a.St.SetSetting("alert_cooldown_min", *body.AlertCooldownMin)
		} else {
			errJSON(w, errors.New("alert_cooldown_min must be 1-1440 minutes"), http.StatusUnprocessableEntity)
			return
		}
	}
	if body.AlertTelegramToken != nil && *body.AlertTelegramToken != "" {
		_ = a.St.SetSetting("alert_telegram_token", strings.TrimSpace(*body.AlertTelegramToken))
	}
	if body.AlertTelegramChat != nil {
		_ = a.St.SetSetting("alert_telegram_chat", strings.TrimSpace(*body.AlertTelegramChat))
	}
	if body.AlertWebhookURL != nil && *body.AlertWebhookURL != "" {
		if !strings.HasPrefix(*body.AlertWebhookURL, "http://") && !strings.HasPrefix(*body.AlertWebhookURL, "https://") {
			errJSON(w, errors.New("alert_webhook_url must be an http(s) URL"), http.StatusUnprocessableEntity)
			return
		}
		_ = a.St.SetSetting("alert_webhook_url", strings.TrimSpace(*body.AlertWebhookURL))
	}
	if body.AlertCPUMin != nil {
		_ = a.St.SetSetting("alert_cpu_min", strconv.Itoa(clampPct(*body.AlertCPUMin)))
	}
	if body.AlertRAMMin != nil {
		_ = a.St.SetSetting("alert_ram_min", strconv.Itoa(clampPct(*body.AlertRAMMin)))
	}
	if body.AlertDiskMin != nil {
		_ = a.St.SetSetting("alert_disk_min", strconv.Itoa(clampPct(*body.AlertDiskMin)))
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

// clampPct keeps resource alert thresholds in 0..100 (0 = disabled).
func clampPct(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
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
