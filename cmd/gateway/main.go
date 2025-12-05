package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/prompt-gateway/internal/analyzer"
	"github.com/prompt-gateway/internal/api"
	"github.com/prompt-gateway/internal/audit"
	"github.com/prompt-gateway/internal/cache"
	"github.com/prompt-gateway/internal/config"
	"github.com/prompt-gateway/internal/policy"
	"github.com/redis/go-redis/v9"
)

func main() {
	log.Println("🚀 Starting Prompt Analysis Gateway...")

	// This loads variables from .env into the environment
	if err := godotenv.Load(); err != nil {
		log.Println("⚠️  No .env file found, using environment variables")
	}

	// 1. Load configuration from environment variables
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	log.Printf("✓ Configuration loaded (Port: %s)", cfg.Port)

	// 2. Connect to PostgreSQL
	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Configure connection pool
	// Similar to pool settings in asyncpg/SQLAlchemy
	db.SetMaxOpenConns(cfg.DBMaxOpenConns) // Max connections from config
	db.SetMaxIdleConns(cfg.DBMaxIdleConns) // Idle connections from config
	db.SetConnMaxLifetime(5 * time.Minute) // Connection lifetime

	// Test database connection
	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}
	log.Println("✓ Connected to PostgreSQL")

	// 3. Connect to Redis
	opt, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		log.Fatalf("Failed to parse Redis URL: %v", err)
	}

	// Configure connection pool for high throughput
	opt.PoolSize = cfg.RedisPoolSize
	opt.MinIdleConns = cfg.RedisMinIdle
	opt.PoolTimeout = time.Duration(cfg.RedisPoolTimeout) * time.Second
	opt.MaxRetries = cfg.RedisMaxRetries

	rdb := redis.NewClient(opt)
	defer rdb.Close()

	// Test Redis connection
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	log.Printf("✓ Connected to Redis (Pool: %d, MinIdle: %d)", cfg.RedisPoolSize, cfg.RedisMinIdle)

	// 4. Initialize dependencies (Dependency Injection)
	policyRepo := policy.NewRepository(db)
	policyCache := cache.NewPolicyCache(policyRepo)
	if err := policyCache.Start(ctx); err != nil {
		log.Fatalf("Failed to start policy cache: %v", err)
	}
	defer policyCache.Stop()

	analyzerSvc := analyzer.NewAnalyzer()

	// Initialize Redis audit sync worker (Redis → Postgres for audit logs)
	syncInterval := time.Duration(cfg.RedisSyncInterval) * time.Second
	redisCache := cache.NewRedisCache(db, rdb, syncInterval)
	if err := redisCache.Start(ctx); err != nil {
		log.Fatalf("Failed to start Redis audit sync: %v", err)
	}
	defer redisCache.Stop() // Ensure graceful shutdown and final sync

	// Initialize async audit logger - writes to Redis, synced by Redis audit worker
	auditConfig := audit.Config{
		BufferSize: cfg.AuditBufferSize,
		Workers:    cfg.AuditWorkers,
	}
	auditLogger := audit.NewLoggerWithConfig(db, rdb, auditConfig)
	defer auditLogger.Close() // Ensure graceful shutdown

	log.Printf("✓ Services initialized (Policy cache: in-memory+Postgres refresh, Audit: %d workers→Redis, %d buffer, Redis→Postgres sync: %v)", cfg.AuditWorkers, cfg.AuditBufferSize, syncInterval)

	// 5. Create HTTP handler with dependencies
	handler := api.NewHandler(policyRepo, policyCache, analyzerSvc, auditLogger)

	// 6. Set up routes with request timeout
	requestTimeout := time.Duration(cfg.RequestTimeout) * time.Second
	mux := api.SetupRoutes(handler, requestTimeout)
	log.Printf("✓ Routes configured (timeout: %v)", requestTimeout)

	// 7. Create HTTP server
	server := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.Port),
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// 8. Set up graceful shutdown
	// Create channel to listen for OS interrupt signals
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Start server in a goroutine so it doesn't block
	go func() {
		log.Printf("✓ Server listening on port %s", cfg.Port)
		log.Println("📡 Endpoints:")
		log.Println("   POST http://localhost:" + cfg.Port + "/v1/analyze")
		log.Println("   GET  http://localhost:" + cfg.Port + "/v1/policies")
		log.Println("   POST http://localhost:" + cfg.Port + "/v1/policies")
		log.Println("   GET  http://localhost:" + cfg.Port + "/v1/health")

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	// Block until we receive a shutdown signal
	<-quit
	log.Println("\n🛑 Shutting down server gracefully...")

	// Create a context with timeout for shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown the HTTP server gracefully
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	log.Println("✓ Server stopped")
	log.Println("✓ All background workers will finish on defer cleanup")
}
