package services

import (
	"fmt"
	"strings"
	"time"
)

const (
	assignSrcAuto    = "auto"
	assignSrcManual  = "manual"
	assignSrcBlocked = "blocked"

	assignStateAssigned = "assigned"
	assignStateNoMatch  = "no_match"
	assignStateOverlap  = "overlap"
	assignStateBlocked  = "blocked"
)

func loadLocationOrUTC(name string) *time.Location {
	name = strings.TrimSpace(name)
	if name == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}

func parseRFC3339ToUTC(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	// RFC3339Nano accepts both with/without fractional seconds.
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

func parsePaymentTimeToUTC(s string, defaultLoc *time.Location) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty transaction_time")
	}

	// Prefer explicit offsets if present.
	if t, err := parseRFC3339ToUTC(s); err == nil {
		return t, nil
	}

	if defaultLoc == nil {
		defaultLoc = time.UTC
	}

	// OCR often yields "YYYY-MM-DD HH:mm:ss" (no offset).
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", s, defaultLoc); err == nil {
		return t.UTC(), nil
	}
	// Sometimes date-only.
	if t, err := time.ParseInLocation("2006-01-02", s, defaultLoc); err == nil {
		return t.UTC(), nil
	}

	return time.Time{}, fmt.Errorf("unsupported transaction_time format: %q", s)
}

func unixMilli(t time.Time) int64 {
	return t.UTC().UnixNano() / int64(time.Millisecond)
}
