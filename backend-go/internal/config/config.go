package config

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"os"
	"strconv"
)

type Config struct {
	Port          string
	JWTSecret     string
	JWTExpiresIn  string
	AdminPassword string
	NodeEnv       string
	DataDir       string
	UploadsDir    string
}

var AppConfig *Config

func Load() *Config {
	config := &Config{
		Port:          getEnv("PORT", "3001"),
		JWTSecret:     getJWTSecret(),
		JWTExpiresIn:  getEnv("JWT_EXPIRES_IN", "168h"), // 7 days
		AdminPassword: os.Getenv("ADMIN_PASSWORD"),
		NodeEnv:       getEnv("NODE_ENV", "development"),
		DataDir:       getEnv("DATA_DIR", "./data"),
		UploadsDir:    getEnv("UPLOADS_DIR", "./uploads"),
	}
	AppConfig = config
	return config
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

func getJWTSecret() string {
	secret := os.Getenv("JWT_SECRET")
	if secret != "" {
		return secret
	}

	// In production, warn about missing JWT_SECRET
	if os.Getenv("NODE_ENV") == "production" {
		log.Println("⚠️ WARNING: JWT_SECRET not set in production. Using generated secret (will change on restart).")
	}

	// Generate a random secret
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		log.Fatal("Failed to generate JWT secret:", err)
	}
	return hex.EncodeToString(bytes)
}
