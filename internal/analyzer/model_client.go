package analyzer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ModelEvaluation captures the result of evaluating content with an external model.
type ModelEvaluation struct {
	Triggered bool
	Detail    string
}

// ModelClient defines an interface for content-safety model integrations.
type ModelClient interface {
	Evaluate(ctx context.Context, model string, content string) (ModelEvaluation, error)
}

// NemoClient calls NVIDIA's NeMo Guardrails content safety endpoint.
type NemoClient struct {
	apiKey     string
	endpoint   string
	httpClient *http.Client
}

// NewNemoClient constructs a client for the NeMo Guardrails API.
func NewNemoClient(apiKey string, endpoint string, httpClient *http.Client) *NemoClient {
	client := httpClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	return &NemoClient{
		apiKey:     apiKey,
		endpoint:   endpoint,
		httpClient: client,
	}
}

type nemoRequest struct {
	Model    string        `json:"model"`
	Messages []nemoMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type nemoMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type nemoResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Evaluate sends the content to the NeMo API and returns whether it was flagged unsafe.
func (c *NemoClient) Evaluate(ctx context.Context, model string, content string) (ModelEvaluation, error) {
	if strings.TrimSpace(model) == "" {
		return ModelEvaluation{}, fmt.Errorf("model identifier is required for NeMo evaluation")
	}

	payload := nemoRequest{
		Model: model,
		Messages: []nemoMessage{
			{Role: "user", Content: content},
		},
		Stream: false,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return ModelEvaluation{}, fmt.Errorf("failed to encode NeMo request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return ModelEvaluation{}, fmt.Errorf("failed to create NeMo request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ModelEvaluation{}, fmt.Errorf("neMo API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return ModelEvaluation{}, fmt.Errorf("neMo API returned status %d", resp.StatusCode)
	}

	var decoded nemoResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return ModelEvaluation{}, fmt.Errorf("failed to decode NeMo response: %w", err)
	}

	if len(decoded.Choices) == 0 {
		return ModelEvaluation{}, fmt.Errorf("neMo response contained no choices")
	}

	contentJSON := strings.TrimSpace(decoded.Choices[0].Message.Content)
	if contentJSON == "" {
		return ModelEvaluation{}, nil
	}

	// The model returns a JSON object inside the message content string.
	var verdict map[string]string
	if err := json.Unmarshal([]byte(contentJSON), &verdict); err != nil {
		return ModelEvaluation{}, fmt.Errorf("failed to parse NeMo verdict: %w", err)
	}

	userSafety := strings.TrimSpace(verdict["User Safety"])
	if strings.EqualFold(userSafety, "unsafe") {
		return ModelEvaluation{Triggered: true, Detail: fmt.Sprintf("User Safety verdict: %s", userSafety)}, nil
	}

	return ModelEvaluation{Triggered: false}, nil
}
