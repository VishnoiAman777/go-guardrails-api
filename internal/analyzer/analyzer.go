package analyzer

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	goaway "github.com/TwiN/go-away"
	"github.com/prompt-gateway/pkg/models"
)

// Analyzer handles prompt/response analysis against policies
type Analyzer struct {
	// Cache compiled regex patterns to avoid recompiling
	patternCache map[string]*regexp.Regexp
	mu           sync.RWMutex // Protects patternCache
	profanityDet *goaway.ProfanityDetector
}

// NewAnalyzer creates a new Analyzer
func NewAnalyzer() *Analyzer {
	return &Analyzer{
		patternCache: make(map[string]*regexp.Regexp),
		profanityDet: goaway.NewProfanityDetector().WithSanitizeLeetSpeak(true).WithSanitizeSpecialCharacters(true),
	}
}

// policyResult holds the result of a single policy check
type policyResult struct {
	match models.PolicyMatch
	err   error
	found bool
}

// Analyze checks content against policies and returns matches
// Uses concurrent goroutines to check all policies in parallel
func (a *Analyzer) Analyze(ctx context.Context, content string, policies []models.Policy) ([]models.PolicyMatch, error) {
	// Filter enabled policies first
	enabledPolicies := make([]models.Policy, 0, len(policies))
	for _, policy := range policies {
		if policy.Enabled {
			enabledPolicies = append(enabledPolicies, policy)
		}
	}

	if len(enabledPolicies) == 0 {
		return []models.PolicyMatch{}, nil
	}

	// Create buffered channel to collect results from all goroutines
	resultCh := make(chan policyResult, len(enabledPolicies))

	// Launch a goroutine for each policy (concurrent checking)
	for _, policy := range enabledPolicies {
		go func(p models.Policy) {
			// Check if content matches this policy
			matched, matchedPattern, err := a.checkPolicyMatch(p, content)

			if err != nil {
				resultCh <- policyResult{err: fmt.Errorf("error matching policy %s: %w", p.Name, err)}
				return
			}

			if matched {
				resultCh <- policyResult{
					match: models.PolicyMatch{
						PolicyID:       p.ID,
						PolicyName:     p.Name,
						Severity:       p.Severity,
						MatchedPattern: matchedPattern,
					},
					found: true,
				}
			} else {
				resultCh <- policyResult{found: false}
			}
		}(policy) // Pass policy as parameter to avoid closure issues
	}

	// Collect results from all goroutines
	matches := []models.PolicyMatch{}
	for i := 0; i < len(enabledPolicies); i++ {
		result := <-resultCh

		if result.err != nil {
			return nil, result.err
		}

		if result.found {
			matches = append(matches, result.match)
		}
	}

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
	case "profanity":
		return a.matchProfanity(content)
	default:
		return false, "", fmt.Errorf("unknown pattern type: %s", policy.PatternType)
	}
}

// getCompiledPattern returns a cached compiled regex or compiles and caches it
func (a *Analyzer) getCompiledPattern(pattern string) (*regexp.Regexp, error) {
	// Try to read from cache first (read lock allows multiple concurrent readers)
	a.mu.RLock()
	re, exists := a.patternCache[pattern]
	a.mu.RUnlock()

	if exists {
		return re, nil
	}

	// Pattern not in cache, compile it
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %w", err)
	}

	// Store in cache (write lock for exclusive access)
	a.mu.Lock()
	a.patternCache[pattern] = re
	a.mu.Unlock()

	return re, nil
}

// matchRegex checks if content matches a regex pattern using cached compilation
func (a *Analyzer) matchRegex(pattern, content string) (bool, string, error) {
	// Get compiled pattern from cache or compile and cache it
	re, err := a.getCompiledPattern(pattern)
	if err != nil {
		return false, "", err
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

// matchProfanity checks if content contains profanity using go-away library
func (a *Analyzer) matchProfanity(content string) (bool, string, error) {
	if a.profanityDet.IsProfane(content) {
		return true, "profanity detected", nil
	}
	return false, "", nil
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
			re, err := a.getCompiledPattern(policy.PatternValue)
			if err == nil {
				redacted = re.ReplaceAllString(redacted, "[REDACTED]")
			}
		} else if policy.PatternType == "keyword" {
			// Case-insensitive keyword replacement
			re := regexp.MustCompile("(?i)" + regexp.QuoteMeta(policy.PatternValue))
			redacted = re.ReplaceAllString(redacted, "[REDACTED]")
		} else if policy.PatternType == "profanity" {
			// Censor profanity using go-away
			redacted = a.profanityDet.Censor(redacted)
		}
	}

	return redacted
}
