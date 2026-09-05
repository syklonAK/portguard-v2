package ratelimit

import (
	"strings"
	"testing"
)

func TestParseBandwidth(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"10Mbps", 10_000_000},
		{"10mbps", 10_000_000},
		{"512Kbps", 512_000},
		{"1Gbps", 1_000_000_000},
		{"10000000", 10_000_000},
		{"unlimited", 0},
		{"", 0},
		{"0", 0},
		{"2.5Mbps", 0,}, // fractions rejected: bps are integers
		{"-5Mbps", 0},
		{"abc", 0},
	}
	for _, c := range cases {
		got, err := ParseBandwidth(c.in)
		if c.want == 0 && c.in != "" && c.in != "unlimited" && c.in != "0" {
			if err == nil {
				t.Errorf("ParseBandwidth(%q) = %d, want error", c.in, got)
			}
			continue
		}
		if err != nil && c.want != 0 {
			t.Errorf("ParseBandwidth(%q) error: %v", c.in, err)
			continue
		}
		if got != c.want && !(c.want == 0 && err != nil) {
			t.Errorf("ParseBandwidth(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestFormatBandwidth(t *testing.T) {
	if FormatBandwidth(0) != "unlimited" {
		t.Error("0 must format as unlimited")
	}
	if FormatBandwidth(5_000_000) != "5Mbps" {
		t.Errorf("5Mbps: %q", FormatBandwidth(5_000_000))
	}
	if FormatBandwidth(512_000) != "512Kbps" {
		t.Errorf("512Kbps: %q", FormatBandwidth(512_000))
	}
	if FormatBandwidth(1_000_000_000) != "1Gbps" {
		t.Errorf("1Gbps: %q", FormatBandwidth(1_000_000_000))
	}
}

func TestUUIDValidation(t *testing.T) {
	valid := []string{
		"550e8400-e29b-41d4-a716-446655440000",
		"7C1E1234-AAAA-BBBB-CCCC-DDDDEEEEFFFF",
	}
	for _, u := range valid {
		if !ValidUUID(NormalizeUUID(u)) {
			t.Errorf("expected valid: %q (normalized %q)", u, NormalizeUUID(u))
		}
	}
	invalid := []string{
		"",
		"not-a-uuid",
		"550e8400e29b41d4a716446655440000",   // no dashes
		"550e8400-e29b-41d4-a716-4466554400zz", // non-hex
		"550e8400-e29b-41d4-a716-4466554400000", // too long
	}
	for _, u := range invalid {
		if ValidUUID(u) {
			t.Errorf("expected invalid: %q", u)
		}
	}
	// normalize variants
	n := NormalizeUUID("  URN:UUID:{550E8400-E29B-41D4-A716-446655440000} ")
	if n != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("normalize = %q", n)
	}
}

func TestDiff(t *testing.T) {
	current := []Rule{
		{SourceIP: "1.1.1.1", UUID: "a", DownloadBPS: 5_000_000, UploadBPS: 2_000_000},
		{SourceIP: "2.2.2.2", UUID: "b", DownloadBPS: 10_000_000, UploadBPS: 5_000_000},
	}
	desired := []Rule{
		{SourceIP: "1.1.1.1", UUID: "a", DownloadBPS: 5_000_000, UploadBPS: 2_000_000}, // unchanged
		{SourceIP: "2.2.2.2", UUID: "b", DownloadBPS: 20_000_000, UploadBPS: 5_000_000}, // changed
		{SourceIP: "3.3.3.3", UUID: "c", DownloadBPS: 1_000_000, UploadBPS: 500_000},  // new
	}
	add, update, remove := Diff(current, desired)
	if len(add) != 1 || add[0].SourceIP != "3.3.3.3" {
		t.Errorf("add = %+v", add)
	}
	if len(update) != 1 || update[0].SourceIP != "2.2.2.2" {
		t.Errorf("update = %+v", update)
	}
	if len(remove) != 0 {
		t.Errorf("remove = %+v", remove)
	}

	// a user going unlimited disappears from the desired set
	desired2 := []Rule{{SourceIP: "1.1.1.1", UUID: "a", DownloadBPS: 5_000_000, UploadBPS: 2_000_000}}
	_, _, remove2 := Diff(current, desired2)
	if len(remove2) != 1 || remove2[0].SourceIP != "2.2.2.2" {
		t.Errorf("remove2 = %+v", remove2)
	}
}

func TestClassID(t *testing.T) {
	a := classID("1.2.3.4")
	b := classID("5.6.7.8")
	if a == b {
		t.Errorf("collision: %s == %s", a, b)
	}
	if !strings.HasPrefix(a, "1:") {
		t.Errorf("classID must be under root: %q", a)
	}
}
