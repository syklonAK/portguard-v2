package store

import (
	"path/filepath"
	"testing"
)

func TestRateLimitingCRUD(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.DB.Close()

	// profiles
	p := RateProfile{Name: "Premium", DownloadBPS: 30_000_000, UploadBPS: 10_000_000, Enabled: true}
	pid, err := st.CreateRateProfile(&p)
	if err != nil {
		t.Fatal(err)
	}
	p.ID = pid
	p.DownloadBPS = 100_000_000
	if err := st.UpdateRateProfile(&p); err != nil {
		t.Fatal(err)
	}
	got, _ := st.ListRateProfiles()
	if len(got) != 1 || got[0].DownloadBPS != 100_000_000 {
		t.Fatalf("profile update failed: %+v", got)
	}

	// pasarguard users: upsert twice by uuid must not duplicate
	u := PasarguardUser{UUID: "550e8400-e29b-41d4-a716-446655440000", Username: "ali", Enabled: true}
	if err := st.UpsertPasarguardUser(&u); err != nil {
		t.Fatal(err)
	}
	u.Username = "ali2"
	if err := st.UpsertPasarguardUser(&u); err != nil {
		t.Fatal(err)
	}
	users, _ := st.ListPasarguardUsers()
	if len(users) != 1 || users[0].Username != "ali2" {
		t.Fatalf("user upsert not idempotent: %+v", users)
	}

	// policies: unique per (uuid, node)
	pol := RateLimitPolicy{UUID: u.UUID, NodeID: 7, ProfileID: &pid, DownloadBPS: 30_000_000, UploadBPS: 10_000_000, Enabled: true}
	if err := st.UpsertRatePolicy(&pol); err != nil {
		t.Fatal(err)
	}
	pol.DownloadBPS = 50_000_000
	if err := st.UpsertRatePolicy(&pol); err != nil {
		t.Fatal(err)
	}
	pols, _ := st.ListRatePolicies()
	if len(pols) != 1 || pols[0].DownloadBPS != 50_000_000 || pols[0].Status != "pending" {
		t.Fatalf("policy upsert wrong: %+v", pols)
	}

	// second node same uuid -> separate policy
	pol2 := RateLimitPolicy{UUID: u.UUID, NodeID: 9, DownloadBPS: 10_000_000, UploadBPS: 5_000_000, Enabled: true}
	if err := st.UpsertRatePolicy(&pol2); err != nil {
		t.Fatal(err)
	}
	pols, _ = st.ListRatePolicies()
	if len(pols) != 2 {
		t.Fatalf("expected 2 policies (uuid,node unique), got %d", len(pols))
	}

	// status + version
	if err := st.SetPolicyStatus(u.UUID, 7, "synced", "", 42); err != nil {
		t.Fatal(err)
	}
	pols, _ = st.ListRatePolicies()
	for _, p := range pols {
		if p.NodeID == 7 && (p.Status != "synced" || p.LastPushedVer != 42) {
			t.Fatalf("SetPolicyStatus failed: %+v", p)
		}
	}
	if v := st.PolicyPlanVersion(); v <= 0 {
		t.Fatalf("plan version must be positive, got %d", v)
	}

	// delete policy only removes that node's row
	if err := st.DeleteRatePolicy(u.UUID, 7); err != nil {
		t.Fatal(err)
	}
	pols, _ = st.ListRatePolicies()
	if len(pols) != 1 || pols[0].NodeID != 9 {
		t.Fatalf("delete removed wrong row: %+v", pols)
	}
	if err := st.DeleteRatePolicy(u.UUID, 7); err == nil {
		t.Fatal("second delete must return ErrNotFound")
	}

	// profile delete detaches but keeps policy values
	if err := st.DeleteRateProfile(pid); err != nil {
		t.Fatal(err)
	}
	pols, _ = st.ListRatePolicies()
	if len(pols) != 1 || pols[0].ProfileID != nil || pols[0].DownloadBPS != 10_000_000 {
		t.Fatalf("profile delete broke policy: %+v", pols)
	}
}
