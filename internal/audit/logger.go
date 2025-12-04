package audit

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"log"
	"sync"

	"github.com/lib/pq"
	"github.com/prompt-gateway/pkg/models"
)

// Logger handles audit log persistence with async background workers
type Logger struct {
	db         *sql.DB
	logChannel chan models.AuditLog // Buffered channel for async logging
	stopCh     chan struct{}        // Signal to stop workers
	wg         sync.WaitGroup       // Wait for workers to finish
	workers    int                  // Number of background workers
}

// Config holds logger configuration
type Config struct {
	BufferSize int // Size of the buffered channel
	Workers    int // Number of concurrent workers
}

// DefaultConfig returns sensible defaults for async logging
func DefaultConfig() Config {
	return Config{
		BufferSize: 5000, // Can queue 5000 log entries
		Workers:    50,    // 50 concurrent workers processing logs
	}
}

// NewLogger creates a new Logger with default config
func NewLogger(db *sql.DB) *Logger {
	return NewLoggerWithConfig(db, DefaultConfig())
}

// NewLoggerWithConfig creates a new Logger with custom config
func NewLoggerWithConfig(db *sql.DB, config Config) *Logger {
	logger := &Logger{
		db:         db,
		logChannel: make(chan models.AuditLog, config.BufferSize),
		stopCh:     make(chan struct{}),
		workers:    config.Workers,
	}
	
	// Start background workers
	logger.startWorkers()
	
	return logger
}

// startWorkers launches background goroutines to process logs
func (l *Logger) startWorkers() {
	for i := 0; i < l.workers; i++ {
		l.wg.Add(1)
		go l.worker(i + 1) // Worker IDs start from 1
	}
	log.Printf("✓ Started %d audit log workers", l.workers)
}

// worker is a background goroutine that processes audit log entries
func (l *Logger) worker(id int) {
	defer l.wg.Done()
	
	log.Printf("Audit worker #%d started", id)
	
	for {
		select {
		case entry := <-l.logChannel:
			// Process the log entry
			if err := l.writeToDatabase(entry); err != nil {
				log.Printf("Worker #%d failed to write audit log: %v", id, err)
			}
			
		case <-l.stopCh:
			// Drain remaining logs before stopping
			log.Printf("Worker #%d draining remaining logs...", id)
			for {
				select {
				case entry := <-l.logChannel:
					if err := l.writeToDatabase(entry); err != nil {
						log.Printf("Worker #%d failed to write audit log during shutdown: %v", id, err)
					}
				default:
					log.Printf("Worker #%d stopped", id)
					return
				}
			}
		}
	}
}

// Log sends an audit entry to the background workers (non-blocking)
// This method returns immediately without waiting for DB write
func (l *Logger) Log(entry models.AuditLog) error {
	select {
	case l.logChannel <- entry:
		// Successfully queued for background processing
		return nil
	default:
		// Channel is full - this is a backpressure situation
		// Log synchronously to avoid dropping the audit entry
		log.Println("⚠️  Audit log buffer full, writing synchronously")
		return l.writeToDatabase(entry)
	}
}

// writeToDatabase performs the actual database write
func (l *Logger) writeToDatabase(entry models.AuditLog) error {
	ctx := context.Background()

	query := `
		INSERT INTO audit_logs (
			request_id, client_id, prompt_hash, response_hash,
			policies_triggered, action_taken, latency_ms
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	// Convert UUID slice to PostgreSQL array
	// In Python: you'd just pass the list directly with asyncpg
	policyIDs := make([]string, len(entry.PoliciesTriggered))
	for i, id := range entry.PoliciesTriggered {
		policyIDs[i] = id.String()
	}

	_, err := l.db.ExecContext(
		ctx, query,
		entry.RequestID,
		entry.ClientID,
		entry.PromptHash,
		entry.ResponseHash,
		pq.Array(policyIDs), // pq.Array to handle array in case multiple actions are taken
		entry.ActionTaken,
		entry.LatencyMs,
	)

	if err != nil {
		return fmt.Errorf("failed to log audit entry: %w", err)
	}

	return nil
}

// Close gracefully shuts down the logger
// It stops accepting new logs and waits for workers to finish
func (l *Logger) Close() error {
	log.Println("Shutting down audit logger...")
	
	// Signal workers to stop
	close(l.stopCh)
	
	// Wait for all workers to finish processing
	l.wg.Wait()
	
	log.Println("✓ Audit logger stopped gracefully")
	return nil
}

// HashContent creates a SHA256 hash of content for audit logging
// Used to log prompts/responses without storing sensitive data
func HashContent(content string) string {
	hash := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", hash)
}
