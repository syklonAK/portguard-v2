package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// ---- alerts ----

func (a *App) handleListAlerts(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	alerts, err := a.St.ListAlerts(limit)
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"alerts":         alerts,
		"unacknowledged": a.St.UnackedAlertCount(),
		"enabled":        a.St.GetSettingOr("alerts_enabled", "true") == "true",
	})
}

func (a *App) handleAckAlert(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err := a.St.AckAlert(id); err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleTestAlert fires a synthetic alert through the whole pipeline
// (dedup/notify/SSE) so admins can verify their channel configuration.
func (a *App) handleTestAlert(w http.ResponseWriter, r *http.Request) {
	if a.Alerter == nil {
		errJSON(w, errString("alerter is not running on this process"), http.StatusServiceUnavailable)
		return
	}
	go a.Alerter.Fire("info", "test", "Test alert from PortGuard", "If you can read this, your alert channels are configured correctly.", "panel", "")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "note": "test alert dispatched"})
}

// handleAlertSettings is part of the settings map: alerts_enabled,
// alert_cooldown_min, alert_telegram_token/chat, alert_webhook_url,
// alert_cpu_min/ram_min/disk_min — all read/written through PUT /api/settings.
