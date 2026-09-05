package domain

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestAccountInputNormalizationAndBoundaries(t *testing.T) {
	if value, err := NormalizeLoginEmail(" ＯＷＮＥＲ@example.invalid "); err != nil || value != "owner@example.invalid" {
		t.Fatal("canonical email mismatch")
	}
	for _, value := range []string{"", "Name <owner@example.invalid>", "owner", strings.Repeat("x", 255) + "@example.invalid"} {
		if _, err := NormalizeLoginEmail(value); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("invalid email accepted")
		}
	}
	if value, err := NormalizeAccountName("  合成姓名  "); err != nil || value != "合成姓名" {
		t.Fatal("name normalization mismatch")
	}
	for _, value := range []string{" ", strings.Repeat("名", 101), string([]byte{255})} {
		if _, err := NormalizeAccountName(value); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("invalid name accepted")
		}
	}
	if value, err := NormalizeAccountReason(" 合成操作 "); err != nil || value != "合成操作" {
		t.Fatal("reason normalization mismatch")
	}
	for _, value := range []string{" ", strings.Repeat("由", 501), string([]byte{255})} {
		if _, err := NormalizeAccountReason(value); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("invalid reason accepted")
		}
	}
	if !ValidInvitationToken(base64.RawURLEncoding.EncodeToString(make([]byte, 32))) {
		t.Fatal("canonical token rejected")
	}
	for _, value := range []string{"", strings.Repeat("a", 42), strings.Repeat("a", 43), strings.Repeat("?", 43), strings.Repeat("a", 44)} {
		if ValidInvitationToken(value) {
			t.Fatal("noncanonical token accepted")
		}
	}
	if !errors.Is(InvalidInvitation(), ErrInvalidInput) || !errors.Is(InvalidCredentials(), ErrUnauthenticated) {
		t.Fatal("account error category mismatch")
	}
}
