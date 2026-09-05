package domain

import "testing"

func TestTripDetailsAreIndependentOfEvidence(t *testing.T) {
	got, err := NormalizeTripDetails(TripDetails{Name: " 合成出差 ", StartDate: "2026-09-04", EndDate: "2026-09-04", Timezone: "Asia/Shanghai", Notes: " 多张凭证 "})
	if err != nil || got.Name != "合成出差" || got.Notes != "多张凭证" {
		t.Fatalf("normalize trip: %v", err)
	}
}

func TestTripDetailsRejectInvalidBoundaries(t *testing.T) {
	base := TripDetails{Name: "synthetic", StartDate: "2026-09-04", EndDate: "2026-09-05", Timezone: "Asia/Shanghai"}
	for _, test := range []struct {
		name string
		edit func(*TripDetails)
	}{
		{"empty_name", func(v *TripDetails) { v.Name = " " }},
		{"invalid_date", func(v *TripDetails) { v.StartDate = "2026-02-30" }},
		{"reverse_dates", func(v *TripDetails) { v.EndDate = "2026-09-03" }},
		{"missing_timezone", func(v *TripDetails) { v.Timezone = "" }},
		{"implicit_timezone", func(v *TripDetails) { v.Timezone = "Local" }},
		{"unknown_timezone", func(v *TripDetails) { v.Timezone = "Synthetic/Missing" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := base
			test.edit(&value)
			if _, err := NormalizeTripDetails(value); err == nil {
				t.Fatal("invalid trip accepted")
			}
		})
	}
}
