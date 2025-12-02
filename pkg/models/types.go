package models

import (
	"time"

	"github.com/google/uuid"
)

// Policy represents a security policy
type Policy struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description,omitempty"`
	PatternType  string    `json:"pattern_type"`  // "regex" or "keyword"
	PatternValue string    `json:"pattern_value"`
	Severity     string    `json:"severity"`      // "low", "medium", "high", "critical"
	Action       string    `json:"action"`        // "log", "block", "redact"
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// AnalyzeRequest is the input for prompt analysis
type AnalyzeRequest struct {
	ClientID string          `json:"client_id"`
	Prompt   string          `json:"prompt"`
	Response string          `json:"response,omitempty"`
	Context  *RequestContext `json:"context,omitempty"`
}

type RequestContext struct {
	Model     string `json:"model,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}

// AnalyzeResponse is the output of prompt analysis
type AnalyzeResponse struct {
	RequestID         uuid.UUID       `json:"request_id"`
	Allowed           bool            `json:"allowed"`
	Action            string          `json:"action"`
	TriggeredPolicies []PolicyMatch   `json:"triggered_policies"`
	RedactedPrompt    string          `json:"redacted_prompt,omitempty"`
	LatencyMs         int64           `json:"latency_ms"`
}

type PolicyMatch struct {
	PolicyID       uuid.UUID `json:"policy_id"`
	PolicyName     string    `json:"policy_name"`
	Severity       string    `json:"severity"`
	MatchedPattern string    `json:"matched_pattern"`
}

// CreatePolicyRequest is the input for creating a policy
type CreatePolicyRequest struct {
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	PatternType  string `json:"pattern_type"`
	PatternValue string `json:"pattern_value"`
	Severity     string `json:"severity"`
	Action       string `json:"action"`
}

// AuditLog represents an audit log entry
type AuditLog struct {
	ID                uuid.UUID   `json:"id"`
	RequestID         uuid.UUID   `json:"request_id"`
	ClientID          string      `json:"client_id"`
	PromptHash        string      `json:"prompt_hash"`
	ResponseHash      string      `json:"response_hash,omitempty"`
	PoliciesTriggered []uuid.UUID `json:"policies_triggered"`
	ActionTaken       string      `json:"action_taken"`
	LatencyMs         int         `json:"latency_ms"`
	CreatedAt         time.Time   `json:"created_at"`
}

// HealthResponse is the health check response
type HealthResponse struct {
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
	Version   string    `json:"version"`
}
