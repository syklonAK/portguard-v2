// ACME certificate issuance: the panel shells out to acme.sh (preferred, the
// widest DNS-01 provider support, works for wildcards) or certbot, then
// imports the issued PEM pair into the certificate store so HTTPS mappings
// can use it like any manually uploaded certificate.
//
// HTTP-01 uses standalone mode on port 80 (works when no engine owns :80).
// DNS-01 works everywhere and is the only way to issue wildcard SANs — the
// DNS provider API credentials are passed through the environment and never
// logged or persisted outside the (root-only) issue profile in settings.
package api

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"portguard/internal/store"
	"portguard/internal/tools"
)

// envField documents one DNS provider credential the UI should collect.
type envField struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Secret   bool   `json:"secret"`
	Required bool   `json:"required"`
}

type credLine struct{ Template, EnvKey string } // credentials-file line template

// acmeProvider is one DNS API provider supported for DNS-01 challenges.
type acmeProvider struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	AcmeShDNS    string     `json:"acmesh_dns,omitempty"`   // acme.sh --dns plugin (empty = acme.sh unsupported)
	Env          []envField `json:"env,omitempty"`          // credential fields
	CertbotPkg   string     `json:"certbot_pkg,omitempty"`  // apt package (empty = certbot unsupported)
	CertbotFlag  string     `json:"certbot_flag,omitempty"` // e.g. --dns-cloudflare
	CertbotCreds []credLine `json:"-"`                      // credentials file template
}

var acmeProviders = []acmeProvider{
	{
		ID: "cloudflare", Name: "Cloudflare",
		AcmeShDNS:  "dns_cf",
		CertbotPkg: "python3-certbot-dns-cloudflare", CertbotFlag: "--dns-cloudflare",
		Env: []envField{
			{Key: "CF_Token", Label: "API Token", Secret: true, Required: true},
			{Key: "CF_Account_ID", Label: "Account ID (optional)", Secret: false},
			{Key: "CF_Zone_ID", Label: "Zone ID (optional)", Secret: false},
		},
		CertbotCreds: []credLine{{Template: "dns_cloudflare_api_token = %s", EnvKey: "CF_Token"}},
	},
	{
		ID: "arvancloud", Name: "ArvanCloud",
		AcmeShDNS: "dns_arvan",
		Env:       []envField{{Key: "Arvan_Token", Label: "API Token", Secret: true, Required: true}},
	},
	{
		ID: "hetzner", Name: "Hetzner DNS",
		AcmeShDNS: "dns_hetzner",
		Env:       []envField{{Key: "HETZNER_Token", Label: "API Token", Secret: true, Required: true}},
	},
	{
		ID: "digitalocean", Name: "DigitalOcean",
		AcmeShDNS:  "dns_doapi",
		CertbotPkg: "python3-certbot-dns-digitalocean", CertbotFlag: "--dns-digitalocean",
		Env:          []envField{{Key: "DO_LETOKEN", Label: "API Token", Secret: true, Required: true}},
		CertbotCreds: []credLine{{Template: "dns_digitalocean_api_token = %s", EnvKey: "DO_LETOKEN"}},
	},
	{
		ID: "route53", Name: "AWS Route 53",
		AcmeShDNS:  "dns_aws",
		CertbotPkg: "python3-certbot-dns-route53", CertbotFlag: "--dns-route53",
		Env: []envField{
			{Key: "AWS_ACCESS_KEY_ID", Label: "Access Key ID", Secret: true, Required: true},
			{Key: "AWS_SECRET_ACCESS_KEY", Label: "Secret Access Key", Secret: true, Required: true},
		},
	},
	{
		ID: "ovh", Name: "OVH",
		AcmeShDNS:  "dns_ovh",
		CertbotPkg: "python3-certbot-dns-ovh", CertbotFlag: "--dns-ovh",
		Env: []envField{
			{Key: "OVH_END_POINT", Label: "Endpoint (ovh-eu, ovh-ca, ...)", Secret: false, Required: true},
			{Key: "OVH_APPLICATION_KEY", Label: "Application Key", Secret: true, Required: true},
			{Key: "OVH_APPLICATION_SECRET", Label: "Application Secret", Secret: true, Required: true},
			{Key: "OVH_CONSUMER_KEY", Label: "Consumer Key", Secret: true, Required: true},
		},
		CertbotCreds: []credLine{
			{Template: "dns_ovh_endpoint = %s", EnvKey: "OVH_END_POINT"},
			{Template: "dns_ovh_application_key = %s", EnvKey: "OVH_APPLICATION_KEY"},
			{Template: "dns_ovh_application_secret = %s", EnvKey: "OVH_APPLICATION_SECRET"},
			{Template: "dns_ovh_consumer_key = %s", EnvKey: "OVH_CONSUMER_KEY"},
		},
	},
	{
		ID: "godaddy", Name: "GoDaddy",
		AcmeShDNS: "dns_gd",
		Env: []envField{
			{Key: "GD_Key", Label: "API Key", Secret: true, Required: true},
			{Key: "GD_Secret", Label: "API Secret", Secret: true, Required: true},
		},
	},
	{
		ID: "vultr", Name: "Vultr",
		AcmeShDNS: "dns_vultr",
		Env:       []envField{{Key: "VULTR_API_KEY", Label: "API Key", Secret: true, Required: true}},
	},
	{
		ID: "aliyun", Name: "Aliyun (Alibaba Cloud)",
		AcmeShDNS: "dns_ali",
		Env: []envField{
			{Key: "Ali_Key", Label: "Access Key ID", Secret: true, Required: true},
			{Key: "Ali_Secret", Label: "Access Key Secret", Secret: true, Required: true},
		},
	},
	{
		ID: "dnspod", Name: "DNSPod (Tencent)",
		AcmeShDNS: "dns_dp",
		Env: []envField{
			{Key: "DP_Id", Label: "ID", Secret: false, Required: true},
			{Key: "DP_Key", Label: "Key", Secret: true, Required: true},
		},
	},
	{
		ID: "duckdns", Name: "DuckDNS",
		AcmeShDNS: "dns_duckdns",
		Env:       []envField{{Key: "DuckDNS_Token", Label: "Token", Secret: true, Required: true}},
	},
	{
		ID: "namesilo", Name: "NameSilo",
		AcmeShDNS: "dns_namesilo",
		Env:       []envField{{Key: "Namesilo_Key", Label: "API Key", Secret: true, Required: true}},
	},
	{
		ID: "gandi", Name: "Gandi LiveDNS",
		AcmeShDNS: "dns_gandi_livedns",
		Env:       []envField{{Key: "GANDI_LIVEDNS_KEY", Label: "API Key", Secret: true, Required: true}},
	},
	{
		ID: "desec", Name: "deSEC",
		AcmeShDNS: "dns_desec",
		Env:       []envField{{Key: "DESEC_TOKEN", Label: "Token", Secret: true, Required: true}},
	},
	{
		ID: "he", Name: "Hurricane Electric",
		AcmeShDNS: "dns_he",
		Env: []envField{
			{Key: "HE_Username", Label: "Username", Secret: false, Required: true},
			{Key: "HE_Password", Label: "Password", Secret: true, Required: true},
		},
	},
}

func acmeProviderByID(id string) *acmeProvider {
	for i := range acmeProviders {
		if acmeProviders[i].ID == id {
			return &acmeProviders[i]
		}
	}
	return nil
}

var acmeDomainRe = regexp.MustCompile(`(?i)^(\*\.)?[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

const (
	acmeShBin  = "/root/.acme.sh/acme.sh"
	acmeCABase = "https://acme-v02.api.letsencrypt.org/directory"
)

// issueRequest is the POST /api/certs/issue body.
type issueRequest struct {
	Tool    string            `json:"tool"`    // acmesh | certbot
	Method  string            `json:"method"`  // http01 | dns01
	Domains []string          `json:"domains"` // example.com, *.example.com
	Email   string            `json:"email"`
	DNS     string            `json:"dns_provider"`
	DNSEnv  map[string]string `json:"dns_env"`
	Name    string            `json:"name"`
}

// handleACMEProviders lists supported DNS providers so the UI can render
// dynamic credential fields.
func (a *App) handleACMEProviders(w http.ResponseWriter, r *http.Request) {
	out := make([]map[string]any, 0, len(acmeProviders))
	for _, p := range acmeProviders {
		out = append(out, map[string]any{
			"id": p.ID, "name": p.Name,
			"acmesh_dns":   p.AcmeShDNS,
			"certbot_pkg":  p.CertbotPkg,
			"certbot_flag": p.CertbotFlag,
			"env":          p.Env,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleIssueCert runs the ACME flow and imports the issued certificate.
func (a *App) handleIssueCert(w http.ResponseWriter, r *http.Request) {
	var req issueRequest
	if !readJSON(w, r, &req) {
		return
	}
	if req.Tool == "" {
		req.Tool = "acmesh"
	}
	if req.Tool != "acmesh" && req.Tool != "certbot" {
		errJSON(w, errors.New("tool must be acmesh or certbot"), http.StatusBadRequest)
		return
	}
	if req.Method != "http01" && req.Method != "dns01" {
		errJSON(w, errors.New("method must be http01 or dns01"), http.StatusBadRequest)
		return
	}
	if len(req.Domains) == 0 {
		errJSON(w, errors.New("at least one domain is required"), http.StatusBadRequest)
		return
	}
	for i, d := range req.Domains {
		d = strings.TrimSpace(strings.ToLower(d))
		if !acmeDomainRe.MatchString(d) {
			errJSON(w, fmt.Errorf("invalid domain %q", req.Domains[i]), http.StatusBadRequest)
			return
		}
		req.Domains[i] = d
	}
	wildcard := false
	for _, d := range req.Domains {
		if strings.HasPrefix(d, "*.") {
			wildcard = true
		}
	}
	if wildcard && req.Method != "dns01" {
		errJSON(w, errors.New("wildcard domains require DNS-01"), http.StatusBadRequest)
		return
	}
	if req.Method == "dns01" {
		p := acmeProviderByID(req.DNS)
		if p == nil {
			errJSON(w, errors.New("unknown dns_provider"), http.StatusBadRequest)
			return
		}
		if req.Tool == "acmesh" && p.AcmeShDNS == "" {
			errJSON(w, fmt.Errorf("provider %s is not supported by acme.sh", p.ID), http.StatusBadRequest)
			return
		}
		if req.Tool == "certbot" && p.CertbotPkg == "" {
			errJSON(w, fmt.Errorf("provider %s is not supported by certbot (use acme.sh)", p.ID), http.StatusBadRequest)
			return
		}
		if err := checkEnvFields(p, req.DNSEnv); err != nil {
			errJSON(w, err, http.StatusBadRequest)
			return
		}
	}
	if req.Email == "" && req.Tool == "certbot" {
		errJSON(w, errors.New("email is required for certbot registration"), http.StatusBadRequest)
		return
	}

	// stream mode: run in a background job so the UI can show a live
	// terminal of the ACME client output
	if r.URL.Query().Get("stream") == "1" && a.Jobs != nil {
		jobName := "ACME issue: " + strings.Join(req.Domains, ", ")
		j := a.Jobs.Start(jobName, func(job *Job) error {
			c, err := a.importIssued(&req, job.Write, actorFrom(r.Context()))
			if err != nil {
				return err
			}
			job.Write("[portguard] done — certificate #" + strconv.FormatInt(c.ID, 10) + " imported (" + c.Name + ")")
			return nil
		})
		writeJSON(w, http.StatusAccepted, map[string]any{"job_id": j.ID, "name": jobName})
		return
	}

	c, err := a.importIssued(&req, nil, actorFrom(r.Context()))
	if err != nil {
		errJSON(w, err, http.StatusUnprocessableEntity)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"cert": c, "note": "certificate imported; renew anytime via POST /api/certs/" + strconv.FormatInt(c.ID, 10) + "/renew",
	})
}

// importIssued runs the ACME client, then stores the issued pair as a new
// acme certificate together with its renewal profile.
func (a *App) importIssued(req *issueRequest, sink func(string), actor string) (store.Cert, error) {
	certPEM, keyPEM, err := a.runACMEIssue(req, sink)
	if err != nil {
		a.St.Audit(actor, "cert.issue", strings.Join(req.Domains, ","), "error")
		return store.Cert{}, err
	}

	// pick a primary domain (first non-wildcard SAN preferred for the name)
	primary := req.Domains[0]
	for _, d := range req.Domains {
		if !strings.HasPrefix(d, "*.") {
			primary = d
			break
		}
	}
	exp, err := parseCertExpiry(certPEM)
	if err != nil {
		return store.Cert{}, fmt.Errorf("issued certificate unreadable: %v", err)
	}
	name := req.Name
	if name == "" {
		name = primary
	}
	c := store.Cert{Name: name, Type: "acme", CertPEM: certPEM, KeyPEM: keyPEM, Domains: req.Domains, ExpiresAt: exp}
	id, err := a.St.CreateCert(&c)
	if err != nil {
		return store.Cert{}, err
	}
	c.ID = id
	// persist the issuance parameters so /renew can replay the same flow
	profile, _ := json.Marshal(map[string]any{
		"tool": req.Tool, "method": req.Method, "dns": req.DNS,
		"env": req.DNSEnv, "domains": req.Domains, "email": req.Email,
	})
	_ = a.St.SetSetting(fmt.Sprintf("acme_profile_%d", id), string(profile))
	a.St.Audit(actor, "cert.issue", name+" ("+req.Tool+", "+req.Method+")", "ok")
	if a.Broker != nil {
		a.Broker.Publish("cert", map[string]any{"action": "issued", "id": id, "name": name})
	}
	return c, nil
}

// handleRenewCert replays the stored issuance flow for an ACME certificate
// and swaps the stored PEM pair atomically.
func (a *App) handleRenewCert(w http.ResponseWriter, r *http.Request) {
	id := idParam(r)
	if id == 0 {
		errJSON(w, errors.New("bad id"), http.StatusBadRequest)
		return
	}
	c, err := a.St.GetCert(id)
	if err != nil {
		errJSON(w, errors.New("certificate not found"), http.StatusNotFound)
		return
	}
	raw, _ := a.St.GetSetting(fmt.Sprintf("acme_profile_%d", id))
	if raw == "" {
		errJSON(w, errors.New("no issuance profile stored for this certificate (manual/self-signed certificates cannot be renewed here)"), http.StatusUnprocessableEntity)
		return
	}
	var prof issueRequest
	if err := json.Unmarshal([]byte(raw), &prof); err != nil {
		errJSON(w, fmt.Errorf("corrupt issuance profile: %v", err), http.StatusInternalServerError)
		return
	}
	actor := actorFrom(r.Context())

	// stream mode: background job with live terminal output
	if r.URL.Query().Get("stream") == "1" && a.Jobs != nil {
		jobName := "ACME renew: " + c.Name
		j := a.Jobs.Start(jobName, func(job *Job) error {
			certPEM, keyPEM, err := a.runACMEIssue(&prof, job.Write)
			if err != nil {
				return err
			}
			exp, err := parseCertExpiry(certPEM)
			if err != nil {
				return fmt.Errorf("renewed certificate unreadable: %v", err)
			}
			if err := a.St.UpdateCertPEM(id, certPEM, keyPEM, exp); err != nil {
				return err
			}
			a.St.Audit(actor, "cert.renew", c.Name, "ok")
			if a.Broker != nil {
				a.Broker.Publish("cert", map[string]any{"action": "renewed", "id": id, "name": c.Name})
			}
			job.Write("[portguard] done — certificate renewed, valid until " + exp.Format("2006-01-02"))
			return nil
		})
		writeJSON(w, http.StatusAccepted, map[string]any{"job_id": j.ID, "name": jobName})
		return
	}

	certPEM, keyPEM, err := a.runACMEIssue(&prof, nil)
	if err != nil {
		a.St.Audit(actor, "cert.renew", c.Name, "error")
		errJSON(w, err, http.StatusUnprocessableEntity)
		return
	}
	exp, err := parseCertExpiry(certPEM)
	if err != nil {
		errJSON(w, fmt.Errorf("renewed certificate unreadable: %v", err), http.StatusInternalServerError)
		return
	}
	if err := a.St.UpdateCertPEM(id, certPEM, keyPEM, exp); err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "cert.renew", c.Name, "ok")
	if a.Broker != nil {
		a.Broker.Publish("cert", map[string]any{"action": "renewed", "id": id, "name": c.Name})
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "expires_at": exp})
}

// ---- ACME client runners ----

// runACMEIssue dispatches to the selected client and returns the fullchain
// PEM + private key PEM.
func (a *App) runACMEIssue(req *issueRequest, sink func(string)) (certPEM, keyPEM string, err error) {
	if req.Tool == "certbot" {
		return a.runCertbot(req, sink)
	}
	return a.runAcmeSh(req, sink)
}

// acmeEnv builds the process environment: parent env + validated provider
// credentials. Only keys declared in the provider registry are forwarded.
func acmeEnv(p *acmeProvider, provided map[string]string) []string {
	env := os.Environ()
	if p != nil {
		for _, f := range p.Env {
			if v, ok := provided[f.Key]; ok && v != "" {
				env = append(env, f.Key+"="+v)
			}
		}
	}
	return env
}

func checkEnvFields(p *acmeProvider, provided map[string]string) error {
	for _, f := range p.Env {
		if f.Required && strings.TrimSpace(provided[f.Key]) == "" {
			return fmt.Errorf("missing required credential %q for provider %s", f.Key, p.ID)
		}
	}
	return nil
}

// ensureAcmeSh installs acme.sh via the tools registry when missing so the
// issue button works even before visiting the Tools page.
func ensureAcmeSh(sink func(string)) error {
	if _, err := os.Stat(acmeShBin); err == nil {
		return nil
	}
	if sink != nil {
		sink("[portguard] acme.sh not found — installing it now (Tools page)")
	}
	res, err := tools.InstallStream("acmesh", sink)
	if err != nil {
		return fmt.Errorf("acme.sh is not installed and auto-install failed: %v", err)
	}
	if !res.OK {
		return fmt.Errorf("acme.sh auto-install failed: %s", res.Error)
	}
	if sink != nil {
		sink("[portguard] acme.sh installed successfully")
	}
	return nil
}

func (a *App) runAcmeSh(req *issueRequest, sink func(string)) (string, string, error) {
	if err := ensureAcmeSh(sink); err != nil {
		return "", "", err
	}
	var p *acmeProvider
	if req.Method == "dns01" {
		p = acmeProviderByID(req.DNS)
	}
	primary := req.Domains[0]
	for _, d := range req.Domains {
		if !strings.HasPrefix(d, "*.") {
			primary = d
			break
		}
	}
	// acme.sh registers the ACME account on first use and reads the contact
	// email from account.conf, ignoring --accountemail once ACCOUNT_EMAIL is
	// stored there — and the installer default (portguard@localhost) is
	// rejected by Let's Encrypt. Rewrite account.conf with a valid email
	// before every issue.
	email := req.Email
	if email == "" {
		email = "portguard@" + primary
	}
	conf := "/root/.acme.sh/account.conf"
	if raw, err := os.ReadFile(conf); err == nil {
		nl := "\n"
		lines := strings.Split(string(raw), nl)
		found := false
		for i, ln := range lines {
			if strings.HasPrefix(ln, "ACCOUNT_EMAIL=") {
				lines[i] = "ACCOUNT_EMAIL='" + email + "'"
				found = true
			}
		}
		if !found {
			lines = append(lines, "ACCOUNT_EMAIL='"+email+"'")
		}
		if err := os.WriteFile(conf, []byte(strings.Join(lines, nl)), 0o600); err != nil {
			return "", "", fmt.Errorf("cannot update acme.sh account email: %v", err)
		}
	}
	args := []string{"--issue", "--server", acmeCABase, "--accountemail", email}
	for _, d := range req.Domains {
		args = append(args, "-d", d)
	}
	if req.Method == "dns01" {
		args = append(args, "--dns", p.AcmeShDNS, "--dnssleep", "30")
	} else {
		// standalone: binds :80 itself
		args = append(args, "--standalone")
	}
	cmd := exec.Command(acmeShBin, args...)
	cmd.Env = acmeEnv(p, req.DNSEnv)
	if t, err := execStream(cmd, sink); err != nil {
		return "", "", fmt.Errorf("acme.sh failed: %v: %s", err, t)
	}

	// acme.sh writes to ~/.acme.sh/<primary>[_ecc]/
	candidates := []string{primary + "_ecc", primary}
	var certFile, keyFile string
	for _, c := range candidates {
		dir := "/root/.acme.sh/" + c
		cer, key := dir+"/fullchain.cer", dir+"/"+primary+".key"
		if fileReadable(cer) && fileReadable(key) {
			certFile, keyFile = cer, key
			break
		}
	}
	if certFile == "" {
		return "", "", fmt.Errorf("acme.sh reported success but the certificate files were not found under /root/.acme.sh")
	}
	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		return "", "", err
	}
	keyPEM, err := os.ReadFile(keyFile)
	if err != nil {
		return "", "", err
	}
	return string(certPEM), string(keyPEM), nil
}

func (a *App) runCertbot(req *issueRequest, sink func(string)) (string, string, error) {
	var p *acmeProvider
	if req.Method == "dns01" {
		p = acmeProviderByID(req.DNS)
		// plugin package must be present
		if _, err := os.Stat("/usr/lib/python3/dist-packages/certbot_" + strings.TrimPrefix(p.CertbotPkg, "python3-certbot-") + "/__init__.py"); err != nil {
			cmd := exec.Command("bash", "-c",
				"DEBIAN_FRONTEND=noninteractive apt-get update -y -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq "+p.CertbotPkg)
			if t, err := execStream(cmd, sink); err != nil {
				return "", "", fmt.Errorf("failed to install %s: %v: %s", p.CertbotPkg, err, tailLines(t, 10))
			}
		}
	}
	args := []string{"certonly", "--non-interactive", "--agree-tos", "-m", req.Email, "--server", acmeCABase}
	var env []string
	if req.Method == "dns01" {
		credsPath := ""
		if len(p.CertbotCreds) > 0 {
			var lines []string
			for _, cl := range p.CertbotCreds {
				v := req.DNSEnv[cl.EnvKey]
				if v == "" {
					continue
				}
				if strings.Contains(v, "\n") {
					return "", "", fmt.Errorf("credential %s must not contain newlines", cl.EnvKey)
				}
				lines = append(lines, fmt.Sprintf(cl.Template, v))
			}
			credsPath = fmt.Sprintf("/root/.secrets/portguard-%s.ini", p.ID)
			if err := os.MkdirAll("/root/.secrets", 0o700); err != nil {
				return "", "", err
			}
			if err := os.WriteFile(credsPath, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
				return "", "", err
			}
			args = append(args, p.CertbotFlag, "--"+strings.TrimPrefix(p.CertbotFlag, "--")+"-credentials", credsPath)
		}
		env = acmeEnv(p, req.DNSEnv) // route53-style providers read env directly
	} else {
		args = append(args, "--standalone")
	}
	for _, d := range req.Domains {
		args = append(args, "-d", d)
	}
	cmd := exec.Command("certbot", args...)
	cmd.Env = env
	if t, err := execStream(cmd, sink); err != nil {
		return "", "", fmt.Errorf("certbot failed: %v: %s", err, t)
	}
	primary := req.Domains[0]
	for _, d := range req.Domains {
		if !strings.HasPrefix(d, "*.") {
			primary = d
			break
		}
	}
	dir := "/etc/letsencrypt/live/" + primary
	certPEM, err := os.ReadFile(dir + "/fullchain.pem")
	if err != nil {
		return "", "", fmt.Errorf("certbot succeeded but %s/fullchain.pem is missing", dir)
	}
	keyPEM, err := os.ReadFile(dir + "/privkey.pem")
	if err != nil {
		return "", "", fmt.Errorf("certbot succeeded but %s/privkey.pem is missing", dir)
	}
	return string(certPEM), string(keyPEM), nil
}

// ---- small helpers ----

// execStream runs cmd, forwarding every stdout/stderr line to sink (may be
// nil, e.g. the live terminal view) and returning the last lines for error
// context when the command fails.
func execStream(cmd *exec.Cmd, sink func(string)) (string, error) {
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return "", err
	}
	var lines []string
	sc := bufio.NewScanner(pipe)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		l := sc.Text()
		lines = append(lines, l)
		if sink != nil {
			sink(l)
		}
	}
	waitErr := cmd.Wait()
	_ = pipe.Close()
	nl := "\n"
	return tailLines(strings.Join(lines, nl), 15), waitErr
}

func fileReadable(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	f.Close()
	return true
}
