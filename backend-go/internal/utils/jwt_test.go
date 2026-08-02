package utils

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestTokenManagerRoundTrip(t *testing.T) {
	manager, err := NewTokenManager(strings.Repeat("s", 32), time.Hour)
	if err != nil {
		t.Fatalf("创建 JWT 管理器失败: %v", err)
	}
	now := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	token, err := manager.GenerateToken("user-1", "alice", "admin")
	if err != nil {
		t.Fatalf("签发 JWT 失败: %v", err)
	}
	claims, err := manager.VerifyToken(token)
	if err != nil {
		t.Fatalf("校验 JWT 失败: %v", err)
	}
	if claims.UserID != "user-1" || claims.Username != "alice" || claims.Role != "admin" {
		t.Fatalf("JWT 声明不一致: %#v", claims)
	}
}

func TestTokenManagerRejectsExpiredAndWrongAlgorithmTokens(t *testing.T) {
	manager, err := NewTokenManager(strings.Repeat("s", 32), time.Hour)
	if err != nil {
		t.Fatalf("创建 JWT 管理器失败: %v", err)
	}
	issuedAt := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return issuedAt }
	token, err := manager.GenerateToken("user-1", "alice", "user")
	if err != nil {
		t.Fatalf("签发 JWT 失败: %v", err)
	}
	manager.now = func() time.Time { return issuedAt.Add(2 * time.Hour) }
	if _, err := manager.VerifyToken(token); err == nil {
		t.Fatal("应拒绝过期 JWT")
	}

	claims := Claims{UserID: "user-1", RegisteredClaims: jwt.RegisteredClaims{Issuer: jwtIssuer, Subject: "user-1", ExpiresAt: jwt.NewNumericDate(issuedAt.Add(time.Hour))}}
	noneToken := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	signed, err := noneToken.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("创建无签名测试 JWT 失败: %v", err)
	}
	manager.now = func() time.Time { return issuedAt }
	if _, err := manager.VerifyToken(signed); err == nil {
		t.Fatal("应拒绝非 HS256 JWT")
	}
}
