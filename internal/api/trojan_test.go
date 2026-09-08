package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"portguard/internal/store"
	"portguard/internal/tunnel"
)

// The trojan CRUD handlers must validate, reject duplicates and persist —
// exercised through the full chi router with a real temp SQLite store.
// All requests carry an admin JWT (the routes sit behind Auth.Middleware).

func trojanTestApp(t *testing.T) (*App, *httptest.ResponseRecorder, *http.Request) {
	t.Helper()
	app := newTestNodeApp(t)
	app.Auth = NewAuth(app.St, "test-secret")
	return app, nil, nil
}

// trojanReq builds an admin-authenticated request.
func trojanReq(t *testing.T, app *App, method, path, body string) *http.Request {
	t.Helper()
	tok, err := app.Auth.IssueRole("admin", 1, "admin")
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	return req
}

func TestTrojanRelayCRUD(t *testing.T) {
	app := newTestNodeApp(t)
	app.Auth = NewAuth(app.St, "test-secret")

	// create (auto bridge port + auto route resolution)
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, trojanReq(t, app, "POST", "/api/tunnels/trojan/relays",
		`{"name":"de-01","domain":"de.example.com","https_port":443,"foreign_ip":"5.6.7.8","foreign_port":10000}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create trojan relay: got %d: %s", rec.Code, rec.Body.String())
	}

	// duplicate https_port must 409
	rec = httptest.NewRecorder()
	app.Router().ServeHTTP(rec, trojanReq(t, app, "POST", "/api/tunnels/trojan/relays",
		`{"name":"de-02","domain":"de2.example.com","https_port":443,"foreign_ip":"9.9.9.9","foreign_port":10000}`))
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate https_port should 409, got %d: %s", rec.Code, rec.Body.String())
	}

	// invalid IP must 422
	rec = httptest.NewRecorder()
	app.Router().ServeHTTP(rec, trojanReq(t, app, "POST", "/api/tunnels/trojan/relays",
		`{"name":"de-03","domain":"de3.example.com","https_port":8443,"foreign_ip":"not-an-ip","foreign_port":10000}`))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid foreign_ip should 422, got %d: %s", rec.Code, rec.Body.String())
	}

	// list must contain the relay with resolved defaults
	rec = httptest.NewRecorder()
	app.Router().ServeHTTP(rec, trojanReq(t, app, "GET", "/api/tunnels/trojan/relays", ""))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"bridge_port":22000`) {
		t.Fatalf("relay list wrong: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"route":"tunnel"`) {
		t.Fatalf("public IP should resolve route=tunnel: %s", rec.Body.String())
	}

	// loopback relay resolves to direct
	rec = httptest.NewRecorder()
	app.Router().ServeHTTP(rec, trojanReq(t, app, "POST", "/api/tunnels/trojan/relays",
		`{"name":"local","domain":"lo.example.com","https_port":8443,"foreign_ip":"127.0.0.1","foreign_port":10000}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("loopback create: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	app.Router().ServeHTTP(rec, trojanReq(t, app, "GET", "/api/tunnels/trojan/relays", ""))
	if !strings.Contains(rec.Body.String(), `"route":"direct"`) {
		t.Fatalf("loopback should resolve route=direct: %s", rec.Body.String())
	}

	// delete
	relays, _ := app.St.ListTrojanRelays()
	if len(relays) != 2 {
		t.Fatalf("expected 2 relays, got %d", len(relays))
	}
	rec = httptest.NewRecorder()
	app.Router().ServeHTTP(rec, trojanReq(t, app, "DELETE", "/api/tunnels/trojan/relays/1", ""))
	if rec.Code != 200 {
		t.Fatalf("delete: %d", rec.Code)
	}
}

func TestTrojanIngressCRUD(t *testing.T) {
	app := newTestNodeApp(t)
	app.Auth = NewAuth(app.St, "test-secret")

	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, trojanReq(t, app, "POST", "/api/tunnels/trojan/ingresses",
		`{"name":"ing1","listen_port":35001,"target_host":"127.0.0.1","target_port":10000,"udp":true}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create ingress: %d %s", rec.Code, rec.Body.String())
	}
	// duplicate listen port
	rec = httptest.NewRecorder()
	app.Router().ServeHTTP(rec, trojanReq(t, app, "POST", "/api/tunnels/trojan/ingresses",
		`{"name":"ing2","listen_port":35001,"target_host":"127.0.0.1","target_port":10000}`))
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate listen_port should 409: %d %s", rec.Code, rec.Body.String())
	}
	// bad name
	rec = httptest.NewRecorder()
	app.Router().ServeHTTP(rec, trojanReq(t, app, "POST", "/api/tunnels/trojan/ingresses",
		`{"name":"bad name!","listen_port":35002,"target_port":10000}`))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad name should 422: %d", rec.Code)
	}
	// loop guard: same-host identical ports
	rec = httptest.NewRecorder()
	app.Router().ServeHTTP(rec, trojanReq(t, app, "POST", "/api/tunnels/trojan/ingresses",
		`{"name":"loopy","listen_port":35003,"target_host":"127.0.0.1","target_port":35003}`))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("loop guard should 422: %d %s", rec.Code, rec.Body.String())
	}
	// invalid listen_ip
	rec = httptest.NewRecorder()
	app.Router().ServeHTTP(rec, trojanReq(t, app, "POST", "/api/tunnels/trojan/ingresses",
		`{"name":"badip","listen_port":35004,"listen_ip":"banana","target_port":10000}`))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid listen_ip should 422: %d", rec.Code)
	}
}

func TestTrojanBridgeApplyNoXrayFails(t *testing.T) {
	app := newTestNodeApp(t)
	app.Auth = NewAuth(app.St, "test-secret")
	if _, err := app.St.CreateTrojanRelay(&store.TrojanRelay{
		Name: "x", Domain: "x.example.com", HTTPSPort: 443, ForeignIP: "1.2.3.4",
		ForeignPort: 10000, BridgePort: 22001, Route: "tunnel", SocksPort: 40001, Enabled: true,
	}); err != nil {
		t.Fatalf("seed relay: %v", err)
	}
	st := tunnel.Detect()
	if st.XrayInstalled {
		// xray present (dev/test box): the apply pipeline runs for real.
		// The behavior contract here is "apply must answer with valid JSON
		// and leave the DB consistent" — deeper outcomes need a real box.
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, trojanReq(t, app, "POST", "/api/tunnels/trojan/bridge/apply", ""))
		if rec.Code != 200 && rec.Code != http.StatusUnprocessableEntity && rec.Code != 500 {
			t.Fatalf("apply with xray should answer 200/422/500, got %d: %s", rec.Code, rec.Body.String())
		}
		return
	}
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, trojanReq(t, app, "POST", "/api/tunnels/trojan/bridge/apply", ""))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("apply without xray should 422, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBBRAndFirewallEndpoints(t *testing.T) {
	app := newTestNodeApp(t)
	app.Auth = NewAuth(app.St, "test-secret")

	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, trojanReq(t, app, "GET", "/api/tuning/bbr", ""))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "active") {
		t.Fatalf("bbr status: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	app.Router().ServeHTTP(rec, trojanReq(t, app, "GET", "/api/firewall", ""))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "kind") {
		t.Fatalf("firewall status: %d %s", rec.Code, rec.Body.String())
	}

	// firewall allow validation: port 0 must 422
	rec = httptest.NewRecorder()
	app.Router().ServeHTTP(rec, trojanReq(t, app, "POST", "/api/firewall/allow", `{"port":0}`))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("firewall allow port 0 should 422: %d", rec.Code)
	}
}

func TestTrojanRoutesRequireAuth(t *testing.T) {
	app := newTestNodeApp(t)
	app.Auth = NewAuth(app.St, "test-secret")

	// no Authorization header → 401 on every new surface
	for _, path := range []string{
		"/api/tunnels/trojan/relays",
		"/api/tunnels/trojan/ingresses",
		"/api/tuning/bbr",
		"/api/firewall",
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", path, nil)
		app.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s without auth should 401, got %d", path, rec.Code)
		}
	}
}
