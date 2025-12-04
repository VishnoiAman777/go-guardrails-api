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

	"github.com/joho/godotenv" // Load .env files (like python-dotenv)
	_ "github.com/lib/pq"      // PostgreSQL driver (like psycopg2 in Python)
	"github.com/prompt-gateway/internal/analyzer"
	"github.com/prompt-gateway/internal/api"
	"github.com/prompt-gateway/internal/audit"
	"github.com/prompt-gateway/internal/config"
	"github.com/prompt-gateway/internal/policy"
	"github.com/redis/go-redis/v9" // Redis client (like redis-py in Python)
)

func main() {
	log.Println("🚀 Starting Prompt Analysis Gateway...")

	// 0. Load .env file (like python-dotenv)
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
	// In Python: asyncpg.create_pool() or databases.Database()
	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Configure connection pool
	// Similar to pool settings in asyncpg/SQLAlchemy
	db.SetMaxOpenConns(20)                 // Max connections
	db.SetMaxIdleConns(20)                  // Idle connections
	db.SetConnMaxLifetime(5 * time.Minute) // Connection lifetime

	// Test database connection
	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}
	log.Println("✓ Connected to PostgreSQL")

	// 3. Connect to Redis
	// In Python: redis.from_url() or aioredis.create_redis_pool()
	opt, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		log.Fatalf("Failed to parse Redis URL: %v", err)
	}
	
	rdb := redis.NewClient(opt)
	defer rdb.Close()

	// Test Redis connection
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	log.Println("✓ Connected to Redis")

	// 4. Initialize dependencies (Dependency Injection)
	policyRepo := policy.NewRepository(db)
	analyzerSvc := analyzer.NewAnalyzer()
	
	// Initialize async audit logger with config from environment
	auditConfig := audit.Config{
		BufferSize: cfg.AuditBufferSize,
		Workers:    cfg.AuditWorkers,
	}
	auditLogger := audit.NewLoggerWithConfig(db, auditConfig)
	defer auditLogger.Close() // Ensure graceful shutdown
	
	log.Printf("✓ Services initialized (Audit: %d workers, %d buffer)", cfg.AuditWorkers, cfg.AuditBufferSize)

	// 5. Create HTTP handler with dependencies
	handler := api.NewHandler(policyRepo, analyzerSvc, auditLogger)

	// 6. Set up routes
	mux := api.SetupRoutes(handler)
	log.Println("✓ Routes configured")

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

