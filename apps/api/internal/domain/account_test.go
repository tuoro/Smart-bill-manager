package domain

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestAccountInputNormalizationAndBoundaries(t *testing.T) {
	if value, err := NormalizeLoginIdentifier(" ＯＷＮＥＲ@example.invalid "); err != nil || value != "owner@example.invalid" {
		t.Fatal("canonical email mismatch")
	}
	// 不含 "@" 时按用户名接受，使不发信的自托管部署无需编造邮箱地址。
	for _, value := range []string{" ＡＤＭＩＮ ", "admin", "a.b_c-1", strings.Repeat("u", 64)} {
		if _, err := NormalizeLoginIdentifier(value); err != nil {
			t.Fatalf("username %q rejected: %v", value, err)
		}
	}
	if value, err := NormalizeLoginIdentifier(" ＡＤＭＩＮ "); err != nil || value != "admin" {
		t.Fatal("username normalization mismatch")
	}
	for _, value := range []string{
		"", "Name <owner@example.invalid>", strings.Repeat("x", 255) + "@example.invalid",
		"ab", strings.Repeat("u", 65), ".admin", "-admin", "_admin", "ad min", "admin!", "管理员",
	} {
		if _, err := NormalizeLoginIdentifier(value); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid identifier accepted: %q", value)
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
