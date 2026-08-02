package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const minimumJWTSecretLength = 32

type Config struct {
	Port               string
	JWTSecret          string
	JWTSecretGenerated bool
	JWTExpiresIn       time.Duration
	AdminPassword      string
	NodeEnv            string
	DataDir            string
	UploadsDir         string
	CORSAllowedOrigins []string
}

func Load() (*Config, error) {
	nodeEnv := strings.ToLower(strings.TrimSpace(getEnv("NODE_ENV", "development")))
	port := strings.TrimSpace(getEnv("PORT", "3001"))
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return nil, fmt.Errorf("PORT 必须是 1 到 65535 之间的整数")
	}

	expiresIn, err := time.ParseDuration(strings.TrimSpace(getEnv("JWT_EXPIRES_IN", "168h")))
	if err != nil || expiresIn <= 0 {
		return nil, fmt.Errorf("JWT_EXPIRES_IN 必须是正数时长")
	}
	jwtSecret, generated, err := loadJWTSecret(nodeEnv)
	if err != nil {
		return nil, err
	}
	origins, err := loadCORSOrigins(nodeEnv)
	if err != nil {
		return nil, err
	}

	return &Config{
		Port:               port,
		JWTSecret:          jwtSecret,
		JWTSecretGenerated: generated,
		JWTExpiresIn:       expiresIn,
		AdminPassword:      os.Getenv("ADMIN_PASSWORD"),
		NodeEnv:            nodeEnv,
		DataDir:            getEnv("DATA_DIR", "./data"),
		UploadsDir:         getEnv("UPLOADS_DIR", "./uploads"),
		CORSAllowedOrigins: origins,
	}, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func loadJWTSecret(nodeEnv string) (string, bool, error) {
	secret := strings.TrimSpace(os.Getenv("JWT_SECRET"))
	if secret != "" {
		if len(secret) < minimumJWTSecretLength {
			return "", false, fmt.Errorf("JWT_SECRET 长度至少为 %d 个字符", minimumJWTSecretLength)
		}
		return secret, false, nil
	}
	if nodeEnv == "production" {
		return "", false, fmt.Errorf("生产环境必须配置 JWT_SECRET")
	}

	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", false, fmt.Errorf("生成开发环境 JWT 密钥失败: %w", err)
	}
	return hex.EncodeToString(randomBytes), true, nil
}

func loadCORSOrigins(nodeEnv string) ([]string, error) {
	raw := strings.TrimSpace(os.Getenv("CORS_ALLOWED_ORIGINS"))
	if raw == "" {
		if nodeEnv == "production" {
			return []string{}, nil
		}
		return []string{"http://localhost:5173", "http://127.0.0.1:5173"}, nil
	}

	seen := make(map[string]struct{})
	origins := make([]string, 0)
	for _, item := range strings.Split(raw, ",") {
		origin := strings.TrimSpace(item)
		if origin == "" {
			continue
		}
		if origin == "*" && nodeEnv == "production" {
			return nil, fmt.Errorf("生产环境 CORS_ALLOWED_ORIGINS 不允许使用通配符")
		}
		if _, exists := seen[origin]; exists {
			continue
		}
		seen[origin] = struct{}{}
		origins = append(origins, origin)
	}
	return origins, nil
}
