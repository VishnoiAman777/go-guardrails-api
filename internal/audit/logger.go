package audit

import (
	"github.com/prompt-gateway/pkg/models"
)

// Logger handles audit log persistence
type Logger struct {
	// TODO: Add database connection
}

// NewLogger creates a new Logger
func NewLogger() *Logger {
	return &Logger{}
}

// Log records an audit entry
// TODO: Implement audit logging
//
// Day 1: Synchronous logging
// Day 2: Async logging with buffered channel
//
// Consider:
//   - Don't block the request on audit writes
//   - Batch inserts for performance
//   - Graceful shutdown (flush pending logs)
func (l *Logger) Log(entry models.AuditLog) error {
	return nil
}
