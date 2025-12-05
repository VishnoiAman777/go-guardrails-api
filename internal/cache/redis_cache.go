package cache

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/prompt-gateway/internal/policy"
	"github.com/prompt-gateway/pkg/models"
	"github.com/redis/go-redis/v9"
)

const (
	policiesKey      = "policies:all"
	policyKeyPrefix  = "policy:"
	redisTTL         = 10 * time.Minute // Redis cache TTL
	refreshInterval  = 10 * time.Minute // In-memory refresh from Redis
)

// RedisCache provides a three-tier cache: In-Memory → Redis → Postgres
// Reads are served from in-memory cache (fastest)
// In-memory cache refreshes from Redis every 10 minutes
// Redis has 10-minute TTL, refreshes from Postgres on cache miss
// Writes go to all three layers with periodic Postgres sync
type RedisCache struct {
	repo           *policy.Repository
	db             *sql.DB         // Direct DB access for audit logs
	rdb            *redis.Client
	mu             sync.RWMutex // Protects in-memory cache and pending operations
	policies       []models.Policy // In-memory cache (fastest)
	pendingWrites  []models.Policy
	syncTicker     *time.Ticker    // Postgres sync ticker
	refreshTicker  *time.Ticker    // In-memory refresh ticker
	stopChan       chan struct{}
	syncOnce       sync.Once
	refreshOnce    sync.Once
	syncInterval   time.Duration
}

// NewRedisCache creates a new three-tier cache (In-Memory → Redis → Postgres)
func NewRedisCache(repo *policy.Repository, db *sql.DB, rdb *redis.Client, syncInterval time.Duration) *RedisCache {
	return &RedisCache{
		repo:          repo,
		db:            db,
		rdb:           rdb,
		policies:      make([]models.Policy, 0),
		pendingWrites: make([]models.Policy, 0),
		stopChan:      make(chan struct{}),
		syncInterval:  syncInterval,
	}
}

// Start initializes the three-tier cache and starts background workers
func (rc *RedisCache) Start(ctx context.Context) error {
	// Initial load: Postgres → Redis → In-Memory
	if err := rc.loadFromPostgres(ctx); err != nil {
		return fmt.Errorf("failed to load policies from Postgres: %w", err)
	}

	// Load into in-memory cache
	if err := rc.refreshFromRedis(ctx); err != nil {
		return fmt.Errorf("failed to load policies into memory: %w", err)
	}

	log.Printf("✓ Three-tier cache initialized with %d policies (Memory → Redis → Postgres)", len(rc.policies))

	// Start Postgres sync worker (writes: Redis → Postgres every 2 min)
	rc.syncOnce.Do(func() {
		rc.syncTicker = time.NewTicker(rc.syncInterval)
		go rc.syncWorker(ctx)
		log.Printf("✓ Redis→Postgres sync worker started (interval: %v)", rc.syncInterval)
	})

	// Start in-memory refresh worker (reads: Redis → Memory every 10 min)
	rc.refreshOnce.Do(func() {
		rc.refreshTicker = time.NewTicker(refreshInterval)
		go rc.refreshWorker(ctx)
		log.Printf("✓ Memory refresh worker started (interval: %v)", refreshInterval)
	})

	return nil
}

// syncWorker runs in the background and syncs pending writes to Postgres periodically
func (rc *RedisCache) syncWorker(ctx context.Context) {
	for {
		select {
		case <-rc.syncTicker.C:
			// Sync policies
			if err := rc.syncToPostgres(ctx); err != nil {
				log.Printf("⚠️  Failed to sync policies to Postgres: %v", err)
			}
			// Sync audit logs
			if err := rc.syncAuditLogsToPostgres(ctx); err != nil {
				log.Printf("⚠️  Failed to sync audit logs to Postgres: %v", err)
			}
		case <-rc.stopChan:
			// Final sync before shutdown
			if err := rc.syncToPostgres(ctx); err != nil {
				log.Printf("⚠️  Failed to perform final policy sync to Postgres: %v", err)
			}
			if err := rc.syncAuditLogsToPostgres(ctx); err != nil {
				log.Printf("⚠️  Failed to perform final audit log sync to Postgres: %v", err)
			}
			rc.syncTicker.Stop()
			log.Println("✓ Redis→Postgres sync worker stopped")
			return
		}
	}
}

// refreshWorker runs in the background and refreshes in-memory cache from Redis
func (rc *RedisCache) refreshWorker(ctx context.Context) {
	for {
		select {
		case <-rc.refreshTicker.C:
			if err := rc.refreshFromRedis(ctx); err != nil {
				log.Printf("⚠️  Failed to refresh in-memory cache from Redis: %v", err)
			} else {
				log.Printf("✓ In-memory cache refreshed: %d policies loaded from Redis", len(rc.policies))
			}
		case <-rc.stopChan:
			rc.refreshTicker.Stop()
			log.Println("✓ Memory refresh worker stopped")
			return
		}
	}
}

// refreshFromRedis loads policies from Redis into in-memory cache
func (rc *RedisCache) refreshFromRedis(ctx context.Context) error {
	// Get all policy IDs from the set
	policyIDs, err := rc.rdb.SMembers(ctx, policiesKey).Result()
	if err != nil {
		return fmt.Errorf("failed to get policy IDs: %w", err)
	}

	policies := make([]models.Policy, 0, len(policyIDs))
	for _, idStr := range policyIDs {
		policy, err := rc.getFromRedis(ctx, idStr)
		if err != nil {
			log.Printf("⚠️  Failed to get policy %s from Redis: %v", idStr, err)
			continue
		}
		policies = append(policies, *policy)
	}

	// Update in-memory cache
	rc.mu.Lock()
	rc.policies = policies
	rc.mu.Unlock()

	return nil
}

// loadFromPostgres loads all policies from Postgres into Redis
func (rc *RedisCache) loadFromPostgres(ctx context.Context) error {
	policies, err := rc.repo.List(ctx)
	if err != nil {
		return err
	}

	// Clear existing policies in Redis
	if err := rc.rdb.Del(ctx, policiesKey).Err(); err != nil {
		return fmt.Errorf("failed to clear Redis cache: %w", err)
	}

	// Store each policy in Redis
	for _, p := range policies {
		if err := rc.set(ctx, p); err != nil {
			return fmt.Errorf("failed to cache policy %s: %w", p.ID, err)
		}
	}

	return nil
}

// syncToPostgres syncs pending writes from Redis to Postgres
func (rc *RedisCache) syncToPostgres(ctx context.Context) error {
	rc.mu.Lock()
	toSync := rc.pendingWrites
	rc.pendingWrites = make([]models.Policy, 0)
	rc.mu.Unlock()

	if len(toSync) == 0 {
		return nil
	}

	log.Printf("🔄 Syncing %d policies from Redis to Postgres...", len(toSync))

	syncCount := 0
	for _, p := range toSync {
		// For now, we'll just create new policies
		// In production, you'd want to implement Update logic as well
		if _, err := rc.repo.Create(ctx, models.CreatePolicyRequest{
			Name:         p.Name,
			Description:  p.Description,
			PatternType:  p.PatternType,
			PatternValue: p.PatternValue,
			Severity:     p.Severity,
			Action:       p.Action,
		}); err != nil {
			log.Printf("⚠️  Failed to sync policy %s to Postgres: %v", p.Name, err)
			continue
		}
		syncCount++
	}

	log.Printf("✓ Synced %d/%d policies to Postgres", syncCount, len(toSync))
	return nil
}

// syncAuditLogsToPostgres syncs audit logs from Redis to Postgres
func (rc *RedisCache) syncAuditLogsToPostgres(ctx context.Context) error {
	// Check queue size before syncing
	queueSize, err := rc.rdb.LLen(ctx, "audit_logs:pending").Result()
	if err != nil {
		log.Printf("⚠️  Failed to get audit log queue size: %v", err)
	} else if queueSize > 0 {
		log.Printf("📊 Audit log queue size: %d logs pending", queueSize)
	}

	// Get batch of audit logs from Redis list (up to 10K at a time)
	batchSize := int64(10000)
	
	// Pop logs from the right side of the list (FIFO order) - REMOVES from Redis!
	logs, err := rc.rdb.RPopCount(ctx, "audit_logs:pending", int(batchSize)).Result()
	if err == redis.Nil || len(logs) == 0 {
		// No logs to sync
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read audit logs from Redis: %w", err)
	}

	log.Printf("🔄 Syncing %d audit logs from Redis to Postgres...", len(logs))

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

// Get returns all cached policies from in-memory cache (thread-safe, fastest)
func (rc *RedisCache) Get(ctx context.Context) ([]models.Policy, error) {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	// Return a copy to prevent external modifications
	result := make([]models.Policy, len(rc.policies))
	copy(result, rc.policies)
	return result, nil
}

// GetByID returns a single policy by ID from in-memory cache first, then Redis
func (rc *RedisCache) GetByID(ctx context.Context, id string) (*models.Policy, error) {
	// Try in-memory cache first
	rc.mu.RLock()
	for _, p := range rc.policies {
		if p.ID.String() == id {
			rc.mu.RUnlock()
			return &p, nil
		}
	}
	rc.mu.RUnlock()

	// Cache miss: Try Redis
	policy, err := rc.getFromRedis(ctx, id)
	if err == nil {
		// Add to in-memory cache
		rc.mu.Lock()
		rc.policies = append(rc.policies, *policy)
		rc.mu.Unlock()
		return policy, nil
	}

	// Redis miss: Load from Postgres
	return rc.loadPolicyFromPostgres(ctx, id)
}

// getFromRedis retrieves a policy from Redis
func (rc *RedisCache) getFromRedis(ctx context.Context, id string) (*models.Policy, error) {
	key := policyKeyPrefix + id
	data, err := rc.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, fmt.Errorf("policy not found in Redis")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get policy from Redis: %w", err)
	}

	var policy models.Policy
	if err := json.Unmarshal([]byte(data), &policy); err != nil {
		return nil, fmt.Errorf("failed to unmarshal policy: %w", err)
	}

	return &policy, nil
}

// loadPolicyFromPostgres loads a single policy from Postgres on cache miss
func (rc *RedisCache) loadPolicyFromPostgres(ctx context.Context, id string) (*models.Policy, error) {
	policyID, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("invalid policy ID: %w", err)
	}

	policy, err := rc.repo.GetByID(ctx, policyID)
	if err != nil {
		return nil, fmt.Errorf("failed to load policy from Postgres: %w", err)
	}

	// Cache in Redis with TTL
	if err := rc.set(ctx, *policy); err != nil {
		log.Printf("⚠️  Failed to cache policy in Redis: %v", err)
	}

	// Add to in-memory cache
	rc.mu.Lock()
	rc.policies = append(rc.policies, *policy)
	rc.mu.Unlock()

	log.Printf("✓ Policy %s loaded from Postgres (cache miss)", policy.Name)
	return policy, nil
}

// Create creates a new policy in all three cache layers
func (rc *RedisCache) Create(ctx context.Context, req models.CreatePolicyRequest) (*models.Policy, error) {
	// Create policy object
	policy := models.Policy{
		ID:           uuid.New(),
		Name:         req.Name,
		Description:  req.Description,
		PatternType:  req.PatternType,
		PatternValue: req.PatternValue,
		Severity:     req.Severity,
		Action:       req.Action,
		Enabled:      true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	// Store in Redis immediately with TTL
	if err := rc.set(ctx, policy); err != nil {
		return nil, fmt.Errorf("failed to cache policy in Redis: %w", err)
	}

	// Add to in-memory cache immediately
	rc.mu.Lock()
	rc.policies = append(rc.policies, policy)
	rc.pendingWrites = append(rc.pendingWrites, policy)
	rc.mu.Unlock()

	log.Printf("✓ Policy %s created in memory+Redis, queued for Postgres sync", policy.Name)
	return &policy, nil
}

// set stores a policy in Redis with 10-minute TTL
func (rc *RedisCache) set(ctx context.Context, policy models.Policy) error {
	// Serialize policy to JSON
	data, err := json.Marshal(policy)
	if err != nil {
		return fmt.Errorf("failed to marshal policy: %w", err)
	}

	// Store policy data with TTL
	key := policyKeyPrefix + policy.ID.String()
	if err := rc.rdb.Set(ctx, key, data, redisTTL).Err(); err != nil {
		return fmt.Errorf("failed to set policy: %w", err)
	}

	// Add policy ID to the set
	if err := rc.rdb.SAdd(ctx, policiesKey, policy.ID.String()).Err(); err != nil {
		return fmt.Errorf("failed to add policy to set: %w", err)
	}

	return nil
}

// Count returns the number of policies in Redis
func (rc *RedisCache) Count(ctx context.Context) (int64, error) {
	return rc.rdb.SCard(ctx, policiesKey).Result()
}

// Invalidate reloads all policies from Postgres into all cache layers
func (rc *RedisCache) Invalidate(ctx context.Context) error {
	log.Println("🔄 Invalidating all cache layers, reloading from Postgres...")
	
	// Load Postgres → Redis
	if err := rc.loadFromPostgres(ctx); err != nil {
		return err
	}
	
	// Load Redis → Memory
	return rc.refreshFromRedis(ctx)
}

// Stop gracefully stops all background workers
func (rc *RedisCache) Stop() {
	close(rc.stopChan)
}
