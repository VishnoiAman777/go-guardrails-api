package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/prompt-gateway/internal/analyzer"
	"github.com/prompt-gateway/internal/audit"
	"github.com/prompt-gateway/internal/policy"
	"github.com/prompt-gateway/pkg/models"
)

// Handler holds dependencies for HTTP handlers
type Handler struct {
	policyRepo *policy.Repository
	analyzer   *analyzer.Analyzer
	auditLog   *audit.Logger
}

// NewHandler creates a new Handler with all dependencies
func NewHandler(policyRepo *policy.Repository, analyzer *analyzer.Analyzer, auditLog *audit.Logger) *Handler {
	return &Handler{
		policyRepo: policyRepo,
		analyzer:   analyzer,
		auditLog:   auditLog,
	}
}

// HandleAnalyze analyzes prompt/response against security policies
// POST /v1/analyze
func (h *Handler) HandleAnalyze(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	
	// Parse JSON request body
	// In Python: FastAPI does this automatically with Pydantic
	// In Go: We need to decode manually
	var req models.AnalyzeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Error decoding JSON: %v", err)
		respondError(w, http.StatusBadRequest, fmt.Sprintf("Invalid request body: %v", err))
		return
	}

	// Validate request
	if req.ClientID == "" {
		respondError(w, http.StatusBadRequest, "client_id is required")
		return
	}
	if req.Prompt == "" {
		respondError(w, http.StatusBadRequest, "prompt is required")
		return
	}

	// Fetch all active policies from database
	policies, err := h.policyRepo.List(r.Context())
	if err != nil {
		log.Printf("Error fetching policies: %v", err)
		respondError(w, http.StatusInternalServerError, "Failed to fetch policies")
		return
	}

	// Combine prompt and response for analysis
	contentToAnalyze := req.Prompt
	if req.Response != "" {
		contentToAnalyze += "\n" + req.Response
	}

	// Analyze content against policies
	matches, err := h.analyzer.Analyze(r.Context(), contentToAnalyze, policies)
	if err != nil {
		log.Printf("Error analyzing content: %v", err)
		respondError(w, http.StatusInternalServerError, "Analysis failed")
		return
	}

	// Determine action based on triggered policies
	action := "allow"
	allowed := true
	highestSeverity := ""

	for _, match := range matches {
		// Find the policy to get its action
		for _, p := range policies {
			if p.ID == match.PolicyID {
				if p.Action == "block" {
					action = "block"
					allowed = false
				}
				// Track highest severity
				if highestSeverity == "" || severityWeight(match.Severity) > severityWeight(highestSeverity) {
					highestSeverity = match.Severity
				}
				break
			}
		}
	}

	// Redact content if needed
	redactedPrompt := ""
	if len(matches) > 0 {
		redactedPrompt = h.analyzer.RedactContent(req.Prompt, matches, policies)
	}

	// Calculate latency
	latencyMs := time.Since(startTime).Milliseconds()

	// Generate request ID for tracking
	requestID := uuid.New()

	// Create response
	response := models.AnalyzeResponse{
		RequestID:         requestID,
		Allowed:           allowed,
		Action:            action,
		TriggeredPolicies: matches,
		RedactedPrompt:    redactedPrompt,
		LatencyMs:         latencyMs,
	}

	// Log audit entry
	policyIDs := make([]uuid.UUID, len(matches))
	for i, m := range matches {
		policyIDs[i] = m.PolicyID
	}

	auditEntry := models.AuditLog{
		ID:                uuid.New(),
		RequestID:         requestID,
		ClientID:          req.ClientID,
		PromptHash:        audit.HashContent(req.Prompt),
		ResponseHash:      audit.HashContent(req.Response),
		PoliciesTriggered: policyIDs,
		ActionTaken:       action,
		LatencyMs:         int(latencyMs),
		CreatedAt:         time.Now(),
	}

	// Log audit entry synchronously
	if err := h.auditLog.Log(auditEntry); err != nil {
		log.Printf("Failed to log audit entry: %v", err)
		// Don't fail the request if audit logging fails
	}

	// Send JSON response
	respondJSON(w, http.StatusOK, response)
}

// HandleListPolicies returns all active policies
// GET /v1/policies
func (h *Handler) HandleListPolicies(w http.ResponseWriter, r *http.Request) {
	policies, err := h.policyRepo.List(r.Context())
	if err != nil {
		log.Printf("Error listing policies: %v", err)
		respondError(w, http.StatusInternalServerError, "Failed to list policies")
		return
	}

	respondJSON(w, http.StatusOK, policies)
}

// HandleCreatePolicy creates a new security policy
// POST /v1/policies
func (h *Handler) HandleCreatePolicy(w http.ResponseWriter, r *http.Request) {
	var req models.CreatePolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Create policy in database
	policy, err := h.policyRepo.Create(r.Context(), req)
	if err != nil {
		log.Printf("Error creating policy: %v", err)
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, policy)
}

// HandleHealth returns service health status
// GET /v1/health
func (h *Handler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	response := models.HealthResponse{
		Status:    "healthy",
		Timestamp: time.Now(),
		Version:   "1.0.0",
	}

	respondJSON(w, http.StatusOK, response)
}

// Helper functions

// respondJSON sends a JSON response
func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// respondError sends an error response
func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]string{"error": message})
}

// severityWeight returns numeric weight for severity comparison
func severityWeight(severity string) int {
	weights := map[string]int{
		"low":      1,
		"medium":   2,
		"high":     3,
		"critical": 4,
	}
	return weights[severity]
}
