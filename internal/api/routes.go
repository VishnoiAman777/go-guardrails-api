package api

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// ctxKey is a custom type for context keys to avoid collisions
type ctxKey string

// Context keys
const (
    requestIDKey ctxKey = "request_id"
)

// SetupRoutes configures all HTTP routes
// In Go: We manually register routes with a ServeMux (router)
func SetupRoutes(handler *Handler, requestTimeout time.Duration) *http.ServeMux {
	mux := http.NewServeMux()

	// Register routes with timeout middleware
	mux.HandleFunc("/v1/analyze", withMiddleware(handler.HandleAnalyze, requestTimeout, "POST"))
	mux.HandleFunc("/v1/policies", withMiddleware(policiesHandler(handler), requestTimeout, "GET", "POST"))
	mux.HandleFunc("/v1/health", withMiddleware(handler.HandleHealth, requestTimeout, "GET"))

	return mux
}

// policiesHandler routes GET/POST to appropriate handlers
// Go's http.ServeMux doesn't support method-based routing natively
func policiesHandler(h *Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.HandleListPolicies(w, r)
		case http.MethodPost:
			h.HandleCreatePolicy(w, r)
		default:
			respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		}
	}
}

// withMiddleware wraps a handler with timeout, logging and request validation
func withMiddleware(handler http.HandlerFunc, timeout time.Duration, allowedMethods ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Generate request ID for tracing
		requestID := uuid.New().String()
		w.Header().Set("X-Request-ID", requestID)

		// Create context with timeout for this request
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel() // Ensure context is cancelled to free resources

		// Store request ID in context so handlers can access it
		ctx = context.WithValue(ctx, requestIDKey, requestID)
		r = r.WithContext(ctx)
		// Add CORS headers (for browser-based clients)
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		// Handle preflight requests
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Check if method is allowed
		methodAllowed := false
		for _, method := range allowedMethods {
			if r.Method == method {
				methodAllowed = true
				break
			}
		}

		if !methodAllowed {
			respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}

		// Log request
		start := time.Now()
		log.Printf("[%s] %s %s - Started (timeout: %v)", requestID, r.Method, r.URL.Path, timeout)

		// Execute handler - context timeout is already set and will be enforced
		// within handler operations (Redis, DB, etc.) that respect context
		handler(w, r)

		// Check if context timed out after handler completes
		if ctx.Err() == context.DeadlineExceeded {
			duration := time.Since(start)
			log.Printf("[%s] %s %s - Timeout after %v", requestID, r.Method, r.URL.Path, duration)
		} else {
			duration := time.Since(start)
			log.Printf("[%s] %s %s - Completed in %v", requestID, r.Method, r.URL.Path, duration)
		}
	}
}
