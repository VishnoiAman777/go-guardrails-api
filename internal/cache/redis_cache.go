package cache

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/lib/pq"
	"github.com/prompt-gateway/internal/metrics"
	"github.com/prompt-gateway/pkg/models"
	"github.com/redis/go-redis/v9"
)

// RedisCache now coordinates audit log persistence between Redis and Postgres.
type RedisCache struct {
	db           *sql.DB
	rdb          *redis.Client
	syncTicker   *time.Ticker
	stopChan     chan struct{}
	stopOnce     sync.Once
	syncInterval time.Duration
}

// NewRedisCache creates a new RedisCache focused on audit log syncing.
func NewRedisCache(db *sql.DB, rdb *redis.Client, syncInterval time.Duration) *RedisCache {
	return &RedisCache{
		db:           db,
		rdb:          rdb,
		stopChan:     make(chan struct{}),
		syncInterval: syncInterval,
	}
}

// Start begins the background worker that periodically syncs audit logs
// from Redis to Postgres.
func (rc *RedisCache) Start(ctx context.Context) error {
	if rc.syncInterval <= 0 {
		return fmt.Errorf("invalid sync interval: %v", rc.syncInterval)
	}

	rc.syncTicker = time.NewTicker(rc.syncInterval)
	go rc.syncWorker(ctx)
	log.Printf("✓ Redis→Postgres audit sync worker started (interval: %v)", rc.syncInterval)

	return nil
}

// syncWorker runs in the background and syncs audit logs to Postgres.
func (rc *RedisCache) syncWorker(ctx context.Context) {
	for {
		select {
		case <-rc.syncTicker.C:
			if err := rc.syncAuditLogsToPostgres(ctx); err != nil {
				log.Printf("⚠️  Failed to sync audit logs to Postgres: %v", err)
			}
		case <-rc.stopChan:
			if rc.syncTicker != nil {
				rc.syncTicker.Stop()
			}
			// Best effort final sync
			if err := rc.syncAuditLogsToPostgres(ctx); err != nil {
				log.Printf("⚠️  Failed to perform final audit log sync to Postgres: %v", err)
			}
			log.Println("✓ Redis→Postgres audit sync worker stopped")
			return
		case <-ctx.Done():
			if rc.syncTicker != nil {
				rc.syncTicker.Stop()
			}
			log.Println("✓ Redis→Postgres audit sync worker stopped (context cancelled)")
			return
		}
	}
}

// syncAuditLogsToPostgres syncs audit logs from Redis to Postgres
func (rc *RedisCache) syncAuditLogsToPostgres(ctx context.Context) error {
	// Check queue size before syncing
	queueSize, err := rc.rdb.LLen(ctx, "audit_logs:pending").Result()
	if err != nil {
		log.Printf("⚠️  Failed to get audit log queue size: %v", err)
	} else {
		metrics.AuditQueueLength.Set(float64(queueSize))
		if queueSize > 0 {
			log.Printf("📊 Audit log queue size: %d logs pending", queueSize)
		}
	}

	// Get batch of audit logs from Redis list (up to 10K at a time)
	batchSize := int64(10000)

	// Pop logs from the right side of the list (FIFO order) - REMOVES from Redis!
	logs, err := rc.rdb.RPopCount(ctx, "audit_logs:pending", int(batchSize)).Result()
	if err == redis.Nil || len(logs) == 0 {
		// No logs to sync
		metrics.AuditQueueLength.Set(0)
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read audit logs from Redis: %w", err)
	}

	log.Printf("🔄 Syncing %d audit logs from Redis to Postgres...", len(logs))
	remaining := queueSize - int64(len(logs))
	if remaining < 0 {
		remaining = 0
	}
	metrics.AuditQueueLength.Set(float64(remaining))

	// Parse all logs first
	entries := make([]models.AuditLog, 0, len(logs))
	failedLogs := make([]string, 0)

	for _, logData := range logs {
		var entry models.AuditLog
		if err := json.Unmarshal([]byte(logData), &entry); err != nil {
			log.Printf("⚠️  Failed to unmarshal audit log: %v", err)
			continue // Skip bad JSON
		}
		entries = append(entries, entry)
	}

	if len(entries) == 0 {
		return nil
	}

	// Use bulk COPY for maximum performance
	if err := rc.bulkWriteAuditLogs(ctx, entries); err != nil {
		log.Printf("⚠️  Bulk insert failed: %v, falling back to individual inserts", err)

		// Fallback: individual inserts with retry logic
		syncCount := 0
		for i, entry := range entries {
			if err := rc.writeAuditLogToPostgres(ctx, entry); err != nil {
				log.Printf("⚠️  Failed to write audit log to Postgres: %v", err)
				failedLogs = append(failedLogs, logs[i])
				continue
			}
			syncCount++
		}

		// Re-push failed logs back to Redis for retry
		if len(failedLogs) > 0 {
			for _, logData := range failedLogs {
				if err := rc.rdb.LPush(ctx, "audit_logs:pending", logData).Err(); err != nil {
					log.Printf("⚠️  Failed to re-queue audit log: %v", err)
				}
			}
			log.Printf("⚠️  Re-queued %d failed audit logs for retry", len(failedLogs))
		}

		log.Printf("✓ Synced %d/%d audit logs to Postgres (fallback mode)", syncCount, len(entries))
		return nil
	}

	log.Printf("✓ Bulk synced %d audit logs to Postgres", len(entries))
	return nil
}

// bulkWriteAuditLogs uses PostgreSQL COPY for high-performance bulk inserts
func (rc *RedisCache) bulkWriteAuditLogs(ctx context.Context, entries []models.AuditLog) error {
	// Begin transaction
	tx, err := rc.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // Rollback if not committed

	// Prepare COPY statement
	stmt, err := tx.PrepareContext(ctx, pq.CopyIn(
		"audit_logs",
		"request_id",
		"client_id",
		"prompt_hash",
		"response_hash",
		"policies_triggered",
		"action_taken",
		"latency_ms",
	))
	if err != nil {
		return fmt.Errorf("failed to prepare COPY statement: %w", err)
	}
	defer stmt.Close()

	// Execute COPY for all entries
	for _, entry := range entries {
		// Convert UUID slice to string slice for PostgreSQL array
		policyIDs := make([]string, len(entry.PoliciesTriggered))
		for i, id := range entry.PoliciesTriggered {
			policyIDs[i] = id.String()
		}

		_, err = stmt.ExecContext(
			ctx,
			entry.RequestID,
			entry.ClientID,
			entry.PromptHash,
			entry.ResponseHash,
			pq.Array(policyIDs),
			entry.ActionTaken,
			entry.LatencyMs,
		)
		if err != nil {
			return fmt.Errorf("failed to add row to COPY: %w", err)
		}
	}

	// Flush the COPY buffer
	if _, err := stmt.ExecContext(ctx); err != nil {
		return fmt.Errorf("failed to flush COPY: %w", err)
	}

	// Close statement before commit
	if err := stmt.Close(); err != nil {
		return fmt.Errorf("failed to close COPY statement: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// writeAuditLogToPostgres writes a single audit log to Postgres (fallback only)
func (rc *RedisCache) writeAuditLogToPostgres(ctx context.Context, entry models.AuditLog) error {
	query := `
		INSERT INTO audit_logs (
			request_id, client_id, prompt_hash, response_hash,
			policies_triggered, action_taken, latency_ms
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	// Convert UUID slice to string slice for PostgreSQL array
	policyIDs := make([]string, len(entry.PoliciesTriggered))
	for i, id := range entry.PoliciesTriggered {
		policyIDs[i] = id.String()
	}

	_, err := rc.db.ExecContext(
		ctx, query,
		entry.RequestID,
		entry.ClientID,
		entry.PromptHash,
		entry.ResponseHash,
		pq.Array(policyIDs),
		entry.ActionTaken,
		entry.LatencyMs,
	)

	if err != nil {
		return fmt.Errorf("failed to insert audit log: %w", err)
	}

	return nil
}

// Stop gracefully stops the background worker.
func (rc *RedisCache) Stop() {
	rc.stopOnce.Do(func() {
		close(rc.stopChan)
	})
}
