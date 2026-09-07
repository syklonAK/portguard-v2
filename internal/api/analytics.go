package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"portguard/internal/store"
	"portguard/internal/sysinfo"
)

// ---- services ----

func (a *App) handleListServices(w http.ResponseWriter, r *http.Request) {
	services, err := a.St.ListServices()
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	mappings, _ := a.St.ListMappings()
	healths, _ := a.St.ListHealth()
	type serviceView struct {
		store.Service
		MappingCount int `json:"mapping_count"`
		EnabledCount int `json:"enabled_count"`
		BackendsUp   int `json:"backends_up"`
		BackendsDown int `json:"backends_down"`
	}
	upSet := map[string]bool{}
	for _, h := range healths {
		if h.Status == "up" {
			upSet[strconv.FormatInt(h.MappingID, 10)+":"+strconv.Itoa(h.TargetIndex)] = true
		}
	}
	out := make([]serviceView, 0, len(services))
	for _, sv := range services {
		v := serviceView{Service: sv}
		for _, m := range mappings {
			if m.ServiceID == nil || *m.ServiceID != sv.ID {
				continue
			}
			v.MappingCount++
			if m.Enabled {
				v.EnabledCount++
				for i := range m.Targets {
					if upSet[strconv.FormatInt(m.ID, 10)+":"+strconv.Itoa(i)] {
						v.BackendsUp++
					} else {
						v.BackendsDown++
					}
				}
			}
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) handleCreateService(w http.ResponseWriter, r *http.Request) {
	var sv store.Service
	if !readJSON(w, r, &sv) {
		return
	}
	if strings.TrimSpace(sv.Name) == "" {
		errJSON(w, errString("name is required"), http.StatusUnprocessableEntity)
		return
	}
	id, err := a.St.CreateService(&sv)
	if err != nil {
		errJSON(w, errString("a service with this name already exists"), http.StatusConflict)
		return
	}
	sv.ID = id
	a.St.Audit(actorFrom(r.Context()), "service.create", sv.Name, "ok")
	writeJSON(w, http.StatusCreated, sv)
}

func (a *App) handleUpdateService(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var sv store.Service
	if !readJSON(w, r, &sv) {
		return
	}
	sv.ID = id
	if strings.TrimSpace(sv.Name) == "" {
		errJSON(w, errString("name is required"), http.StatusUnprocessableEntity)
		return
	}
	if err := a.St.UpdateService(&sv); err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "service.update", sv.Name, "ok")
	writeJSON(w, http.StatusOK, sv)
}

func (a *App) handleDeleteService(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err := a.St.DeleteService(id); err != nil {
		errJSON(w, err, http.StatusNotFound)
		return
	}
	a.St.Audit(actorFrom(r.Context()), "service.delete", "#"+chi.URLParam(r, "id"), "ok")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleServiceDetail returns one service with its member mappings.
func (a *App) handleServiceDetail(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	services, err := a.St.ListServices()
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	var sv *store.Service
	for i := range services {
		if services[i].ID == id {
			sv = &services[i]
			break
		}
	}
	if sv == nil {
		errJSON(w, errString("service not found"), http.StatusNotFound)
		return
	}
	mappings, _ := a.St.ListMappings()
	var members []store.Mapping
	for _, m := range mappings {
		if m.ServiceID != nil && *m.ServiceID == id {
			members = append(members, m)
		}
	}
	healths, _ := a.St.ListHealth()
	writeJSON(w, http.StatusOK, map[string]any{
		"service":  sv,
		"mappings": members,
		"health":   healths,
	})
}

// ---- metrics ----

// handleMetrics returns chartable samples for a node (0 = this panel) and
// a time range: ?range=5m|1h|24h|7d or explicit from/to unix seconds.
func (a *App) handleMetrics(w http.ResponseWriter, r *http.Request) {
	nodeID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if nodeID == 0 {
		nodeID, _ = strconv.ParseInt(r.URL.Query().Get("node_id"), 10, 64)
	}
	now := time.Now().Unix()
	var from int64
	switch r.URL.Query().Get("range") {
	case "5m":
		from = now - 300
	case "1h":
		from = now - 3600
	case "7d":
		from = now - 7*86400
	default: // 24h
		from = now - 86400
	}
	if v := r.URL.Query().Get("from"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			from = n
		}
	}
	pts, err := a.St.QueryMetrics(nodeID, from, now)
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	if pts == nil {
		pts = []store.MetricPoint{}
	}
	// totals over the window
	var rxTotal, txTotal uint64
	var rxBpsMax, txBpsMax int64
	for _, p := range pts {
		if p.RxBps > rxBpsMax {
			rxBpsMax = p.RxBps
		}
		if p.TxBps > txBpsMax {
			txBpsMax = p.TxBps
		}
		if p.RxBytes > rxTotal {
			rxTotal = p.RxBytes // cumulative counter: take the max
		}
		if p.TxBytes > txTotal {
			txTotal = p.TxBytes
		}
	}
	var lastConns int
	if len(pts) > 0 {
		lastConns = pts[len(pts)-1].Conns
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"node_id":      nodeID,
		"points":       pts,
		"rx_total":     rxTotal,
		"tx_total":     txTotal,
		"rx_peak":      rxBpsMax,
		"tx_peak":      txBpsMax,
		"active_conns": lastConns,
	})
}

// SampleNodeMetrics records one metric point for the local host, computing
// per-interval rates from the previous cumulative counters.
func (a *App) SampleLocalMetrics(prev *store.MetricPoint) store.MetricPoint {
	s := sysinfo.SnapshotNow()
	p := store.MetricPoint{
		NodeID:      0,
		CPUPercent:  s.CPUPercent,
		MemPercent:  s.MemPercent,
		DiskPercent: s.DiskPercent,
		RxBytes:     s.NetRxTotal,
		TxBytes:     s.NetTxTotal,
		TS:          time.Now().Unix(),
	}
	if prev != nil && p.TS > prev.TS {
		dt := p.TS - prev.TS
		if p.RxBytes >= prev.RxBytes {
			p.RxBps = int64((p.RxBytes - prev.RxBytes) / uint64(dt))
		}
		if p.TxBytes >= prev.TxBytes {
			p.TxBps = int64((p.TxBytes - prev.TxBytes) / uint64(dt))
		}
	}
	conns, _ := a.St.ListConnections()
	p.Conns = len(conns)
	_ = a.St.InsertMetric(p)
	return p
}

// SampleRemoteMetrics pulls a summary from a node agent and stores it as a
// metric point for that node (rates computed from the previous sample).
func (a *App) SampleRemoteMetrics(nodeID int64, prev *store.MetricPoint) {
	n, err := a.St.GetServerNode(nodeID)
	if err != nil || !n.Enabled {
		return
	}
	cli := a.nodeClientFor(n)
	sum, err := cli.GetSummary()
	if err != nil {
		return
	}
	sys := sum.System
	if sys == nil {
		return
	}
	p := store.MetricPoint{
		NodeID: nodeID,
		TS:     time.Now().Unix(),
	}
	getF := func(k string) float64 {
		if v, ok := sys[k].(float64); ok {
			return v
		}
		return 0
	}
	p.CPUPercent = getF("cpu_percent")
	p.MemPercent = getF("mem_percent")
	p.DiskPercent = getF("disk_percent")
	p.RxBytes = uint64(getF("net_rx_total"))
	p.TxBytes = uint64(getF("net_tx_total"))
	if prev != nil && p.TS > prev.TS {
		dt := p.TS - prev.TS
		if p.RxBytes >= prev.RxBytes {
			p.RxBps = int64((p.RxBytes - prev.RxBytes) / uint64(dt))
		}
		if p.TxBytes >= prev.TxBytes {
			p.TxBps = int64((p.TxBytes - prev.TxBytes) / uint64(dt))
		}
	}
	_ = a.St.InsertMetric(p)
}
