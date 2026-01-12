package services

import "testing"

type headerGetOnly map[string]string

func (h headerGetOnly) Get(key string) string {
	return h[key]
}

func TestContentTypeLowerFromHeader_GetOnly(t *testing.T) {
	h := headerGetOnly{
		"Content-Type": "Text/HTML; charset=UTF-8",
	}
	got := contentTypeLowerFromHeader(h)
	if got != "text/html" {
		t.Fatalf("unexpected content type: %q", got)
	}
}

