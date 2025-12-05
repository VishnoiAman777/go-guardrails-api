package cache

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/prompt-gateway/internal/policy"
	"github.com/prompt-gateway/pkg/models"
)

// PolicyCache provides an in-memory cache for policies with automatic refresh
type PolicyCache struct {
	repo          *policy.Repository
	policies      []models.Policy
	mu            sync.RWMutex // Protects policies slice
	refreshTicker *time.Ticker
	stopChan      chan struct{}
	refreshOnce   sync.Once
}

// NewPolicyCache creates a new policy cache
func NewPolicyCache(repo *policy.Repository) *PolicyCache {
	return &PolicyCache{
		repo:     repo,
		policies: make([]models.Policy, 0),
		stopChan: make(chan struct{}),
	}
}

// Start initializes the cache and starts the background refresh worker
// It performs an initial load and then refreshes every 10 minutes
func (pc *PolicyCache) Start(ctx context.Context) error {
	// Initial load
	if err := pc.refresh(ctx); err != nil {
		return err
	}
	log.Printf("✓ Policy cache initialized with %d policies", len(pc.policies))

	// Start background refresh worker
	pc.refreshOnce.Do(func() {
		pc.refreshTicker = time.NewTicker(10 * time.Minute)
		go pc.refreshWorker(ctx)
		log.Println("✓ Policy cache refresh worker started (interval: 10 minutes)")
	})

	return nil
}

// refreshWorker runs in the background and refreshes the cache periodically
func (pc *PolicyCache) refreshWorker(ctx context.Context) {
	for {
		select {
		case <-pc.refreshTicker.C:
			if err := pc.refresh(ctx); err != nil {
				log.Printf("⚠️  Failed to refresh policy cache: %v", err)
			} else {
				log.Printf("✓ Policy cache refreshed: %d policies loaded", len(pc.policies))
			}
		case <-pc.stopChan:
			pc.refreshTicker.Stop()
			log.Println("✓ Policy cache refresh worker stopped")
			return
		}
	}
}

// refresh fetches policies from the database and updates the cache
func (pc *PolicyCache) refresh(ctx context.Context) error {
	policies, err := pc.repo.List(ctx)
	if err != nil {
		return err
	}

	// Update cache with write lock
	pc.mu.Lock()
	pc.policies = policies
	pc.mu.Unlock()

	return nil
}

// Get returns all cached policies (thread-safe)
func (pc *PolicyCache) Get() []models.Policy {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	// Return a copy to prevent external modifications
	result := make([]models.Policy, len(pc.policies))
	copy(result, pc.policies)
	return result
}

// Invalidate forces an immediate cache refresh
// Useful when policies are created/updated/deleted
func (pc *PolicyCache) Invalidate(ctx context.Context) error {
	log.Println("🔄 Invalidating policy cache...")
	return pc.refresh(ctx)
}

// Stop gracefully stops the background refresh worker
func (pc *PolicyCache) Stop() {
	close(pc.stopChan)
}
