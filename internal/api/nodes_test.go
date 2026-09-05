package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"portguard/internal/store"
)

// newTestApp builds a minimal App backed by a temp SQLite DB.
func newTestNodeApp(t *testing.T) *App {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.DB.Close() })
	return &App{St: st, PanelPort: 8080, Version: "test"}
}

func TestNodeAPIRequiresToken(t *testing.T) {
	app := newTestNodeApp(t)

	// no token configured -> always unauthorized
	req := httptest.NewRequest(http.MethodGet, "/api/node/ping", nil)
	rec := httptest.NewRecorder()
	app.handleNodePing(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without configured token, got %d", rec.Code)
	}

	// configure token
	_ = app.St.SetSetting("node_token", "sekrit")

	// wrong token
	req = httptest.NewRequest(http.MethodGet, "/api/node/ping", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec = httptest.NewRecorder()
	app.handleNodePing(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with wrong token, got %d", rec.Code)
	}

	// correct token
	req = httptest.NewRequest(http.MethodGet, "/api/node/ping", nil)
	req.Header.Set("Authorization", "Bearer sekrit")
	rec = httptest.NewRecorder()
	app.handleNodePing(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 with correct token, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		OK      bool   `json:"ok"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || !body.OK {
		t.Errorf("bad ping body: %v %s", err, rec.Body.String())
	}
}

func TestNodeSummaryPayload(t *testing.T) {
	app := newTestNodeApp(t)
	_ = app.St.SetSetting("node_token", "sekrit")
	_ = app.St.SetSetting("node_role", "iran")
	// one enabled mapping, one disabled
	_, _ = app.St.CreateMapping(&store.Mapping{Name: "a", Enabled: true, Engine: "nginx", Protocol: "http",
		ListenIP: "0.0.0.0", ListenPort: 8081, Targets: []store.Target{{Host: "127.0.0.1", Port: 90}}})

	req := httptest.NewRequest(http.MethodGet, "/api/node/summary", nil)
	req.Header.Set("Authorization", "Bearer sekrit")
	rec := httptest.NewRecorder()
	app.handleNodeSummary(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["role"] != "iran" {
		t.Errorf("role = %v, want iran", body["role"])
	}
	mp := body["mappings"].(map[string]any)
	if mp["total"] != float64(1) {
		t.Errorf("mappings.total = %v, want 1", mp["total"])
	}
	if _, ok := body["system"]; !ok {
		t.Error("system section missing")
	}
	if _, ok := body["tunnel"]; !ok {
		t.Error("tunnel section missing")
	}
}

func TestServerNodeCRUD(t *testing.T) {
	app := newTestNodeApp(t)
	n := store.ServerNode{Name: "ir-01", Host: "1.2.3.4", Port: 8080, APIToken: "tok", Role: "iran", Enabled: true}
	id, err := app.St.CreateServerNode(&n)
	if err != nil {
		t.Fatal(err)
	}
	got, err := app.St.GetServerNode(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "ir-01" || got.Role != "iran" || got.APIToken != "tok" {
		t.Errorf("mismatch: %+v", got)
	}
	// public list must not leak the token
	list, err := app.St.ListServerNodesPublic()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].APIToken != "" {
		t.Error("public list leaked api_token")
	}
	// touch status
	if err := app.St.TouchServerNode(id, "online"); err != nil {
		t.Fatal(err)
	}
	got, _ = app.St.GetServerNode(id)
	if got.Status != "online" || got.LastSeen == nil {
		t.Errorf("touch failed: %+v", got)
	}
	// token rotation
	if err := app.St.UpdateServerNodeToken(id, "tok2"); err != nil {
		t.Fatal(err)
	}
	got, _ = app.St.GetServerNode(id)
	if got.APIToken != "tok2" {
		t.Errorf("token rotation failed: %q", got.APIToken)
	}
	// delete
	if err := app.St.DeleteServerNode(id); err != nil {
		t.Fatal(err)
	}
	if _, err := app.St.GetServerNode(id); err != store.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

var _ = os.Getenv
