package api

import (
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// SetupRoutes configures all HTTP routes
// In Python/FastAPI: You'd use @app.get() and @app.post() decorators
// In Go: We manually register routes with a ServeMux (router)
func SetupRoutes(handler *Handler) *http.ServeMux {
	mux := http.NewServeMux()

	// Register routes
	// In Python/FastAPI:
	//   @app.post("/v1/analyze")
	// In Go:
	//   mux.HandleFunc("/v1/analyze", handler.HandleAnalyze)
	
	mux.HandleFunc("/v1/analyze", withMiddleware(handler.HandleAnalyze, "POST"))
	mux.HandleFunc("/v1/policies", withMiddleware(policiesHandler(handler), "GET", "POST"))
	mux.HandleFunc("/v1/health", withMiddleware(handler.HandleHealth, "GET"))

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

// withMiddleware wraps a handler with logging and request validation
// In Python/FastAPI: You'd use @app.middleware("http") or dependencies
// In Go: We use the middleware pattern (function that returns a function)
func withMiddleware(handler http.HandlerFunc, allowedMethods ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Generate request ID for tracing
		requestID := uuid.New().String()
		w.Header().Set("X-Request-ID", requestID)

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
		log.Printf("[%s] %s %s - Started", requestID, r.Method, r.URL.Path)

		// Call the actual handler
		handler(w, r)

		// Log completion
		duration := time.Since(start)
		log.Printf("[%s] %s %s - Completed in %v", requestID, r.Method, r.URL.Path, duration)
	}
}
