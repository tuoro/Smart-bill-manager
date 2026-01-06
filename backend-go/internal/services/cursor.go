package services

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type paymentCursor struct {
	Ts int64  `json:"ts"`
	ID string `json:"id"`
}

func encodePaymentCursor(ts int64, id string) string {
	id = strings.TrimSpace(id)
	if ts <= 0 || id == "" {
		return ""
	}
	b, _ := json.Marshal(paymentCursor{Ts: ts, ID: id})
	return base64.RawURLEncoding.EncodeToString(b)
}

func EncodePaymentCursor(ts int64, id string) string {
	return encodePaymentCursor(ts, id)
}

func decodePaymentCursor(raw string) (int64, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, "", nil
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return 0, "", fmt.Errorf("invalid cursor: %w", err)
	}
	var c paymentCursor
	if err := json.Unmarshal(b, &c); err != nil {
		return 0, "", fmt.Errorf("invalid cursor: %w", err)
	}
	if c.Ts <= 0 || strings.TrimSpace(c.ID) == "" {
		return 0, "", fmt.Errorf("invalid cursor payload")
	}
	return c.Ts, strings.TrimSpace(c.ID), nil
}

type invoiceCursor struct {
	CreatedAt string `json:"created_at"`
	ID        string `json:"id"`
}

func encodeInvoiceCursor(t time.Time, id string) string {
	id = strings.TrimSpace(id)
	if t.IsZero() || id == "" {
		return ""
	}
	b, _ := json.Marshal(invoiceCursor{CreatedAt: t.UTC().Format(time.RFC3339Nano), ID: id})
	return base64.RawURLEncoding.EncodeToString(b)
}

func EncodeInvoiceCursor(t time.Time, id string) string {
	return encodeInvoiceCursor(t, id)
}

func decodeInvoiceCursor(raw string) (time.Time, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, "", nil
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("invalid cursor: %w", err)
	}
	var c invoiceCursor
	if err := json.Unmarshal(b, &c); err != nil {
		return time.Time{}, "", fmt.Errorf("invalid cursor: %w", err)
	}
	id := strings.TrimSpace(c.ID)
	if id == "" || strings.TrimSpace(c.CreatedAt) == "" {
		return time.Time{}, "", fmt.Errorf("invalid cursor payload")
	}
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(c.CreatedAt))
	if err != nil {
		return time.Time{}, "", fmt.Errorf("invalid cursor payload: %w", err)
	}
	return t, id, nil
}
