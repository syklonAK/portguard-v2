package api

import (
	"net/http"
	"strconv"

	"portguard/internal/conntrack"
)

// handleListConnections returns the live connection snapshot enriched with
// managed/unmanaged classification (always in sync with current mappings).
func (a *App) handleListConnections(w http.ResponseWriter, r *http.Request) {
	conns, err := a.St.ListConnections()
	if err != nil {
		errJSON(w, err, http.StatusInternalServerError)
		return
	}
	mappings, _ := a.St.ListMappings()
	conntrack.EnrichManaged(conns, mappings, a.PanelPort)

	// optional filters
	q := r.URL.Query()
	dst := q.Get("dst_port")
	managed := q.Get("managed")
	unmanaged := q.Get("unmanaged")
	if dst != "" {
		filtered := conns[:0]
		for _, c := range conns {
			if strconv.Itoa(c.DstPort) == dst {
				filtered = append(filtered, c)
			}
		}
		conns = filtered
	}
	if managed == "1" || unmanaged == "1" {
		want := managed == "1"
		filtered := conns[:0]
		for _, c := range conns {
			if c.Managed == want {
				filtered = append(filtered, c)
			}
		}
		conns = filtered
	}

	talkers, err := a.St.TopTalkers(10)
	if err != nil {
		talkers = nil
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"connections": conns,
		"top_talkers": talkers,
		"total":       len(conns),
	})
}
