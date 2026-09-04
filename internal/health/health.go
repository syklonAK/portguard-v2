package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"portguard/internal/store"
)

// Checker runs periodic TCP/HTTP checks against every enabled mapping target.
type Checker struct {
	St       *store.Store
	Interval time.Duration
	Broker   EventSink
}

type EventSink interface {
	Publish(event string, payload any)
}

func New(st *store.Store, interval time.Duration, broker EventSink) *Checker {
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	return &Checker{St: st, Interval: interval, Broker: broker}
}

func (c *Checker) Run(ctx context.Context) {
	// first round promptly, then on the ticker
	c.Round()
	t := time.NewTicker(c.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.Round()
		}
	}
}

// Round checks every enabled mapping once.
func (c *Checker) Round() {
	mappings, err := c.St.ListMappings()
	if err != nil {
		return
	}
	existing, _ := c.St.ListHealth()
	prev := map[string]store.TargetHealth{}
	for _, h := range existing {
		prev[fmt.Sprintf("%d:%d", h.MappingID, h.TargetIndex)] = h
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	results := []store.TargetHealth{}

	for _, m := range mappings {
		if !m.Enabled {
			continue
		}
		for i, tgt := range m.Targets {
			key := fmt.Sprintf("%d:%d", m.ID, i)
			prevH, known := prev[key]
			wg.Add(1)
			go func(m store.Mapping, i int, tgt store.Target, prevH store.TargetHealth, known bool) {
				defer wg.Done()
				h := c.checkTarget(m, i, tgt, prevH, known)
				mu.Lock()
				results = append(results, h)
				mu.Unlock()
			}(m, i, tgt, prevH, known)
		}
	}
	wg.Wait()

	for _, h := range results {
		if err := c.St.UpsertHealth(h); err != nil {
			continue
		}
		key := fmt.Sprintf("%d:%d", h.MappingID, h.TargetIndex)
		if p, ok := prev[key]; ok && p.Status != h.Status {
			if c.Broker != nil {
				c.Broker.Publish("health", h)
			}
		} else if _, ok := prev[key]; !ok && c.Broker != nil {
			c.Broker.Publish("health", h)
		}
	}
}

func (c *Checker) checkTarget(m store.Mapping, i int, tgt store.Target, prev store.TargetHealth, known bool) store.TargetHealth {
	h := store.TargetHealth{MappingID: m.ID, TargetIndex: i, Host: tgt.Host, Port: tgt.Port, Status: "down"}

	start := time.Now()
	deadline := 3 * time.Second

	if m.Protocol == "http" || m.Protocol == "https" {
		scheme := "http"
		if m.Protocol == "https" {
			scheme = "https"
		}
		url := scheme + "://" + tgt.Host + ":" + strconv.Itoa(tgt.Port) + "/"
		client := &http.Client{Timeout: deadline}
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err == nil {
			req.Header.Set("User-Agent", "PortGuard-Health/1.0")
			req.Header.Set("Host", firstServerName(m))
			resp, err := client.Do(req)
			if err == nil {
				resp.Body.Close()
				h.LatencyMS = float64(time.Since(start).Microseconds()) / 1000.0
				if resp.StatusCode < 500 {
					h.Status = "up"
				}
			}
		}
	}

	// always also verify TCP reachability (http check alone can be blocked by Host routing)
	if h.Status != "up" {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(tgt.Host, strconv.Itoa(tgt.Port)), deadline)
		if err == nil {
			conn.Close()
			h.LatencyMS = float64(time.Since(start).Microseconds()) / 1000.0
			h.Status = "up"
		}
	}

	now := time.Now()
	h.LastCheckAt = &now

	// fail count bookkeeping from the previous round passed by the caller
	if known {
		if h.Status == "down" {
			h.FailCount = prev.FailCount + 1
		}
	}
	return h
}

func firstServerName(m store.Mapping) string {
	if len(m.ServerNames) > 0 {
		return m.ServerNames[0]
	}
	return ""
}

// Summary aggregates health for the dashboard.
func Summary(ms []store.Mapping, hs []store.TargetHealth) (map[string]any, error) {
	up, down, unknown := 0, 0, 0
	byMapping := map[string]any{}
	for _, m := range ms {
		if !m.Enabled {
			continue
		}
		total := len(m.Targets)
		mUp := 0
		for _, h := range hs {
			if h.MappingID == m.ID {
				switch h.Status {
				case "up":
					mUp++
					up++
				case "down":
					down++
				default:
					unknown++
				}
			}
		}
		if total > 0 {
			byMapping[strconv.FormatInt(m.ID, 10)] = map[string]any{"up": mUp, "total": total}
		}
	}
	payload, _ := json.Marshal(map[string]any{"up": up, "down": down, "unknown": unknown})
	var out map[string]any
	_ = json.Unmarshal(payload, &out)
	return out, nil
}
