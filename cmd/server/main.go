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
	"syscall"
	"time"

	"portguard/internal/api"
	"portguard/internal/conntrack"
	"portguard/internal/health"
	"portguard/internal/proxy"
	"portguard/internal/scanner"
	"portguard/internal/service"
	"portguard/internal/store"
	"portguard/internal/sysinfo"
)

var version = "2.0.0"

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
	broker.Publish("scan", map[string]int{"count": len(entries)})
}

func main() {
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
	_ = os.MkdirAll(paths.CertsDir, 0o750)
	_ = os.MkdirAll(paths.BackupsDir, 0o755)

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
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	interval := 30 * time.Second
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
			return 5 * time.Minute
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
	go sysinfo.RunSampler(ctx, 5*time.Second, func(s sysinfo.Snapshot) {
		broker.Publish("system", s)
	})

	// live connection log -> DB snapshot + SSE ping on change
	connSampler := conntrack.NewSampler(st, *port)
	connSampler.OnChange(func(count int) {
		broker.Publish("conns", map[string]int{"count": count})
	})
	go connSampler.Run(5*time.Second, ctx.Done())

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
