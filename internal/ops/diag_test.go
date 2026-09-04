package ops

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTCPCheck(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("cannot listen on loopback")
	}
	defer ln.Close()
	go func() {
		c, _ := ln.Accept()
		if c != nil {
			c.Close()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port

	res := TCPCheck("127.0.0.1", port, 0)
	if ok, _ := res["success"].(bool); !ok {
		t.Errorf("tcp check must succeed: %+v", res)
	}

	res = TCPCheck("127.0.0.1", 1, 0)
	if ok, _ := res["success"].(bool); ok {
		t.Errorf("tcp check to port 1 must fail: %+v", res)
	}
}

func TestHTTPCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test", "yes")
		w.WriteHeader(200)
	}))
	defer srv.Close()
	host, portS, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	var port int
	for _, c := range portS {
		port = port*10 + int(c-'0')
	}
	res := HTTPCheck(host, port, false, "/", "", 0)
	if ok, _ := res["success"].(bool); !ok {
		t.Fatalf("http check failed: %+v", res)
	}
	if status, _ := res["status"].(int); status != 200 {
		t.Errorf("expected 200, got %v", res["status"])
	}
}

func TestTemplatesValid(t *testing.T) {
	tpl := Templates()
	if len(tpl) < 5 {
		t.Fatalf("expected at least 5 templates, got %d", len(tpl))
	}
	seen := map[string]bool{}
	for _, tp := range tpl {
		if seen[tp.ID] {
			t.Errorf("duplicate template id %s", tp.ID)
		}
		seen[tp.ID] = true
	}
}
