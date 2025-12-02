package analyzer

import (
	"context"
	"testing"

	"github.com/prompt-gateway/pkg/models"
)

func TestAnalyzer_Analyze(t *testing.T) {
	// TODO: Implement tests
	//
	// Test cases to consider:
	// - Regex pattern matches correctly
	// - Keyword pattern matches (case insensitive)
	// - Multiple policies, one matches
	// - Multiple policies, multiple match
	// - No policies match
	// - Invalid regex pattern handling
	// - Empty content
	// - Context cancellation
	
	tests := []struct {
		name     string
		content  string
		policies []models.Policy
		wantLen  int
		wantErr  bool
	}{
		{
			name:    "empty content",
			content: "",
			policies: []models.Policy{
				{Name: "test", PatternType: "keyword", PatternValue: "test"},
			},
			wantLen: 0,
			wantErr: false,
		},
		// TODO: Add more test cases
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := NewAnalyzer()
			got, err := a.Analyze(context.Background(), tt.content, tt.policies)
			
			if (err != nil) != tt.wantErr {
				t.Errorf("Analyze() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			
			if len(got) != tt.wantLen {
				t.Errorf("Analyze() returned %d matches, want %d", len(got), tt.wantLen)
			}
		})
	}
}
