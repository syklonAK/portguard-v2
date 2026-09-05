package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"portguard/internal/store"
)

// ---- config versions ----

func (a *App) handleListVersions(w http.ResponseWriter, r *http.Request) {
	versions, err := a.St.ListConfigVersions()
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, versions)
}

func (a *App) handleGetVersion(w http.ResponseWriter, r *http.Request) {
	ver, err := strconv.ParseInt(chi.URLParam(r, "v"), 10, 64)
	if err != nil {
		errJSON(w, errString("bad version"), http.StatusBadRequest)
		return
	}
	v, mappingsJSON, relaysJSON, err := a.St.GetConfigVersion(ver)
	if err != nil {
		errJSON(w, err, http.StatusNotFound)
		return
	}
	// project the snapshot for the viewer
	var mappings []store.Mapping
	_ = json.Unmarshal([]byte(mappingsJSON), &mappings)
	var relays []store.TunnelRelay
	_ = json.Unmarshal([]byte(relaysJSON), &relays)
	writeJSON(w, http.StatusOK, map[string]any{
		"version":   v,
		"mappings":  mappings,
		"relays":    relays,
	})
}

// handleDiffVersions compares two versions' mapping sets in JSON.
func (a *App) handleDiffVersions(w http.ResponseWriter, r *http.Request) {
	fromV, _ := strconv.ParseInt(chi.URLParam(r, "from"), 10, 64)
	toV, _ := strconv.ParseInt(chi.URLParam(r, "to"), 10, 64)
	_, fromM, _, err := a.St.GetConfigVersion(fromV)
	if err != nil {
		errJSON(w, errString("version "+strconv.FormatInt(fromV, 10)+" not found"), http.StatusNotFound)
		return
	}
	_, toM, _, err := a.St.GetConfigVersion(toV)
	if err != nil {
		errJSON(w, errString("version "+strconv.FormatInt(toV, 10)+" not found"), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"diff": diffMappingSets(fromM, toM)})
}

// diffMappingSets produces a readable added/removed/changed summary between
// two mapping snapshots (JSON-based, name keyed).
func diffMappingSets(fromJSON, toJSON string) []string {
	type light struct {
		ID      int64    `json:"id"`
		Name    string   `json:"name"`
		Enabled bool     `json:"enabled"`
		Engine  string   `json:"engine"`
		Listen  string   `json:"-"`
	}
	var fa, ta []map[string]any
	_ = json.Unmarshal([]byte(fromJSON), &fa)
	_ = json.Unmarshal([]byte(toJSON), &ta)
	idx := map[string]map[string]any{}
	for _, m := range fa {
		if n, ok := m["name"].(string); ok {
			idx[n] = m
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, m := range ta {
		name, _ := m["name"].(string)
		seen[name] = true
		prev, ok := idx[name]
		if !ok {
			out = append(out, "+ added: "+name)
			continue
		}
		pb, _ := json.Marshal(prev)
		cb, _ := json.Marshal(m)
		if string(pb) != string(cb) {
			// summarize the interesting fields
			details := []string{}
			for _, k := range []string{"enabled", "listen_port", "protocol", "engine", "path_prefix"} {
				pv, cv := prev[k], m[k]
				if pv != cv {
					details = append(details, k+": "+jsonStr(pv)+" -> "+jsonStr(cv))
				}
			}
			out = append(out, "~ changed: "+name+" ("+strings.Join(details, ", ")+")")
		}
	}
	for _, m := range fa {
		if n, _ := m["name"].(string); n != "" && !seen[n] {
			out = append(out, "- removed: "+n)
		}
	}
	return out
}

func jsonStr(v any) string {
	switch t := v.(type) {
	case nil:
		return "∅"
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

// handleRestoreVersion replaces the current mappings with a snapshot.
// Destructive: the current state is itself snapshotted first, so the restore
// is itself restorable.
func (a *App) handleRestoreVersion(w http.ResponseWriter, r *http.Request) {
	ver, err := strconv.ParseInt(chi.URLParam(r, "v"), 10, 64)
	if err != nil {
		errJSON(w, errString("bad version"), http.StatusBadRequest)
		return
	}
	v, mappingsJSON, relaysJSON, err := a.St.GetConfigVersion(ver)
	if err != nil {
		errJSON(w, err, http.StatusNotFound)
		return
	}
	actor := actorFrom(r.Context())

	// safety snapshot of the CURRENT state before overwriting
	_, _ = a.St.CreateConfigVersion(actor, "pre-restore of v"+strconv.FormatInt(ver, 10), "")

	var mappings []store.Mapping
	if err := json.Unmarshal([]byte(mappingsJSON), &mappings); err != nil {
		errJSON(w, errString("snapshot corrupt"), http.StatusInternalServerError)
		return
	}
	var relays []store.TunnelRelay
	_ = json.Unmarshal([]byte(relaysJSON), &relays)

	// replace everything in one go
	current, _ := a.St.ListMappings()
	for _, m := range current {
		_ = a.St.DeleteMapping(m.ID)
	}
	for i := range mappings {
		m := mappings[i]
		m.ID = 0
		if _, err := a.St.CreateMapping(&m); err != nil {
			a.St.Audit(actor, "version.restore", "failed at mapping "+m.Name+": "+err.Error(), "error")
			errJSON(w, errString("restore failed midway — current state may be partial; use Apply to reconcile"), http.StatusInternalServerError)
			return
		}
	}
	curRelays, _ := a.St.ListTunnelRelays()
	for _, rl := range curRelays {
		_ = a.St.DeleteTunnelRelay(rl.ID)
	}
	for i := range relays {
		rl := relays[i]
		rl.ID = 0
		_, _ = a.St.CreateTunnelRelay(&rl)
	}
	a.St.Audit(actor, "version.restore", "restored v"+strconv.FormatInt(v.Version, 10)+" ("+strconv.Itoa(len(mappings))+" mappings)", "ok")
	a.maybeAutoApply(actor)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "restored_mappings": len(mappings)})
}

// handleDownloadVersion returns the snapshot as a JSON export file.
func (a *App) handleDownloadVersion(w http.ResponseWriter, r *http.Request) {
	ver, err := strconv.ParseInt(chi.URLParam(r, "v"), 10, 64)
	if err != nil {
		errJSON(w, errString("bad version"), http.StatusBadRequest)
		return
	}
	_, mappingsJSON, relaysJSON, err := a.St.GetConfigVersion(ver)
	if err != nil {
		errJSON(w, err, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="portguard-v`+strconv.FormatInt(ver, 10)+`.json"`)
	_, _ = w.Write([]byte(`{"exported_version":` + strconv.FormatInt(ver, 10) + `,"mappings":` + mappingsJSON + `,"relays":` + relaysJSON + `}`))
}
