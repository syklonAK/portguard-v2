package proxy

import (
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func TestParseStatCSVAndSummary(t *testing.T) {
	csv := `# pxname,svname,qcur,qmax,scur,smax,slim,stot,bin,bout,status
fe_pg10,FRONTEND,,,5,20,2000,150,100000,900000,OPEN
be_pg10,BACKEND,0,0,3,10,200,140,90000,850000,UP
be_pg10,s0,0,0,2,5,200,80,50000,400000,UP
be_pg10,s1,0,0,1,5,200,60,40000,450000,DOWN
stats,STATS,0,0,0,0,0,0,0,0,UP
`
	rows := ParseStatCSV(csv)
	if len(rows) != 5 {
		t.Fatalf("expected 5 rows, got %d", len(rows))
	}
	if rows[0]["pxname"] != "fe_pg10" || rows[0]["scur"] != "5" {
		t.Fatalf("header parsing broken: %+v", rows[0])
	}
	s := SummaryStats(rows)
	if s["frontends"] != 1 || s["backends"] != 1 || s["servers"] != 2 {
		t.Errorf("unexpected counts: %+v", s)
	}
	if s["down"] != 1 || s["up"] != 1 {
		t.Errorf("unexpected up/down: %+v", s)
	}
	if s["sessions"] != 5 || s["bytes_in"] != 100000 || s["bytes_out"] != 900000 {
		t.Errorf("unexpected traffic: %+v", s)
	}
}

// fakeSocketServer spins a canned-response runtime socket for one command.
func fakeSocketServer(response string, captured *string) DialFunc {
	return func(path string, timeout time.Duration) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			buf := make([]byte, 1024)
			n, err := server.Read(buf)
			if err == nil && captured != nil {
				*captured = string(buf[:n])
			}
			_, _ = server.Write([]byte(response))
		}()
		return client, nil
	}
}

func TestRuntimeClientSendAndInfo(t *testing.T) {
	var got string
	client := &RuntimeClient{
		SocketPath: "/run/haproxy/admin.sock",
		Dial:       fakeSocketServer("Nbthread: 4\nUptime_sec: 123\n\n", &got),
	}
	info, err := client.ShowInfo()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "show info") {
		t.Errorf("command not sent: %q", got)
	}
	if info["Nbthread"] != "4" || info["Uptime_sec"] != "123" {
		t.Errorf("info parsing broken: %+v", info)
	}
}

func TestRuntimeClientSetServerStateValidation(t *testing.T) {
	var got string
	client := &RuntimeClient{
		SocketPath: "/run/haproxy/admin.sock",
		Dial:       fakeSocketServer("", &got),
	}
	if err := client.SetServerState("be_pg10", "s0", "bogus"); err == nil {
		t.Error("bogus state must be rejected")
	}
	if err := client.SetServerState("be;rm", "s0", "maint"); err == nil {
		t.Error("injection in backend name must be rejected")
	}
	if err := client.SetServerState("be_pg10", "s0", "maint"); err != nil {
		t.Errorf("valid command rejected: %v", err)
	}
	if !strings.Contains(got, "set server be_pg10/s0 state maint") {
		t.Errorf("wrong command sent: %q", got)
	}
}

func TestRuntimeClientDialError(t *testing.T) {
	client := &RuntimeClient{
		SocketPath: "/nonexistent.sock",
		Dial: func(string, time.Duration) (net.Conn, error) {
			return nil, errors.New("no such file")
		},
	}
	if _, err := client.ShowInfo(); err == nil {
		t.Error("expected dial error")
	}
}
