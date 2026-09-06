package api

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"portguard/internal/nodeclient"
	"portguard/internal/ratelimit"
	"portguard/internal/store"
)

// ---- PasarGuard synchronization ----

// pasarguardClient talks to a PasarGuard panel API. It authenticates with a
// bearer token, can mint/refresh that token itself from username+password
// (admin tokens expire, so the manual-token-only flow kept breaking), and
// tolerates self-signed TLS panels (common on localhost installs) by falling
// back to skipping certificate verification after a verification failure.
type pasarguardClient struct {
	baseURL    string
	token      string
	username   string
	password   string
	skipTLS    bool
	onNewToken func(string)
	onSkipTLS  func()
}

func newPasarGuardClient(baseURL, token string) *pasarguardClient {
	return &pasarguardClient{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		token:   token,
	}
}

func (c *pasarguardClient) transport() *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: c.skipTLS},
		},
	}
}

// login exchanges username+password for a fresh admin access token.
func (c *pasarguardClient) login() error {
	if c.username == "" {
		return fmt.Errorf("no pasarguard token and no username/password configured (Settings → PasarGuard)")
	}
	// PasarGuard/Marzban implement the OAuth2 password flow: form-encoded
	// fields, not JSON
	form := url.Values{"username": {c.username}, "password": {c.password}}
	resp, err := c.transport().PostForm(c.baseURL+"/api/admin/token", form)
	if err != nil {
		if !c.skipTLS && isTLSVerifyError(err) {
			// self-signed panel: accept it and remember the decision
			c.skipTLS = true
			if c.onSkipTLS != nil {
				c.onSkipTLS()
			}
			resp, err = c.transport().PostForm(c.baseURL+"/api/admin/token", form)
		}
		if err != nil {
			return err
		}
	}
	defer resp.Body.Close()
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("pasarguard login failed with %d (check admin username/password)", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.AccessToken == "" {
		return fmt.Errorf("pasarguard login response unreadable")
	}
	c.token = out.AccessToken
	if c.onNewToken != nil {
		c.onNewToken(c.token)
	}
	return nil
}

func isTLSVerifyError(err error) bool {
	if err == nil {
		return false
	}
	var uv x509.UnknownAuthorityError
	var hn x509.HostnameError
	var ce *tls.CertificateVerificationError
	return errors.As(err, &uv) || errors.As(err, &hn) || errors.As(err, &ce)
}

func (c *pasarguardClient) get(path string, out any) error {
	status, err := c.attempt(path, out)
	if isTLSVerifyError(err) && !c.skipTLS {
		// self-signed panel: retry once without verification
		c.skipTLS = true
		if c.onSkipTLS != nil {
			c.onSkipTLS()
		}
		status, err = c.attempt(path, out)
	}
	if err != nil && (status == http.StatusUnauthorized || status == http.StatusForbidden) {
		// token missing/expired → login once and retry
		if lerr := c.login(); lerr != nil {
			return lerr
		}
		_, err = c.attempt(path, out)
	}
	return err
}

func (c *pasarguardClient) attempt(path string, out any) (int, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.transport().Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return resp.StatusCode, fmt.Errorf("pasarguard returned %d", resp.StatusCode)
	}
	return resp.StatusCode, json.NewDecoder(resp.Body).Decode(out)
}

// pgUser is the subset of the PasarGuard user payload we need.
type pgUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	UUID     string `json:"uuid"`
	Enabled  bool   `json:"enabled"`
	Expired  bool   `json:"expired"`
	Status   string `json:"status"`
	NodeIDs  []int64 `json:"node_ids"`
}

// handlePasarGuardSync pulls users from the configured PasarGuard panel and
// upserts them by UUID. Idempotent: no duplicates on repeated runs.
func (a *App) handlePasarGuardSync(w http.ResponseWriter, r *http.Request) {
	if err := a.SyncPasarGuardUsers(); err != nil {
		a.St.Audit(actorFrom(r.Context()), "ratelimit.sync", err.Error(), "error")
		errJSON(w, errString("pasarguard sync failed: "+err.Error()), http.StatusBadGateway)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "ratelimit.sync", "manual sync completed", "ok")
	users, _ := a.St.ListPasarguardUsers()
	writeJSON(w, http.StatusOK, map[string]any{"synced": len(users), "ok": true})
}

// ---- rate profiles ----

func (a *App) handleListRateProfiles(w http.ResponseWriter, r *http.Request) {
	ps, err := a.St.ListRateProfiles()
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, ps)
}

func (a *App) handleCreateRateProfile(w http.ResponseWriter, r *http.Request) {
	var p store.RateProfile
	if !readJSON(w, r, &p) {
		return
	}
	if strings.TrimSpace(p.Name) == "" {
		errJSON(w, errString("name is required"), http.StatusUnprocessableEntity)
		return
	}
	if err := ratelimit.ValidateLimits(ratelimit.Limits{DownloadBPS: p.DownloadBPS, UploadBPS: p.UploadBPS}); err != nil {
		errJSON(w, err, http.StatusUnprocessableEntity)
		return
	}
	id, err := a.St.CreateRateProfile(&p)
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	p.ID = id
	a.St.Audit(actorFrom(r.Context()), "ratelimit.profile.create", p.Name, "ok")
	writeJSON(w, http.StatusCreated, p)
}

func (a *App) handleUpdateRateProfile(w http.ResponseWriter, r *http.Request) {
	id, _ := parseInt64(chi.URLParam(r, "id"))
	var p store.RateProfile
	if !readJSON(w, r, &p) {
		return
	}
	p.ID = id
	if strings.TrimSpace(p.Name) == "" {
		errJSON(w, errString("name is required"), http.StatusUnprocessableEntity)
		return
	}
	if err := ratelimit.ValidateLimits(ratelimit.Limits{DownloadBPS: p.DownloadBPS, UploadBPS: p.UploadBPS}); err != nil {
		errJSON(w, err, http.StatusUnprocessableEntity)
		return
	}
	if err := a.St.UpdateRateProfile(&p); err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "ratelimit.profile.update", p.Name, "ok")
	writeJSON(w, http.StatusOK, p)
}

func (a *App) handleDeleteRateProfile(w http.ResponseWriter, r *http.Request) {
	id, _ := parseInt64(chi.URLParam(r, "id"))
	if err := a.St.DeleteRateProfile(id); err != nil {
		errJSON(w, err, http.StatusNotFound)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "ratelimit.profile.delete", "#"+chi.URLParam(r, "id"), "ok")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- policies (per UUID) ----

func (a *App) handleListRatePolicies(w http.ResponseWriter, r *http.Request) {
	policies, err := a.St.ListRatePolicies()
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	users, _ := a.St.ListPasarguardUsers()
	userByName := map[string]string{}
	for _, u := range users {
		userByName[u.UUID] = u.Username
	}
	profiles, _ := a.St.ListRateProfiles()
	profileName := map[int64]string{}
	for _, p := range profiles {
		profileName[p.ID] = p.Name
	}
	nodes, _ := a.St.ListServerNodesPublic()
	nodeName := map[int64]string{}
	for _, n := range nodes {
		nodeName[n.ID] = n.Name
	}
	type policyView struct {
		store.RateLimitPolicy
		Username    string `json:"username"`
		ProfileName string `json:"profile_name"`
		NodeName    string `json:"node_name"`
	}
	out := make([]policyView, 0, len(policies))
	for _, p := range policies {
		v := policyView{RateLimitPolicy: p}
		v.Username = userByName[p.UUID]
		if p.ProfileID != nil {
			v.ProfileName = profileName[*p.ProfileID]
		}
		v.NodeName = nodeName[p.NodeID]
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) validNode(id int64) (*store.ServerNode, error) {
	n, err := a.St.GetServerNode(id)
	if err != nil {
		return nil, errString("node not found")
	}
	if !n.Enabled {
		return nil, errString("node is disabled")
	}
	return &n, nil
}

func (a *App) handleUpsertRatePolicy(w http.ResponseWriter, r *http.Request) {
	var p store.RateLimitPolicy
	if !readJSON(w, r, &p) {
		return
	}
	p.UUID = ratelimit.NormalizeUUID(p.UUID)
	if !ratelimit.ValidUUID(p.UUID) {
		errJSON(w, errString("invalid xray uuid"), http.StatusUnprocessableEntity)
		return
	}
	if _, err := a.validNode(p.NodeID); err != nil {
		errJSON(w, err, http.StatusUnprocessableEntity)
		return
	}
	if err := ratelimit.ValidateLimits(ratelimit.Limits{DownloadBPS: p.DownloadBPS, UploadBPS: p.UploadBPS}); err != nil {
		errJSON(w, err, http.StatusUnprocessableEntity)
		return
	}
	if p.ProfileID != nil {
		if _, err := a.St.ListRateProfiles(); err != nil {
			errJSON(w, err, http.StatusInternalServerError)
			return
		}
	}
	if err := a.St.UpsertRatePolicy(&p); err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "ratelimit.policy.set",
		fmt.Sprintf("uuid=%s node=%d dl=%d ul=%d", p.UUID[:8], p.NodeID, p.DownloadBPS, p.UploadBPS), "ok")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "pending"})
}

func (a *App) handleDeleteRatePolicy(w http.ResponseWriter, r *http.Request) {
	uuid := ratelimit.NormalizeUUID(chi.URLParam(r, "uuid"))
	if !ratelimit.ValidUUID(uuid) {
		errJSON(w, errString("invalid xray uuid"), http.StatusUnprocessableEntity)
		return
	}
	nodeID, _ := parseInt64(r.URL.Query().Get("node_id"))
	if err := a.St.DeleteRatePolicy(uuid, nodeID); err != nil {
		errJSON(w, err, http.StatusNotFound)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "ratelimit.policy.delete", uuid[:8], "ok")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// SyncPasarGuardUsers pulls users from the configured PasarGuard endpoint
// and upserts them (used by both the manual endpoint and the background loop).
func (a *App) SyncPasarGuardUsers() error {
	base := a.St.GetSettingOr("pasarguard_url", "")
	if base == "" {
		return fmt.Errorf("pasarguard not configured")
	}
	cli := newPasarGuardClient(base, a.St.GetSettingOr("pasarguard_token", ""))
	cli.username = a.St.GetSettingOr("pasarguard_username", "")
	cli.password = a.St.GetSettingOr("pasarguard_password", "")
	cli.skipTLS = a.St.GetSettingOr("pasarguard_skip_tls", "") == "true"
	cli.onNewToken = func(tok string) { _ = a.St.SetSetting("pasarguard_token", tok) }
	cli.onSkipTLS = func() { _ = a.St.SetSetting("pasarguard_skip_tls", "true") }
	if cli.token == "" && cli.username != "" {
		if err := cli.login(); err != nil {
			return err
		}
	}
	var users []pgUser
	var wrapped struct {
		Users []pgUser `json:"users"`
	}
	if err := cli.get("/api/users", &wrapped); err != nil {
		// older panels return a bare array instead of {"users": [...]}
		var plain []pgUser
		if err := cli.get("/api/users", &plain); err != nil {
			return err
		}
		users = plain
	} else {
		users = wrapped.Users
	}
	synced := 0
	for _, u := range users {
		uuid := ratelimit.NormalizeUUID(u.UUID)
		if !ratelimit.ValidUUID(uuid) {
			continue
		}
		pu := store.PasarguardUser{
			UUID: uuid, Username: u.Username, Enabled: u.Enabled, Expired: u.Expired,
		}
		if err := a.St.UpsertPasarguardUser(&pu); err == nil {
			synced++
		}
	}
	_ = a.St.SetSetting("pasarguard_last_sync", time.Now().Format(time.RFC3339))
	return nil
}

// PushRateLimitPlans pushes the desired plan to every enabled node and
// records per-policy status (used by the background loop and the endpoint).
func (a *App) PushRateLimitPlans() {
	nodes, err := a.St.ListServerNodesPublic()
	if err != nil {
		return
	}
	policies, _ := a.St.ListRatePolicies()
	byNode := map[int64][]string{}
	for _, p := range policies {
		byNode[p.NodeID] = append(byNode[p.NodeID], p.UUID)
	}
	enabled := make([]store.ServerNode, 0, len(nodes))
	for _, n := range nodes {
		if n.Enabled {
			enabled = append(enabled, n)
		}
	}
	// push to all nodes concurrently: a slow node must not delay the rest
	var wg sync.WaitGroup
	for _, n := range enabled {
		wg.Add(1)
		go func(n store.ServerNode) {
			defer wg.Done()
			err := a.pushNodePlan(n.ID)
			status, errMsg := "synced", ""
			if err != nil {
				status, errMsg = "failed", err.Error()
			}
			for _, uuid := range byNode[n.ID] {
				_ = a.St.SetPolicyStatus(uuid, n.ID, status, errMsg, 0)
			}
			if err != nil {
				a.St.Audit("system", "ratelimit.push", n.Name+": "+err.Error(), "error")
			}
		}(n)
	}
	wg.Wait()
}

// handleListPasarguardUsers returns the synced user list joined with their
// policy/profile state for the Bandwidth page.
func (a *App) handleListPasarguardUsers(w http.ResponseWriter, r *http.Request) {
	users, err := a.St.ListPasarguardUsers()
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	policies, _ := a.St.ListRatePolicies()
	polByUUID := map[string]store.RateLimitPolicy{}
	for _, p := range policies {
		polByUUID[p.UUID] = p
	}
	profiles, _ := a.St.ListRateProfiles()
	profileName := map[int64]string{}
	for _, p := range profiles {
		profileName[p.ID] = p.Name
	}
	type userView struct {
		store.PasarguardUser
		HasPolicy   bool   `json:"has_policy"`
		Custom      bool   `json:"custom"`
		DownloadBPS int64  `json:"policy_download_bps"`
		UploadBPS   int64  `json:"policy_upload_bps"`
		ProfileName string `json:"profile_name"`
		PolicyState string `json:"policy_state"`
	}
	out := make([]userView, 0, len(users))
	for _, u := range users {
		v := userView{PasarguardUser: u}
		if p, ok := polByUUID[u.UUID]; ok {
			v.HasPolicy = true
			v.Custom = p.Custom
			v.DownloadBPS = p.DownloadBPS
			v.UploadBPS = p.UploadBPS
			if p.ProfileID != nil {
				v.ProfileName = profileName[*p.ProfileID]
			}
			v.PolicyState = p.Status
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

// buildNodePlan computes the desired tc rules for one node from its
// policies + the last synced user source IPs.
func (a *App) buildNodePlan(nodeID int64) ratelimit.Plan {
	plan := ratelimit.Plan{Version: a.St.PolicyPlanVersion()}
	policies, _ := a.St.ListRatePolicies()
	users, _ := a.St.ListPasarguardUsers()
	ipByUUID := map[string]store.PasarguardUser{}
	for _, u := range users {
		ipByUUID[u.UUID] = u
	}
	for _, p := range policies {
		if p.NodeID != nodeID || !p.Enabled {
			continue
		}
		if p.DownloadBPS <= 0 && p.UploadBPS <= 0 {
			continue // unlimited: no rule
		}
		u, ok := ipByUUID[p.UUID]
		if !ok || u.LastIP == "" {
			continue // no known source IP yet: cannot enforce per-IP
		}
		plan.Rules = append(plan.Rules, ratelimit.Rule{
			UUID: p.UUID, Username: u.Username, SourceIP: u.LastIP,
			DownloadBPS: p.DownloadBPS, UploadBPS: p.UploadBPS,
		})
	}
	return plan
}

// pushNodePlan sends the plan to the node's agent (token-authenticated).
func (a *App) pushNodePlan(nodeID int64) error {
	n, err := a.validNode(nodeID)
	if err != nil {
		return err
	}
	plan := a.buildNodePlan(nodeID)
	cli := nodeclient.New(n.Host, n.Port, n.APIToken)
	_, err = cli.Post("/ratelimit/apply", plan)
	return err
}

// handleRateLimitPush pushes the current plan of one node (or all nodes).
func (a *App) handleRateLimitPush(w http.ResponseWriter, r *http.Request) {
	nodes, err := a.St.ListServerNodesPublic()
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	type result struct {
		NodeID int64  `json:"node_id"`
		OK     bool   `json:"ok"`
		Error  string `json:"error,omitempty"`
	}
	policies, _ := a.St.ListRatePolicies()
	statusByNode := map[int64]map[string]store.RateLimitPolicy{}
	for _, p := range policies {
		if statusByNode[p.NodeID] == nil {
			statusByNode[p.NodeID] = map[string]store.RateLimitPolicy{}
		}
		statusByNode[p.NodeID][p.UUID] = p
	}
	enabled := make([]store.ServerNode, 0, len(nodes))
	for _, n := range nodes {
		if n.Enabled {
			enabled = append(enabled, n)
		}
	}
	results := make([]result, len(enabled))
	// push concurrently; results are written to per-index slots
	var wg sync.WaitGroup
	for i, n := range enabled {
		wg.Add(1)
		go func(i int, n store.ServerNode) {
			defer wg.Done()
			res := result{NodeID: n.ID, OK: true}
			if err := a.pushNodePlan(n.ID); err != nil {
				res.OK = false
				res.Error = err.Error()
			}
			results[i] = res
			// record per-policy push status
			if m, ok := statusByNode[n.ID]; ok {
				for uuid := range m {
					status, errMsg := "synced", ""
					if !res.OK {
						status, errMsg = "failed", res.Error
					}
					_ = a.St.SetPolicyStatus(uuid, n.ID, status, errMsg, 0)
				}
			}
		}(i, n)
	}
	wg.Wait()
	a.St.Audit(actorFrom(r.Context()), "ratelimit.push", fmt.Sprintf("%d nodes", len(results)), "ok")
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// handleRateLimitStatus summarizes the bandwidth subsystem.
func (a *App) handleRateLimitStatus(w http.ResponseWriter, r *http.Request) {
	users, _ := a.St.ListPasarguardUsers()
	policies, _ := a.St.ListRatePolicies()
	nodes, _ := a.St.ListServerNodesPublic()
	limited, failed, pending := 0, 0, 0
	for _, p := range policies {
		if !p.Enabled {
			continue
		}
		if p.DownloadBPS > 0 || p.UploadBPS > 0 {
			limited++
		}
		switch p.Status {
		case "failed":
			failed++
		case "pending":
			pending++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":       a.St.GetSettingOr("rate_limiting_enabled", "false") == "true",
		"total_users":   len(users),
		"limited_users": limited,
		"failed":        failed,
		"pending":       pending,
		"active_nodes":  len(nodes),
		"last_sync":     a.St.GetSettingOr("pasarguard_last_sync", ""),
	})
}
