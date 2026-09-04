package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"portguard/internal/proxy"
	"portguard/internal/store"
)

func testPaths(root string) proxy.Paths {
	return proxy.Paths{
		NginxConf: "/etc/nginx/nginx.conf", HAProxyConf: "/etc/haproxy/haproxy.cfg",
		CertsDir: "/var/lib/portguard/certs", BackupsDir: root,
		NginxBin: "/usr/sbin/nginx", HAProxyBin: "/usr/sbin/haproxy",
		HAProxySocket: "/run/haproxy/admin.sock",
	}
}

func TestBackupsListDiffDelete(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.DB.Close()
	root := t.TempDir()
	svc := New(st, testPaths(root), 8080)
	bk := &Backups{Svc: svc, St: st}

	ts := "20260102-030405"
	cfgDir := filepath.Join(root, ts, "etc", "haproxy")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "haproxy.cfg"), []byte("global\n    maxconn 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	list, err := bk.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Timestamp != ts {
		t.Fatalf("expected one backup %s, got %+v", ts, list)
	}
	if len(list[0].Files) != 1 || list[0].Files[0].Engine != "haproxy" ||
		list[0].Files[0].LivePath != "/etc/haproxy/haproxy.cfg" {
		t.Fatalf("backup file classification broken: %+v", list[0].Files)
	}

	diffs, err := bk.Diff(ts)
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 1 || diffs[0].Engine != "haproxy" {
		t.Fatalf("diff classification broken: %+v", diffs)
	}

	// deleting removes the folder and it disappears from the list
	if err := bk.Delete(ts); err != nil {
		t.Fatal(err)
	}
	list, _ = bk.List()
	if len(list) != 0 {
		t.Fatalf("backup not deleted: %+v", list)
	}
}

func TestBackupsRestoreRejectsInvalidConfig(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.DB.Close()
	root := t.TempDir()
	svc := New(st, testPaths(root), 8080)
	bk := &Backups{Svc: svc, St: st}

	ts := "20260102-030406"
	cfgDir := filepath.Join(root, ts, "etc", "haproxy")
	_ = os.MkdirAll(cfgDir, 0o755)
	_ = os.WriteFile(filepath.Join(cfgDir, "haproxy.cfg"), []byte("this is not a valid haproxy config <<<\n"), 0o644)

	// validation against the real binary must reject the restore (binary missing
	// on dev machines counts as failure too — the live system is never touched)
	err = bk.Restore("tester", ts)
	if err == nil {
		t.Fatal("restore of an invalid config must fail")
	}
	if !strings.Contains(err.Error(), "failed validation") {
		t.Fatalf("unexpected error: %v", err)
	}
	// an audit entry must exist
	logs, _ := st.ListAudit(10)
	if len(logs) == 0 || logs[0].Action != "backup.restore" {
		t.Fatalf("restore rejection not audited: %+v", logs)
	}
}

func TestBackupsRejectsBadTimestamp(t *testing.T) {
	st, _ := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	defer st.DB.Close()
	svc := New(st, testPaths(t.TempDir()), 8080)
	bk := &Backups{Svc: svc, St: st}
	if err := bk.Delete("../escape"); err == nil {
		t.Error("path traversal via timestamp must be rejected")
	}
	if err := bk.Restore("tester", "../../etc"); err == nil {
		t.Error("path traversal via timestamp must be rejected")
	}
}

func TestManagerFor(t *testing.T) {
	st, _ := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	defer st.DB.Close()
	svc := New(st, testPaths(t.TempDir()), 8080)
	for _, eng := range []string{"nginx", "haproxy"} {
		mgr, err := svc.ManagerFor(eng)
		if err != nil || mgr == nil || mgr.Service != eng {
			t.Errorf("ManagerFor(%s) = %v, %v", eng, mgr, err)
		}
	}
	if _, err := svc.ManagerFor("traefik"); err == nil {
		t.Error("unknown engine must be rejected")
	}
}

var _ = time.Now
