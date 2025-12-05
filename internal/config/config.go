package config

import (
	"fmt"
	"os"
)

// Config holds application configuration
// In Python/FastAPI: You might use pydantic-settings or python-dotenv
// In Go: We read from os.Getenv() directly
type Config struct {
	Port              string
	DatabaseURL       string
	RedisURL          string
	LogLevel          string
	AuditBufferSize   int // Audit logger buffer size
	AuditWorkers      int // Number of audit log workers
	DBMaxOpenConns    int // Maximum number of open database connections
	DBMaxIdleConns    int // Maximum number of idle database connections
}

// Load reads configuration from environment variables
// Similar to: from pydantic import BaseSettings in Python
func Load() (*Config, error) {
	config := &Config{
		Port:            getEnv("PORT", "8080"),
		DatabaseURL:     getEnv("DATABASE_URL", ""),
		RedisURL:        getEnv("REDIS_URL", ""),
		LogLevel:        getEnv("LOG_LEVEL", "debug"),
		AuditBufferSize: getEnvAsInt("AUDIT_BUFFER_SIZE", 1000),
		AuditWorkers:    getEnvAsInt("AUDIT_WORKERS", 5),
		DBMaxOpenConns:  getEnvAsInt("DB_MAX_OPEN_CONNS", 20),
		DBMaxIdleConns:  getEnvAsInt("DB_MAX_IDLE_CONNS", 20),
	}

	// Validate required fields
	if config.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if config.RedisURL == "" {
		return nil, fmt.Errorf("REDIS_URL is required")
	}

	return config, nil
}

// getEnv reads an environment variable with a default fallback
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvAsInt reads an environment variable as integer with a default fallback
func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		var intValue int
		if _, err := fmt.Sscanf(value, "%d", &intValue); err == nil {
			return intValue
		}
	}
	return defaultValue
}
