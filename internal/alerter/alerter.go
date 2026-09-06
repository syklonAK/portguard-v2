// Package alerter evaluates alert conditions on a schedule, deduplicates
// with cooldowns, records alerts in the store and notifies the configured
// channels (Telegram / generic webhook / Discord via webhook).
package alerter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"portguard/internal/nodeclient"
	"portguard/internal/store"
)

type Alerter struct {
	St  *store.Store
	Bus func(event string, payload any) // optional SSE broker publish

	httpClient *http.Client
}

func New(st *store.Store, bus func(string, any)) *Alerter {
	return &Alerter{St: st, Bus: bus, httpClient: &http.Client{Timeout: 10 * time.Second}}
}

func (a *Alerter) Run(stop <-chan struct{}) {
	// first pass after a short delay, then every 60s
	time.Sleep(10 * time.Second)
	a.Round()
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			a.Round()
		}
	}
}

// Round evaluates all alert sources: node reachability, backend health,
// certificate expiry and threshold settings.
func (a *Alerter) Round() {
	if a.St.GetSettingOr("alerts_enabled", "true") != "true" {
		return
	}
	a.checkNodes()
	a.checkBackends()
	a.checkCerts()
	a.checkResourceThresholds()
}

// Fire records, dedups (cooldown minutes), notifies channels and publishes
// SSE. Exported so the API can fire test/system alerts through the pipeline.
func (a *Alerter) Fire(sev, category, title, detail, target, dedupKey string) {
	a.fire(sev, category, title, detail, target, dedupKey)
}

// fire records, dedups (cooldown minutes), notifies channels and publishes SSE.
func (a *Alerter) fire(sev, category, title, detail, target, dedupKey string) {
	cooldown := 10 * time.Minute
	if v := a.St.GetSettingOr("alert_cooldown_min", "10"); v != "" {
		if n, err := time.ParseDuration(v + "m"); err == nil && n > 0 {
			cooldown = n
		}
	}
	if dedupKey != "" {
		if last, _ := a.St.LastAlertByDedup(dedupKey); last > 0 && time.Since(time.Unix(last, 0)) < cooldown {
			return // still cooling down
		}
	}
	al := store.Alert{
		Severity: sev, Category: category, Title: title, Detail: detail,
		Target: target, DedupKey: dedupKey,
	}
	_ = a.St.InsertAlert(al)
	a.notify(title, detail, sev)
	if a.Bus != nil {
		a.Bus("alert", map[string]any{"severity": sev, "title": title, "target": target})
	}
}

// notify pushes to every configured channel. Errors are swallowed per
// channel (one broken webhook must not break the others).
func (a *Alerter) notify(title, detail, sev string) {
	if tg := a.St.GetSettingOr("alert_telegram_token", ""); tg != "" {
		if chat := a.St.GetSettingOr("alert_telegram_chat", ""); chat != "" {
			a.sendTelegram(tg, chat, fmt.Sprintf("[%s] %s\n%s", strings.ToUpper(sev), title, detail))
		}
	}
	if hook := a.St.GetSettingOr("alert_webhook_url", ""); hook != "" {
		a.sendWebhook(hook, map[string]any{
			"severity": sev, "title": title, "detail": detail,
			"source": "PortGuard", "time": time.Now().UTC().Format(time.RFC3339),
		})
	}
}

func (a *Alerter) sendTelegram(token, chat, text string) {
	body, _ := json.Marshal(map[string]string{"chat_id": chat, "text": text})
	resp, err := a.httpClient.Post("https://api.telegram.org/bot"+token+"/sendMessage", "application/json", bytes.NewReader(body))
	if err == nil {
		resp.Body.Close()
	}
}

func (a *Alerter) sendWebhook(url string, payload map[string]any) {
	body, _ := json.Marshal(payload)
	resp, err := a.httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err == nil {
		resp.Body.Close()
	}
}

// --- sources ---

func (a *Alerter) checkNodes() {
	// ListServerNodes (not Public) so we have each node's API token —
	// pinging with an empty token always 401s and reports nodes offline.
	nodes, err := a.St.ListServerNodes()
	if err != nil {
		return
	}
	var wg sync.WaitGroup
	for _, n := range nodes {
		if !n.Enabled {
			continue
		}
		wg.Add(1)
		go func(n store.ServerNode) {
			defer wg.Done()
			cli := nodeclient.New(n.Host, n.Port, n.APIToken)
			online := cli.Ping() == nil
			prev := n.Status
			if online && (prev == "offline") {
				a.fire("info", "node", "Node back online: "+n.Name, "Node "+n.Name+" ("+n.Host+") is reachable again.", n.Name, "node-up:"+fmt.Sprint(n.ID))
				_ = a.St.TouchServerNode(n.ID, "online")
			} else if !online && prev != "offline" {
				a.fire("critical", "node", "Node offline: "+n.Name, "Node "+n.Name+" ("+n.Host+":"+fmt.Sprint(n.Port)+") is unreachable.", n.Name, "node-down:"+fmt.Sprint(n.ID))
				_ = a.St.TouchServerNode(n.ID, "offline")
			}
		}(n)
	}
	wg.Wait()
}

func (a *Alerter) checkBackends() {
	healths, err := a.St.ListHealth()
	if err != nil {
		return
	}
	mappings, _ := a.St.ListMappings()
	nameByID := map[int64]string{}
	for _, m := range mappings {
		nameByID[m.ID] = m.Name
	}
	// probe transitions: the checker records consecutive failures; alert
	// only when a target has been down for multiple rounds (fail_count >= 3)
	for _, h := range healths {
		if h.Status != "down" {
			continue
		}
		mname := nameByID[h.MappingID]
		if h.FailCount == 3 { // exactly at the threshold — one alert per outage
			a.fire("warning", "backend",
				fmt.Sprintf("Backend down: %s → %s:%d", mname, h.Host, h.Port),
				fmt.Sprintf("Target %s:%d of mapping %q has failed %d consecutive health checks.", h.Host, h.Port, mname, h.FailCount),
				fmt.Sprintf("%s|%s:%d", mname, h.Host, h.Port),
				fmt.Sprintf("backend-down:%d:%s:%d", h.MappingID, h.Host, h.Port))
		}
	}
}

func (a *Alerter) checkCerts() {
	certs, err := a.St.ListCerts()
	if err != nil {
		return
	}
	for _, c := range certs {
		if c.ExpiresAt == nil {
			continue
		}
		days := time.Until(*c.ExpiresAt).Hours() / 24
		if days <= 0 {
			a.fire("critical", "cert", "Certificate expired: "+c.Name, fmt.Sprintf("Certificate %q (domains: %s) has expired — HTTPS mappings using it will fail on renewal/reload.", c.Name, strings.Join(c.Domains, ", ")), c.Name, "cert-expired:"+fmt.Sprint(c.ID))
		} else if days <= 14 {
			a.fire("warning", "cert", fmt.Sprintf("Certificate expires in %dd: %s", int(days), c.Name),
				fmt.Sprintf("Certificate %q expires in %.0f days. Renew it before it breaks HTTPS mappings.", c.Name, days), c.Name, "cert-expiring:"+fmt.Sprint(c.ID))
		}
	}
}

func (a *Alerter) checkResourceThresholds() {
	// local system only (remote nodes report via their summaries; a full
	// fleet-threshold sweep happens when metrics land in v2.8)
	cpuMin := settingInt(a.St, "alert_cpu_min", 0) // 0 = disabled
	ramMin := settingInt(a.St, "alert_ram_min", 0)
	diskMin := settingInt(a.St, "alert_disk_min", 0)
	if cpuMin == 0 && ramMin == 0 && diskMin == 0 {
		return
	}
	resourceAlerts(a, cpuMin, ramMin, diskMin)
}

func settingInt(st *store.Store, key string, def int) int {
	if v := st.GetSettingOr(key, ""); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return def
}
