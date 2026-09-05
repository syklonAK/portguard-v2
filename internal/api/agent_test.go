package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"portguard/internal/store"
)

// Agent router must reject every request without a token, and accept the
// full surface with one. Uses the same App harness as nodes_test.go.
func TestAgentRouterAuth(t *testing.T) {
	app := newTestNodeApp(t)
	agent := &Agent{App: app}
	_ = app.St.SetSetting("node_token", "sekrit")

	// no token -> 401 everywhere
	for _, path := range []string{"/ping", "/summary", "/mappings", "/certs", "/tools", "/tunnel"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		agent.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without token: got %d, want 401", path, rec.Code)
		}
	}

	// wrong token -> 401
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec := httptest.NewRecorder()
	agent.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong token: got %d, want 401", rec.Code)
	}

	// correct token -> ping works and reports the version
	req = httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Authorization", "Bearer sekrit")
	rec = httptest.NewRecorder()
	agent.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ping: got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		OK      bool   `json:"ok"`
		Version string `json:"version"`
		Role    string `json:"role"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || !body.OK {
		t.Fatalf("bad ping body: %v %s", err, rec.Body.String())
	}
	if body.Version != "test" || body.Role != "generic" {
		t.Errorf("ping payload wrong: %+v", body)
	}
}

func TestAgentMappingCRUD(t *testing.T) {
	app := newTestNodeApp(t)
	agent := &Agent{App: app}
	_ = app.St.SetSetting("node_token", "sekrit")
	h := func(method, path, bodyJSON string) *httptest.ResponseRecorder {
		var req *http.Request
		if bodyJSON == "" {
			req = httptest.NewRequest(method, path, nil)
		} else {
			req = httptest.NewRequest(method, path, strings.NewReader(bodyJSON))
		}
		req.Header.Set("Authorization", "Bearer sekrit")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		agent.Router().ServeHTTP(rec, req)
		return rec
	}

	// create
	rec := h(http.MethodPost, "/mappings", `{"name":"web","enabled":true,"engine":"nginx","protocol":"http","listen_ip":"0.0.0.0","listen_port":8081,"server_names":["a.com"],"targets":[{"host":"127.0.0.1","port":3000}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.ID == 0 {
		t.Fatal("create did not return an id")
	}

	// conflict: same port must be rejected
	rec = h(http.MethodPost, "/mappings", `{"name":"web2","enabled":true,"engine":"nginx","protocol":"http","listen_ip":"0.0.0.0","listen_port":8081,"server_names":["b.com"],"targets":[{"host":"127.0.0.1","port":3000}]}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("conflict create: got %d, want 422", rec.Code)
	}

	// list
	rec = h(http.MethodGet, "/mappings", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}
	var list []store.Mapping
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list) != 1 {
		t.Fatalf("list: %v %s", err, rec.Body.String())
	}

	// delete
	rec = h(http.MethodDelete, "/mappings/"+jsonInt(created.ID), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d", rec.Code)
	}
	list, _ = app.St.ListMappings()
	if len(list) != 0 {
		t.Fatalf("delete did not remove the mapping: %d left", len(list))
	}
}

func TestAgentCertCreateAndListScrubbed(t *testing.T) {
	app := newTestNodeApp(t)
	agent := &Agent{App: app}
	_ = app.St.SetSetting("node_token", "sekrit")

	// create cert via the agent API
	body := `{"name":"node-cert","type":"manual","cert_pem":"-----BEGIN CERTIFICATE-----\nX\n-----END CERTIFICATE-----\n","key_pem":"-----BEGIN PRIVATE KEY-----\nY\n-----END PRIVATE KEY-----\n","domains":["x.com"]}`
	req := httptest.NewRequest(http.MethodPost, "/certs", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer sekrit")
	rec := httptest.NewRecorder()
	agent.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("cert create: %d %s", rec.Code, rec.Body.String())
	}

	// list must be metadata-only (no PEM leakage)
	req = httptest.NewRequest(http.MethodGet, "/certs", nil)
	req.Header.Set("Authorization", "Bearer sekrit")
	rec = httptest.NewRecorder()
	agent.Router().ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), "BEGIN CERTIFICATE") || strings.Contains(rec.Body.String(), "PRIVATE KEY") {
		t.Errorf("cert list leaked PEM material: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "node-cert") {
		t.Errorf("cert list missing name: %s", rec.Body.String())
	}
}

func jsonInt(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
