package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadRejectsMissingProductionSecret(t *testing.T) {
	t.Setenv("NODE_ENV", "production")
	t.Setenv("JWT_SECRET", "")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "必须配置") {
		t.Fatalf("应拒绝缺少生产 JWT 密钥，实际错误为 %v", err)
	}
}

func TestLoadDevelopmentDefaults(t *testing.T) {
	t.Setenv("NODE_ENV", "development")
	t.Setenv("JWT_SECRET", "")
	t.Setenv("JWT_EXPIRES_IN", "")
	t.Setenv("CORS_ALLOWED_ORIGINS", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("加载开发配置失败: %v", err)
	}
	if !cfg.JWTSecretGenerated || len(cfg.JWTSecret) < minimumJWTSecretLength {
		t.Fatal("开发环境应生成临时安全密钥")
	}
	if cfg.JWTExpiresIn != 168*time.Hour {
		t.Fatalf("默认 JWT 时长错误: %v", cfg.JWTExpiresIn)
	}
	if len(cfg.CORSAllowedOrigins) != 2 {
		t.Fatalf("开发环境 CORS 默认值错误: %#v", cfg.CORSAllowedOrigins)
	}
}

func TestLoadParsesAndDeduplicatesOrigins(t *testing.T) {
	t.Setenv("NODE_ENV", "production")
	t.Setenv("JWT_SECRET", strings.Repeat("s", minimumJWTSecretLength))
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://bill.example.com, https://bill.example.com,https://admin.example.com")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("加载生产配置失败: %v", err)
	}
	if len(cfg.CORSAllowedOrigins) != 2 {
		t.Fatalf("CORS 来源未去重: %#v", cfg.CORSAllowedOrigins)
	}
}

func TestLoadRejectsUnsafeValues(t *testing.T) {
	tests := []struct {
		name   string
		key    string
		value  string
		secret string
	}{
		{name: "端口非法", key: "PORT", value: "70000", secret: strings.Repeat("s", minimumJWTSecretLength)},
		{name: "过期时间非法", key: "JWT_EXPIRES_IN", value: "forever", secret: strings.Repeat("s", minimumJWTSecretLength)},
		{name: "密钥过短", key: "JWT_SECRET", value: "short", secret: "short"},
		{name: "生产跨域通配符", key: "CORS_ALLOWED_ORIGINS", value: "*", secret: strings.Repeat("s", minimumJWTSecretLength)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("NODE_ENV", "production")
			t.Setenv("JWT_SECRET", test.secret)
			t.Setenv(test.key, test.value)
			if _, err := Load(); err == nil {
				t.Fatal("应拒绝不安全配置")
			}
		})
	}
}
