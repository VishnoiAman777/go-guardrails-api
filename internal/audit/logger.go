package audit

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"

	"github.com/lib/pq"
	"github.com/prompt-gateway/pkg/models"
)

// Logger handles audit log persistence
type Logger struct {
	db *sql.DB
}

// NewLogger creates a new Logger
func NewLogger(db *sql.DB) *Logger {
	return &Logger{db: db}
}

// Log records an audit entry to the database
func (l *Logger) Log(entry models.AuditLog) error {
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

// HashContent creates a SHA256 hash of content for audit logging
// Used to log prompts/responses without storing sensitive data
func HashContent(content string) string {
	hash := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", hash)
}
