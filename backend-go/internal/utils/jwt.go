package utils

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const jwtIssuer = "smart-bill-manager"

type Claims struct {
	UserID   string `json:"userId"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

type TokenManager struct {
	secret    []byte
	expiresIn time.Duration
	now       func() time.Time
}

func NewTokenManager(secret string, expiresIn time.Duration) (*TokenManager, error) {
	if len(secret) < 32 {
		return nil, errors.New("JWT 密钥长度至少为 32 个字符")
	}
	if expiresIn <= 0 {
		return nil, errors.New("JWT 有效期必须为正数")
	}
	return &TokenManager{secret: []byte(secret), expiresIn: expiresIn, now: time.Now}, nil
}

func (manager *TokenManager) GenerateToken(userID, username, role string) (string, error) {
	if manager == nil {
		return "", errors.New("JWT 管理器未初始化")
	}
	now := manager.now().UTC()
	claims := Claims{
		UserID:   userID,
		Username: username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    jwtIssuer,
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(now.Add(manager.expiresIn)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(manager.secret)
}

func (manager *TokenManager) VerifyToken(tokenString string) (*Claims, error) {
	if manager == nil {
		return nil, errors.New("JWT 管理器未初始化")
	}
	token, err := jwt.ParseWithClaims(
		tokenString,
		&Claims{},
		func(token *jwt.Token) (any, error) {
			if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
				return nil, fmt.Errorf("不支持的 JWT 签名算法: %s", token.Method.Alg())
			}
			return manager.secret, nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(jwtIssuer),
		jwt.WithTimeFunc(manager.now),
	)
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid || claims.UserID == "" || claims.Subject != claims.UserID {
		return nil, errors.New("JWT 无效")
	}
	return claims, nil
}
