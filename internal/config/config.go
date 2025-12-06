package config

import (
	"fmt"
	"os"
)

// Config holds application configuration
type Config struct {
	Port              string
	DatabaseURL       string
	RedisURL          string
	LogLevel          string
	AuditBufferSize   int // Audit logger buffer size
	AuditWorkers      int // Number of audit log workers
	DBMaxOpenConns    int // Maximum number of open database connections
	DBMaxIdleConns    int // Maximum number of idle database connections
	RequestTimeout    int // Request timeout in seconds
	RedisPoolSize     int // Maximum number of Redis connections in pool
	RedisMinIdle      int // Minimum number of idle Redis connections
	RedisPoolTimeout  int // Redis pool timeout in seconds
	RedisMaxRetries   int    // Maximum number of retries for Redis commands
	RedisSyncInterval int    // Redis to Postgres sync interval in seconds
	NemoAPIKey        string // NVIDIA NeMo API Key
	NemoEndpoint      string // NVIDIA NeMo API Endpoint
}

// Load reads configuration from environment variables
func Load() (*Config, error) {
	config := &Config{
		Port:             getEnv("PORT", "8080"),
		DatabaseURL:      getEnv("DATABASE_URL", ""),
		RedisURL:         getEnv("REDIS_URL", ""),
		LogLevel:         getEnv("LOG_LEVEL", "debug"),
		AuditBufferSize:  getEnvAsInt("AUDIT_BUFFER_SIZE", 1000),
		AuditWorkers:     getEnvAsInt("AUDIT_WORKERS", 5),
		DBMaxOpenConns:   getEnvAsInt("DB_MAX_OPEN_CONNS", 20),
		DBMaxIdleConns:   getEnvAsInt("DB_MAX_IDLE_CONNS", 20),
		RequestTimeout:   getEnvAsInt("REQUEST_TIMEOUT", 300),
		RedisPoolSize:     getEnvAsInt("REDIS_POOL_SIZE", 100),
		RedisMinIdle:      getEnvAsInt("REDIS_MIN_IDLE", 20),
		RedisPoolTimeout:  getEnvAsInt("REDIS_POOL_TIMEOUT", 4),
		RedisMaxRetries:   getEnvAsInt("REDIS_MAX_RETRIES", 3),
		RedisSyncInterval: getEnvAsInt("REDIS_SYNC_INTERVAL", 120),
		NemoAPIKey:        getEnv("NVIDIA_NEMO_API", ""),
		NemoEndpoint:      getEnv("NVIDIA_NEMO_ENDPOINT", ""),
	}

	// Validate required fields
	if config.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if config.RedisURL == "" {
		return nil, fmt.Errorf("REDIS_URL is required")
	}
	if config.NemoAPIKey == "" {
		return nil, fmt.Errorf("NVIDIA_NEMO_API is required")
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
