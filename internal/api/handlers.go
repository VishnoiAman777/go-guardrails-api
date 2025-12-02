package api

// Handler holds dependencies for HTTP handlers
type Handler struct {
	// TODO: Add dependencies (services, repositories, etc.)
}

// NewHandler creates a new Handler
func NewHandler() *Handler {
	return &Handler{}
}

// TODO: Implement handlers
// - HandleAnalyze    POST /v1/analyze
// - HandleListPolicies   GET /v1/policies
// - HandleCreatePolicy   POST /v1/policies
// - HandleHealth     GET /v1/health
