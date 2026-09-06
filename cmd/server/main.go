package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"portguard/internal/alerter"
	"portguard/internal/config"
	"portguard/internal/api"
	"portguard/internal/conntrack"
	"portguard/internal/health"
	"portguard/internal/proxy"
	"portguard/internal/ratelimit"
	"portguard/internal/scanner"
	"portguard/internal/service"
	"portguard/internal/store"
	"portguard/internal/sysinfo"
)

var version = "2.10.0"

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	// agent subcommand: run as a headless managed node instead of a full
	// panel — new servers need only this binary + the master token.
	if len(os.Args) > 1 && os.Args[1] == "agent" {
		runAgent(os.Args[2:])
		return
	}

	port := flag.Int("port", envInt("PORTGUARD_PORT", 8080), "panel listen port")
	host := flag.String("host", envStr("PORTGUARD_HOST", "0.0.0.0"), "panel listen host")
	dbPath := flag.String("db", envStr("PORTGUARD_DB", "/var/lib/portguard/portguard.db"), "sqlite database path")
	flag.Parse()

	log.Printf("PortGuard v%s starting on %s:%d", version, *host, *port)

	if err := os.MkdirAll(filepath.Dir(*dbPath), 0o755); err != nil {
		log.Fatalf("cannot create data dir: %v", err)
	}

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer st.DB.Close()

	paths := proxy.DefaultPaths()
	for _, kv := range []struct {
		key string
		dst *string
	}{
		{"nginx_conf", &paths.NginxConf},
		{"haproxy_conf", &paths.HAProxyConf},
		{"certs_dir", &paths.CertsDir},
		{"backups_dir", &paths.BackupsDir},
		{"nginx_bin", &paths.NginxBin},
		{"haproxy_bin", &paths.HAProxyBin},
		{"haproxy_socket", &paths.HAProxySocket},
	} {
		if v, err := st.GetSetting(kv.key); err == nil && v != "" {
			*kv.dst = v
		}
	}
	if err := os.MkdirAll(paths.CertsDir, 0o750); err != nil {
		log.Printf("[startup] cannot create certs dir %s: %v (certificate deploy will fail)", paths.CertsDir, err)
	}
	if err := os.MkdirAll(paths.BackupsDir, 0o755); err != nil {
		log.Printf("[startup] cannot create backups dir %s: %v (backups will fail)", paths.BackupsDir, err)
	}

	secret, err := st.Secret("jwt_secret")
	if err != nil {
		log.Fatalf("jwt secret: %v", err)
	}

	broker := api.NewBroker()
	svc := service.New(st, paths, *port)
	auth := api.NewAuth(st, secret)
	scn := scanner.New()

	app := &api.App{
		St: st, Svc: svc, Auth: auth, Broker: broker,
		Scanner: scn, PanelPort: *port, Version: version,
		Jobs: api.NewJobManager(broker.Publish),
		RateApplierFactory: func() *ratelimit.Applier {
			return &ratelimit.Applier{StateDir: "/var/lib/portguard"}
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// alert engine: evaluates node/backend/cert/threshold conditions every
	// minute, dedups with cooldowns and notifies the configured channels
	alertEngine := alerter.New(st, broker.Publish)
	app.Alerter = alertEngine
	go alertEngine.Run(ctx.Done())

	interval := config.DefaultCheckInterval
	if v, err := st.GetSetting("check_interval"); err == nil && v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 5 {
			interval = time.Duration(n) * time.Second
		}
	}
	checker := health.New(st, interval, broker)
	go checker.Run(ctx)

	// periodic + initial port scan (interval re-read each round so Settings
	// changes apply without a restart)
	go func() {
		scanEvery := func() time.Duration {
			if v, err := st.GetSetting("scan_interval"); err == nil && v != "" {
				if n, err := strconv.Atoi(v); err == nil && n >= 30 {
					return time.Duration(n) * time.Second
				}
			}
			return config.DefaultScanInterval
		}
		time.Sleep(3 * time.Second)
		runScan(st, scn, *port, broker)
		for {
			t := time.NewTimer(scanEvery())
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case <-t.C:
			}
			runScan(st, scn, *port, broker)
		}
	}()

	// system stats -> SSE every 5s
	go sysinfo.RunSampler(ctx, config.SSESampleInterval, func(s sysinfo.Snapshot) {
		broker.Publish("system", s)
	})

	// live connection log -> DB snapshot + SSE ping on change
	connSampler := conntrack.NewSampler(st, *port)
	connSampler.OnChange(func(count int) {
		broker.Publish("conns", map[string]int{"count": count})
	})
	go connSampler.Run(config.ConntrackSampleInterval, ctx.Done())

	// v2.6: bandwidth sync loop — periodically pull PasarGuard users and
	// push plans to enabled nodes (both intervals re-read each round from
	// settings so they apply live)
	go func() {
		every := func(key string, def int) time.Duration {
			if v, err := st.GetSetting(key); err == nil && v != "" {
				if n, err := strconv.Atoi(v); err == nil && n >= 10 {
					return time.Duration(n) * time.Second
				}
			}
			return time.Duration(def) * time.Second
		}
		enabled := func() bool {
			return st.GetSettingOr("rate_limiting_enabled", "false") == "true"
		}
		// wait until the HTTP API is serving before the first push
		time.Sleep(5 * time.Second)
		lastPlan := int64(-1)
		for {
			if enabled() {
				// sync users when the pasarguard endpoint is configured
				if st.GetSettingOr("pasarguard_url", "") != "" {
					app.SyncPasarGuardUsers()
				}
				plan := st.PolicyPlanVersion()
				if plan != lastPlan {
					app.PushRateLimitPlans()
					lastPlan = plan
				}
			}
			t := time.NewTimer(every("rate_limiting_sync_interval", 60))
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case <-t.C:
			}
		}
	}()

	// v2.8: metrics sampler — records one point per node (0 = this panel)
	// every 30s; rates derive from the previous cumulative counters. Old
	// samples are pruned to a 7-day retention.
	go func() {
		prevLocal := &store.MetricPoint{}
		type prevEntry struct {
			point store.MetricPoint
			known bool
		}
		prevByNode := map[int64]prevEntry{}
		time.Sleep(8 * time.Second)
		for {
			np := app.SampleLocalMetrics(prevLocal)
			*prevLocal = np
			nodes, _ := st.ListServerNodesPublic()
			// sample all enabled nodes concurrently: one slow/unreachable
			// node (15s timeout) must not stretch the whole round
			var wg sync.WaitGroup
			for _, n := range nodes {
				if !n.Enabled {
					continue
				}
				wg.Add(1)
				go func(id int64) {
					defer wg.Done()
					if pe, ok := prevByNode[id]; ok {
						p := pe.point
						app.SampleRemoteMetrics(id, &p)
					} else {
						app.SampleRemoteMetrics(id, nil)
					}
				}(n.ID)
			}
			wg.Wait()
			for _, n := range nodes {
				if !n.Enabled {
					continue
				}
				if pts, err := st.QueryMetrics(n.ID, time.Now().Unix()-120, time.Now().Unix()); err == nil && len(pts) > 0 {
					prevByNode[n.ID] = prevEntry{point: pts[len(pts)-1], known: true}
				}
			}
			st.PruneMetrics(config.MetricsRetention)
			t := time.NewTimer(config.MetricsSampleInterval)
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case <-t.C:
			}
		}
	}()

	srv := &http.Server{
		Addr:              net.JoinHostPort(*host, strconv.Itoa(*port)),
		Handler:           app.Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()
	log.Printf("PortGuard ready: http://%s:%d", displayHost(*host), *port)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Println("shutting down...")
	shCtx, shCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shCancel()
	_ = srv.Shutdown(shCtx)
	cancel()
}

func displayHost(h string) string {
	if h == "0.0.0.0" || h == "::" {
		return "127.0.0.1"
	}
	return h
}

// markManaged sets the Managed flag using enabled mappings; proxy engines
// themselves (nginx/haproxy processes) are considered managed by the tool too.
func markManaged(st *store.Store, entries []store.PortEntry) {
	mappings, err := st.ListMappings()
	if err != nil {
		return
	}
	managedPorts := map[int]bool{}
	for _, m := range mappings {
		if m.Enabled {
			managedPorts[m.ListenPort] = true
		}
	}
	for i := range entries {
		e := &entries[i]
		e.Managed = managedPorts[e.Port] ||
			e.Classification == "web-server" || e.Classification == "load-balancer"
	}
}

func runScan(st *store.Store, scn *scanner.Scanner, port int, broker *api.Broker) {
	entries, err := scn.Scan(port)
	if err != nil {
		log.Printf("scan error: %v", err)
		return
	}
	markManaged(st, entries)
	if err := st.ReplacePorts(entries); err != nil {
		log.Printf("scan store error: %v", err)
		return
	}
	_ = st.SetSetting("last_scan_at", time.Now().Format(time.RFC3339))
	if broker != nil {
		broker.Publish("scan", map[string]int{"count": len(entries)})
	}
}

// runAgent is the headless node mode: the same single binary runs without
// the web UI, admin accounts or the master features — it only exposes the
// token-authenticated /api/node/* surface for its master, plus periodic
// port scans and connection sampling so summaries stay live.
func runAgent(args []string) {
	fs := flag.NewFlagSet("agent", flag.ExitOnError)
	port := fs.Int("port", envInt("PORTGUARD_PORT", 8081), "agent listen port")
	host := fs.String("host", envStr("PORTGUARD_HOST", "0.0.0.0"), "agent listen host")
	dbPath := fs.String("db", envStr("PORTGUARD_DB", "/var/lib/portguard/node.db"), "sqlite database path")
	token := fs.String("token", envStr("PORTGUARD_NODE_TOKEN", ""), "master token (also settable via the DB)")
	role := fs.String("role", envStr("PORTGUARD_NODE_ROLE", "generic"), "node role: generic|iran|foreign")
	_ = fs.Parse(args)

	log.Printf("PortGuard agent v%s starting on %s:%d", version, *host, *port)

	if err := os.MkdirAll(filepath.Dir(*dbPath), 0o755); err != nil {
		log.Fatalf("cannot create data dir: %v", err)
	}
	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer st.DB.Close()

	if *token != "" {
		_ = st.SetSetting("node_token", *token)
	}
	if *role != "" {
		_ = st.SetSetting("node_role", *role)
	}
	if st.GetSettingOr("node_token", "") == "" {
		log.Fatal("no node token: pass -token or PORTGUARD_NODE_TOKEN (the master generates one when adding the server)")
	}

	paths := proxy.DefaultPaths()
	if err := os.MkdirAll(paths.CertsDir, 0o750); err != nil {
		log.Printf("[startup] cannot create certs dir %s: %v (certificate deploy will fail)", paths.CertsDir, err)
	}
	if err := os.MkdirAll(paths.BackupsDir, 0o755); err != nil {
		log.Printf("[startup] cannot create backups dir %s: %v (backups will fail)", paths.BackupsDir, err)
	}

	broker := api.NewBroker()
	svc := service.New(st, paths, *port)
	scn := scanner.New()

	app := &api.App{
		St: st, Svc: svc, Broker: broker,
		Scanner: scn, PanelPort: *port, Version: version,
		Jobs: api.NewJobManager(broker.Publish),
		RateApplierFactory: func() *ratelimit.Applier {
			return &ratelimit.Applier{StateDir: "/var/lib/portguard"}
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// periodic port scan keeps Ports/summary data fresh
	go func() {
		time.Sleep(2 * time.Second)
		runScan(st, scn, *port, broker)
		for {
			t := time.NewTimer(5 * time.Minute)
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case <-t.C:
			}
			runScan(st, scn, *port, broker)
		}
	}()

	// health checks feed the master's overview
	checker := health.New(st, 30*time.Second, broker)
	go checker.Run(ctx)

	// live connection sampling
	connSampler := conntrack.NewSampler(st, *port)
	go connSampler.Run(config.ConntrackSampleInterval, ctx.Done())

	// bandwidth policy recovery: on boot the agent asks the master for its
	// current plan (the master pushes on connect anyway, but the pull makes
	// recovery independent of push timing)
	go func() {
		time.Sleep(6 * time.Second)
		ap := app.RateApplier()
		st := ap.LoadState()
		if st.Iface != "" && len(st.Rules) > 0 {
			log.Printf("ratelimit: recovered %d bandwidth rules on %s from the applied state", len(st.Rules), st.Iface)
		}
	}()

	agent := &api.Agent{App: app}
	srv := &http.Server{
		Addr:              net.JoinHostPort(*host, strconv.Itoa(*port)),
		Handler:           agent.Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("agent listen: %v", err)
		}
	}()
	log.Printf("PortGuard agent ready on %s:%d (role=%s)", displayHost(*host), *port, st.GetSettingOr("node_role", "generic"))

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Println("agent shutting down...")
	shCtx, shCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shCancel()
	_ = srv.Shutdown(shCtx)
	cancel()
}
