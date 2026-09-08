package store

import (
	"path/filepath"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "trojan.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.DB.Close() })
	return st
}

func TestTrojanRelayCRUD(t *testing.T) {
	st := openTestStore(t)

	r := &TrojanRelay{
		Name: "de-01", Domain: "de.example.com", HTTPSPort: 443,
		ForeignIP: "5.6.7.8", ForeignPort: 10000, BridgePort: 22001,
		Route: "tunnel", SocksPort: 40001, Enabled: true,
	}
	id, err := st.CreateTrojanRelay(r)
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.GetTrojanRelay(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "de-01" || got.HTTPSPort != 443 || got.BridgePort != 22001 || !got.Enabled {
		t.Errorf("roundtrip mismatch: %+v", got)
	}
	got.Notes = "updated"
	got.HTTPSPort = 8443
	if err := st.UpdateTrojanRelay(&got); err != nil {
		t.Fatal(err)
	}
	got2, _ := st.GetTrojanRelay(id)
	if got2.HTTPSPort != 8443 || got2.Notes != "updated" {
		t.Errorf("update not persisted: %+v", got2)
	}
	list, _ := st.ListTrojanRelays()
	if len(list) != 1 {
		t.Fatalf("list: %d", len(list))
	}
	if err := st.DeleteTrojanRelay(id); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetTrojanRelay(id); err != ErrNotFound {
		t.Errorf("deleted relay should be ErrNotFound, got %v", err)
	}
}

func TestTrojanIngressCRUD(t *testing.T) {
	st := openTestStore(t)

	ing := &TrojanIngress{Name: "ing1", ListenPort: 35001, NodePort: 10000, Enabled: true}
	id, err := st.CreateTrojanIngress(ing)
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.GetTrojanIngress(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.ListenPort != 35001 || got.NodePort != 10000 {
		t.Errorf("roundtrip mismatch: %+v", got)
	}
	got.Enabled = false
	if err := st.UpdateTrojanIngress(&got); err != nil {
		t.Fatal(err)
	}
	got2, _ := st.GetTrojanIngress(id)
	if got2.Enabled {
		t.Error("enabled flag not persisted")
	}
	if err := st.DeleteTrojanIngress(id); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetTrojanIngress(id); err != ErrNotFound {
		t.Errorf("deleted ingress should be ErrNotFound, got %v", err)
	}
}

func TestTrojanTablesSurviveReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trojan.db")
	st1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st1.CreateTrojanRelay(&TrojanRelay{
		Name: "keep", Domain: "k.example.com", HTTPSPort: 443,
		ForeignIP: "1.2.3.4", ForeignPort: 10000, BridgePort: 22000, Route: "tunnel", SocksPort: 40001, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st1.CreateTrojanIngress(&TrojanIngress{Name: "keep-ing", ListenPort: 35000, NodePort: 10000, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	st1.DB.Close()

	// reopen: the CREATE TABLE IF NOT EXISTS path must not clobber data
	st2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.DB.Close()
	relays, _ := st2.ListTrojanRelays()
	ings, _ := st2.ListTrojanIngresses()
	if len(relays) != 1 || len(ings) != 1 {
		t.Fatalf("data lost after reopen: %d relays, %d ingresses", len(relays), len(ings))
	}
}
