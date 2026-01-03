package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port               string
	SessionCookieName  string
	SessionExpiresIn   string
	CookieSameSite     string
	CookieSecure       *bool
	PasetoV4LocalKey   string
	CORSAllowedOrigins string
	AdminPassword      string
	NodeEnv            string
	DataDir            string
	UploadsDir         string
}

var AppConfig *Config

func Load() *Config {
	nodeEnv := getEnv("NODE_ENV", "development")
	corsDefault := ""
	if nodeEnv != "production" {
		// Dev default: allow Vite dev server to call the API with cookies.
		corsDefault = "http://localhost:5173,http://127.0.0.1:5173"
	}
	config := &Config{
		Port:               getEnv("PORT", "3001"),
		SessionCookieName:  getEnv("SESSION_COOKIE_NAME", "sbm_session"),
		SessionExpiresIn:   getEnv("SESSION_EXPIRES_IN", "168h"), // 7 days
		CookieSameSite:     getEnv("COOKIE_SAMESITE", "Lax"),     // Lax|Strict|None
		CookieSecure:       getEnvBoolPtr("COOKIE_SECURE"),       // default: production=true, otherwise false
		PasetoV4LocalKey:   getEnv("PASETO_V4_LOCAL_KEY", ""),
		CORSAllowedOrigins: getEnv("CORS_ALLOWED_ORIGINS", corsDefault),
		AdminPassword:      os.Getenv("ADMIN_PASSWORD"),
		NodeEnv:            nodeEnv,
		DataDir:            getEnv("DATA_DIR", "./data"),
		UploadsDir:         getEnv("UPLOADS_DIR", "./uploads"),
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

func getEnvBoolPtr(key string) *bool {
	v := os.Getenv(key)
	if v == "" {
		return nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return nil
	}
	return &b
}
