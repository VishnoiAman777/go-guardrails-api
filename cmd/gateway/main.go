package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/joho/godotenv" // Load .env files (like python-dotenv)
	_ "github.com/lib/pq"       // PostgreSQL driver (like psycopg2 in Python)
	"github.com/prompt-gateway/internal/analyzer"
	"github.com/prompt-gateway/internal/api"
	"github.com/prompt-gateway/internal/audit"
	"github.com/prompt-gateway/internal/config"
	"github.com/prompt-gateway/internal/policy"
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
	db.SetMaxOpenConns(25)                 // Max connections
	db.SetMaxIdleConns(5)                  // Idle connections
	db.SetConnMaxLifetime(5 * time.Minute) // Connection lifetime

	// Test database connection
	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}
	log.Println("✓ Connected to PostgreSQL")

	// 3. Initialize dependencies (Dependency Injection)
	policyRepo := policy.NewRepository(db)
	analyzerSvc := analyzer.NewAnalyzer()
	auditLogger := audit.NewLogger(db)
	log.Println("✓ Services initialized")

	// 4. Create HTTP handler with dependencies
	handler := api.NewHandler(policyRepo, analyzerSvc, auditLogger)

	// 5. Set up routes
	mux := api.SetupRoutes(handler)
	log.Println("✓ Routes configured")

	// 6. Create HTTP server
	server := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.Port),
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// 7. Start server
	log.Printf("✓ Server listening on port %s", cfg.Port)
	log.Println("📡 Endpoints:")
	log.Println("   POST http://localhost:" + cfg.Port + "/v1/analyze")
	log.Println("   GET  http://localhost:" + cfg.Port + "/v1/policies")
	log.Println("   POST http://localhost:" + cfg.Port + "/v1/policies")
	log.Println("   GET  http://localhost:" + cfg.Port + "/v1/health")
	
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
