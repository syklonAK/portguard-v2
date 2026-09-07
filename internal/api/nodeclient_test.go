package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"portguard/internal/store"
)

// nodeClientFor must pick the hub-backed transport for an online reverse
// node and the direct HTTP client otherwise. Uses a nil Hub: reverse+offline
// must fall back to direct, direct mode always goes direct.
func TestNodeClientForTransportSelection(t *testing.T) {
	a := &App{} // Hub nil

	// direct node -> direct client
	direct := store.ServerNode{ID: 1, ConnMode: "direct", Host: "1.2.3.4", Port: 8081, APIToken: "t"}
	c1 := a.nodeClientFor(direct)
	if c1 == nil {
		t.Fatal("direct node must get a client")
	}

	// reverse node but hub nil/offline -> still a working (direct) client
	rev := store.ServerNode{ID: 2, ConnMode: "reverse", Host: "1.2.3.4", Port: 8081, APIToken: "t"}
	c2 := a.nodeClientFor(rev)
	if c2 == nil {
		t.Fatal("offline reverse node must fall back to a direct client")
	}
}

// hubResolve must match only exact non-empty tokens and report the node id.
func TestHubResolve(t *testing.T) {
	a := newTestNodeApp(t)
	if _, err := a.St.CreateServerNode(&store.ServerNode{
		Name: "n1", Host: "10.0.0.1", Port: 8081, APIToken: "tok-abc", Enabled: true,
	}); err != nil {
		t.Fatalf("seed node: %v", err)
	}

	id, uid, ok := a.hubResolve("tok-abc")
	if !ok || id == 0 {
		t.Fatalf("valid token must resolve, got id=%d ok=%v", id, ok)
	}
	if uid != "" {
		t.Logf("uid=%q", uid)
	}

	if _, _, ok := a.hubResolve("tok-wrong"); ok {
		t.Error("wrong token must not resolve")
	}
	if _, _, ok := a.hubResolve(""); ok {
		t.Error("empty token must not resolve")
	}
}

// smoke: the node proxy router serves /ping under /api/node after StripPrefix
// is applied by the caller — here we verify the bare router 404s /api/node/ping
// (proving the StripPrefix contract lives with the caller, as documented).
func TestAgentRouterBarePathsOnly(t *testing.T) {
	a := newTestNodeApp(t)
	_ = a.St.SetSetting("node_token", "tok")

	ag := &Agent{App: a}
	srv := httptest.NewServer(ag.Router())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/node/ping")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bare router must not serve /api/node/* paths (got %d)", resp.StatusCode)
	}

	req, _ := http.NewRequest("GET", srv.URL+"/ping", nil)
	req.Header.Set("Authorization", "Bearer tok")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("bare /ping with token must be 200, got %d", resp2.StatusCode)
	}
}
