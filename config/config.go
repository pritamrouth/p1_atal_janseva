package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all runtime configuration loaded from environment variables.
// Variable names match the original atal_janseva backend so the same .env works.
// This service is Meta Cloud API only – Plivo variables are ignored if present.
type Config struct {
	// WhatsApp Cloud API (Meta)
	WAPhoneNumberID string
	WAAccessToken   string
	WAVerifyToken   string

	// PostgreSQL
	DatabaseURL    string
	DBMaxOpenConns int
	DBMaxIdleConns int

	// Redis (local)
	RedisAddr     string // e.g. localhost:6379
	RedisPassword string // empty string = no auth
	RedisDB       int    // default 0

	// Worker pool
	WorkerCount int // number of concurrent message-processing goroutines
	QueueDepth  int // buffered channel depth

	// Inactivity monitoring: delete user session after 90 seconds of inactivity
	// (no message sent). User starts fresh from language picker on next message.
	EnableInactivityMonitor bool

	// Server
	Port string
}

// Load reads config from environment.
func Load() (*Config, error) {
	c := &Config{
		WAPhoneNumberID:         os.Getenv("WA_PHONE_NUMBER_ID"),
		WAAccessToken:           os.Getenv("WA_ACCESS_TOKEN"),
		WAVerifyToken:           os.Getenv("WA_VERIFY_TOKEN"),
		DatabaseURL:             os.Getenv("DATABASE_URL"),
		RedisAddr:               getEnvOr("REDIS_ADDR", "localhost:6379"),
		RedisPassword:           os.Getenv("REDIS_PASSWORD"),
		RedisDB:                 getEnvInt("REDIS_DB", 0),
		DBMaxOpenConns:          getEnvInt("DB_MAX_OPEN_CONNS", 25),
		DBMaxIdleConns:          getEnvInt("DB_MAX_IDLE_CONNS", 10),
		WorkerCount:             getEnvInt("WORKER_COUNT", 50),
		QueueDepth:              getEnvInt("QUEUE_DEPTH", 2000),
		Port:                    getEnvOr("PORT", "8080"),
		EnableInactivityMonitor: getEnvBool("ENABLE_INACTIVITY_MONITOR", true),
	}
	if c.WAPhoneNumberID == "" {
		return nil, fmt.Errorf("WA_PHONE_NUMBER_ID is required")
	}
	if c.WAAccessToken == "" {
		return nil, fmt.Errorf("WA_ACCESS_TOKEN is required")
	}
	if c.WAVerifyToken == "" {
		return nil, fmt.Errorf("WA_VERIFY_TOKEN is required")
	}
	if c.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	return c, nil
}

func getEnvOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		return v == "true" || v == "1" || v == "yes"
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
