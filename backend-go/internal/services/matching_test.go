package services

import "testing"

func TestParseFlexibleDateTime(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"2025-10-11", true},
		{"2025/10/11", true},
		{"2025.10.11", true},
		{"2025年10月11日", true},
		{"2025-10-11T10:11:12Z", true},
		{"", false},
		{"not-a-date", false},
	}

	for _, tc := range cases {
		_, ok := parseFlexibleDateTime(tc.in)
		if ok != tc.want {
			t.Fatalf("parseFlexibleDateTime(%q) ok=%v want=%v", tc.in, ok, tc.want)
		}
	}
}

func TestNormalizeNameAndSimilarity(t *testing.T) {
	a := normalizeName("上海郡徕实业有限公司")
	b := normalizeName("上海郡徕实业有限公司(910360)")
	if a == "" || b == "" {
		t.Fatalf("expected normalized non-empty")
	}
	if bigramJaccard(a, b) < 0.6 {
		t.Fatalf("expected high similarity, got %v", bigramJaccard(a, b))
	}
}

