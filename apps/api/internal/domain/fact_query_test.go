package domain

import (
	"strings"
	"testing"
	"time"
)

func TestFactFilterCanonicalDatesLiteralTextAndBoundaries(t *testing.T) {
	for _, input := range []FactFilter{{}, {DateFrom: "2026-01-01"}, {DateTo: "2026-01-01"}, {DateFrom: "2026-01-01", DateTo: "2026-01-01", Query: "  合成  %_\\  名称 "}} {
		got, err := CanonicalFactFilter(input)
		if err != nil || got.AllocationStatus != "all" {
			t.Fatal("valid filter rejected")
		}
		if input.Query != "" && got.Query != "合成 %_\\ 名称" {
			t.Fatal("literal text changed")
		}
	}
	for _, input := range []FactFilter{{DateFrom: "2026-02-30"}, {DateTo: "2026-1-1"}, {DateFrom: "0000-01-01"}, {DateFrom: "2026-02-01", DateTo: "2026-01-01"}, {Query: strings.Repeat("字", 201)}, {Query: "abc\x00"}, {Query: string([]byte{255})}, {AllocationStatus: "hidden"}} {
		if _, err := CanonicalFactFilter(input); err == nil {
			t.Fatal("invalid filter accepted")
		}
	}
	for _, status := range []string{"unallocated", "partial", "allocated"} {
		if _, err := CanonicalFactFilter(FactFilter{AllocationStatus: status}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFactSortKeyUsesImmutableTimestampAndValidatedIdentity(t *testing.T) {
	valid := FactSortKey{CreatedAt: time.Date(2026, 9, 4, 0, 0, 0, 123000, time.UTC), ID: "00000000-0000-0000-0000-000000000001"}
	if !valid.Valid() {
		t.Fatal("valid key rejected")
	}
	for _, id := range []string{"", strings.Repeat("z", 36), "00000000x0000-0000-0000-000000000001"} {
		key := valid
		key.ID = id
		if key.Valid() {
			t.Fatal("invalid key accepted")
		}
	}
	valid.CreatedAt = time.Time{}
	if valid.Valid() {
		t.Fatal("empty time accepted")
	}
}
