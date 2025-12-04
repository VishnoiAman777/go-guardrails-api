package analyzer

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/prompt-gateway/pkg/models"
)

// Analyzer handles prompt/response analysis against policies
type Analyzer struct {
	// Cache the regex patterns needed for later
}

// NewAnalyzer creates a new Analyzer
func NewAnalyzer() *Analyzer {
	return &Analyzer{}
}

// Analyze checks content against policies and returns matches
// The (a *Analyzer) part means this is a METHOD on the Analyzer struct (like self in Python)
func (a *Analyzer) Analyze(ctx context.Context, content string, policies []models.Policy) ([]models.PolicyMatch, error) {
	// Initialize empty list to store matches
	matches := []models.PolicyMatch{}

	// Loop through each policy
	for _, policy := range policies {
		// Skip disabled policies (like: if not policy.enabled: continue)
		if !policy.Enabled {
			continue
		}

		// Check if content matches this policy
		matched, matchedPattern, err := a.checkPolicyMatch(policy, content)
		
		// Handle errors
		if err != nil {
			return nil, fmt.Errorf("error matching policy %s: %w", policy.Name, err)
		}

		// If we found a match, add it to our results
		if matched {
			match := models.PolicyMatch{
				PolicyID:       policy.ID,
				PolicyName:     policy.Name,
				Severity:       policy.Severity,
				MatchedPattern: matchedPattern,
			}
			matches = append(matches, match) // append() is like list.append() in Python
		}
	}

	// Return results and nil error (nil is like None in Python)
	return matches, nil
}

// checkPolicyMatch checks if a single policy matches the content
// This is a helper method to make the main Analyze function cleaner
func (a *Analyzer) checkPolicyMatch(policy models.Policy, content string) (matched bool, pattern string, err error) {
	// Check what type of pattern this policy uses
	switch policy.PatternType {
	case "regex":
		return a.matchRegex(policy.PatternValue, content)
	case "keyword":
		isMatch, matchedText := a.matchKeyword(policy.PatternValue, content)
		return isMatch, matchedText, nil
	default:
		return false, "", fmt.Errorf("unknown pattern type: %s", policy.PatternType)
	}
}

// matchRegex checks if content matches a regex pattern
func (a *Analyzer) matchRegex(pattern, content string) (bool, string, error) {
	// Compile the pattern fresh each time (Day 1 - simple approach)
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false, "", fmt.Errorf("invalid regex pattern: %w", err)
	}

	// Find the first match
	matches := re.FindStringSubmatch(content)
	if len(matches) > 0 {
		return true, matches[0], nil // matches[0] is the full match
	}

	return false, "", nil
}

// matchKeyword checks if content contains a keyword (case-insensitive)
func (a *Analyzer) matchKeyword(keyword, content string) (bool, string) {
	// Convert both to lowercase for case-insensitive matching
	lowerContent := strings.ToLower(content)
	lowerKeyword := strings.ToLower(keyword)

	if strings.Contains(lowerContent, lowerKeyword) {
		return true, keyword
	}

	return false, ""
}

// RedactContent redacts matched patterns from content
// Used when policy action is "redact"
func (a *Analyzer) RedactContent(content string, matches []models.PolicyMatch, policies []models.Policy) string {
	redacted := content

	// Create a map of policy IDs for quick lookup
	policyMap := make(map[string]models.Policy)
	for _, p := range policies {
		policyMap[p.ID.String()] = p
	}

	// Redact each match
	for _, match := range matches {
		policy, exists := policyMap[match.PolicyID.String()]
		if !exists || policy.Action != "redact" {
			continue
		}

		// Replace matched pattern with [REDACTED]
		if policy.PatternType == "regex" {
			re, err := regexp.Compile(policy.PatternValue)
			if err == nil {
				redacted = re.ReplaceAllString(redacted, "[REDACTED]")
			}
		} else if policy.PatternType == "keyword" {
			// Case-insensitive keyword replacement
			re := regexp.MustCompile("(?i)" + regexp.QuoteMeta(policy.PatternValue))
			redacted = re.ReplaceAllString(redacted, "[REDACTED]")
		}
	}

	return redacted
}
